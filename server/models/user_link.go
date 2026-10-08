package models

import "time"

// UserLink 表示"主账户"与"关联账户"之间的授权关系。
//
// 关联账户（linked）本身是一个完整的账户，可以独立登录、独立收发邮件；
// 主账户（primary）获得关联账户的权限，可以切换到关联账户的身份读写邮件。
//
// 该表是本功能唯一新增的表，不影响 user / email / user_email 等既有表的数据。
type UserLink struct {
	ID        int       `xorm:"id int unsigned not null pk autoincr"`
	PrimaryID int       `xorm:"primary_id int not null unique('uk_primary_linked') index comment('主账户id')"`
	LinkedID  int       `xorm:"linked_id int not null unique('uk_primary_linked') index comment('关联账户id')"`
	Created   time.Time `xorm:"create datetime created"`
}

func (p UserLink) TableName() string {
	return "user_link"
}
