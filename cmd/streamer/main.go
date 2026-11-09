package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"github.com/aqi/qlink-server/internal/common/telemetry"
	"github.com/aqi/qlink-server/internal/streamer"
)

const serviceName = "qlink-streamer"

func main() {
	godotenv.Load()

	shutdown := telemetry.Setup(serviceName)
	defer telemetry.ShutdownWithTimeout(shutdown)

	kafkaBrokers := getEnv("KAFKA_BROKERS", "localhost:9092")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	redisPwd := getEnv("REDIS_PWD", "")
	redisDB := 0
	clickhouseAddr := getEnv("CLICKHOUSE_ADDR", "localhost:8123")
	clickhouseUser := getEnv("CLICKHOUSE_USER", "qlink")
	clickhousePwd := getEnv("CLICKHOUSE_PWD", "")
	clickhouseDB := getEnv("CLICKHOUSE_DB", "qlink_analytics")
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

	// streamer 无业务 HTTP 端口，单独暴露 8007 供 Prometheus 抓取。
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", telemetry.MetricsHTTPHandler())
	metricsMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	metricsSrv := &http.Server{
		Addr:              ":" + getEnv("STREAMER_METRICS_PORT", "8007"),
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[WARN] streamer metrics server: %v", err)
		}
	}()

	// 每 30s 发一条心跳 span，让无 HTTP 入口的 streamer 在 OAP 中可见。
	heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
	defer cancelHeartbeat()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				_, end := telemetry.StartSpan(heartbeatCtx, "qlink/streamer", "streamer.heartbeat")
				end()
			}
		}
	}()

	log.Println("[Streamer] All 4 pipeline stages running")
	log.Println("[Streamer] ODS ??DWD ??DWM-Wide ??DWM-UV ??DWS ??ClickHouse")

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	signal.Stop(sigCh)
	log.Printf("[Streamer] Received signal %v, shutting down...", sig)

	pipeline.Stop()

	metricsCtx, cancelMetrics := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelMetrics()
	if err := metricsSrv.Shutdown(metricsCtx); err != nil {
		log.Printf("[WARN] streamer metrics shutdown: %v", err)
	}

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
