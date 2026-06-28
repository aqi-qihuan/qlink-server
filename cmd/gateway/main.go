package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"github.com/aqi/qlink-server/internal/common/middleware"
	"github.com/aqi/qlink-server/internal/common/registry"
	"github.com/gin-gonic/gin"
)

// Shared transport with connection pooling
var transport = &http.Transport{
	MaxIdleConns:        500,
	MaxIdleConnsPerHost: 100,
	MaxConnsPerHost:     200,
	IdleConnTimeout:     90 * time.Second,
}

func main() {
	godotenv.Load()

	port := getEnv("PORT", "8888")
	linkAddr := getEnv("LINK_SERVICE", "http://localhost:8003")
	dataAddr := getEnv("DATA_SERVICE", "http://localhost:8002")
	accountAddr := getEnv("ACCOUNT_SERVICE", "http://localhost:8001")
	shopAddr := getEnv("SHOP_SERVICE", "http://localhost:8005")
	aiAddr := getEnv("AI_SERVICE", "http://localhost:8006")

	// Redis for distributed rate limiting
	redisHost := getEnv("REDIS_HOST", "192.168.192.21")
	redisPort := getEnv("REDIS_PORT", "6379")
	redisPwd := getEnv("REDIS_PWD", "aqi1015")
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", redisHost, redisPort),
		Password: redisPwd,
	})

	// Service Registry
	reg := registry.New(30 * time.Second)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CorsMiddleware())

	// Distributed rate limiter: 5000 req/s per IP, 10s sliding window
	// Falls back to local limiter if Redis is down
	r.Use(middleware.RedisRateLimiter(rdb, 5000, 10*time.Second))

	// Health check
	r.GET("/health", middleware.HealthHandler("gateway"))

	// Mount registry endpoints
	r.Any("/registry/*path", gin.WrapH(reg.HTTPHandler()))

	// Route 1: /* -> link-service (short link redirect)
	r.Any("/:shortLinkCode", reverseProxy(linkAddr))

	// Route 2-5: /xxx-server/** -> service
	r.Any("/link-server/*path", stripPrefixProxy("/link-server", linkAddr))
	r.Any("/data-server/*path", stripPrefixProxy("/data-server", dataAddr))
	r.Any("/account-server/*path", stripPrefixProxy("/account-server", accountAddr))
	r.Any("/shop-server/*path", stripPrefixProxy("/shop-server", shopAddr))
	r.Any("/ai-server/*path", stripPrefixProxy("/ai-server", aiAddr))

	// Route 6: /api/callback/* -> shop-service
	r.Any("/api/callback/*path", reverseProxy(shopAddr))

	log.Printf("Gateway starting on port %s (rate limit: 5000 req/s per IP)", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("gateway start failed: %v", err)
	}
}

func reverseProxy(target string) gin.HandlerFunc {
	remote, err := url.Parse(target)
	if err != nil {
		log.Fatalf("invalid upstream target: %s, error: %v", target, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(remote)
	proxy.Transport = transport
	return func(c *gin.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	}
}

func stripPrefixProxy(prefix string, target string) gin.HandlerFunc {
	remote, err := url.Parse(target)
	if err != nil {
		log.Fatalf("invalid upstream target: %s, error: %v", target, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(remote)
	proxy.Transport = transport
	return func(c *gin.Context) {
		originalPath := c.Request.URL.Path
		c.Request.URL.Path = strings.TrimPrefix(originalPath, prefix)
		if c.Request.URL.Path == "" {
			c.Request.URL.Path = "/"
		}
		proxy.ServeHTTP(c.Writer, c.Request)
		c.Request.URL.Path = originalPath
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
