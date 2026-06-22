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

type DomainController struct {
	db     *gorm.DB
	opLog  *service.OperationLogService
}

func NewDomainController(db *gorm.DB, opLog *service.OperationLogService) *DomainController {
	return &DomainController{db: db, opLog: opLog}
}

// List handles GET /api/domain/v1/list - all active domains.
func (ctrl *DomainController) List(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var domains []model.DomainDO
	ctrl.db.Where("del = 0").Find(&domains)
	list := make([]vo.DomainVO, len(domains))
	for i, d := range domains {
		list[i] = vo.DomainVO{
			ID:         d.ID,
			AccountNo:  d.AccountNo,
			DomainType: d.DomainType,
			Value:      d.Value,
			State:      d.State,
		}
	}
	response.JSON(c, response.BuildSuccessData(list))
}

// Create handles POST /api/domain/v1/create.
func (ctrl *DomainController) Create(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.DomainCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	d := model.DomainDO{
		AccountNo:  loginUser.AccountNo,
		DomainType: req.DomainType,
		Value:      req.Value,
		State:      "ACTIVE",
	}
	if err := ctrl.db.Create(&d).Error; err != nil {
		response.JSON(c, response.BuildError("create domain failed: "+err.Error()))
		return
	}

	ctrl.opLog.Record(loginUser.AccountNo, "domain:create", "domain", "", c.ClientIP(), c.GetHeader("User-Agent"),
		map[string]interface{}{"value": req.Value, "type": req.DomainType})

	response.JSON(c, response.BuildSuccessData(vo.DomainVO{
		ID: d.ID, AccountNo: d.AccountNo, DomainType: d.DomainType, Value: d.Value, State: d.State,
	}))
}

// Update handles POST /api/domain/v1/update.
func (ctrl *DomainController) Update(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.DomainUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	updates := map[string]interface{}{}
	if req.DomainType != "" { updates["domain_type"] = req.DomainType }
	if req.Value != "" { updates["value"] = req.Value }
	if len(updates) == 0 {
		response.JSON(c, response.BuildError("nothing to update"))
		return
	}

	result := ctrl.db.Model(&model.DomainDO{}).Where("id = ? AND del = 0", req.ID).Updates(updates)
	if result.RowsAffected == 0 {
		response.JSON(c, response.BuildError("domain not found"))
		return
	}

	ctrl.opLog.Record(loginUser.AccountNo, "domain:update", "domain", "", c.ClientIP(), c.GetHeader("User-Agent"),
		map[string]interface{}{"id": req.ID, "updates": updates})

	response.JSON(c, response.BuildSuccess())
}

// Delete handles POST /api/domain/v1/delete.
func (ctrl *DomainController) Delete(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.DomainDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	result := ctrl.db.Model(&model.DomainDO{}).Where("id = ? AND del = 0", req.ID).Update("del", 1)
	if result.RowsAffected == 0 {
		response.JSON(c, response.BuildError("domain not found"))
		return
	}

	ctrl.opLog.Record(loginUser.AccountNo, "domain:delete", "domain", "", c.ClientIP(), c.GetHeader("User-Agent"),
		map[string]interface{}{"id": req.ID})

	response.JSON(c, response.BuildSuccess())
}

// Status handles POST /api/domain/v1/status - toggle ACTIVE/INACTIVE.
func (ctrl *DomainController) Status(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.DomainStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}
	if req.State != "ACTIVE" && req.State != "INACTIVE" {
		response.JSON(c, response.BuildError("state must be ACTIVE or INACTIVE"))
		return
	}

	result := ctrl.db.Model(&model.DomainDO{}).Where("id = ? AND del = 0", req.ID).Update("state", req.State)
	if result.RowsAffected == 0 {
		response.JSON(c, response.BuildError("domain not found"))
		return
	}

	ctrl.opLog.Record(loginUser.AccountNo, "domain:status", "domain", "", c.ClientIP(), c.GetHeader("User-Agent"),
		map[string]interface{}{"id": req.ID, "state": req.State})

	response.JSON(c, response.BuildSuccess())
}
