package controller

import (
	"github.com/aqi/qlink-server/internal/account/request"
	"github.com/aqi/qlink-server/internal/account/service"
	"github.com/aqi/qlink-server/internal/common/enums"
	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/gin-gonic/gin"
)

type ApiTokenController struct {
	svc *service.ApiTokenService
}

func NewApiTokenController(svc *service.ApiTokenService) *ApiTokenController {
	return &ApiTokenController{svc: svc}
}

// Create handles POST /api/account/v1/api_token/create.
func (ctrl *ApiTokenController) Create(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.ApiTokenCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	result, err := ctrl.svc.Create(loginUser.AccountNo, &req)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}

// List handles POST /api/account/v1/api_token/list.
func (ctrl *ApiTokenController) List(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}

	result, err := ctrl.svc.List(loginUser.AccountNo)
	if err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccessData(result))
}

// Delete handles POST /api/account/v1/api_token/delete.
func (ctrl *ApiTokenController) Delete(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.ApiTokenDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	if err := ctrl.svc.Delete(loginUser.AccountNo, req.ID); err != nil {
		response.JSON(c, response.BuildError(err.Error()))
		return
	}
	response.JSON(c, response.BuildSuccess())
}
