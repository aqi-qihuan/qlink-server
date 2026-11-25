package controller

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/aqi/qlink-server/internal/common/constant"
	"github.com/aqi/qlink-server/internal/common/model"
	"github.com/aqi/qlink-server/internal/common/mq"
	"github.com/aqi/qlink-server/internal/common/util"
	"github.com/aqi/qlink-server/internal/link/service"
	"github.com/aqi/qlink-server/internal/link/sharding"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	shortLinkCacheTTL = 10 * time.Minute
	// negativeCacheTTL bounds the window in which a newly created link may
	// still be invisible while its MQ write is in flight. Keep it short so a
	// "create then share immediately" flow recovers in seconds, not minutes.
	negativeCacheTTL = 5 * time.Second
	// negativeCacheField marks a cache entry as "DB lookup missed" instead of
	// reusing del=1, which is the state of a genuinely deleted/locked link.
	// Mixing the two made a freshly created link answer 403 for a full minute.
	negativeCacheField = "nf"
)

type LinkApiController struct {
	dbs           []*gorm.DB // 3 sharded DBs: ds0, ds1, dsa
	rdb           *redis.Client
	kafka         *mq.KafkaProducer
	codeRegexp    *regexp.Regexp
	abTestService *service.ABTestService
}

func NewLinkApiController(dbs []*gorm.DB, kafka *mq.KafkaProducer, rdb *redis.Client) *LinkApiController {
	return &LinkApiController{
		dbs:        dbs,
		kafka:      kafka,
		rdb:        rdb,
		codeRegexp: regexp.MustCompile(`^[a-z0-9A-Z]+$`),
	}
}

// SetABTestService injects AB test service for Dispatch routing.
func (ctrl *LinkApiController) SetABTestService(svc *service.ABTestService) {
	ctrl.abTestService = svc
}

