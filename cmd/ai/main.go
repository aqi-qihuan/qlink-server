package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"

	aiconfig "github.com/aqi/qlink-server/internal/ai/config"
	"github.com/aqi/qlink-server/internal/ai/handler"
	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/middleware"
	"github.com/gin-gonic/gin"
)

func main() {
	godotenv.Load()

	port := getEnv("PORT", "8006")

	cfg := aiconfig.DefaultConfig()
	aiHandler := handler.NewAIHandler(cfg)

	r := gin.Default()
	r.Use(middleware.CorsMiddleware())

	// Health check (Docker HEALTHCHECK)
	r.GET("/health", middleware.HealthHandler("ai"))

	api := r.Group("/api")
	{
		ai := api.Group("/ai/v1")
		ai.Use(interceptor.LoginInterceptor())
		{
			ai.POST("/recommend", aiHandler.Recommend)
			ai.POST("/analytics", aiHandler.Analytics)
			ai.POST("/check_safety", aiHandler.CheckSafety)
		}
	}

	log.Printf("AI service starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("AI service start failed: %v", err)
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
