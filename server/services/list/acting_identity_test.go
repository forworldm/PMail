package list

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/link"
	"github.com/Jinnrry/pmail/utils/context"
)

// setupActingTestDB 构造两个用户各自的邮件，并建立 primary -> linked 的授权关系。
func setupActingTestDB(t *testing.T) (primaryCtx *context.Context, linkedCtx *context.Context) {
	t.Helper()

	config.Instance.DbType = config.DBTypeSQLite
	config.Instance.DbDSN = filepath.Join(t.TempDir(), "acting_test.db")
	config.Instance.Domain = "example.com"
	config.Instance.Domains = []string{"example.com"}
	config.Instance.LogLevel = "error"

	if err := db.Init(""); err != nil {
		t.Fatalf("db init failed: %v", err)
	}

	primary := &models.User{Account: "primary", Name: "Primary"}
	linked := &models.User{Account: "linked", Name: "Linked"}
	if _, err := db.Instance.Insert(primary, linked); err != nil {
		t.Fatalf("insert users failed: %v", err)
	}

	admin := &models.User{Account: "admin", Name: "Admin", IsAdmin: 1}
	if _, err := db.Instance.Insert(admin); err != nil {
		t.Fatalf("insert admin failed: %v", err)
	}
	if _, err := link.Add(&context.Context{IsAdmin: true, RealUserID: admin.ID}, "primary", "linked"); err != nil {
		t.Fatalf("link.Add failed: %v", err)
	}

	// 每个用户各写一封收件箱邮件
	for _, u := range []*models.User{primary, linked} {
		email := &models.Email{
			Type:        0,
			Subject:     "mail-for-" + u.Account,
			FromAddress: "someone@outside.com",
			Status:      1,
			SendDate:    time.Now(),
			CreateTime:  time.Now(),
		}
		if _, err := db.Instance.Insert(email); err != nil {
			t.Fatalf("insert email failed: %v", err)
		}
		ue := &models.UserEmail{UserID: u.ID, EmailID: email.Id, Status: 0, IsRead: 0}
		if _, err := db.Instance.Insert(ue); err != nil {
			t.Fatalf("insert user_email failed: %v", err)
		}
	}

	primaryCtx = &context.Context{
		UserID: primary.ID, UserAccount: "primary", UserName: "Primary",
		RealUserID: primary.ID, RealUserAccount: "primary",
	}
	linkedCtx = &context.Context{
		UserID: linked.ID, UserAccount: "linked", UserName: "Linked",
		RealUserID: linked.ID, RealUserAccount: "linked",
	}
	return primaryCtx, linkedCtx
}

var inboxTag = dto.SearchTag{Type: 0, Status: -1, GroupId: 0}

// TestListIsolatedByActingIdentity 是本次功能的核心回归测试：
// 不修改任何业务查询代码，仅仅把 ctx.UserID 换成关联账户，
// 收件箱列表就必须只返回关联账户的邮件。
func TestListIsolatedByActingIdentity(t *testing.T) {
	primaryCtx, _ := setupActingTestDB(t)

	// 未切换身份：只能看到自己的邮件
	before, total := GetEmailList(primaryCtx, inboxTag, "", false, 0, 15)
	if total != 1 {
		t.Fatalf("expect 1 email before switching, got %d", total)
	}
	if len(before) != 1 || before[0].Subject != "mail-for-primary" {
		t.Fatalf("unexpected list before switching: %+v", before)
	}

	// 切换到关联账户身份
	if err := link.ApplyIdentity(primaryCtx, "linked"); err != nil {
		t.Fatalf("ApplyIdentity failed: %v", err)
	}

	after, totalAfter := GetEmailList(primaryCtx, inboxTag, "", false, 0, 15)
	if totalAfter != 1 {
		t.Fatalf("expect 1 email after switching, got %d", totalAfter)
	}
	if len(after) != 1 || after[0].Subject != "mail-for-linked" {
		t.Fatalf("unexpected list after switching: %+v", after)
	}

	// 真实登录身份仍然可用于审计
	if primaryCtx.RealUserID == primaryCtx.UserID {
		t.Fatalf("RealUserID should differ from acting UserID after switching")
	}
}

// TestApplyIdentityRejectsFullAddress 固化"只接受账号名"契约：
// 传入完整邮箱地址一律失败，绝不猜测数据类型。
func TestApplyIdentityRejectsFullAddress(t *testing.T) {
	primaryCtx, _ := setupActingTestDB(t)

	for _, ref := range []string{"linked@example.com", "LINKED@example.com"} {
		if err := link.ApplyIdentity(primaryCtx, ref); err == nil {
			t.Fatalf("ApplyIdentity(%q) should be rejected, got nil", ref)
		}
		if primaryCtx.UserAccount != "primary" {
			t.Fatalf("identity must stay unchanged, got %q", primaryCtx.UserAccount)
		}
	}
}

// TestStatIsolatedByActingIdentity 验证未读数统计同样跟随身份切换。
func TestStatIsolatedByActingIdentity(t *testing.T) {
	primaryCtx, _ := setupActingTestDB(t)

	total, _ := Stat(primaryCtx)
	if total != 1 {
		t.Fatalf("expect 1 unread before switching, got %d", total)
	}

	if err := link.ApplyIdentity(primaryCtx, "linked"); err != nil {
		t.Fatalf("ApplyIdentity failed: %v", err)
	}

	total, _ = Stat(primaryCtx)
	if total != 1 {
		t.Fatalf("expect 1 unread after switching, got %d", total)
	}
}

// TestIdentitySwitchDoesNotLeakAcrossUsers 验证无法切换到未关联的账户，
// 且切换失败时列表仍然只属于原账户。
func TestIdentitySwitchDoesNotLeakAcrossUsers(t *testing.T) {
	primaryCtx, _ := setupActingTestDB(t)

	if err := link.ApplyIdentity(primaryCtx, "other"); err == nil {
		t.Fatalf("switching to an unrelated account should fail")
	}

	_, total := GetEmailList(primaryCtx, inboxTag, "", false, 0, 15)
	if total != 1 {
		t.Fatalf("expect 1 email after failed switch, got %d", total)
	}
	if primaryCtx.UserAccount != "primary" {
		t.Fatalf("identity must stay unchanged, got %q", primaryCtx.UserAccount)
	}
}
