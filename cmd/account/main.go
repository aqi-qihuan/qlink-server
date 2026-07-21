package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/aqi/qlink-server/internal/account/config"
	"github.com/aqi/qlink-server/internal/account/controller"
	"github.com/aqi/qlink-server/internal/account/listener"
	"github.com/aqi/qlink-server/internal/account/service"
	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/middleware"
	"github.com/aqi/qlink-server/internal/common/mq"
	"github.com/aqi/qlink-server/internal/common/registry"
	"github.com/aqi/qlink-server/internal/common/scheduler"
	"github.com/aqi/qlink-server/internal/common/sms"
	"github.com/aqi/qlink-server/internal/common/storage"
	"github.com/aqi/qlink-server/internal/common/util"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	godotenv.Load()

	// Startup security checks
	if err := util.ValidateJWTSecret(); err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	port := getEnv("PORT", "8001")
	mysqlHost := getEnv("MYSQL_HOST", "localhost")
	mysqlPort := getEnv("MYSQL_PORT", "3306")
	mysqlUser := getEnv("MYSQL_USER", "root")
	mysqlPwd := getEnv("MYSQL_PWD", "")
	redisHost := getEnv("REDIS_HOST", "localhost")
	redisPort := getEnv("REDIS_PORT", "6379")
	redisPwd := getEnv("REDIS_PWD", "")
	rabbitURL := getEnv("RABBITMQ_URL", "")
	shopServiceURL := getEnv("SHOP_SERVICE", "http://localhost:8005")
	storageType := getEnv("STORAGE_TYPE", "local") // "local" or "minio"

	// Account DB
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/aqicloud_account?charset=utf8mb4&parseTime=True&loc=Local",
		mysqlUser, mysqlPwd, mysqlHost, mysqlPort)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("connect account db failed: %v", err)
	}

	// Traffic tables (traffic_0, traffic_1) are in the same aqicloud_account database
	// Sharding is done at the application layer by table name
	trafficDBs := []*gorm.DB{db, db}

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", redisHost, redisPort),
		Password: redisPwd,
	})

	// RabbitMQ
	rmq, err := mq.NewRabbitMQ(rabbitURL)
	if err != nil {
		log.Printf("rabbitmq connect failed: %v (MQ features disabled)", err)
	}

	// Setup MQ exchanges and queues
	if rmq != nil {
		config.SetupExchangesAndQueues(rmq)
	}

	// SMS provider
	smsProvider := getEnv("SMS_PROVIDER", "log") // "log", "alibaba", "tencent"
	smsProv := sms.NewProvider(smsProvider, map[string]string{
		"access_key":    getEnv("SMS_ACCESS_KEY", ""),
		"access_secret": getEnv("SMS_ACCESS_SECRET", ""),
		"sign_name":     getEnv("SMS_SIGN_NAME", "AqiCloud"),
		"app_id":        getEnv("SMS_APP_ID", ""),
		"secret_id":     getEnv("SMS_SECRET_ID", ""),
		"secret_key":    getEnv("SMS_SECRET_KEY", ""),
	})

	// OAuth (optional, enabled via OAUTH_GOOGLE_CLIENT_ID etc.)
	oauthCfg := config.LoadOAuthConfig()

	// Services
	notifySvc := service.NewNotifyService(rdb, smsProv)
	accountSvc := service.NewAccountService(db, rdb, rmq, notifySvc)
	trafficSvc := service.NewTrafficService(trafficDBs, rdb, rmq, shopServiceURL, db)
	apiTokenSvc := service.NewApiTokenService(db)

	// Start MQ listeners
	if rmq != nil {
		listener.StartTrafficListeners(rmq, trafficSvc)
		log.Println("Traffic MQ listeners started")
	}

	// Scheduler (replaces XXL-JOB)
	sched := scheduler.New()
	sched.Register(&scheduler.Task{
		Name:     "trafficExpiredHandler",
		Interval: 1 * time.Hour,
		Handler:  trafficSvc.DeleteExpireTraffic,
	})
	sched.Start()

	// Service Registry (replaces Nacos)
	reg := registry.New(30 * time.Second)
	reg.Register(&registry.ServiceInstance{
		ServiceName: "aqicloud-account",
		Host:        getEnv("HOST", "localhost"),
		Port:        port,
	})

	// File storage
	var store storage.Storage
	switch storageType {
	case "minio":
		minioEndpoint := getEnv("MINIO_ENDPOINT", "localhost:9000")
		minioBucket := getEnv("MINIO_BUCKET", "aqicloud-link")
		minioAccessKey := getEnv("MINIO_ACCESS_KEY", "")
		minioSecretKey := getEnv("MINIO_SECRET_KEY", "")
		minioPublicURL := getEnv("MINIO_PUBLIC_URL", "http://localhost:9000/aqicloud-link")
		useSSL := getEnv("MINIO_USE_SSL", "false") == "true"
		store = storage.NewMinIOStorage(minioEndpoint, minioBucket, minioAccessKey, minioSecretKey, useSSL, minioPublicURL)
		log.Printf("Using MinIO storage: %s/%s", minioEndpoint, minioBucket)
	default:
		localPath := getEnv("UPLOAD_PATH", "/data/uploads")
		localURL := getEnv("UPLOAD_URL", fmt.Sprintf("http://localhost:%s/uploads", port))
		store = storage.NewLocalStorage(localPath, localURL)
		log.Printf("Using local storage: %s", localPath)
	}

	// Controllers
	accountCtrl := controller.NewAccountController(accountSvc, store)
	trafficCtrl := controller.NewTrafficController(trafficSvc)
	apiTokenCtrl := controller.NewApiTokenController(apiTokenSvc)

	// Gin router
	r := gin.Default()

	// OAuth controller (only active when at least one provider is configured)
	if len(oauthCfg.Providers) > 0 {
		oauthSvc := service.NewOAuthService(db, rdb)
		oauthCtrl := controller.NewOAuthController(oauthSvc, oauthCfg)
		r.GET("/api/account/v1/oauth/providers", oauthCtrl.Providers)
		r.GET("/api/account/v1/oauth/:provider/login", oauthCtrl.Login)
		r.GET("/api/account/v1/oauth/:provider/callback", oauthCtrl.Callback)
		log.Println("OAuth routes registered")
	}
	r.Use(middleware.CorsMiddleware())

	// Health check (Docker HEALTHCHECK)
	r.GET("/health", middleware.HealthHandler("account"))

	// Serve uploaded files (local storage only)
	if storageType == "local" {
		localPath := getEnv("UPLOAD_PATH", "/data/uploads")
		r.Static("/uploads", localPath)
	}

	// Public endpoints (no login required)
	r.POST("/api/account/v1/register", accountCtrl.Register)
	r.POST("/api/account/v1/login", accountCtrl.Login)
	r.POST("/api/account/v1/upload", accountCtrl.Upload)
	r.GET("/api/notify/v1/captcha", accountCtrl.Captcha(notifySvc))
	r.POST("/api/notify/v1/send_code", accountCtrl.SendCode(notifySvc))
	// Backward-compatible aliases (Java legacy paths)
	r.GET("/api/account/v1/captcha", accountCtrl.Captcha(notifySvc))
	r.POST("/api/account/v1/send_code", accountCtrl.SendCode(notifySvc))
	r.POST("/api/traffic/v1/reduce", trafficCtrl.Reduce) // RPC internal, protected by rpc-token middleware

	// Protected endpoints (login required)
	auth := r.Group("")
	auth.Use(interceptor.LoginInterceptor())
	{
		auth.GET("/api/account/v1/detail", accountCtrl.Detail)
		auth.POST("/api/account/v1/update", accountCtrl.Update)
		auth.GET("/api/traffic/v1/page", trafficCtrl.Page)
		auth.GET("/api/traffic/v1/detail/:trafficId", trafficCtrl.Detail)
		auth.GET("/api/traffic/v1/claim_free", trafficCtrl.ClaimFree)

		// API Token management
		apiToken := auth.Group("/api/account/v1/api_token")
		{
			apiToken.POST("/create", apiTokenCtrl.Create)
			apiToken.POST("/list", apiTokenCtrl.List)
			apiToken.POST("/delete", apiTokenCtrl.Delete)
		}
	}

	log.Printf("Account service starting on port %s", port)
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
