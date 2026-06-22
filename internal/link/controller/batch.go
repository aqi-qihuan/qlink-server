package controller

import (
	"fmt"
	"log"

	"github.com/aqi/qlink-server/internal/common/enums"
	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/model"
	"github.com/aqi/qlink-server/internal/common/mq"
	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/aqi/qlink-server/internal/common/util"
	"github.com/aqi/qlink-server/internal/link/request"
	"github.com/aqi/qlink-server/internal/link/service"
	"github.com/aqi/qlink-server/internal/link/sharding"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type BatchController struct {
	dbs   []*gorm.DB
	rmq   *mq.RabbitMQ
	opLog *service.OperationLogService
}

func NewBatchController(dbs []*gorm.DB, rmq *mq.RabbitMQ, opLog *service.OperationLogService) *BatchController {
	return &BatchController{dbs: dbs, rmq: rmq, opLog: opLog}
}

// Delete handles POST /api/link/v1/batch_delete.
// Soft-deletes multiple group_code_mapping entries and their short_link counterparts.
func (ctrl *BatchController) Delete(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	// Batch delete via MQ (async) for audit trail and reliability
	for _, id := range req.IDs {
		eventMsg := model.EventMessage{
			MessageId:        util.GenerateUUID(),
			EventMessageType: string(enums.SHORT_LINK_DEL),
			BizId:            fmt.Sprintf("%d", util.GenerateSnowflakeID()),
			AccountNo:        loginUser.AccountNo,
		}
		eventMsg.Content = fmt.Sprintf(`{"groupId":%d,"mappingId":%d,"accountNo":%d,"code":""}`, req.GroupID, id, loginUser.AccountNo)

		if ctrl.rmq != nil {
			if err := ctrl.rmq.PublishJSON("short_link.event.exchange",
				"short_link.del.link.mapping.routing.key", eventMsg); err != nil {
				log.Printf("[MQ] batch delete publish error: %v", err)
			}
		}

		// Also sync delete from group_code_mapping
		dbIdx, tableIdx := sharding.RouteGroupCodeMapping(loginUser.AccountNo, req.GroupID)
		tableName := sharding.GetTableName("group_code_mapping", fmt.Sprintf("%d", tableIdx))
		ctrl.dbs[dbIdx].Table(tableName).
			Where("id = ? AND account_no = ? AND group_id = ?", id, loginUser.AccountNo, req.GroupID).
			Update("del", 1)
	}

	ctrl.opLog.Record(loginUser.AccountNo, "link:batch_delete", "short_link", "", c.ClientIP(), c.GetHeader("User-Agent"),
		map[string]interface{}{"count": len(req.IDs), "ids": req.IDs})

	response.JSON(c, response.BuildSuccessData(gin.H{"deleted": len(req.IDs)}))
}

// Status handles POST /api/link/v1/batch_status.
// Toggles state for a single short link (one-at-a-time batch UX).
func (ctrl *BatchController) Status(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.BatchStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	allowedStates := map[string]bool{"ACTIVE": true, "INACTIVE": true, "LOCK": true}
	if !allowedStates[req.State] {
		response.JSON(c, response.BuildError("invalid state"))
		return
	}

	dbPrefix, tableSuffix := sharding.RouteShortLink(req.Code)
	dbIdx := sharding.GetDBIndexByPrefix(dbPrefix)
	tableName := sharding.GetTableName("short_link", tableSuffix)

	result := ctrl.dbs[dbIdx].Table(tableName).
		Where("code = ? AND account_no = ? AND del = 0", req.Code, loginUser.AccountNo).
		Update("state", req.State)
	if result.RowsAffected == 0 {
		response.JSON(c, response.BuildError("short link not found"))
		return
	}

	ctrl.opLog.Record(loginUser.AccountNo, "link:status", "short_link", req.Code, c.ClientIP(), c.GetHeader("User-Agent"),
		map[string]interface{}{"state": req.State})

	response.JSON(c, response.BuildSuccess())
}
