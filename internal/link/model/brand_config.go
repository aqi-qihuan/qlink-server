package model

import "time"

// BrandConfigDO maps to the brand_config table (link ds0).
type BrandConfigDO struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	AccountNo    int64     `gorm:"column:account_no;uniqueIndex" json:"account_no"`
	SiteName     string    `gorm:"column:site_name;size:128" json:"site_name"`
	LogoURL      string    `gorm:"column:logo_url;size:512" json:"logo_url"`
	FaviconURL   string    `gorm:"column:favicon_url;size:512" json:"favicon_url"`
	PrimaryColor string    `gorm:"column:primary_color;size:16" json:"primary_color"`
	Copyright    string    `gorm:"column:copyright;size:256" json:"copyright"`
	GmtCreate    time.Time `gorm:"column:gmt_create;autoCreateTime" json:"gmt_create"`
	GmtModified  time.Time `gorm:"column:gmt_modified;autoUpdateTime" json:"gmt_modified"`
}

func (BrandConfigDO) TableName() string {
	return "brand_config"
}
