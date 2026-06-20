package middleware

import (
	"net/http"
	"runtime"

	"github.com/gin-gonic/gin"
)

// BuildInfo is set at compile time via ldflags.
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// HealthHandler returns a simple liveness probe handler.
// Usage: r.GET("/health", middleware.HealthHandler("service-name"))
func HealthHandler(service string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"service":   service,
			"version":   Version,
			"buildTime": BuildTime,
			"gitCommit": GitCommit,
			"goVersion": runtime.Version(),
		})
	}
}
