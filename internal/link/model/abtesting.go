package model

import "time"

// ABTestDO A/B test experiment, stored in ds0 (non-sharded).
type ABTestDO struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	AccountNo     int64      `gorm:"column:account_no;not null;index:idx_acc_code" json:"account_no"`
	ShortLinkCode string     `gorm:"column:short_link_code;size:32;not null;index:idx_acc_code" json:"short_link_code"`
	GroupID       int64      `gorm:"column:group_id" json:"group_id"`
	Name          string     `gorm:"column:name;size:255;not null" json:"name"`
	Description   string     `gorm:"column:description;size:500" json:"description"`
	Status        string     `gorm:"column:status;size:20;default:draft" json:"status"`
	TrafficSplit  string     `gorm:"column:traffic_split;size:20;default:equal" json:"traffic_split"`
	StartTime     *time.Time `gorm:"column:start_time" json:"start_time"`
	EndTime       *time.Time `gorm:"column:end_time" json:"end_time"`
	Del           int        `gorm:"column:del;default:0" json:"del"`
	GmtCreate     time.Time  `gorm:"column:gmt_create;autoCreateTime" json:"gmt_create"`
	GmtModified   time.Time  `gorm:"column:gmt_modified;autoUpdateTime" json:"gmt_modified"`
}

func (ABTestDO) TableName() string {
	return "ab_test"
}

// IsRunning checks whether the AB test is currently running.
func (a *ABTestDO) IsRunning() bool {
	if a.Status != "running" || a.Del == 1 {
		return false
	}
	now := time.Now()
	if a.StartTime != nil && now.Before(*a.StartTime) {
		return false
	}
	if a.EndTime != nil && now.After(*a.EndTime) {
		return false
	}
	return true
}

// ABTestVariantDO variant with target URL and weight.
type ABTestVariantDO struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ABTestID    int64     `gorm:"column:ab_test_id;not null;index" json:"ab_test_id"`
	Name        string    `gorm:"column:name;size:100;not null" json:"name"`
	TargetURL   string    `gorm:"column:target_url;size:2048;not null" json:"target_url"`
	Weight      int       `gorm:"column:weight;default:50" json:"weight"`
	IsControl   int       `gorm:"column:is_control;default:0" json:"is_control"`
	Description string    `gorm:"column:description;size:500" json:"description"`
	IsActive    int       `gorm:"column:is_active;default:1" json:"is_active"`
	GmtCreate   time.Time `gorm:"column:gmt_create;autoCreateTime" json:"gmt_create"`
}

func (ABTestVariantDO) TableName() string {
	return "ab_test_variant"
}
