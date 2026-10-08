package controllers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/services/link"
	"github.com/Jinnrry/pmail/utils/context"
	log "github.com/sirupsen/logrus"
)

// readJSON 读取并解析请求体。解析失败时直接写出错误响应并返回 false，
// 避免调用方在拿到零值结构体后继续执行。
func readJSON(ctx *context.Context, w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.WithContext(ctx).Errorf("read request body failed: %+v", err)
		response.NewErrorResponse(response.ParamsError, "params error", err.Error()).FPrint(w)
		return false
	}

	// 允许空请求体，按零值处理
	if len(body) == 0 {
		return true
	}

	if err := json.Unmarshal(body, v); err != nil {
		log.WithContext(ctx).Errorf("unmarshal request body failed: %+v", err)
		response.NewErrorResponse(response.ParamsError, "params error", err.Error()).FPrint(w)
		return false
	}
	return true
}

type linkListRequest struct {
	// Account 要查询的主账户账号名；为空表示当前登录账户。仅管理员可查询他人。
	Account string `json:"account"`
}

type linkAddRequest struct {
	// Primary 主账户账号名
	Primary string `json:"primary"`
	// Linked 关联账户账号名
	Linked string `json:"linked"`
}

type linkDelRequest struct {
	// Primary 主账户账号名
	Primary string `json:"primary"`
	// Linked 关联账户账号名
	Linked string `json:"linked"`
}

// LinkList 查询授权关系。
// 普通用户只能查询自己的关联账户；管理员可以查询任意账户。
func LinkList(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData linkListRequest
	if !readJSON(ctx, w, req, &reqData) {
		return
	}

	// 目标主账户：缺省为当前登录账户
	targetAccount := ctx.RealUserAccount
	if reqData.Account != "" {
		if !ctx.IsAdmin {
			response.NewErrorResponse(response.NoAccessPrivileges, "No Access Privileges", "").FPrint(w)
			return
		}
		targetAccount = reqData.Account
	}

	target, err := link.GetUserByAccount(targetAccount)
	if err != nil {
		response.NewErrorResponse(mapLinkError(err), err.Error(), "").FPrint(w)
		return
	}

	linked, err := link.LinkedAccountsOf(ctx, target.ID)
	if err != nil {
		response.NewErrorResponse(response.ServerError, err.Error(), "").FPrint(w)
		return
	}

	type linkedItem struct {
		Account  string `json:"account"`
		Name     string `json:"name"`
		Disabled int    `json:"disabled"`
	}

	items := make([]linkedItem, 0, len(linked))
	for _, u := range linked {
		items = append(items, linkedItem{
			Account:  u.Account,
			Name:     u.Name,
			Disabled: u.Disabled,
		})
	}

	response.NewSuccessResponse(map[string]any{
		"account":        target.Account,
		"linked_account": items,
	}).FPrint(w)
}

// LinkAdd 建立"主账户 → 关联账户"授权关系，仅管理员可操作。
func LinkAdd(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !ctx.IsAdmin {
		response.NewErrorResponse(response.NoAccessPrivileges, "No Access Privileges", "").FPrint(w)
		return
	}

	var reqData linkAddRequest
	if !readJSON(ctx, w, req, &reqData) {
		return
	}

	if reqData.Primary == "" {
		response.NewErrorResponse(response.ParamsError, "Params Error", "primary is required").FPrint(w)
		return
	}
	if reqData.Linked == "" {
		response.NewErrorResponse(response.ParamsError, "Params Error", "linked is required").FPrint(w)
		return
	}

	record, err := link.Add(ctx, reqData.Primary, reqData.Linked)
	if err != nil {
		response.NewErrorResponse(mapLinkError(err), err.Error(), "").FPrint(w)
		return
	}

	primary, err := link.GetUserByAccount(reqData.Primary)
	if err != nil {
		response.NewErrorResponse(mapLinkError(err), err.Error(), "").FPrint(w)
		return
	}
	linked, err := link.GetUserByAccount(reqData.Linked)
	if err != nil {
		response.NewErrorResponse(mapLinkError(err), err.Error(), "").FPrint(w)
		return
	}

	response.NewSuccessResponse(map[string]any{
		"primary_account": primary.Account,
		"linked_account":  linked.Account,
		"created":         record.Created,
	}).FPrint(w)
}

// LinkDel 解除授权关系，仅管理员可操作。
func LinkDel(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !ctx.IsAdmin {
		response.NewErrorResponse(response.NoAccessPrivileges, "No Access Privileges", "").FPrint(w)
		return
	}

	var reqData linkDelRequest
	if !readJSON(ctx, w, req, &reqData) {
		return
	}

	if reqData.Primary == "" || reqData.Linked == "" {
		response.NewErrorResponse(response.ParamsError, "Params Error", "primary and linked are required").FPrint(w)
		return
	}

	if err := link.Remove(ctx, reqData.Primary, reqData.Linked); err != nil {
		response.NewErrorResponse(mapLinkError(err), err.Error(), "").FPrint(w)
		return
	}

	response.NewSuccessResponse("succ").FPrint(w)
}

// LinkAdminList 返回全部授权关系，仅管理员可操作，供管理界面展示。
func LinkAdminList(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !ctx.IsAdmin {
		response.NewErrorResponse(response.NoAccessPrivileges, "No Access Privileges", "").FPrint(w)
		return
	}

	list, err := link.ListForAdmin(ctx)
	if err != nil {
		response.NewErrorResponse(response.ServerError, err.Error(), "").FPrint(w)
		return
	}

	response.NewSuccessResponse(map[string]any{
		"list": list,
	}).FPrint(w)
}

// mapLinkError 把服务层错误映射成对外的错误码。
func mapLinkError(err error) int {
	switch err {
	case link.ErrEmptyAccount, link.ErrUserNotFound, link.ErrSelfLink,
		link.ErrLinkExists, link.ErrReverseLink, link.ErrLinkNotFound:
		return response.ParamsError
	case link.ErrNoPermission:
		return response.NoAccessPrivileges
	default:
		return response.ServerError
	}
}
