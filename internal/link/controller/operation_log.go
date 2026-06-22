package controller

import (
	"time"

	"github.com/aqi/qlink-server/internal/common/enums"
	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/aqi/qlink-server/internal/link/request"
	"github.com/aqi/qlink-server/internal/link/service"
	"github.com/aqi/qlink-server/internal/link/vo"
	"github.com/gin-gonic/gin"
)

type OperationLogController struct {
	svc *service.OperationLogService
}

func NewOperationLogController(svc *service.OperationLogService) *OperationLogController {
	return &OperationLogController{svc: svc}
}

// Page handles POST /api/operation_log/v1/page.
func (ctrl *OperationLogController) Page(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.OperationLogPageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}
	if req.Page <= 0 { req.Page = 1 }
	if req.Size <= 0 { req.Size = 20 }

	var st, et time.Time
	if req.StartTime != "" { st, _ = time.Parse("2006-01-02", req.StartTime) }
	if req.EndTime != "" { et, _ = time.Parse("2006-01-02", req.EndTime) }

	list, total, err := ctrl.svc.Page(loginUser.AccountNo, req.Action, req.ResourceID, st, et, req.Page, req.Size)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}

	vlist := make([]vo.OperationLogVO, len(list))
	for i, l := range list {
		vlist[i] = vo.OperationLogVO{
			ID: l.ID, AccountNo: l.AccountNo, Action: l.Action, Resource: l.Resource,
			ResourceID: l.ResourceID, IP: l.IP, UserAgent: l.UserAgent, Details: l.Details,
			CreatedAt: l.GmtCreate.Format("2006-01-02 15:04:05"),
		}
	}

	response.JSON(c, response.BuildSuccessData(gin.H{
		"page":  req.Page,
		"size":  req.Size,
		"total": total,
		"list":  vlist,
	}))
}

// Action types for the frontend dropdown
func (ctrl *OperationLogController) ActionTypes(c *gin.Context) {
	types := []string{
		"link:create", "link:update", "link:delete", "link:status",
		"domain:create", "domain:update", "domain:delete", "domain:status",
		"group:create", "group:update", "group:delete",
	}
	response.JSON(c, response.BuildSuccessData(types))
}
