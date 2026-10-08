package context

import (
	"context"
)

const (
	LogID = "LogID"
)

type Context struct {
	context.Context `json:"-"`
	UserID          int
	UserAccount     string
	UserName        string
	Values          map[string]any
	Lang            string
	IsAdmin         bool
	// RealUserID / RealUserAccount 是真正登录的账户。
	// 当用户切换到关联账户身份时，UserID 等字段变为关联账户，这两个字段仍然指向登录者，
	// 用于修改密码、操作审计等"必须作用于真实登录者"的场景。
	RealUserID      int
	RealUserAccount string
}

func (c *Context) SetValue(key string, value any) {
	if c.Values == nil {
		c.Values = map[string]any{}
	}
	c.Values[key] = value

}

func (c *Context) GetValue(key string) any {
	if c.Values == nil {
		return nil
	}
	return c.Values[key]
}
