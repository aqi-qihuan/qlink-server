package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aqi/qlink-server/internal/common/constant"
	"github.com/aqi/qlink-server/internal/common/enums"
	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/model"
	"github.com/aqi/qlink-server/internal/common/mq"
	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/aqi/qlink-server/internal/common/util"
	"github.com/aqi/qlink-server/internal/link/component"
	linkmodel "github.com/aqi/qlink-server/internal/link/model"
	"github.com/aqi/qlink-server/internal/link/request"
	"github.com/aqi/qlink-server/internal/link/service"
	"github.com/aqi/qlink-server/internal/link/sharding"
	linkvo "github.com/aqi/qlink-server/internal/link/vo"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type ShortLinkController struct {
	dbs    []*gorm.DB // 3 datasources: ds0, ds1, dsa
	rdb    *redis.Client
	rmq    *mq.RabbitMQ
	kafka  *mq.KafkaProducer
	opLog  *service.OperationLogService
}

func NewShortLinkController(dbs []*gorm.DB, rdb *redis.Client, rmq *mq.RabbitMQ, kafka *mq.KafkaProducer, opLog *service.OperationLogService) *ShortLinkController {
	return &ShortLinkController{dbs: dbs, rdb: rdb, rmq: rmq, kafka: kafka, opLog: opLog}
}

// Check handles GET /api/link/v1/check (RPC internal).
func (ctrl *ShortLinkController) Check(c *gin.Context) {
	var req request.ShortLinkCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("code is required"))
		return
	}
	code := req.Code
	// Check existence across shards
	dbPrefix, tableSuffix := sharding.RouteShortLink(code)
	dbIdx := sharding.GetDBIndexByPrefix(dbPrefix)
	tableName := sharding.GetTableName("short_link", tableSuffix)

	var count int64
	ctrl.dbs[dbIdx].Table(tableName).Where("code = ? AND del = 0", code).Count(&count)
	response.JSON(c, response.BuildSuccessData(count > 0))
}

// Add handles POST /api/link/v1/add.
func (ctrl *ShortLinkController) Add(c *gin.Context) {
 loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}

	var req request.ShortLinkAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	// Validate domain exists
	if req.DomainType != "" {
		normalizedType := strings.ToUpper(req.DomainType)
		// Handle common frontend typo: "offical" "OFFICIAL"
		if normalizedType == "OFFICAL" {
			normalizedType = "OFFICIAL"
		}
		req.DomainType = normalizedType
		var domain linkmodel.DomainDO
		err := ctrl.dbs[0].Table("domain").
			Where("domain_type = ? AND del = 0", req.DomainType).First(&domain).Error
		if err != nil {
			response.JSON(c, response.BuildError("domain not found"))
			return
		}
	}

	// Validate group exists
	if req.GroupID > 0 {
		groupDbIdx := sharding.RouteLinkGroup(loginUser.AccountNo)
		var group linkmodel.LinkGroupDO
		err := ctrl.dbs[groupDbIdx].Table("link_group").
			Where("id = ? AND account_no = ?", req.GroupID, loginUser.AccountNo).First(&group).Error
		if err != nil {
			response.JSON(c, response.BuildError("group not found"))
			return
		}
	}

	// Check traffic quota via Redis Lua
	if !ctrl.checkTrafficQuota(c.Request.Context(), loginUser.AccountNo) {
		response.JSON(c, response.BuildResult(enums.TRAFFIC_REDUCE_FAIL))
		return
	}

	// Generate short link code with collision retry
	prefixedUrl := util.AddUrlPrefix(req.OriginalUrl)
	sign := util.MD5(req.OriginalUrl)
	code := component.CreateShortLinkCode(prefixedUrl)

	// Check for collision and retry
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		exists := ctrl.checkCodeExists(code)
		if !exists {
			break
		}
		// Collision: increment version and re-hash
		prefixedUrl = util.AddUrlPrefixVersion(prefixedUrl)
		code = component.CreateShortLinkCode(prefixedUrl)
	}

	// Acquire distributed lock via Redis Lua
	acquired := ctrl.acquireCodeLock(c.Request.Context(), code, loginUser.AccountNo)
	if !acquired {
		response.JSON(c, response.BuildError("short link code locked by another user"))
		return
	}

	// Publish MQ event for async DB writes
	eventMsg := model.EventMessage{
		MessageId:       util.GenerateUUID(),
		EventMessageType: string(enums.SHORT_LINK_ADD),
		BizId:           fmt.Sprintf("%d", util.GenerateSnowflakeID()),
		AccountNo:       loginUser.AccountNo,
	}
	content, _ := json.Marshal(map[string]interface{}{
		"groupId":      req.GroupID,
		"title":        req.Title,
		"originalUrl":  prefixedUrl,
		"domain":       req.DomainType,
		"code":         code,
		"sign":         sign,
		"expired":      req.Expired,
		"accountNo":    loginUser.AccountNo,
		"password":     req.Password,
		"passwordHint": req.PasswordHint,
		"state":        string(enums.SL_ACTIVE),
		"linkType":     string(enums.TRAFFIC_FIRST),
	})
	eventMsg.Content = string(content)

	if ctrl.rmq != nil {
		if err := ctrl.rmq.PublishJSON("short_link.event.exchange",
			"short_link.add.link.mapping.routing.key", eventMsg); err != nil {
			log.Printf("[MQ] publish short link add error: %v", err)
		}
	}

	response.JSON(c, response.BuildSuccessData(gin.H{
		"code":         code,
		"original_url": req.OriginalUrl,
		"title":        req.Title,
	}))

	if ctrl.opLog != nil {
		ctrl.opLog.Record(loginUser.AccountNo, "link:create", "short_link", code,
			c.ClientIP(), c.GetHeader("User-Agent"),
			map[string]interface{}{"title": req.Title, "domain": req.DomainType})
	}
}

