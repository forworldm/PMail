package link

import (
	"path/filepath"
	"testing"

	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/utils/context"
)

// setupTestDB 用一个临时的 sqlite 库初始化 db.Instance，并写入测试账户。
// 直接设置 config.Instance 字段，避免依赖运行目录下的 config.json，
// 使命移测试可以在任意环境下运行。
func setupTestDB(t *testing.T) {
	t.Helper()

	config.Instance.DbType = config.DBTypeSQLite
	config.Instance.DbDSN = filepath.Join(t.TempDir(), "link_test.db")
	config.Instance.Domain = "example.com"
	config.Instance.Domains = []string{"example.com"}
	config.Instance.LogLevel = "error"

	if err := db.Init(""); err != nil {
		t.Fatalf("db init failed: %v", err)
	}
}

// seedUsers 写入测试账户：admin(管理员) / primary / linked / other
func seedUsers(t *testing.T) map[string]*models.User {
	t.Helper()

	users := []*models.User{
		{Account: "admin", Name: "Admin", Password: "x", IsAdmin: 1},
		{Account: "primary", Name: "Primary", Password: "x"},
		{Account: "linked", Name: "Linked", Password: "x"},
		{Account: "other", Name: "Other", Password: "x"},
	}
	for _, u := range users {
		if _, err := db.Instance.Insert(u); err != nil {
			t.Fatalf("insert user %s failed: %v", u.Account, err)
		}
	}

	ret := map[string]*models.User{}
	for _, u := range users {
		ret[u.Account] = u
	}
	return ret
}

// newCtx 构造一个已登录的上下文。isAdmin 决定是否具备关联管理权限。
func newCtx(user *models.User, isAdmin bool) *context.Context {
	return &context.Context{
		UserID:          user.ID,
		UserAccount:     user.Account,
		UserName:        user.Name,
		IsAdmin:         isAdmin,
		RealUserID:      user.ID,
		RealUserAccount: user.Account,
	}
}
