package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/joho/godotenv"

	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/middleware"
	"github.com/aqi/qlink-server/internal/common/registry"
	"github.com/aqi/qlink-server/internal/data/controller"
	"github.com/aqi/qlink-server/internal/data/service"
	"github.com/gin-gonic/gin"
)

func main() {
	godotenv.Load()

	port := getEnv("PORT", "8002")
	chAddr := getEnv("CLICKHOUSE_ADDR", "192.168.100.21:8123")
	chUser := getEnv("CLICKHOUSE_USER", "default")
	chPwd := getEnv("CLICKHOUSE_PWD", "aqi1015!")
	chDB := getEnv("CLICKHOUSE_DB", "default")

	// ClickHouse native API with HTTP protocol (port 8123) ??native protocol on 9000 has handshake timeout with CH 26.x
	log.Printf("Connecting to ClickHouse: %s/%s", chAddr, chDB)
	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddr},
		Auth: clickhouse.Auth{
			Database: chDB,
			Username: chUser,
			Password: chPwd,
		},
		Protocol:        clickhouse.HTTP,
		DialTimeout:     10 * time.Second,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Hour,
	})
	if err != nil {
		log.Fatalf("connect clickhouse failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := chConn.Ping(ctx); err != nil {
		log.Fatalf("ping clickhouse failed: %v", err)
	}
	log.Println("ClickHouse connected")

	// Service + Controller
	statsSvc := service.NewVisitStatsService(chConn)
	statsCtrl := controller.NewVisitStatsController(statsSvc)

	// Gin router
	r := gin.Default()
	r.Use(middleware.CorsMiddleware())

	// Health check (Docker HEALTHCHECK)
	r.GET("/health", middleware.HealthHandler("data"))

	// Protected endpoints
	api := r.Group("/api")
	api.Use(interceptor.LoginInterceptor())
	{
		stats := api.Group("/visit_stats/v1")
		{
			stats.Any("/page_record", statsCtrl.PageRecord)
			stats.Any("/region_day", statsCtrl.RegionDay)
			stats.Any("/trend", statsCtrl.Trend)
			stats.Any("/frequent_ip", statsCtrl.FrequentIP)
			stats.Any("/frequent_referer", statsCtrl.FrequentReferer)
			stats.Any("/device_info", statsCtrl.DeviceInfo)
			stats.Any("/dashboard", statsCtrl.Dashboard)
			stats.Any("/analysis", statsCtrl.Analysis)
			stats.Any("/export", statsCtrl.ExportCSV)
			stats.Any("/geo", statsCtrl.Geo)
		}
	}

	// Service Registry (replaces Nacos)
	reg := registry.New(30 * time.Second)
	reg.Register(&registry.ServiceInstance{
		ServiceName: "aqicloud-data",
		Host:        getEnv("HOST", "localhost"),
		Port:        port,
	})

	log.Printf("Data service starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("server start failed: %v", err)
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