// Page handles POST /api/link/v1/page.
func (ctrl *ShortLinkController) Page(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.ShortLinkPageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}
	if req.Page <= 0 { req.Page = 1 }
	if req.Size <= 0 { req.Size = 20 }

	dbIdx, tableIdx := sharding.RouteGroupCodeMapping(loginUser.AccountNo, req.GroupID)
	tableName := sharding.GetTableName("group_code_mapping", fmt.Sprintf("%d", tableIdx))

	var total int64
	ctrl.dbs[dbIdx].Table(tableName).
		Where("account_no = ? AND group_id = ? AND del = 0", loginUser.AccountNo, req.GroupID).
		Count(&total)

	var list []linkmodel.GroupCodeMappingDO
	offset := (req.Page - 1) * req.Size
	ctrl.dbs[dbIdx].Table(tableName).
		Where("account_no = ? AND group_id = ? AND del = 0", loginUser.AccountNo, req.GroupID).
		Order("gmt_create DESC").
		Offset(offset).Limit(req.Size).
		Find(&list)

	response.JSON(c, response.BuildSuccessData(gin.H{
		"page":  req.Page,
		"size":  req.Size,
		"total": total,
		"list":  list,
	}))
}

// Detail handles POST /api/link/v1/detail.
func (ctrl *ShortLinkController) Detail(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.ShortLinkDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	dbIdx, tableIdx := sharding.RouteGroupCodeMapping(loginUser.AccountNo, req.GroupID)
	tableName := sharding.GetTableName("group_code_mapping", fmt.Sprintf("%d", tableIdx))

	var mapping linkmodel.GroupCodeMappingDO
	err := ctrl.dbs[dbIdx].Table(tableName).
		Where("id = ? AND account_no = ? AND group_id = ? AND del = 0", req.MappingID, loginUser.AccountNo, req.GroupID).
		First(&mapping).Error
	if err != nil {
		response.JSON(c, response.BuildError("short link not found"))
		return
	}

	response.JSON(c, response.BuildSuccessData(mapping))
}

