## qlink-server Java → Go 四项功能移植方案

编写时间: 2026-07-01

---

### 现状评估（重要发现）

经过详细代码扫描，四个功能的实际状态远比预想的要好：

| 功能 | Java 版 (aqicloud) | Go 版 (qlink-server) 现状 | 差距 |
|---|---|---|---|
| **Kafka 访问日志** | KafkaTemplate 生产 → Flink 4 层消费 → ClickHouse | **已有 Producer**（`internal/common/mq/kafka.go`），302 跳转时已发 `ods_link_visit_topic` | 缺 Consumer 端（替代 Flink 管道） |
| **微信支付** | WeChat Pay SDK，V3 API | **已完整实现**（`internal/shop/component/pay_strategy.go`），V2 XML + MD5 签名，含下单/查询/关闭/退款 | 仅缺配置和联调 |
| **MinIO 文件存储** | MinIO SDK + Aliyun OSS 双存储 | **已完整实现**（`internal/common/storage/`），自研 S3 V4 签名，LocalStorage + MinIOStorage 双后端 | 仅缺配置 |
| **SkyWalking APM** | skywalking-java agent 注入 | **零实现**，无任何 tracing/metrics 代码 | 全新开发 |

结论：**微信支付和 MinIO 已就绪**，只需配置凭证即可上线。真正需要开发的是 Kafka Consumer 和 APM。

---

### 一、Kafka 访问日志 — Consumer 管道（核心工作量）

#### 1.1 当前状态

Go 版 link 服务已在 302 跳转时异步发送 Kafka 消息：

```
用户点击 /:code → link_api.go sendVisitLog() → go func() → kafka.PublishJSON("ods_link_visit_topic", code, LogRecord)
```

消息格式（与 Java 版完全兼容）：
```json
{
  "ip": "14.197.9.110",
  "ts": 1700000000000,
  "event": "SHORT_LINK_TYPE",
  "bizId": "abc123XYZ",
  "data": {
    "user-agent": "Mozilla/5.0 ...",
    "referer": "https://douyin.com",
    "accountNo": "123456789"
  }
}
```

依赖 `segmentio/kafka-go v0.4.51` 已在 go.mod。

#### 1.2 移植方案：Go Stream Consumer 替代 Flink

Java 版的 Flink 4 层管道（DWD→DWM×2→DWS）过于重量级。Go 版用单个 Stream Consumer 服务替代，逻辑等价但架构更轻：

```
[ods_link_visit_topic] → Go Streamer Consumer
  ├── 解析 LogRecord JSON
  ├── 设备指纹生成 (UDID)          ← 替代 DWD
  ├── 解析 User-Agent (设备信息)   ← 替代 DWM DeviceMapFunction
  ├── 异步 IP 地理定位查询          ← 替代 DWM AsyncLocationRequestFunction
  ├── 新老访客判定 (Redis SET)      ← 替代 DWM UniqueVisitorFilterFunction
  ├── 时间窗口聚合 (tumbling 10s)   ← 替代 DWS 窗口函数
  └── 批量写入 ClickHouse           ← 替代 DWS ClickHouseSink
```

#### 1.3 新增/修改文件清单

| 文件路径 | 操作 | 说明 |
|---|---|---|
| `cmd/streamer/main.go` | 修改 | 添加 Kafka Consumer 启动逻辑，当前 streamer 可能为空壳 |
| `internal/streamer/consumer.go` | **新增** | Kafka Consumer 主循环，`segmentio/kafka-go` Reader |
| `internal/streamer/processor.go` | **新增** | 消息处理管道：解析→设备指纹→地理定位→聚合 |
| `internal/streamer/device.go` | **新增** | UDID 生成（IP+UA+bizId 的 MD5），等价 Java `DeviceUtil.geneWebUniqueDeviceId` |
| `internal/streamer/geo.go` | **新增** | IP 地理定位，调用太平洋网络/高德 IP 定位 API |
| `internal/streamer/visitor.go` | **新增** | 新老访客判定，Redis `SISMEMBER` 检查 udid 是否已存在 |
| `internal/streamer/aggregator.go` | **新增** | 时间窗口聚合器，按 (code, province, city, referer, is_new, ip, browser, os, device) 分组 |
| `internal/streamer/sink.go` | **新增** | ClickHouse 批量写入（INSERT 或 HTTP POST） |
| `internal/streamer/config.go` | **新增** | Kafka brokers、ClickHouse addr、Redis addr 等配置 |

