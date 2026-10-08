package link

import (
	"testing"

	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/utils/context"
)

// TestAddAndLinkedAccountsOf 验证授权关系的建立与按主账户查询。
func TestAddAndLinkedAccountsOf(t *testing.T) {
	setupTestDB(t)
	users := seedUsers(t)
	adminCtx := newCtx(users["admin"], true)

	if _, err := Add(adminCtx, "primary", "linked"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	linked, err := LinkedAccountsOf(adminCtx, users["primary"].ID)
	if err != nil {
		t.Fatalf("LinkedAccountsOf failed: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expect 1 linked account, got %d", len(linked))
	}
	if linked[0].Account != "linked" {
		t.Fatalf("expect linked account 'linked', got %q", linked[0].Account)
	}

	// 没有关联关系时返回空列表而不是 nil
	empty, err := LinkedAccountsOf(adminCtx, users["other"].ID)
	if err != nil {
		t.Fatalf("LinkedAccountsOf failed: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expect 0 linked account, got %d", len(empty))
	}

	// primaryID 非法时直接返回空
	if got, err := LinkedAccountsOf(adminCtx, 0); err != nil || len(got) != 0 {
		t.Fatalf("LinkedAccountsOf(0) = (%v,%v)", got, err)
	}
}

func TestAddValidation(t *testing.T) {
	setupTestDB(t)
	users := seedUsers(t)
	adminCtx := newCtx(users["admin"], true)

	t.Run("非管理员不能建关联", func(t *testing.T) {
		if _, err := Add(newCtx(users["primary"], false), "primary", "other"); err != ErrNoPermission {
			t.Fatalf("expect ErrNoPermission, got %v", err)
		}
	})

	t.Run("自关联被拒绝", func(t *testing.T) {
		if _, err := Add(adminCtx, "primary", "primary"); err != ErrSelfLink {
			t.Fatalf("expect ErrSelfLink, got %v", err)
		}
	})

	t.Run("账户不存在", func(t *testing.T) {
		if _, err := Add(adminCtx, "primary", "not_exist"); err != ErrUserNotFound {
			t.Fatalf("expect ErrUserNotFound, got %v", err)
		}
	})

	t.Run("空账号名", func(t *testing.T) {
		if _, err := Add(adminCtx, "primary", "  "); err != ErrEmptyAccount {
			t.Fatalf("expect ErrEmptyAccount, got %v", err)
		}
	})

	t.Run("主账户为空", func(t *testing.T) {
		if _, err := Add(adminCtx, "", "linked"); err != ErrEmptyAccount {
			t.Fatalf("expect ErrEmptyAccount, got %v", err)
		}
	})

	t.Run("重复关联被拒绝", func(t *testing.T) {
		if _, err := Add(adminCtx, "primary", "linked"); err != nil {
			t.Fatalf("first Add failed: %v", err)
		}
		if _, err := Add(adminCtx, "primary", "linked"); err != ErrLinkExists {
			t.Fatalf("expect ErrLinkExists, got %v", err)
		}
	})

	t.Run("成环被拒绝", func(t *testing.T) {
		// primary -> linked 已由上一个用例建立，
		// 再建 linked -> primary 会造成双向成环
		if _, err := Add(adminCtx, "linked", "primary"); err != ErrReverseLink {
			t.Fatalf("expect ErrReverseLink, got %v", err)
		}
		// 确认成环的那一条确实没有被写入
		if _, err := ResolveActing(adminCtx, users["linked"].ID, "primary"); err != ErrNoPermission {
			t.Fatalf("reverse link should not exist, got %v", err)
		}
	})

	t.Run("禁用账户不能被关联", func(t *testing.T) {
		// other 置为禁用
		if _, err := db.Instance.ID(users["other"].ID).Cols("disabled").Update(&models.User{Disabled: 1}); err != nil {
			t.Fatalf("disable user failed: %v", err)
		}
		if _, err := Add(adminCtx, "primary", "other"); err != ErrUserDisabled {
			t.Fatalf("expect ErrUserDisabled, got %v", err)
		}
	})

	t.Run("账号名大小写不敏感", func(t *testing.T) {
		if _, err := Add(adminCtx, "PRIMARY", "LINKED"); err != ErrLinkExists {
			t.Fatalf("expect ErrLinkExists, got %v", err)
		}
	})
}

func TestResolveActing(t *testing.T) {
	setupTestDB(t)
	users := seedUsers(t)
	adminCtx := newCtx(users["admin"], true)

	if _, err := Add(adminCtx, "primary", "linked"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	primaryID := users["primary"].ID
	linked := users["linked"]

	t.Run("按账号名切换", func(t *testing.T) {
		got, err := ResolveActing(adminCtx, primaryID, "linked")
		if err != nil {
			t.Fatalf("ResolveActing failed: %v", err)
		}
		if got.ID != linked.ID {
			t.Fatalf("expect id %d, got %d", linked.ID, got.ID)
		}
	})

	t.Run("账号名大小写不敏感", func(t *testing.T) {
		got, err := ResolveActing(adminCtx, primaryID, "LiNkEd")
		if err != nil {
			t.Fatalf("ResolveActing failed: %v", err)
		}
		if got.ID != linked.ID {
			t.Fatalf("expect id %d, got %d", linked.ID, got.ID)
		}
	})

	t.Run("账号名前后空格被裁剪", func(t *testing.T) {
		got, err := ResolveActing(adminCtx, primaryID, "  linked  ")
		if err != nil {
			t.Fatalf("ResolveActing failed: %v", err)
		}
		if got.ID != linked.ID {
			t.Fatalf("expect id %d, got %d", linked.ID, got.ID)
		}
	})

	t.Run("未关联的账户被拒绝", func(t *testing.T) {
		if _, err := ResolveActing(adminCtx, primaryID, "other"); err != ErrNoPermission {
			t.Fatalf("expect ErrNoPermission, got %v", err)
		}
	})

	t.Run("不能切换到自己", func(t *testing.T) {
		if _, err := ResolveActing(adminCtx, primaryID, "primary"); err != ErrSelfLink {
			t.Fatalf("expect ErrSelfLink, got %v", err)
		}
	})

	t.Run("账户不存在", func(t *testing.T) {
		if _, err := ResolveActing(adminCtx, primaryID, "ghost"); err != ErrUserNotFound {
			t.Fatalf("expect ErrUserNotFound, got %v", err)
		}
	})

	t.Run("primaryID 非法", func(t *testing.T) {
		if _, err := ResolveActing(adminCtx, 0, "linked"); err != ErrNoPermission {
			t.Fatalf("expect ErrNoPermission, got %v", err)
		}
	})

	t.Run("禁用的关联账户被拒绝", func(t *testing.T) {
		if _, err := db.Instance.ID(linked.ID).Cols("disabled").Update(&models.User{Disabled: 1}); err != nil {
			t.Fatalf("disable user failed: %v", err)
		}
		if _, err := ResolveActing(adminCtx, primaryID, "linked"); err != ErrUserDisabled {
			t.Fatalf("expect ErrUserDisabled, got %v", err)
		}
	})
}

// TestActAsRefAcceptsAccountOnly 固化"只接受账号名"的契约：
// 传入用户ID形式的字符串、完整邮箱地址一律视为不存在的账户，绝不猜测。
func TestActAsRefAcceptsAccountOnly(t *testing.T) {
	setupTestDB(t)
	users := seedUsers(t)
	adminCtx := newCtx(users["admin"], true)

	if _, err := Add(adminCtx, "primary", "linked"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	primaryID := users["primary"].ID

	// 建一个账号名就是数字的账户，证明不会与用户ID混淆
	numeric := &models.User{Account: "10086", Name: "Numeric", Password: "x"}
	if _, err := db.Instance.Insert(numeric); err != nil {
		t.Fatalf("insert numeric user failed: %v", err)
	}

	rejected := []string{
		"linked@example.com", // 完整邮箱地址
		"LINKED@example.com", // 完整邮箱地址（大写）
		"",                   // 空
		"   ",                // 只有空格
	}
	for _, ref := range rejected {
		if _, err := ResolveActing(adminCtx, primaryID, ref); err == nil {
			t.Fatalf("ResolveActing(%q) should be rejected, got nil", ref)
		}
	}

	// 数字账号名的账户只能按账号名访问，不能按它的用户ID访问
	if _, err := ResolveActing(adminCtx, primaryID, "10086"); err != ErrNoPermission {
		t.Fatalf("numeric account without link: expect ErrNoPermission, got %v", err)
	}
}

func TestApplyIdentity(t *testing.T) {
	setupTestDB(t)
	users := seedUsers(t)
	adminCtx := newCtx(users["admin"], true)

	if _, err := Add(adminCtx, "primary", "linked"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	ctx := newCtx(users["primary"], false)

	if err := ApplyIdentity(ctx, "linked"); err != nil {
		t.Fatalf("ApplyIdentity failed: %v", err)
	}
	if ctx.UserID != users["linked"].ID {
		t.Fatalf("UserID = %d, want %d", ctx.UserID, users["linked"].ID)
	}
	if ctx.UserAccount != "linked" {
		t.Fatalf("UserAccount = %q, want linked", ctx.UserAccount)
	}
	// 真实登录身份必须保留，供改密码、审计使用
	if ctx.RealUserID != users["primary"].ID || ctx.RealUserAccount != "primary" {
		t.Fatalf("RealUser = (%d,%q), want (%d,primary)",
			ctx.RealUserID, ctx.RealUserAccount, users["primary"].ID)
	}

	t.Run("切换失败时身份不被修改", func(t *testing.T) {
		fresh := newCtx(users["primary"], false)
		if err := ApplyIdentity(fresh, "other"); err != ErrNoPermission {
			t.Fatalf("expect ErrNoPermission, got %v", err)
		}
		if fresh.UserID != users["primary"].ID || fresh.UserAccount != "primary" {
			t.Fatalf("identity changed on failure: (%d,%q)", fresh.UserID, fresh.UserAccount)
		}
	})

	t.Run("传完整邮箱地址被拒绝", func(t *testing.T) {
		fresh := newCtx(users["primary"], false)
		if err := ApplyIdentity(fresh, "linked@example.com"); err != ErrUserNotFound {
			t.Fatalf("expect ErrUserNotFound, got %v", err)
		}
	})

	t.Run("未登录上下文被拒绝", func(t *testing.T) {
		empty := &context.Context{}
		if err := ApplyIdentity(empty, "linked"); err != ErrNoPermission {
			t.Fatalf("expect ErrNoPermission, got %v", err)
		}
	})

	t.Run("nil 上下文被拒绝", func(t *testing.T) {
		if err := ApplyIdentity(nil, "linked"); err != ErrNoPermission {
			t.Fatalf("expect ErrNoPermission, got %v", err)
		}
	})
}

func TestRemove(t *testing.T) {
	setupTestDB(t)
	users := seedUsers(t)
	adminCtx := newCtx(users["admin"], true)

	if _, err := Add(adminCtx, "primary", "linked"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	t.Run("非管理员不能删除", func(t *testing.T) {
		if err := Remove(newCtx(users["primary"], false), "primary", "linked"); err != ErrNoPermission {
			t.Fatalf("expect ErrNoPermission, got %v", err)
		}
	})

	t.Run("按账号名删除", func(t *testing.T) {
		if _, err := Add(adminCtx, "primary", "other"); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
		if err := Remove(adminCtx, "primary", "other"); err != nil {
			t.Fatalf("Remove failed: %v", err)
		}
		linked, _ := LinkedAccountsOf(adminCtx, users["primary"].ID)
		if len(linked) != 1 || linked[0].Account != "linked" {
			t.Fatalf("unexpected linked accounts: %+v", linked)
		}
	})

	t.Run("账号名大小写不敏感", func(t *testing.T) {
		if err := Remove(adminCtx, "PRIMARY", "LINKED"); err != nil {
			t.Fatalf("Remove failed: %v", err)
		}
		linked, _ := LinkedAccountsOf(adminCtx, users["primary"].ID)
		if len(linked) != 0 {
			t.Fatalf("expect empty, got %+v", linked)
		}
	})

	t.Run("删除不存在的关联", func(t *testing.T) {
		if err := Remove(adminCtx, "primary", "linked"); err != ErrLinkNotFound {
			t.Fatalf("expect ErrLinkNotFound, got %v", err)
		}
		if err := Remove(adminCtx, "primary", "ghost"); err != ErrUserNotFound {
			t.Fatalf("expect ErrUserNotFound, got %v", err)
		}
	})

	t.Run("空账号名", func(t *testing.T) {
		if err := Remove(adminCtx, "primary", "  "); err != ErrEmptyAccount {
			t.Fatalf("expect ErrEmptyAccount, got %v", err)
		}
		if err := Remove(adminCtx, "", "linked"); err != ErrEmptyAccount {
			t.Fatalf("expect ErrEmptyAccount, got %v", err)
		}
	})
}

func TestListForAdmin(t *testing.T) {
	setupTestDB(t)
	users := seedUsers(t)
	adminCtx := newCtx(users["admin"], true)

	if _, err := Add(adminCtx, "primary", "linked"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if _, err := Add(adminCtx, "other", "linked"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	list, err := ListForAdmin(adminCtx)
	if err != nil {
		t.Fatalf("ListForAdmin failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expect 2 links, got %d", len(list))
	}
	for _, item := range list {
		if item.PrimaryAccount == "" || item.LinkedAccount != "linked" {
			t.Fatalf("unexpected link info: %+v", item)
		}
	}
}

// TestMultiplePrimaryPerLinked 验证同一个关联账户可以被多个主账户关联
func TestMultiplePrimaryPerLinked(t *testing.T) {
	setupTestDB(t)
	users := seedUsers(t)
	adminCtx := newCtx(users["admin"], true)

	if _, err := Add(adminCtx, "primary", "linked"); err != nil {
		t.Fatalf("Add primary->linked failed: %v", err)
	}
	if _, err := Add(adminCtx, "other", "linked"); err != nil {
		t.Fatalf("Add other->linked failed: %v", err)
	}

	for _, account := range []string{"primary", "other"} {
		linked, err := LinkedAccountsOf(adminCtx, users[account].ID)
		if err != nil {
			t.Fatalf("LinkedAccountsOf(%s) failed: %v", account, err)
		}
		if len(linked) != 1 || linked[0].Account != "linked" {
			t.Fatalf("%s should see 'linked', got %+v", account, linked)
		}
	}
}

// TestGetUserByAccount 验证账号名查询的边界行为
func TestGetUserByAccount(t *testing.T) {
	setupTestDB(t)
	seedUsers(t)

	t.Run("按账号名查到", func(t *testing.T) {
		u, err := GetUserByAccount("linked")
		if err != nil {
			t.Fatalf("GetUserByAccount failed: %v", err)
		}
		if u.Account != "linked" {
			t.Fatalf("expect 'linked', got %q", u.Account)
		}
	})

	t.Run("不存在", func(t *testing.T) {
		if _, err := GetUserByAccount("ghost"); err != ErrUserNotFound {
			t.Fatalf("expect ErrUserNotFound, got %v", err)
		}
	})

	t.Run("空", func(t *testing.T) {
		if _, err := GetUserByAccount("  "); err != ErrEmptyAccount {
			t.Fatalf("expect ErrEmptyAccount, got %v", err)
		}
	})
}