// Del handles POST /api/link/v1/del.
func (ctrl *ShortLinkController) Del(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.ShortLinkDelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	// Publish delete event
	eventMsg := model.EventMessage{
		MessageId:       util.GenerateUUID(),
		EventMessageType: string(enums.SHORT_LINK_DEL),
		BizId:           fmt.Sprintf("%d", util.GenerateSnowflakeID()),
		AccountNo:       loginUser.AccountNo,
	}
	content, _ := json.Marshal(map[string]interface{}{
		"groupId":   req.GroupID,
		"mappingId": req.MappingID,
		"code":      req.Code,
		"accountNo": loginUser.AccountNo,
	})
	eventMsg.Content = string(content)

	if ctrl.rmq != nil {
		ctrl.rmq.PublishJSON("short_link.event.exchange",
			"short_link.del.link.mapping.routing.key", eventMsg)
	}

	response.JSON(c, response.BuildSuccess())

	if ctrl.opLog != nil {
		ctrl.opLog.Record(loginUser.AccountNo, "link:delete", "short_link", req.Code,
			c.ClientIP(), c.GetHeader("User-Agent"),
			map[string]interface{}{"mapping_id": req.MappingID, "group_id": req.GroupID})
	}
}

// Update handles POST /api/link/v1/update.
func (ctrl *ShortLinkController) Update(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.ShortLinkUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	// Publish update event
	eventMsg := model.EventMessage{
		MessageId:       util.GenerateUUID(),
		EventMessageType: string(enums.SHORT_LINK_UPDATE),
		BizId:           fmt.Sprintf("%d", util.GenerateSnowflakeID()),
		AccountNo:       loginUser.AccountNo,
	}
	content, _ := json.Marshal(map[string]interface{}{
		"id":          req.ID,
		"groupId":     req.GroupID,
		"title":       req.Title,
		"originalUrl": req.OriginalUrl,
		"domain":      req.Domain,
		"code":        req.Code,
		"expired":     req.Expired,
		"accountNo":   loginUser.AccountNo,
	})
	eventMsg.Content = string(content)

	if ctrl.rmq != nil {
		ctrl.rmq.PublishJSON("short_link.event.exchange",
			"short_link.update.link.mapping.routing.key", eventMsg)
	}

	response.JSON(c, response.BuildSuccess())

	if ctrl.opLog != nil {
		ctrl.opLog.Record(loginUser.AccountNo, "link:update", "short_link", req.Code,
			c.ClientIP(), c.GetHeader("User-Agent"),
			map[string]interface{}{"id": req.ID, "title": req.Title, "domain": req.Domain})
	}
}

// Status handles POST /api/link/v1/status.
// Toggles short link state: ACTIVE ↔ INACTIVE ↔ LOCK.
func (ctrl *ShortLinkController) Status(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}
	var req request.ShortLinkStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("invalid request body"))
		return
	}

	// Validate state value
	allowedStates := map[string]bool{"ACTIVE": true, "INACTIVE": true, "LOCK": true}
	if !allowedStates[req.State] {
		response.JSON(c, response.BuildError("invalid state, must be ACTIVE/INACTIVE/LOCK"))
		return
	}

	// Direct DB update (sync, no MQ needed for simple state toggle)
	dbPrefix, tableSuffix := sharding.RouteShortLink(req.Code)
	dbIdx := sharding.GetDBIndexByPrefix(dbPrefix)
	tableName := sharding.GetTableName("short_link", tableSuffix)

	result := ctrl.dbs[dbIdx].Table(tableName).
		Where("code = ? AND account_no = ? AND del = 0", req.Code, loginUser.AccountNo).
		Update("state", req.State)
	if result.Error != nil {
		response.JSON(c, response.BuildError("update failed: "+result.Error.Error()))
		return
	}
	if result.RowsAffected == 0 {
		response.JSON(c, response.BuildError("short link not found"))
		return
	}

	// Invalidate Redis cache so next Dispatch picks up new state
	if ctrl.rdb != nil {
		ctrl.rdb.Del(c, constant.FormatShortLinkCacheKey(req.Code))
	}

	response.JSON(c, response.BuildSuccess())

	if ctrl.opLog != nil {
		ctrl.opLog.Record(loginUser.AccountNo, "link:status", "short_link", req.Code,
			c.ClientIP(), c.GetHeader("User-Agent"),
			map[string]interface{}{"state": req.State})
	}
}