#### 1.4 关键设计决策

**为什么不用 Flink？** Go 生态没有成熟的 Flink 替代品，且对于当前量级（单机部署），Go goroutine + channel 的轻量流处理完全够用。日访问量达到千万级时再考虑引入 Flink Go connector。

**时间窗口聚合策略：**
- Java Flink 用 `TumblingEventTimeWindows` + `ProcessWindowFunction`
- Go 版用 ticker（10 秒）+ sync.Map 缓冲，定期 flush 到 ClickHouse
- ClickHouse 的 ReplacingMergeTree 本身支持去重，降低精确聚合的压力

**设备指纹（UDID）生成：**
```go
func generateUDID(ip, userAgent, bizId string) string {
    raw := ip + "|" + userAgent + "|" + bizId
    return fmt.Sprintf("%x", md5.Sum([]byte(raw)))
}
```

**新老访客判定：**
- Redis SET `visit:uv:{date}` 存储当日所有 udid
- `SISMEMBER` 检查 → 新访客则 `SADD` + `is_new=1`
- 每日过期或定时清理

#### 1.5 预估工作量

| 任务 | 预估时间 |
|---|---|
| Kafka Consumer 主循环 + 消息解析 | 0.5 天 |
| 设备指纹 + UA 解析 | 0.5 天 |
| IP 地理定位集成 | 0.5 天 |
| 新老访客判定 | 0.5 天 |
| 窗口聚合 + ClickHouse 批量写入 | 1 天 |
| 联调测试 | 1 天 |
| **合计** | **4 天** |

---

### 二、微信支付（已实现，仅需配置联调）

#### 2.1 当前状态

Go 版 shop 服务已完整实现微信支付 V2 策略：

```
internal/shop/component/pay_strategy.go
  ├── PayStrategy 接口 (UnifiedOrder / Refund / QueryPayStatus / CloseOrder)
  ├── AlipayStrategy ✅ (RSA2 签名, sandbox 支持)
  └── WechatPayStrategy ✅ (V2 XML, MD5 签名, sandbox/production)

internal/shop/component/pay_crypto.go
  ├── RSA sign/verify (Alipay)
  ├── AES-256-GCM decrypt (V3 预留)
  └── WeChat V2 MD5 sign

internal/shop/component/pay_config.go
  ├── WECHAT_APP_ID / WECHAT_MCH_ID / WECHAT_API_KEY
  ├── WECHAT_NOTIFY_URL / WECHAT_PAY_ENV
  └── WechatEnabled() 自动检测
```

支付流程已打通：
```
POST /api/order/v1/confirm
  → order_service.Confirm()
    → payFactory.GetStrategy("WECHAT_PAY").UnifiedOrder()
    → 返回 {code_url, out_trade_no}
  → 前端展示微信支付二维码

POST /api/callback/order/v1/wechat
  → callback.WechatCallback()
    → XML 解析 + MD5 签名验证
    → processOrderCallbackMsg()
    → RabbitMQ → 流量包发放
```

#### 2.2 需要做的事

| 任务 | 说明 |
|---|---|
| 申请微信支付商户号 | 获取 MCH_ID 和 API_KEY |
| 配置环境变量 | `WECHAT_APP_ID`, `WECHAT_MCH_ID`, `WECHAT_API_KEY`, `WECHAT_NOTIFY_URL` |
| 配置回调地址 | 微信支付后台设置 `https://yourdomain/api/callback/order/v1/wechat` |
| 联调测试 | 先用 sandbox 环境测试下单→支付→回调全流程 |
| **可选：升级 V3 API** | 当前用 V2 XML，V3 改为 JSON + RSA，更现代但非必须 |

#### 2.3 预估工作量

| 任务 | 预估时间 |
|---|---|
| 商户号申请 + 环境配置 | 1-3 天（等审批） |
| Sandbox 联调 | 0.5 天 |
| 生产环境上线 | 0.5 天 |
| **合计** | **2-4 天**（含等审批） |

---

### 三、MinIO 文件存储（已实现，仅需配置）

#### 3.1 当前状态

