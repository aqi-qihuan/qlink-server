package controller

import (
	"encoding/csv"
	"fmt"

	"github.com/aqi/qlink-server/internal/common/enums"
	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/aqi/qlink-server/internal/data/request"
	"github.com/aqi/qlink-server/internal/data/service"
	"github.com/gin-gonic/gin"
)

type VisitStatsController struct {
	svc *service.VisitStatsService
}

func NewVisitStatsController(svc *service.VisitStatsService) *VisitStatsController {
	return &VisitStatsController{svc: svc}
}

// PageRecord handles POST /api/visit_stats/v1/page_record.
func (ctrl *VisitStatsController) PageRecord(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.VisitRecordPageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}
	page := int(req.Page)
	size := int(req.Size)
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}

	result, err := ctrl.svc.PageRecord(loginUser.AccountNo, req.Code, page, size)
	if err != nil {
		response.JSON(c, response.BuildResult(enums.DATA_OUT_OF_LIMIT_SIZE))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}

// RegionDay handles POST /api/visit_stats/v1/region_day.
func (ctrl *VisitStatsController) RegionDay(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.RegionQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	result, err := ctrl.svc.RegionDay(loginUser.AccountNo, req.Code, req.StartTime, req.EndTime)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}

// Trend handles POST /api/visit_stats/v1/trend.
func (ctrl *VisitStatsController) Trend(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.VisitTrendQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	result, err := ctrl.svc.Trend(loginUser.AccountNo, req.Code, req.Type, req.StartTime, req.EndTime)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}

// FrequentIP handles POST /api/visit_stats/v1/frequent_ip.
func (ctrl *VisitStatsController) FrequentIP(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.FrequentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	result, err := ctrl.svc.FrequentIP(loginUser.AccountNo, req.Code)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}

// FrequentReferer handles POST /api/visit_stats/v1/frequent_referer.
func (ctrl *VisitStatsController) FrequentReferer(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.FrequentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	result, err := ctrl.svc.FrequentReferer(loginUser.AccountNo, req.Code)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}

// DeviceInfo handles POST /api/visit_stats/v1/device_info.
func (ctrl *VisitStatsController) DeviceInfo(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.DeviceInfoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	result, err := ctrl.svc.DeviceInfo(loginUser.AccountNo, req.Code, req.Field)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}

// Dashboard handles POST /api/visit_stats/v1/dashboard.
func (ctrl *VisitStatsController) Dashboard(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.DashboardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	result, err := ctrl.svc.GetDashboard(loginUser.AccountNo, req.StartTime, req.EndTime)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}

// Analysis handles POST /api/visit_stats/v1/analysis.
func (ctrl *VisitStatsController) Analysis(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.AnalysisRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	result, err := ctrl.svc.GetAnalysis(loginUser.AccountNo, req.Code, req.StartTime, req.EndTime)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}

// ExportCSV handles POST /api/visit_stats/v1/export.
func (ctrl *VisitStatsController) ExportCSV(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.ExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	header, data, err := ctrl.svc.ExportCSV(loginUser.AccountNo, req.Code, req.StartTime, req.EndTime)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}

	// Write CSV response
	filename := fmt.Sprintf("visit_stats_%s_%s.csv", req.Code, req.StartTime)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Transfer-Encoding", "chunked")

	// Write BOM for Excel compatibility
	c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(c.Writer)
	writer.Write(header)
	for _, row := range data {
		writer.Write(row)
	}
	writer.Flush()
}

// Geo handles POST /api/visit_stats/v1/geo.
func (ctrl *VisitStatsController) Geo(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.GeoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	result, err := ctrl.svc.GetGeo(loginUser.AccountNo, req.Code, req.StartTime, req.EndTime)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}
