package model

import "time"

// OperationLogDO maps to the operation_log table (link database ds0).
type OperationLogDO struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	AccountNo  int64     `gorm:"column:account_no;index" json:"account_no"`
	Action     string    `gorm:"column:action;size:64" json:"action"`   // e.g. link:create, link:delete, link:status, domain:create
	Resource   string    `gorm:"column:resource;size:128" json:"resource"` // e.g. short_link:aBc123
	ResourceID string    `gorm:"column:resource_id;size:128" json:"resource_id"`
	IP         string    `gorm:"column:ip;size:64" json:"ip"`
	UserAgent  string    `gorm:"column:user_agent;size:512" json:"user_agent"`
	Details    string    `gorm:"column:details;type:text" json:"details"` // JSON details
	GmtCreate  time.Time `gorm:"column:gmt_create;autoCreateTime;index" json:"gmt_create"`
}

func (OperationLogDO) TableName() string {
	return "operation_log"
}