Go 版已自研实现完整的 S3 V4 签名 MinIO 客户端（零外部 SDK 依赖）：

```
internal/common/storage/storage.go
  ├── Storage 接口 (Upload / Delete / GetURL)
  ├── LocalStorage ✅ (本地文件, 用于开发)
  └── MinIOStorage ✅ (S3 V4 签名, PUT/DELETE/GET)

internal/common/storage/s3sign.go
  └── AWS4-HMAC-SHA256 完整实现 (canonical request, string-to-sign, signing key)
```

Account 服务的头像上传已对接 Storage 接口：
```
cmd/account/main.go
  → STORAGE_TYPE=minio 时自动创建 MinIOStorage
  → 注入 AccountController

internal/account/controller/account.go
  → Upload() → storage.Upload(objectKey, reader, contentType)
  → 返回 {url, filename, size}
```

#### 3.2 需要做的事

| 任务 | 说明 |
|---|---|
| 部署 MinIO | Docker 一键启动：`docker run -p 9000:9000 minio/minio server /data` |
| 配置环境变量 | `STORAGE_TYPE=minio`, `MINIO_ENDPOINT`, `MINIO_BUCKET`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_PUBLIC_URL` |
| 创建 Bucket | MinIO Console 或 mc 命令创建 `aqicloud-link` bucket |
| 验证上传 | 调 `/api/account/v1/upload` 测试头像上传→获取 URL |

#### 3.3 预估工作量

| 任务 | 预估时间 |
|---|---|
| MinIO 部署 + 配置 | 0.5 天 |
| 联调验证 | 0.5 天 |
| **合计** | **1 天** |

---

### 四、SkyWalking APM（全新开发）

#### 4.1 当前状态

Go 版零 APM 基础设施。go.mod 中仅有 ClickHouse 驱动间接引入的 `go.opentelemetry.io/otel` 和 `otel/trace`，未主动使用。

middleware 目录仅有：CORS、健康检查、限流、RPC Token 鉴权。无 tracing、metrics、structured logging。

#### 4.2 方案选型

| 方案 | 优点 | 缺点 | 推荐 |
|---|---|---|---|
| **skywalking-go** | 与 Java 版共用 OAP 后端，统一看板 | Go agent 生态较新，自动埋点覆盖有限 | ⭐⭐⭐ |
| **OpenTelemetry + Jaeger** | CNCF 标准，Go 生态成熟 | 需要额外部署 Jaeger，与 Java 版看板分离 | ⭐⭐ |
| **OpenTelemetry + SkyWalking OAP** | OTel 标准 + 统一看板 | 配置稍复杂 | ⭐⭐⭐⭐ |

**推荐方案：OpenTelemetry SDK + SkyWalking OAP 后端**

理由：OTel 是 Go 生态的事实标准，instrumentation 库最全（Gin/GORM/Redis/Kafka 都有官方 adapter），同时 SkyWalking OAP 原生支持 OTLP 协议，可以复用 Java 版的 OAP 后端。

#### 4.3 架构设计

```
Gin Middleware (trace)          ─┐
GORM Callback (db spans)       ─┤
Redis Hook (cache spans)       ─┼─→ OTLP gRPC → SkyWalking OAP → UI
Kafka Hook (mq spans)         ─┤
HTTP Client (outgoing spans)   ─┘
```

#### 4.4 新增/修改文件清单

| 文件路径 | 操作 | 说明 |
|---|---|---|
| `internal/common/tracing/otel.go` | **新增** | OTel SDK 初始化：TracerProvider + Resource + OTLP Exporter |
| `internal/common/middleware/tracing.go` | **新增** | Gin middleware：从 request 提取 trace context，创建 span，记录 HTTP status/duration |
| `internal/common/tracing/gorm.go` | **新增** | GORM callback plugin：自动为 DB 操作创建 span |
| `internal/common/tracing/redis.go` | **新增** | go-redis hook：自动为 Redis 命令创建 span |
| `internal/common/tracing/kafka.go` | **新增** | kafka-go producer/consumer span 注入 |
| `internal/common/config/tracing.go` | **新增** | Tracing 配置：OTEL_ENDPOINT, OTEL_SERVICE_NAME, OTEL_SAMPLE_RATE |
| `cmd/account/main.go` | 修改 | 注入 tracing middleware + GORM/Redis hooks |
| `cmd/link/main.go` | 修改 | 同上 |
| `cmd/data/main.go` | 修改 | 同上 |
| `cmd/shop/main.go` | 修改 | 同上 |
| `cmd/gateway/main.go` | 修改 | 注入 tracing middleware，传播 trace header |
| `docker-compose.yml` | 修改 | 添加 SkyWalking OAP + UI 容器 |

#### 4.5 核心代码结构

**tracing/otel.go — SDK 初始化：**
```go
func InitTracing(serviceName, otlpEndpoint string, sampleRate float64) func() {
    exporter, _ := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(otlpEndpoint))
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithSampler(sdktrace.TraceIDRatioBased(sampleRate)),
        sdktrace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceNameKey.String(serviceName),
        )),
    )
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{}, propagation.Baggage{},
    ))
    return func() { tp.Shutdown(ctx) }
}
```

**middleware/tracing.go — Gin 中间件：**
```go
func TracingMiddleware() gin.HandlerFunc {
    tracer := otel.Tracer("gin")
    return func(c *gin.Context) {
        ctx, span := tracer.Start(c.Request.Context(), c.FullPath(),
            trace.WithSpanKind(trace.SpanKindServer),
            trace.WithAttributes(
                semconv.HTTPMethodKey.String(c.Request.Method),
                semconv.HTTPTargetKey.String(c.Request.URL.Path),
            ),
        )
        defer span.End()
        c.Request = c.Request.WithContext(ctx)
        c.Next()
        span.SetAttributes(semconv.HTTPStatusCodeKey.Int(c.Writer.Status()))
    }
}
```

#### 4.6 docker-compose 新增服务

```yaml
skywalking-oap:
  image: apache/skywalking-oap-server:10.1.0
  ports:
    - "11800:11800"  # gRPC (OTLP)
    - "12800:12800"  # REST
  environment:
    SW_STORAGE: elasticsearch
    SW_STORAGE_ES_CLUSTER_NODES: es:9200