// Summary handles POST /api/link/v1/summary.
// Returns aggregate link counts for the dashboard by querying all shards.
func (ctrl *ShortLinkController) Summary(c *gin.Context) {
	loginUser := interceptor.GetLoginUser(c)
	if loginUser == nil {
		response.JSON(c, response.BuildResult(enums.ACCOUNT_UNLOGIN))
		return
	}

	result := linkvo.LinkSummaryVO{}
	today := time.Now().Format("2006-01-02")
	var prefixes = []string{"0", "1", "a"}

	for _, prefix := range prefixes {
		dbIdx := sharding.GetDBIndexByPrefix(prefix)
		tableName := sharding.GetTableName("short_link", prefix)

		var total, active, todayCreated int64
		ctrl.dbs[dbIdx].Table(tableName).Where("account_no = ? AND del = 0", loginUser.AccountNo).Count(&total)
		ctrl.dbs[dbIdx].Table(tableName).Where("account_no = ? AND state = 'ACTIVE' AND del = 0", loginUser.AccountNo).Count(&active)
		ctrl.dbs[dbIdx].Table(tableName).Where("account_no = ? AND gmt_create >= ? AND del = 0", loginUser.AccountNo, today).Count(&todayCreated)

		result.TotalLinks += total
		result.ActiveLinks += active
		result.TodayCreated += todayCreated
	}

	response.JSON(c, response.BuildSuccessData(result))
}

// checkCodeExists checks if a short link code already exists in the database.
func (ctrl *ShortLinkController) checkCodeExists(code string) bool {
	dbPrefix, tableSuffix := sharding.RouteShortLink(code)
	dbIdx := sharding.GetDBIndexByPrefix(dbPrefix)
	tableName := sharding.GetTableName("short_link", tableSuffix)
	var count int64
	ctrl.dbs[dbIdx].Table(tableName).Where("code = ?", code).Count(&count)
	return count > 0
}

// acquireCodeLock acquires a distributed lock on a short link code via Redis Lua script.
// Returns true if lock acquired (1) or reentrant (2), false if locked by another user (0).
func (ctrl *ShortLinkController) acquireCodeLock(ctx context.Context, code string, accountNo int64) bool {
	if ctrl.rdb == nil {
		return true
	}
	script := redis.NewScript(`
		if redis.call('EXISTS',KEYS[1])==0 then
			redis.call('set',KEYS[1],ARGV[1]);
			redis.call('expire',KEYS[1],ARGV[2]);
			return 1;
		elseif redis.call('get',KEYS[1]) == ARGV[1] then
			return 2;
		else
			return 0;
		end
	`)
	result, err := script.Run(ctx, ctrl.rdb, []string{code},
		fmt.Sprintf("%d", accountNo), 100).Int()
	if err != nil {
		log.Printf("[Redis] acquire code lock error: %v", err)
		return false
	}
	return result == 1 || result == 2
}

// checkTrafficQuota checks and decrements the daily traffic quota via Redis Lua.
// Returns true if quota available, false if exhausted.
func (ctrl *ShortLinkController) checkTrafficQuota(ctx context.Context, accountNo int64) bool {
	if ctrl.rdb == nil {
		return true
	}
	script := redis.NewScript(`
		if redis.call('get',KEYS[1]) then
			return redis.call('decr',KEYS[1])
		else
			return 0
		end
	`)
	key := constant.FormatDayTotalTrafficKey(accountNo)
	result, err := script.Run(ctx, ctrl.rdb, []string{key}).Int()
	if err != nil {
		log.Printf("[Redis] check traffic quota error: %v", err)
		return false
	}
	return result >= 0
}
