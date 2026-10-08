// Package link 实现"主账户 → 关联账户"的授权关系与身份切换。
//
// 关联账户本身是一个完整的账户，可以独立登录、独立收发邮件；
// 主账户通过该授权关系获得关联账户的权限，可以切换到关联账户的身份读写邮件。
//
// 身份切换只发生在请求入口（HTTP 的 contextIterceptor / SMTP 的 Session.Mail）：
// 校验通过后把 context 中的 UserID 等字段替换为关联账户，
// 业务层原有的 "user_id = 当前用户" 鉴权便自动作用于关联账户，无需逐处修改。
//
// 约定：本包对外的参数一律使用 account（账号名，user 表唯一键），
// 不使用用户ID，也不接受 "id 或 account" 之类的多义写法。
package link

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/utils/context"
	log "github.com/sirupsen/logrus"
)

var (
	// ErrEmptyAccount 账号名为空
	ErrEmptyAccount = errors.New("account is required")
	// ErrUserNotFound 账户不存在
	ErrUserNotFound = errors.New("account not found")
	// ErrUserDisabled 账户被禁用
	ErrUserDisabled = errors.New("account is disabled")
	// ErrSelfLink 试图把自己关联给自己
	ErrSelfLink = errors.New("cannot link an account to itself")
	// ErrLinkExists 授权关系已存在
	ErrLinkExists = errors.New("link already exists")
	// ErrReverseLink 反向授权关系已存在（会造成环）
	ErrReverseLink = errors.New("reverse link already exists")
	// ErrLinkNotFound 授权关系不存在
	ErrLinkNotFound = errors.New("link not found")
	// ErrNoPermission 无权以该账户身份操作
	ErrNoPermission = errors.New("no permission to act as this account")
)

// LinkedAccountsOf 返回主账户可以切换到的全部关联账户，按账号名排序。
func LinkedAccountsOf(ctx *context.Context, primaryID int) ([]*models.User, error) {
	if primaryID <= 0 {
		return nil, nil
	}

	var links []*models.UserLink
	err := db.Instance.Where("primary_id=?", primaryID).Asc("linked_id").Find(&links)
	if err != nil {
		log.WithContext(ctx).Errorf("sql error:%+v", err)
		return nil, err
	}
	if len(links) == 0 {
		return []*models.User{}, nil
	}

	ids := make([]int, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.LinkedID)
	}

	var users []*models.User
	err = db.Instance.In("id", ids).Asc("account").Find(&users)
	if err != nil {
		log.WithContext(ctx).Errorf("sql error:%+v", err)
		return nil, err
	}
	return users, nil
}

// ResolveActing 把"以谁的身份操作"的账号名解析成关联账户。
//
// account 必须是账号名（如 "sales"），由调用方显式提供，不做任何格式猜测。
// 只有与该主账户存在授权关系、且未被禁用的账户才能切换成功；
// 任何不满足条件的情况都返回错误（绝不静默回落到主账户身份，
// 避免用户误判自己正在操作哪个邮箱）。
func ResolveActing(ctx *context.Context, primaryID int, account string) (*models.User, error) {
	if primaryID <= 0 {
		return nil, ErrNoPermission
	}

	linked, err := getUserByAccount(account)
	if err != nil {
		return nil, err
	}

	if linked.ID == primaryID {
		return nil, ErrSelfLink
	}
	if linked.Disabled != 0 {
		return nil, ErrUserDisabled
	}

	var l models.UserLink
	has, err := db.Instance.Where("primary_id=? and linked_id=?", primaryID, linked.ID).Get(&l)
	if err != nil && err != sql.ErrNoRows {
		log.WithContext(ctx).Errorf("sql error:%+v", err)
		return nil, err
	}
	if !has {
		return nil, ErrNoPermission
	}

	return linked, nil
}

// ApplyIdentity 校验通过后，把 ctx 的身份切换为关联账户。
//
// 这是身份切换的唯一实现，HTTP 拦截器与 SMTP 会话都调用它：
// 切换后 ctx.UserID / UserAccount / UserName / IsAdmin 变为关联账户，
// 而 ctx.RealUserID / RealUserAccount 仍指向真正登录的账户。
// 任何校验不通过的情况都返回错误，绝不静默保持原身份。
func ApplyIdentity(ctx *context.Context, account string) error {
	if ctx == nil || ctx.RealUserID <= 0 {
		return ErrNoPermission
	}

	linked, err := ResolveActing(ctx, ctx.RealUserID, account)
	if err != nil {
		return err
	}

	ctx.UserID = linked.ID
	ctx.UserAccount = linked.Account
	ctx.UserName = linked.Name
	ctx.IsAdmin = linked.IsAdmin == 1
	return nil
}

