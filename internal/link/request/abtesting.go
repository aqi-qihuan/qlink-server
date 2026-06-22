package request

import "time"

// CreateABTestRequest create AB test
type CreateABTestRequest struct {
	ShortLinkCode string                    `json:"shortLinkCode" binding:"required"`
	GroupID       int64                     `json:"groupId" binding:"required"`
	Name          string                    `json:"name" binding:"required"`
	Description   string                    `json:"description"`
	TrafficSplit  string                    `json:"trafficSplit"`
	StartTime     *time.Time                `json:"startTime"`
	EndTime       *time.Time                `json:"endTime"`
	Variants      []CreateABTestVariantReq  `json:"variants" binding:"required,min=2"`
}

type CreateABTestVariantReq struct {
	Name        string `json:"name" binding:"required"`
	TargetURL   string `json:"targetUrl" binding:"required"`
	Weight      int    `json:"weight"`
	IsControl   int    `json:"isControl"`
	Description string `json:"description"`
}

// UpdateABTestRequest update AB test
type UpdateABTestRequest struct {
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Status       string     `json:"status"`
	TrafficSplit string     `json:"trafficSplit"`
	StartTime    *time.Time `json:"startTime"`
	EndTime      *time.Time `json:"endTime"`
}

// StartABTestRequest start AB test
type StartABTestRequest struct {
	StartTime *time.Time `json:"startTime"`
}

// StopABTestRequest stop AB test
type StopABTestRequest struct {
	EndTime *time.Time `json:"endTime"`
}

// ABTestListRequest list AB tests
type ABTestListRequest struct {
	Page          int    `json:"page" form:"page"`
	Size          int    `json:"size" form:"size"`
	Status        string `json:"status" form:"status"`
	ShortLinkCode string `json:"shortLinkCode" form:"shortLinkCode"`
}
