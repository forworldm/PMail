package smtp_server

import (
	"path/filepath"
	"testing"

	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/link"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/emersion/go-smtp"
	log "github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

// newActingTestEngine 构造一个独立的 sqlite 环境，
// 写入 primary / linked / other 三个账户，并建立 primary -> linked 的授权关系。
func newActingTestEngine(t *testing.T) (*models.User, *models.User) {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "acting.db")
	engine, err := xorm.NewEngine("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	engine.SetMaxOpenConns(1)
	engine.SetMaxIdleConns(1)
	if err = engine.Sync2(&models.User{}, &models.UserLink{}, &models.Email{}, &models.UserEmail{}); err != nil {
		engine.Close()
		t.Fatal(err)
	}

	primary := &models.User{Account: "primary", Name: "Primary"}
	linked := &models.User{Account: "linked", Name: "Linked"}
	if _, err = engine.Insert(primary, linked); err != nil {
		engine.Close()
		t.Fatal(err)
	}
	admin := &models.User{Account: "admin", Name: "Admin", IsAdmin: 1}
	if _, err = engine.Insert(admin); err != nil {
		engine.Close()
		t.Fatal(err)
	}

	oldDB := db.Instance
	oldConfig := config.Instance
	db.Instance = engine
	config.Instance = &config.Config{
		DbType:  config.DBTypeSQLite,
		Domain:  "example.com",
		Domains: []string{"example.com"},
	}
	t.Cleanup(func() {
		db.Instance = oldDB
		config.Instance = oldConfig
		_ = engine.Close()
	})

	if _, err = link.Add(&context.Context{IsAdmin: true, RealUserID: admin.ID}, "primary", "linked"); err != nil {
		t.Fatalf("link.Add failed: %v", err)
	}
	return primary, linked
}

// newActingSession 构造一个已通过 SMTP AUTH 的会话（以 primary 登录）
func newActingSession(primary *models.User) *Session {
	return &Session{
		Ctx: &context.Context{
			UserID:          primary.ID,
			UserAccount:     primary.Account,
			UserName:        primary.Name,
			RealUserID:      primary.ID,
			RealUserAccount: primary.Account,
		},
		loginUserID:      primary.ID,
		loginUserAccount: primary.Account,
		loginUserName:    primary.Name,
	}
}

func TestMailFromSwitchesToLinkedAccount(t *testing.T) {
	log.SetLevel(log.ErrorLevel)
	primary, linked := newActingTestEngine(t)

	s := newActingSession(primary)
	if err := s.Mail("linked@example.com", &smtp.MailOptions{}); err != nil {
		t.Fatalf("Mail failed: %v", err)
	}

	if s.Ctx.UserID != linked.ID {
		t.Fatalf("session identity = %d, want linked %d", s.Ctx.UserID, linked.ID)
	}
	if s.Ctx.UserAccount != "linked" {
		t.Fatalf("session account = %q, want linked", s.Ctx.UserAccount)
	}
	// 真实登录者必须保留，供投递审计使用
	if s.Ctx.RealUserID != primary.ID {
		t.Fatalf("RealUserID = %d, want primary %d", s.Ctx.RealUserID, primary.ID)
	}
}

func TestMailFromUnrelatedKeepsLoginIdentity(t *testing.T) {
	log.SetLevel(log.ErrorLevel)
	primary, _ := newActingTestEngine(t)

	for _, from := range []string{"primary@example.com", "other@example.com", "stranger@outside.com", ""} {
		s := newActingSession(primary)
		if err := s.Mail(from, &smtp.MailOptions{}); err != nil {
			t.Fatalf("Mail(%q) failed: %v", from, err)
		}
		if s.Ctx.UserID != primary.ID || s.Ctx.UserAccount != "primary" {
			t.Fatalf("Mail(%q) changed identity to (%d,%q)", from, s.Ctx.UserID, s.Ctx.UserAccount)
		}
	}
}

// TestResetRestoresLoginIdentityBetweenMessages 覆盖同一条连接连续发多封邮件的场景：
// 第一封切换到关联账户后，Reset 必须还原登录身份，
// 否则第二封以本人地址发信会被误判为越权。
func TestResetRestoresLoginIdentityBetweenMessages(t *testing.T) {
	log.SetLevel(log.ErrorLevel)
	primary, _ := newActingTestEngine(t)

	s := newActingSession(primary)

	// 第一封：以关联账户身份发信
	if err := s.Mail("linked@example.com", &smtp.MailOptions{}); err != nil {
		t.Fatalf("first Mail failed: %v", err)
	}
	if s.Ctx.UserAccount != "linked" {
		t.Fatalf("first message should switch to linked, got %q", s.Ctx.UserAccount)
	}

	s.Reset()
	if s.Ctx.UserID != primary.ID || s.Ctx.UserAccount != "primary" {
		t.Fatalf("Reset should restore login identity, got (%d,%q)", s.Ctx.UserID, s.Ctx.UserAccount)
	}

	// 第二封：用本人地址发信，必须仍然是本人身份
	if err := s.Mail("primary@example.com", &smtp.MailOptions{}); err != nil {
		t.Fatalf("second Mail failed: %v", err)
	}
	if s.Ctx.UserID != primary.ID || s.Ctx.UserAccount != "primary" {
		t.Fatalf("second message identity = (%d,%q), want primary", s.Ctx.UserID, s.Ctx.UserAccount)
	}
}

// TestDisabledLinkedAccountCannotBeUsed 验证关联账户被禁用后无法再通过 SMTP 切换身份
func TestDisabledLinkedAccountCannotBeUsed(t *testing.T) {
	log.SetLevel(log.ErrorLevel)
	primary, linked := newActingTestEngine(t)

	if _, err := db.Instance.ID(linked.ID).Cols("disabled").Update(&models.User{Disabled: 1}); err != nil {
		t.Fatalf("disable linked account failed: %v", err)
	}

	s := newActingSession(primary)
	if err := s.Mail("linked@example.com", &smtp.MailOptions{}); err != nil {
		t.Fatalf("Mail failed: %v", err)
	}
	if s.Ctx.UserID != primary.ID {
		t.Fatalf("disabled linked account must not be usable, identity = %d", s.Ctx.UserID)
	}
}
