package controller

import (
	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/aqi/qlink-server/internal/link/request"
	"github.com/aqi/qlink-server/internal/link/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type ABTestController struct {
	svc *service.ABTestService
}

func NewABTestController(db *gorm.DB, rdb *redis.Client) *ABTestController {
	return &ABTestController{
		svc: service.NewABTestService(db, rdb),
	}
}

// Create POST /api/ab_test/v1/add
func (ctrl *ABTestController) Create(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildError("not logged in"))
		return
	}
	var req request.CreateABTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body: "+err.Error()))
		return
	}
	resp := ctrl.svc.Create(&req, loginUser.AccountNo)
	response.JSON(c, resp)
}

// List POST /api/ab_test/v1/page
func (ctrl *ABTestController) List(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildError("not logged in"))
		return
	}
	var req request.ABTestListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body: "+err.Error()))
		return
	}
	resp := ctrl.svc.List(&req, loginUser.AccountNo)
	response.JSON(c, resp)
}

// Detail POST /api/ab_test/v1/detail
func (ctrl *ABTestController) Detail(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildError("not logged in"))
		return
	}
	var req struct {
		ID int64 `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body: "+err.Error()))
		return
	}
	resp := ctrl.svc.Get(req.ID)
	response.JSON(c, resp)
}

// Update POST /api/ab_test/v1/update
func (ctrl *ABTestController) Update(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildError("not logged in"))
		return
	}
	var req struct {
		ID int64 `json:"id" binding:"required"`
		request.UpdateABTestRequest
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body: "+err.Error()))
		return
	}
	resp := ctrl.svc.Update(req.ID, &req.UpdateABTestRequest)
	response.JSON(c, resp)
}

// Start POST /api/ab_test/v1/start
func (ctrl *ABTestController) Start(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildError("not logged in"))
		return
	}
	var req struct {
		ID int64 `json:"id" binding:"required"`
		request.StartABTestRequest
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body: "+err.Error()))
		return
	}
	resp := ctrl.svc.Start(req.ID, &req.StartABTestRequest)
	response.JSON(c, resp)
}

// Stop POST /api/ab_test/v1/stop
func (ctrl *ABTestController) Stop(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildError("not logged in"))
		return
	}
	var req struct {
		ID int64 `json:"id" binding:"required"`
		request.StopABTestRequest
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body: "+err.Error()))
		return
	}
	resp := ctrl.svc.Stop(req.ID, &req.StopABTestRequest)
	response.JSON(c, resp)
}

// Delete POST /api/ab_test/v1/del
func (ctrl *ABTestController) Delete(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildError("not logged in"))
		return
	}
	var req struct {
		ID int64 `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body: "+err.Error()))
		return
	}
	resp := ctrl.svc.Delete(req.ID)
	response.JSON(c, resp)
}

// Statistics POST /api/ab_test/v1/statistics
func (ctrl *ABTestController) Statistics(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildError("not logged in"))
		return
	}
	var req struct {
		ID int64 `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body: "+err.Error()))
		return
	}
	resp := ctrl.svc.Statistics(req.ID)
	response.JSON(c, resp)
}
