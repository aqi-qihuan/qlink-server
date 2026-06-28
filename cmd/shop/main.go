package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/aqi/qlink-server/internal/common/interceptor"
	"github.com/aqi/qlink-server/internal/common/middleware"
	"github.com/aqi/qlink-server/internal/common/mq"
	"github.com/aqi/qlink-server/internal/common/registry"
	"github.com/aqi/qlink-server/internal/common/util"
	"github.com/aqi/qlink-server/internal/shop/component"
	"github.com/aqi/qlink-server/internal/shop/config"
	"github.com/aqi/qlink-server/internal/shop/controller"
	"github.com/aqi/qlink-server/internal/shop/listener"
	"github.com/aqi/qlink-server/internal/shop/service"
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

	port := getEnv("PORT", "8005")
	mysqlHost := getEnv("MYSQL_HOST", "192.168.192.21")
	mysqlPort := getEnv("MYSQL_PORT", "3307")
	mysqlUser := getEnv("MYSQL_USER", "root")
	mysqlPwd := getEnv("MYSQL_PWD", "aqi1015!")
	redisHost := getEnv("REDIS_HOST", "192.168.192.21")
	redisPort := getEnv("REDIS_PORT", "6379")
	redisPwd := getEnv("REDIS_PWD", "aqi1015")
	rabbitURL := getEnv("RABBITMQ_URL", "amqp://admin:password@192.168.192.21:5672/")

	// Shop DB
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/aqicloud_shop?charset=utf8mb4&parseTime=True&loc=Local",
		mysqlUser, mysqlPwd, mysqlHost, mysqlPort)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("connect shop db failed: %v", err)
	}

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

	// Setup MQ
	if rmq != nil {
		config.SetupExchangesAndQueues(rmq)
	}

	// Payment strategies
	payCfg := component.PayConfigFromEnv()
	payFactory := component.NewPayFactory()
	payFactory.Register("ALI_PAY", component.NewAliPayStrategy(&payCfg))
	payFactory.Register("WECHAT_PAY", component.NewWechatPayStrategy(&payCfg))
	if payCfg.AliEnabled() {
		log.Println("Alipay sandbox enabled")
	} else {
		log.Println("Alipay sandbox not configured, using mock mode")
	}
	if payCfg.WechatEnabled() {
		log.Println("WeChat Pay sandbox enabled")
	} else {
		log.Println("WeChat Pay sandbox not configured, using mock mode")
	}

	// Services
	productSvc := service.NewProductService(db)
	orderSvc := service.NewOrderService(db, rdb, rmq, payFactory)

	// Start MQ listeners
	if rmq != nil {
		listener.StartOrderListeners(rmq, orderSvc)
		log.Println("Order MQ listeners started")
	}

	// Controllers
	productCtrl := controller.NewProductController(productSvc)
	orderCtrl := controller.NewOrderController(orderSvc)
	callbackCtrl := controller.NewCallbackController(rdb, rmq, &payCfg, db)

	// Gin router
	r := gin.Default()
	r.Use(middleware.CorsMiddleware())

	// Health check (Docker HEALTHCHECK)
	r.GET("/health", middleware.HealthHandler("shop"))

	// Public endpoints
	r.GET("/api/product/v1/list", productCtrl.List)
	r.GET("/api/product/v1/detail/:product_id", productCtrl.Detail)
	r.Any("/api/callback/order/v1/wechat", callbackCtrl.WechatCallback)
	r.Any("/api/callback/order/v1/alipay", callbackCtrl.AlipayCallback)

	// Protected endpoints
	auth := r.Group("")
	auth.Use(interceptor.LoginInterceptor())
	{
		auth.GET("/api/order/v1/get_token", orderCtrl.GetToken)
		auth.POST("/api/order/v1/page", orderCtrl.Page)
		auth.GET("/api/order/v1/query_state", orderCtrl.QueryState)
		auth.POST("/api/order/v1/confirm", orderCtrl.Confirm)
	}

	// Service Registry (replaces Nacos)
	reg := registry.New(30 * time.Second)
	reg.Register(&registry.ServiceInstance{
		ServiceName: "aqicloud-shop",
		Host:        getEnv("HOST", "localhost"),
		Port:        port,
	})

	log.Printf("Shop service starting on port %s", port)
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
