package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"github.com/aqi/qlink-server/internal/streamer"
)

func main() {
	godotenv.Load()

	kafkaBrokers := getEnv("KAFKA_BROKERS", "192.168.192.21:9092")
	redisAddr := getEnv("REDIS_ADDR", "192.168.192.21:6379")
	redisPwd := getEnv("REDIS_PWD", "aqi1015")
	redisDB := 0
	clickhouseAddr := getEnv("CLICKHOUSE_ADDR", "192.168.192.21:8123")
	clickhouseUser := getEnv("CLICKHOUSE_USER", "default")
	clickhousePwd := getEnv("CLICKHOUSE_PWD", "aqi1015!")
	clickhouseDB := getEnv("CLICKHOUSE_DB", "default")
	amapKey := getEnv("AMAP_API_KEY", "")

	// Timezone for date formatting (matching Java's ZoneId.systemDefault)
	tzName := getEnv("TZ", "Asia/Shanghai")
	tz, err := time.LoadLocation(tzName)
	if err != nil {
		log.Printf("[WARN] timezone %s not found, using UTC", tzName)
		tz = time.UTC
	}
	streamer.SetTimezone(tz)

	// Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPwd,
		DB:       redisDB,
	})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("[FATAL] Redis connect failed: %v", err)
	}
	log.Println("[OK] Redis connected")

	// ClickHouse client
	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{clickhouseAddr},
		Auth: clickhouse.Auth{
			Database: clickhouseDB,
			Username: clickhouseUser,
			Password: clickhousePwd,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		Protocol:        clickhouse.HTTP,
		DialTimeout:     5 * time.Second,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Hour,
	})
	if err != nil {
		log.Fatalf("[FATAL] ClickHouse connect failed: %v", err)
	}
	if err := chConn.Ping(ctx); err != nil {
		log.Fatalf("[FATAL] ClickHouse ping failed: %v", err)
	}
	log.Println("[OK] ClickHouse connected")

	cfg := &streamer.Config{
		KafkaBrokers: kafkaBrokers,
		AmapAPIKey:   amapKey,
	}

	// Start all 4 pipeline stages
	pipeline := streamer.NewPipeline(cfg, rdb, chConn)
	pipeline.Start()

	log.Println("[Streamer] All 4 pipeline stages running")
	log.Println("[Streamer] ODS ??DWD ??DWM-Wide ??DWM-UV ??DWS ??ClickHouse")

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("[Streamer] Received signal %v, shutting down...", sig)

	pipeline.Stop()
	rdb.Close()
	chConn.Close()
	log.Println("[Streamer] Shutdown complete")
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
