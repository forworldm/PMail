package smtp_server

import (
	"database/sql"
	"errors"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/parsemail"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/link"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/Jinnrry/pmail/utils/id"
	"github.com/Jinnrry/pmail/utils/password"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	log "github.com/sirupsen/logrus"
	"net"
	"strings"
)

// The Backend implements SMTP server methods.
type Backend struct{}

func (bkd *Backend) NewSession(conn *smtp.Conn) (smtp.Session, error) {

	remoteAddress := conn.Conn().RemoteAddr()
	ctx := &context.Context{}
	ctx.SetValue(context.LogID, id.GenLogID())
	log.WithContext(ctx).Debugf("新SMTP连接")

	return &Session{
		RemoteAddress: remoteAddress,
		Ctx:           ctx,
	}, nil
}

// A Session is returned after EHLO.
type Session struct {
	RemoteAddress net.Addr
	User          string
	From          string
	To            []string
	Ctx           *context.Context

	// 登录身份快照。SMTP 连接中可以连续发送多封邮件，
	// 每封邮件的 MAIL FROM 可能不同，需要在每封邮件前还原到登录身份再判断。
	loginUserID      int
	loginUserAccount string
	loginUserName    string
	loginIsAdmin     bool
}

// AuthMechanisms returns a slice of available auth mechanisms
// supported in this example.
func (s *Session) AuthMechanisms() []string {
	return []string{sasl.Plain, sasl.Login}
}

// Auth is the handler for supported authenticators.
func (s *Session) Auth(mech string) (sasl.Server, error) {
	log.WithContext(s.Ctx).Debugf("Auth :%s", mech)
	if mech == sasl.Plain {
		return sasl.NewPlainServer(func(identity, username, password string) error {
			return s.AuthPlain(username, password)
		}), nil
	}

	if mech == sasl.Login {
		return NewLoginServer(func(username, password string) error {
			return s.AuthPlain(username, password)
		}), nil
	}

	return nil, errors.New("Auth Not Supported")
}

func (s *Session) AuthPlain(username, pwd string) error {
	log.WithContext(s.Ctx).Debugf("Auth %s %s", username, pwd)

	s.User = username

	var user models.User

	encodePwd := password.Encode(pwd)

	infos := strings.Split(username, "@")
	if len(infos) > 1 {
		username = infos[0]
	}

	_, err := db.Instance.Where("account =? and password =? and disabled=0", username, encodePwd).Get(&user)
	if err != nil && err != sql.ErrNoRows {
		log.Errorf("%+v", err)
	}

	if user.ID > 0 {
		s.Ctx.UserAccount = user.Account
		s.Ctx.UserID = user.ID
		s.Ctx.UserName = user.Name
		s.Ctx.IsAdmin = user.IsAdmin == 1
		s.Ctx.RealUserID = user.ID
		s.Ctx.RealUserAccount = user.Account
		s.loginUserID = user.ID
		s.loginUserAccount = user.Account
		s.loginUserName = user.Name
		s.loginIsAdmin = user.IsAdmin == 1

		log.WithContext(s.Ctx).Debugf("Auth Success %+v", user)
		return nil
	}

	log.WithContext(s.Ctx).Debugf("登陆错误%s %s", username, pwd)
	return errors.New("password error")
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	log.WithContext(s.Ctx).Debugf("Mail Success %+v %+v", from, opts)
	s.From = from
	s.applyActingIdentity(from)
	return nil
}

// applyActingIdentity 判断本次发信是否以关联账户身份进行。
//
// SMTP 连接没有身份切换请求头，因此从 MAIL FROM 推导：
// 如果 MAIL FROM 是当前登录用户的某个关联账户，就把会话身份切换为该关联账户，
// 后续的中继校验与发件箱归属都会自动按关联账户处理。
// 不是关联账户时保持登录身份不变，行为与原有逻辑一致。
func (s *Session) applyActingIdentity(from string) {
	if s.Ctx == nil || s.Ctx.UserID <= 0 || s.loginUserID <= 0 {
		return
	}

	// 每次发信都从登录身份开始判断，避免上一封邮件残留的切换状态影响下一封
	s.Ctx.UserID = s.loginUserID
	s.Ctx.UserAccount = s.loginUserAccount
	s.Ctx.UserName = s.loginUserName
	s.Ctx.IsAdmin = s.loginIsAdmin
	s.Ctx.RealUserID = s.loginUserID
	s.Ctx.RealUserAccount = s.loginUserAccount

	fromUser := parsemail.BuilderUser(from)
	if fromUser == nil || fromUser.EmailAddress == "" {
		return
	}

	// MAIL FROM 是完整邮箱地址（如 sales@example.com），取本地部分得到账号名，
	// 因为身份切换只接受账号名这一种形式。
	account := fromUser.EmailAddress
	if idx := strings.Index(account, "@"); idx >= 0 {
		account = account[:idx]
	}

	// 不是关联账户时维持登录身份，交由既有校验逻辑处理
	if err := link.ApplyIdentity(s.Ctx, account); err != nil {
		return
	}
	log.WithContext(s.Ctx).Debugf("以关联账户身份发信: %s -> %s", s.loginUserAccount, s.Ctx.UserAccount)
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	log.WithContext(s.Ctx).Debugf("Rcpt Success %+v", to)

	s.To = append(s.To, to)
	return nil
}

func (s *Session) Reset() {
	// 还原到登录身份，避免上一封邮件的身份切换残留
	if s.Ctx != nil && s.loginUserID > 0 {
		s.Ctx.UserID = s.loginUserID
		s.Ctx.UserAccount = s.loginUserAccount
		s.Ctx.UserName = s.loginUserName
		s.Ctx.IsAdmin = s.loginIsAdmin
	}
}

func (s *Session) Logout() error {
	return nil
}
