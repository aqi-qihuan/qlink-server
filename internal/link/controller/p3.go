package controller

import (
	"github.com/aqi/qlink-server/internal/common/enums"
	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/aqi/qlink-server/internal/link/model"
	"github.com/aqi/qlink-server/internal/link/request"
	"github.com/aqi/qlink-server/internal/link/service"
	"github.com/aqi/qlink-server/internal/link/vo"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ============================================================
// URL Safety Check Controller
// ============================================================

type SafeCheckController struct {
	checker *service.URLSafeChecker
}

func NewSafeCheckController(checker *service.URLSafeChecker) *SafeCheckController {
	return &SafeCheckController{checker: checker}
}

// Check handles POST /api/link/v1/url_safe_check.
func (ctrl *SafeCheckController) Check(c *gin.Context) {
	var req request.URLSafeCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}
	result := ctrl.checker.Check(req.URL)
	response.JSON(c, response.BuildSuccessData(result))
}

// ============================================================
// Abuse Report Controller
// ============================================================

type AbuseReportController struct {
	db    *gorm.DB
	opLog *service.OperationLogService
}

func NewAbuseReportController(db *gorm.DB, opLog *service.OperationLogService) *AbuseReportController {
	return &AbuseReportController{db: db, opLog: opLog}
}

// Create handles POST /api/abuse_report/v1/create.
func (ctrl *AbuseReportController) Create(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.AbuseReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	rpt := model.AbuseReportDO{
		Code:         req.Code,
		ReporterAcct: loginUser.AccountNo,
		Reason:       req.Reason,
		Description:  req.Description,
		Status:       "PENDING",
	}
	if err := ctrl.db.Create(&rpt).Error; err != nil {
		response.JSON(c, response.BuildError("create abuse report failed: "+err.Error()))
		return
	}

	ctrl.opLog.Record(loginUser.AccountNo, "abuse:report", "short_link", req.Code, c.ClientIP(), c.GetHeader("User-Agent"),
		map[string]interface{}{"reason": req.Reason})

	response.JSON(c, response.BuildSuccessData(vo.AbuseReportVO{
		ID: rpt.ID, Code: rpt.Code, Reason: rpt.Reason, Description: rpt.Description, Status: rpt.Status,
		CreatedAt: rpt.GmtCreate.Format("2006-01-02 15:04:05"),
	}))
}

// List handles POST /api/abuse_report/v1/list (admin/support only, shows own reports).
func (ctrl *AbuseReportController) List(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.AbuseReportPageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}
	if req.Page <= 0 { req.Page = 1 }
	if req.Size <= 0 { req.Size = 20 }

	q := ctrl.db.Model(&model.AbuseReportDO{}).Where("reporter_account_no = ?", loginUser.AccountNo)
	if req.Status != "" {
		q = q.Where("status = ?", req.Status)
	}
	var total int64
	q.Count(&total)
	var list []model.AbuseReportDO
	q.Order("gmt_create DESC").Offset((req.Page - 1) * req.Size).Limit(req.Size).Find(&list)

	vlist := make([]vo.AbuseReportVO, len(list))
	for i, r := range list {
		vlist[i] = vo.AbuseReportVO{
			ID: r.ID, Code: r.Code, Reason: r.Reason, Description: r.Description,
			Status: r.Status, CreatedAt: r.GmtCreate.Format("2006-01-02 15:04:05"),
		}
	}
	response.JSON(c, response.BuildSuccessData(gin.H{
		"page": req.Page, "size": req.Size, "total": total, "list": vlist,
	}))
}

// ============================================================
// Brand Config Controller
// ============================================================

type BrandConfigController struct {
	db *gorm.DB
}

func NewBrandConfigController(db *gorm.DB) *BrandConfigController {
	return &BrandConfigController{db: db}
}

// Save handles POST /api/brand_config/v1/save - upserts brand config.
func (ctrl *BrandConfigController) Save(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.BrandConfigSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	// Upsert
	var existing model.BrandConfigDO
	err := ctrl.db.Where("account_no = ?", loginUser.AccountNo).First(&existing).Error
	if err != nil {
		// Create
		cfg := model.BrandConfigDO{
			AccountNo:    loginUser.AccountNo,
			SiteName:     req.SiteName,
			LogoURL:      req.LogoURL,
			FaviconURL:   req.FaviconURL,
			PrimaryColor: req.PrimaryColor,
			Copyright:    req.Copyright,
		}
		ctrl.db.Create(&cfg)
	} else {
		ctrl.db.Model(&existing).Updates(map[string]interface{}{
			"site_name": req.SiteName, "logo_url": req.LogoURL, "favicon_url": req.FaviconURL,
			"primary_color": req.PrimaryColor, "copyright": req.Copyright,
		})
	}

	response.JSON(c, response.BuildSuccess())
}

// Get handles GET /api/brand_config/v1/get.
func (ctrl *BrandConfigController) Get(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}

	var cfg model.BrandConfigDO
	if err := ctrl.db.Where("account_no = ?", loginUser.AccountNo).First(&cfg).Error; err != nil {
		response.JSON(c, response.BuildSuccessData(vo.BrandConfigVO{}))
		return
	}

	response.JSON(c, response.BuildSuccessData(vo.BrandConfigVO{
		SiteName: cfg.SiteName, LogoURL: cfg.LogoURL, FaviconURL: cfg.FaviconURL,
		PrimaryColor: cfg.PrimaryColor, Copyright: cfg.Copyright,
	}))
}

// PublicGet handles GET /api/public/brand_config/:account_no - no auth needed.
func (ctrl *BrandConfigController) PublicGet(c *gin.Context) {
	var req struct {
		AccountNo int64 `uri:"account_no" binding:"required"`
	}
	if err := c.ShouldBindUri(&req); err != nil {
		response.JSON(c, response.BuildError("invalid account_no"))
		return
	}

	var cfg model.BrandConfigDO
	if err := ctrl.db.Where("account_no = ?", req.AccountNo).First(&cfg).Error; err != nil {
		response.JSON(c, response.BuildSuccessData(vo.BrandConfigVO{}))
		return
	}
	response.JSON(c, response.BuildSuccessData(vo.BrandConfigVO{
		SiteName: cfg.SiteName, LogoURL: cfg.LogoURL, FaviconURL: cfg.FaviconURL,
		PrimaryColor: cfg.PrimaryColor, Copyright: cfg.Copyright,
	}))
}

// Delete handles POST /api/brand_config/v1/delete.
func (ctrl *BrandConfigController) Delete(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	ctrl.db.Where("account_no = ?", loginUser.AccountNo).Delete(&model.BrandConfigDO{})
	response.JSON(c, response.BuildSuccess())
}
