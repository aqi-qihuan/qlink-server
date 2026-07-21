package model

import "time"

// AbuseReportDO maps to the abuse_report table (link ds0).
type AbuseReportDO struct {
	ID           int64      `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Code         string     `gorm:"column:code;size:16;index" json:"code"`
	ReporterAcct int64      `gorm:"column:reporter_account_no" json:"reporterAccountNo"`
	Reason       string     `gorm:"column:reason;size:256" json:"reason"`
	Description  string     `gorm:"column:description;type:text" json:"description,omitempty"`
	Status       string     `gorm:"column:status;size:16;default:PENDING" json:"status"` // PENDING / RESOLVED / DISMISSED
	ResolvedBy   int64      `gorm:"column:resolved_by" json:"resolvedBy,omitempty"`
	ResolvedNote string     `gorm:"column:resolved_note;size:512" json:"resolvedNote,omitempty"`
	ResolvedAt   *time.Time `gorm:"column:resolved_at" json:"resolvedAt,omitempty"`
	GmtCreate    time.Time  `gorm:"column:gmt_create;autoCreateTime" json:"gmtCreate"`
}

func (AbuseReportDO) TableName() string {
	return "abuse_report"
}