skywalking-ui:
  image: apache/skywalking-ui:10.1.0
  ports:
    - "8868:8080"
  environment:
    SW_OAP_ADDRESS: http://skywalking-oap:12800
```

#### 4.7 新增依赖

```
go.opentelemetry.io/otel
go.opentelemetry.io/otel/sdk
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc
go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin
go.opentelemetry.io/contrib/instrumentation/gorm.io/gorm/otelgorm
go.opentelemetry.io/contrib/instrumentation/github.com/redis/go-redis/otelredis
```

#### 4.8 预估工作量

| 任务 | 预估时间 |
|---|---|
| OTel SDK 初始化 + Gin Middleware | 0.5 天 |
| GORM + Redis + Kafka Hooks | 1 天 |
| 各服务 main.go 注入 + 配置 | 0.5 天 |
| docker-compose SkyWalking 部署 | 0.5 天 |
| 联调验证 + 看板配置 | 1 天 |
| **合计** | **3.5 天** |

---

### 五、总体排期与优先级

| 优先级 | 功能 | 实际工作 | 预估 | 依赖 |
|---|---|---|---|---|
| **P0** | MinIO 文件存储 | 部署 + 配置 | **1 天** | 无 |
| **P0** | 微信支付 | 商户号 + 配置 + 联调 | **2-4 天** | 商户号审批 |
| **P1** | Kafka Consumer | 全新开发 | **4 天** | Kafka 集群 |
| **P2** | SkyWalking APM | 全新开发 | **3.5 天** | OAP 部署 |

**总工作量：10.5-12.5 天**

推荐顺序：MinIO（立即可用）→ 微信支付（等审批期间并行开发）→ Kafka Consumer → APM

---

### 六、依赖与环境

| 中间件 | 本地 Docker | 远程服务器 | 说明 |
|---|---|---|---|
| Kafka | ❌ 未部署 | 192.168.192.21:9092 | Consumer 开发需要本地或远程 Kafka |
| MinIO | ❌ 未部署 | 192.168.192.21:9000 | 需要 `docker run` 启动本地实例 |
| SkyWalking OAP | ❌ 未部署 | — | 需要 docker-compose 新增 |
| 微信支付 Sandbox | — | — | 需要商户号才能测试 |