// Add 建立"主账户 → 关联账户"的授权关系，仅管理员可调用。
// primaryAccount / linkedAccount 均为账号名，必须显式提供。
func Add(ctx *context.Context, primaryAccount, linkedAccount string) (*models.UserLink, error) {
	if ctx == nil || !ctx.IsAdmin {
		return nil, ErrNoPermission
	}

	primary, err := getUserByAccount(primaryAccount)
	if err != nil {
		return nil, err
	}
	linked, err := getUserByAccount(linkedAccount)
	if err != nil {
		return nil, err
	}

	if primary.ID == linked.ID {
		return nil, ErrSelfLink
	}
	if primary.Disabled != 0 {
		return nil, ErrUserDisabled
	}
	if linked.Disabled != 0 {
		return nil, ErrUserDisabled
	}

	var exist models.UserLink
	has, err := db.Instance.Where("primary_id=? and linked_id=?", primary.ID, linked.ID).Get(&exist)
	if err != nil && err != sql.ErrNoRows {
		log.WithContext(ctx).Errorf("sql error:%+v", err)
		return nil, err
	}
	if has {
		return nil, ErrLinkExists
	}

	// 禁止成环：不允许 A→B 与 B→A 同时存在
	var reverse models.UserLink
	has, err = db.Instance.Where("primary_id=? and linked_id=?", linked.ID, primary.ID).Get(&reverse)
	if err != nil && err != sql.ErrNoRows {
		log.WithContext(ctx).Errorf("sql error:%+v", err)
		return nil, err
	}
	if has {
		return nil, ErrReverseLink
	}

	record := models.UserLink{PrimaryID: primary.ID, LinkedID: linked.ID}
	if _, err := db.Instance.Insert(&record); err != nil {
		log.WithContext(ctx).Errorf("sql error:%+v", err)
		return nil, err
	}
	return &record, nil
}

// Remove 解除授权关系，仅管理员可调用。
// primaryAccount / linkedAccount 均为账号名，必须显式提供。
func Remove(ctx *context.Context, primaryAccount, linkedAccount string) error {
	if ctx == nil || !ctx.IsAdmin {
		return ErrNoPermission
	}

	primary, err := getUserByAccount(primaryAccount)
	if err != nil {
		return err
	}
	linked, err := getUserByAccount(linkedAccount)
	if err != nil {
		return err
	}

	affected, err := db.Instance.Where("primary_id=? and linked_id=?", primary.ID, linked.ID).Delete(&models.UserLink{})
	if err != nil {
		log.WithContext(ctx).Errorf("sql error:%+v", err)
		return err
	}
	if affected == 0 {
		return ErrLinkNotFound
	}
	return nil
}

// LinkInfo 一条授权关系的可读信息，供管理界面展示。只包含账号名，不暴露用户ID。
type LinkInfo struct {
	PrimaryAccount string `json:"primary_account"`
	PrimaryName    string `json:"primary_name"`
	LinkedAccount  string `json:"linked_account"`
	LinkedName     string `json:"linked_name"`
	LinkedDisabled int    `json:"linked_disabled"`
}

// ListForAdmin 返回全部授权关系（管理员使用）。
func ListForAdmin(ctx *context.Context) ([]*LinkInfo, error) {
	var links []*models.UserLink
	if err := db.Instance.Asc("id").Find(&links); err != nil {
		log.WithContext(ctx).Errorf("sql error:%+v", err)
		return nil, err
	}

	ret := make([]*LinkInfo, 0, len(links))
	for _, l := range links {
		primary, err := getUserByID(l.PrimaryID)
		if err != nil {
			continue
		}
		linked, err := getUserByID(l.LinkedID)
		if err != nil {
			continue
		}
		ret = append(ret, &LinkInfo{
			PrimaryAccount: primary.Account,
			PrimaryName:    primary.Name,
			LinkedAccount:  linked.Account,
			LinkedName:     linked.Name,
			LinkedDisabled: linked.Disabled,
		})
	}
	return ret, nil
}

// getUserByAccount 按账号名取用户。账号名大小写不敏感，与收信路由保持一致。
func getUserByAccount(account string) (*models.User, error) {
	account = strings.TrimSpace(account)
	if account == "" {
		return nil, ErrEmptyAccount
	}

	var u models.User
	has, err := db.Instance.Where("LOWER(account)=?", strings.ToLower(account)).Get(&u)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if !has || u.ID == 0 {
		return nil, ErrUserNotFound
	}
	return &u, nil
}

// GetUserByAccount 按账号名取用户。供 controller 解析"要操作哪个账户"使用。
func GetUserByAccount(account string) (*models.User, error) {
	return getUserByAccount(account)
}

// getUserByID 按用户ID取用户，仅用于内部组装展示数据。
func getUserByID(id int) (*models.User, error) {
	var u models.User
	has, err := db.Instance.Where("id=?", id).Get(&u)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if !has || u.ID == 0 {
		return nil, ErrUserNotFound
	}
	return &u, nil
}
