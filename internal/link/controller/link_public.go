package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/aqi/qlink-server/internal/common/constant"
	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/aqi/qlink-server/internal/link/request"
	"github.com/aqi/qlink-server/internal/link/sharding"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// LinkPublicController handles public (no-auth) link endpoints.
type LinkPublicController struct {
	dbs []*gorm.DB
	rdb *redis.Client
}

func NewLinkPublicController(dbs []*gorm.DB, rdb *redis.Client) *LinkPublicController {
	return &LinkPublicController{dbs: dbs, rdb: rdb}
}

// VerifyPassword handles POST /api/public/link/verify-password.
// Returns the original URL on success, error on wrong password.
func (ctrl *LinkPublicController) VerifyPassword(c *gin.Context) {
	var req request.PasswordVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSON(c, response.BuildError("code and password are required"))
		return
	}

	dbPrefix, tableSuffix := sharding.RouteShortLink(req.Code)
	dbIdx := sharding.GetDBIndexByPrefix(dbPrefix)
	tableName := sharding.GetTableName("short_link", tableSuffix)

	var link struct {
		OriginalUrl string `gorm:"column:original_url"`
		Password    string `gorm:"column:password"`
		State       string `gorm:"column:state"`
		Del         int    `gorm:"column:del"`
	}
	err := ctrl.dbs[dbIdx].Table(tableName).
		Select("original_url, password, state, del").
		Where("code = ? AND del = 0", req.Code).
		First(&link).Error
	if err != nil {
		response.JSON(c, response.BuildCodeAndMsg(1, "short link not found"))
		return
	}

	if link.State == "LOCK" {
		response.JSON(c, response.BuildCodeAndMsg(1, "short link is locked"))
		return
	}

	if link.Password == "" {
		response.JSON(c, response.BuildSuccessData(gin.H{"url": link.OriginalUrl}))
		return
	}

	// Verify bcrypt password
	if err := bcrypt.CompareHashAndPassword([]byte(link.Password), []byte(req.Password)); err != nil {
		response.JSON(c, response.BuildCodeAndMsg(1, "wrong password"))
		return
	}

	response.JSON(c, response.BuildSuccessData(gin.H{"url": link.OriginalUrl}))
}

// Preview handles GET /preview/:code.
// Returns link metadata for preview (title, original_url, domain, state, has_password).
func (ctrl *LinkPublicController) Preview(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "code is required"})
		return
	}

	// Try Redis cache first
	cacheKey := constant.FormatShortLinkCacheKey(code)
	if ctrl.rdb != nil {
		cached, err := ctrl.rdb.HGetAll(context.Background(), cacheKey).Result()
		if err == nil && len(cached) > 0 {
			if cached["del"] == "1" {
				c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "short link not found"})
				return
			}
			hasPwd := cached["password"] != ""
			c.JSON(http.StatusOK, gin.H{
				"code":         0,
				"short_code":   code,
				"original_url": cached["url"],
				"state":        cached["state"],
				"has_password": hasPwd,
			})
			return
		}
	}

	// Cache miss: query DB
	dbPrefix, tableSuffix := sharding.RouteShortLink(code)
	dbIdx := sharding.GetDBIndexByPrefix(dbPrefix)
	tableName := sharding.GetTableName("short_link", tableSuffix)

	var link struct {
		Title       string    `gorm:"column:title"`
		OriginalUrl string    `gorm:"column:original_url"`
		Domain      string    `gorm:"column:domain"`
		State       string    `gorm:"column:state"`
		Password    string    `gorm:"column:password"`
		Del         int       `gorm:"column:del"`
		GmtCreate   time.Time `gorm:"column:gmt_create"`
	}
	err := ctrl.dbs[dbIdx].Table(tableName).
		Select("title, original_url, domain, state, password, del, gmt_create").
		Where("code = ? AND del = 0", code).
		First(&link).Error
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "short link not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":         0,
		"short_code":   code,
		"title":        link.Title,
		"original_url": link.OriginalUrl,
		"domain":       link.Domain,
		"state":        link.State,
		"has_password": link.Password != "",
		"created_at":   link.GmtCreate.Format("2006-01-02 15:04:05"),
	})
}
