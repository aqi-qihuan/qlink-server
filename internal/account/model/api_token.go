package model

import "time"

// ApiTokenDO maps to the api_token table in account database.
type ApiTokenDO struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	AccountNo int64     `gorm:"column:account_no;index" json:"account_no"`
	Name      string    `gorm:"column:name;size:128" json:"name"`
	Token     string    `gorm:"column:token;size:64;uniqueIndex" json:"token"`
	Scopes    string    `gorm:"column:scopes;size:512" json:"scopes"` // comma-separated: link:read,link:write,stats:read
	ExpiredAt *time.Time `gorm:"column:expired_at" json:"expired_at,omitempty"`
	LastUsed  *time.Time `gorm:"column:last_used" json:"last_used,omitempty"`
	GmtCreate time.Time  `gorm:"column:gmt_create;autoCreateTime" json:"gmt_create"`
}

func (ApiTokenDO) TableName() string {
	return "api_token"
}
