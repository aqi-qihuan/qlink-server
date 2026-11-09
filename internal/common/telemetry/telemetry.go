// Package telemetry 提供 qlink-server 的最小可观测性基线：
// 标准 OpenTelemetry SDK + OTLP/HTTP 导出到现有 SkyWalking OAP，
// 以及每个服务独立的 Prometheus /metrics 注册表。
package telemetry

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// ShutdownFunc 用于进程退出前 flush span。
// 调用方应传入带超时的 context，避免 OAP 不可达时卡住退出。
type ShutdownFunc func(context.Context) error

var (
	metricsOnce       sync.Once
	metricsRegistry   *prometheus.Registry
	requestsTotal     *prometheus.CounterVec
	requestDuration   *prometheus.HistogramVec
	startupTracerOnce sync.Once
)

func ensureMetrics() {
	metricsOnce.Do(func() {
		metricsRegistry = prometheus.NewRegistry()
		metricsRegistry.MustRegister(
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)
		requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qlink_http_requests_total",
			Help: "Total number of HTTP requests processed by qlink services.",
		}, []string{"service", "method", "route", "status"})
		requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "qlink_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"service", "method", "route"})
		metricsRegistry.MustRegister(requestsTotal, requestDuration)
	})
}

// Init 初始化 OTel TracerProvider。
// 环境变量：
//   - OTEL_SDK_DISABLED=true 时只启用 /metrics，不上报 trace
//   - OTEL_EXPORTER_OTLP_ENDPOINT 或 OTEL_EXPORTER_OTLP_TRACES_ENDPOINT，默认 http://127.0.0.1:12800/v1/traces
//   - OTEL_TRACE_SAMPLE_RATIO，默认 1.0
//   - APP_ENV / BUILD_VERSION 写入 resource 属性
//
// 即使 OAP 暂时不可达，也不阻断业务服务启动；错误只记录日志。
func Init(ctx context.Context, serviceName string) (ShutdownFunc, error) {
	ensureMetrics()

	noop := func(context.Context) error { return nil }
	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") {
		log.Printf("[telemetry] OTEL disabled, metrics endpoint still enabled")
		return noop, nil
	}

	host, path, insecure, err := parseOTLPEndpoint()
	if err != nil {
		return noop, err
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(host),
		otlptracehttp.WithURLPath(path),
	}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return noop, fmt.Errorf("create OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", serviceName),
		attribute.String("service.namespace", "qlink"),
		attribute.String("deployment.environment", getenv("APP_ENV", "dev")),
		attribute.String("service.version", getenv("BUILD_VERSION", "dev")),
	))
	if err != nil {
		_ = exporter.Shutdown(ctx)
		return noop, fmt.Errorf("create OTel resource: %w", err)
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio()))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	log.Printf("[telemetry] OTLP traces -> %s%s (service=%s)", host, path, serviceName)
	return tp.Shutdown, nil
}

// Setup 供各 cmd/main.go 一行接入：初始化 OTel、发启动 span、返回 shutdown。
func Setup(serviceName string) ShutdownFunc {
	ctx := context.Background()
	shutdown, err := Init(ctx, serviceName)
	if err != nil {
		log.Printf("[telemetry] init failed, continue without tracing: %v", err)
		return func(context.Context) error { return nil }
	}
	EmitStartupSpan(ctx, serviceName)
	return shutdown
}

// ShutdownWithTimeout 配合 defer 使用，避免 OAP 不可达时阻塞退出。
func ShutdownWithTimeout(shutdown ShutdownFunc) {
	if shutdown == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		log.Printf("[telemetry] shutdown error: %v", err)
	}
}

// MetricsHTTPHandler 返回标准库 http.Handler，供非 gin 服务（streamer）使用。
func MetricsHTTPHandler() http.Handler {
	ensureMetrics()
	return promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{})
}

// MetricsHandler 返回 /metrics 的 gin handler。
// 每个进程使用独立 Registry，避免多次 MustRegister 冲突。
func MetricsHandler() gin.HandlerFunc {
	ensureMetrics()
	return gin.WrapH(MetricsHTTPHandler())
}

// HTTPMiddleware 生成入口 span 并记录 Prometheus 指标。
// /metrics 自身跳过，避免自观测噪声。
func HTTPMiddleware(serviceName string) gin.HandlerFunc {
	ensureMetrics()
	tracer := otel.Tracer("qlink/http")

	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		start := time.Now()
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}

		ctx, span := tracer.Start(c.Request.Context(),
			c.Request.Method+" "+route,
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
			oteltrace.WithAttributes(
				attribute.String("service.name", serviceName),
				attribute.String("http.method", c.Request.Method),
				attribute.String("http.route", route),
				attribute.String("http.target", c.Request.URL.Path),
				attribute.String("http.scheme", scheme(c.Request)),
			),
		)
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		status := c.Writer.Status()
		if status == 0 {
			status = http.StatusOK
		}
		span.SetAttributes(attribute.Int("http.status_code", status))
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, "server error")
			span.RecordError(fmt.Errorf("http status %d", status))
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()

		labels := prometheus.Labels{
			"service": serviceName,
			"method":  c.Request.Method,
			"route":   route,
			"status":  strconv.Itoa(status),
		}
		requestsTotal.With(labels).Inc()
		requestDuration.With(prometheus.Labels{
			"service": serviceName,
			"method":  c.Request.Method,
			"route":   route,
		}).Observe(time.Since(start).Seconds())
	}
}

// StartSpan 供非 HTTP 链路（Kafka / 定时任务 / 启动事件）手工创建 span。
func StartSpan(ctx context.Context, tracerName, spanName string, attrs ...attribute.KeyValue) (context.Context, func()) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, spanName, oteltrace.WithAttributes(attrs...))
	return ctx, func() { span.End() }
}

// EmitStartupSpan 发一条一次性启动 span，用于验证 OTLP 通道。
func EmitStartupSpan(ctx context.Context, serviceName string) {
	startupTracerOnce.Do(func() {
		_, end := StartSpan(ctx, "qlink/startup", serviceName+".startup",
			attribute.String("service.name", serviceName),
			attribute.String("event", "service.startup"),
		)
		end()
	})
}

func parseOTLPEndpoint() (host, path string, insecure bool, err error) {
	raw := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	if raw == "" {
		raw = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if raw == "" {
		raw = "http://127.0.0.1:12800/v1/traces"
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false, fmt.Errorf("parse OTLP endpoint: %w", err)
	}
	if u.Host == "" {
		u, err = url.Parse("http://" + raw)
		if err != nil {
			return "", "", false, fmt.Errorf("parse OTLP endpoint without scheme: %w", err)
		}
	}
	path = u.Path
	if path == "" || path == "/" {
		path = "/v1/traces"
	}
	return u.Host, path, u.Scheme != "https", nil
}

func sampleRatio() float64 {
	raw := os.Getenv("OTEL_TRACE_SAMPLE_RATIO")
	if raw == "" {
		return 1.0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 || v > 1 {
		log.Printf("[telemetry] invalid OTEL_TRACE_SAMPLE_RATIO=%q, fallback 1.0", raw)
		return 1.0
	}
	return v
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return p
	}
	return "http"
}
