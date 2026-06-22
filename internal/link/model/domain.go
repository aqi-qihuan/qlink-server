package model

import "time"

// DomainDO maps to the domain table.
type DomainDO struct {
	ID          int64     `gorm:"column:id;primaryKey" json:"id"`
	AccountNo   int64     `gorm:"column:account_no" json:"account_no"`
	DomainType  string    `gorm:"column:domain_type" json:"domain_type"` // official / custom
	Value       string    `gorm:"column:value" json:"value"`
	State       string    `gorm:"column:state;default:ACTIVE" json:"state"` // ACTIVE / INACTIVE
	Del         int       `gorm:"column:del" json:"del"`
	GmtCreate   time.Time `gorm:"column:gmt_create;autoCreateTime" json:"gmt_create"`
	GmtModified time.Time `gorm:"column:gmt_modified;autoUpdateTime" json:"gmt_modified"`
}

func (DomainDO) TableName() string {
	return "domain"
}
