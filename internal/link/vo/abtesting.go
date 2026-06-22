package vo

import "time"

// ABTestVO response for AB test
type ABTestVO struct {
	ID            int64              `json:"id"`
	AccountNo     int64              `json:"accountNo"`
	ShortLinkCode string             `json:"shortLinkCode"`
	GroupID       int64              `json:"groupId"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	Status        string             `json:"status"`
	TrafficSplit  string             `json:"trafficSplit"`
	StartTime     *time.Time         `json:"startTime"`
	EndTime       *time.Time         `json:"endTime"`
	Variants      []ABTestVariantVO  `json:"variants"`
	GmtCreate     time.Time          `json:"gmtCreate"`
}

// ABTestVariantVO variant in response
type ABTestVariantVO struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	TargetURL   string    `json:"targetUrl"`
	Weight      int       `json:"weight"`
	IsControl   int       `json:"isControl"`
	Description string    `json:"description"`
	IsActive    int       `json:"isActive"`
	GmtCreate   time.Time `json:"gmtCreate"`
}

// ABTestListVO list response
type ABTestListVO struct {
	Total int64      `json:"total"`
	Page  int        `json:"page"`
	Size  int        `json:"size"`
	List  []ABTestVO `json:"list"`
}

// ABTestStatVO statistics
type ABTestStatVO struct {
	ABTestID        int64                 `json:"abTestId"`
	Variants        []ABTestVariantStatVO `json:"variants"`
	WinningVariant  *ABTestVariantVO      `json:"winningVariant"`
}

// ABTestVariantStatVO per-variant statistics
type ABTestVariantStatVO struct {
	Variant     ABTestVariantVO `json:"variant"`
	ClickCount  int64           `json:"clickCount"`
	Percentage  float64         `json:"percentage"`
}