// Dispatch handles GET /{shortLinkCode} -> 302 redirect.
// This is the hot path for short link resolution.
// Cache strategy: Cache-Aside with Redis Hash (10min TTL).
func (ctrl *LinkApiController) Dispatch(c *gin.Context) {
	code := c.Param("shortLinkCode")
	if code == "" || !ctrl.codeRegexp.MatchString(code) {
		c.String(http.StatusBadRequest, "invalid short link code")
		return
	}
	// Reject codes that would route to a non-existent physical shard (e.g. a
	// crawler guessing "NotExistCode123"). Without this check GORM reports a
	// missing table and we leak a 500 instead of a clean 404.
	if !sharding.IsValidShortLinkCode(code) {
		c.String(http.StatusNotFound, "short link not found")
		return
	}

	// Step 1: 尝试从 Redis 缓存获取
	cacheKey := constant.FormatShortLinkCacheKey(code)
	if ctrl.rdb != nil {
		cached, err := ctrl.rdb.HGetAll(c, cacheKey).Result()
		if err == nil && len(cached) > 0 {
			// 缓存命中
			// 负缓存：仅表示"上次回源未找到"，随时可能被异步落库推翻，返回 404。
			if cached[negativeCacheField] == "1" {
				c.String(http.StatusNotFound, "short link not found")
				return
			}
			if cached["del"] == "1" || cached["state"] == "LOCK" {
				c.String(http.StatusForbidden, "short link is locked or deleted")
				return
			}
			if cached["state"] == "INACTIVE" {
				c.String(http.StatusGone, "short link is inactive")
				return
			}
			if cached["password"] != "" {
				c.Redirect(http.StatusFound, "/preview/"+code)
				return
			}
			ctrl.sendVisitLog(c, code, cached["account_no"])
			originalUrl := util.RemoveUrlPrefix(cached["url"])
			c.Redirect(http.StatusFound, originalUrl)
			return
		}
	}

	// Step 2: 缓存未命中，回源 MySQL
	dbPrefix, tableSuffix := sharding.RouteShortLink(code)
	dbIdx := sharding.GetDBIndexByPrefix(dbPrefix)
	tableName := sharding.GetTableName("short_link", tableSuffix)

	var shortLink struct {
		OriginalUrl string  `gorm:"column:original_url"`
		Expired     *string `gorm:"column:expired"`
		State       string  `gorm:"column:state"`
		Del         int     `gorm:"column:del"`
		Code        string  `gorm:"column:code"`
		Password    string  `gorm:"column:password"`
		AccountNo   int64   `gorm:"column:account_no"`
	}
	err := ctrl.dbs[dbIdx].Table(tableName).
		Where("code = ? AND del = 0", code).
		First(&shortLink).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Cache negative result (cache penetration protection) ONLY for
			// genuine "not found" — NOT for DB connection failures, which
			// would otherwise cause all requests to return 404 even after
			// the DB recovers.
			//
			// Use a dedicated nf field + a short TTL: this link may be in the
			// middle of an async MQ insert (the create API returns before the
			// DB write completes). Reusing del=1 here made such links answer
			// 403 until the cache expired, and the MQ consumer had no way to
			// tell a negative placeholder from a real deletion.
			//
			// Race note: if this lookup read the DB just before the MQ commit
			// and writes nf after the consumer's DEL, the marker can survive
			// for up to negativeCacheTTL. That window now yields a clean 404
			// (not 403) and self-heals in seconds, which is the intended
			// trade-off for keeping cache penetration protection.
			if ctrl.rdb != nil {
				ctrl.rdb.HSet(c, cacheKey, negativeCacheField, "1")
				ctrl.rdb.Expire(c, cacheKey, negativeCacheTTL)
			}
			c.String(http.StatusNotFound, "short link not found")
			return
		}
		// DB error: do NOT cache, return 500
		c.String(http.StatusInternalServerError, "internal error")
		return
	}

	// Step 3: 写入 Redis 缓存
	if ctrl.rdb != nil {
		ctx := context.Background()
		ctrl.rdb.HSet(ctx, cacheKey,
			"url", shortLink.OriginalUrl,
			"del", shortLink.Del,
			"state", shortLink.State,
			"password", shortLink.Password,
			"account_no", fmt.Sprintf("%d", shortLink.AccountNo),
		)
		ctrl.rdb.Expire(ctx, cacheKey, shortLinkCacheTTL)
	}

	if shortLink.Del == 1 || shortLink.State == "LOCK" {
		c.String(http.StatusForbidden, "short link is locked or deleted")
		return
	}
	if shortLink.State == "INACTIVE" {
		c.String(http.StatusGone, "short link is inactive")
		return
	}

	// Record visit log AFTER status checks pass — previously it was recorded
	// before, causing PV/UV inflation for locked/inactive links.
	ctrl.sendVisitLog(c, code, fmt.Sprintf("%d", shortLink.AccountNo))

	// Password check: redirect to preview page instead of target URL
	if shortLink.Password != "" {
		c.Redirect(http.StatusFound, "/preview/"+code)
		return
	}

	originalUrl := util.RemoveUrlPrefix(shortLink.OriginalUrl)

	// AB Test routing: if an active AB test exists, redirect to the selected variant
	if ctrl.abTestService != nil {
		ctx := context.Background()
		abURL, _ := ctrl.abTestService.GetVariantForDispatch(ctx, code, c.ClientIP(), c.GetHeader("User-Agent"))
		if abURL != "" {
			c.Redirect(http.StatusFound, util.RemoveUrlPrefix(abURL))
			return
		}
	}

	c.Redirect(http.StatusFound, originalUrl)
}

// sendVisitLog sends visit log to Kafka asynchronously.
// Matches Java's LogServiceImpl.recordShortLinkLog: ip, ts, event=SHORT_LINK_TYPE, bizId=code, data={user-agent, referer, accountNo}.
func (ctrl *LinkApiController) sendVisitLog(c *gin.Context, code string, accountNo string) {
	if ctrl.kafka == nil {
		return
	}
	// Extract all needed fields from gin.Context BEFORE starting the goroutine.
	// gin.Context is recycled via sync.Pool after the handler returns, so the
	// goroutine must NOT reference c. Use context.Background() instead.
	ip := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")
	referer := c.GetHeader("Referer")
	logRecord := model.LogRecord{
		IP:    ip,
		Ts:    util.GetCurrentTimestamp(),
		Event: "SHORT_LINK_TYPE",
		BizId: code,
		Data: map[string]interface{}{
			"user-agent": userAgent,
			"referer":    referer,
			"accountNo":  accountNo,
		},
	}
	go func() {
		ctx := context.Background()
		if err := ctrl.kafka.PublishJSON(ctx, code, logRecord); err != nil {
			log.Printf("[Kafka] publish visit log error: %v", err)
		}
	}()
}
