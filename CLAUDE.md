# Qlink Server

Go rewrite of the Java short-link microservice platform. 8 services (6 HTTP + 1 streaming ETL + 1 CLI tool), 60+ API endpoints, full middleware stack.

## Build

```bash
# Build all services
go build ./cmd/...

# Build individual services
go build ./cmd/gateway/      # :8888 - API gateway
go build ./cmd/account/      # :8001 - User auth, OAuth SSO, traffic, API tokens
go build ./cmd/data/         # :8002 - ClickHouse visit statistics
go build ./cmd/link/         # :8003 - Short link CRUD, redirect, AB testing
go build ./cmd/shop/         # :8005 - Products, orders, Alipay/WeChat payments
go build ./cmd/ai/           # :8006 - AI features (recommend, analytics, safety)
go build ./cmd/streamer/     # Kafka-to-ClickHouse ETL pipeline (no HTTP)
go build ./cmd/initschema/   # One-shot DB schema initializer (CLI)

# Run
go run ./cmd/link/main.go

# Docker (all services + middleware)
docker-compose up -d
```

## Architecture

```
cmd/
  gateway/main.go          # Reverse proxy, CORS, rate limiting (1000 req/s per IP)
  account/main.go          # User auth, OAuth SSO (Google/GitHub/WeChat), traffic quotas, API tokens, SMS
  data/main.go             # ClickHouse visit statistics queries (10 dashboard endpoints)
  link/main.go             # Short link CRUD, 302 redirect (hot path), AB testing, batch ops, domains
  shop/main.go             # Products, orders, Alipay/WeChat payment callbacks
  ai/main.go               # AI agents: URL recommendation, NL-to-SQL analytics, URL safety check
  streamer/main.go         # Headless Kafka consumer -> 4-stage pipeline -> ClickHouse batch insert
  initschema/main.go       # One-shot CLI: executes sql/*.sql DDL files against MySQL

internal/
  common/                  # Shared code across all services
    alert/                 # Webhook alert notifications
    config/                # MySQL, Redis connection factories
    constant/              # Redis key patterns
    enums/                 # 30+ business error codes, state enums
    interceptor/           # JWT login middleware (gin.Context-based)
    middleware/             # CORS, rate limiter, RPC token
    model/                 # EventMessage, LoginUser, LogRecord
    mq/                    # RabbitMQ (amqp091-go) + Kafka (kafka-go) wrappers
    registry/              # Service registry (replaces Nacos)
    response/              # JsonData{Code, Data, Msg} response format
    scheduler/             # Task scheduler (replaces XXL-JOB)
    sms/                   # SMS provider abstraction
    storage/               # File storage (local + MinIO/S3)
    util/                  # JWT, MurmurHash3, Base62, MD5, snowflake ID, captcha
  link/
    component/             # Short link code generation (MurmurHash3 + Base62)
    config/                # RabbitMQ exchange/queue setup
    controller/            # ShortLink, LinkGroup, Domain, LinkApi, ABTesting, Batch, OperationLog, P3 controllers
    listener/              # 7 MQ consumers (add/del/update x link/mapping + error)
    model/                 # ShortLinkDO, GroupCodeMappingDO, LinkGroupDO, DomainDO, ABTestDO, AbuseReportDO, BrandConfigDO
    request/               # Request DTOs
    service/               # ShortLinkService (MQ handler with collision retry)
    sharding/              # Application-layer DB/table routing
    vo/                    # View objects
  account/                 # Account, Traffic, ApiToken, OAuth controllers + services
  shop/                    # Product, Order, Callback controllers + services
  data/                    # ClickHouse visit stats service (10 query endpoints)
  ai/                      # AI agents with OpenAI-compatible LLM client (default: Ollama qwen3.5:9b)
  streamer/                # 4-stage Kafka ETL pipeline: DWD -> DWM-Wide + DWM-UV -> DWS
```

## API Endpoints

### Account Service (:8001)

- `POST /api/account/v1/register` - User registration
- `POST /api/account/v1/login` - User login
- `POST /api/account/v1/upload` - File upload
- `GET /api/notify/v1/captcha` - Captcha image
- `POST /api/notify/v1/send_code` - Send SMS verification code
- `GET /api/account/v1/detail` - Account details (auth)
- `POST /api/account/v1/update` - Update account (auth)
- `GET /api/account/v1/oauth/providers` - List enabled OAuth providers
- `GET /api/account/v1/oauth/:provider/login` - Initiate OAuth login flow
- `GET /api/account/v1/oauth/:provider/callback` - OAuth provider callback
- `POST /api/account/v1/api_token/create` - Create API token (auth)
- `POST /api/account/v1/api_token/list` - List API tokens (auth)
- `POST /api/account/v1/api_token/delete` - Delete API token (auth)

### Traffic Service (:8001)

- `POST /api/traffic/v1/reduce` - Reduce traffic quota (RPC token)
- `GET /api/traffic/v1/page` - Paginate traffic records (auth)
- `GET /api/traffic/v1/detail/:trafficId` - Traffic detail (auth)
- `GET /api/traffic/v1/claim_free` - Claim free traffic tier (auth)

### Link Service (:8003)

- `GET /:shortLinkCode` - **Hot path**: 302 redirect (Redis cache-aside, AB test routing, password protection)
- `GET /preview/:code` - Preview page metadata
- `POST /api/public/link/verify-password` - Verify password for protected links
- `POST /api/link/v1/add` - Create short link (auth)
- `POST /api/link/v1/page` - Paginate short links (auth)
- `POST /api/link/v1/detail` - Short link detail (auth)
- `POST /api/link/v1/del` - Delete short link (auth)
- `POST /api/link/v1/update` - Update short link (auth)
- `POST /api/link/v1/status` - Change status (auth)
- `POST /api/link/v1/summary` - Summary/stats (auth)
- `POST /api/link/v1/batch_delete` - Batch delete (auth)
- `POST /api/link/v1/batch_status` - Batch change status (auth)

### AB Testing (:8003)

- `POST /api/ab_test/v1/add` - Create experiment (auth)
- `POST /api/ab_test/v1/page` - List experiments (auth)
- `POST /api/ab_test/v1/detail` - Experiment detail (auth)
- `POST /api/ab_test/v1/update` - Update experiment (auth)
- `POST /api/ab_test/v1/start` - Start experiment (auth)
- `POST /api/ab_test/v1/stop` - Stop experiment (auth)
- `POST /api/ab_test/v1/del` - Delete experiment (auth)
- `POST /api/ab_test/v1/statistics` - Experiment statistics (auth)

### Domain / Group / Logs / P3 (:8003)

- `GET|POST /api/domain/v1/*` - Domain CRUD + status (auth)
- `GET|POST|DELETE|PUT /api/group/v1/*` - Link group CRUD (auth)
- `POST /api/operation_log/v1/page` - Paginate operation logs (auth)
- `GET /api/operation_log/v1/action_types` - List action types (auth)
- `POST /api/link/v1/url_safe_check` - URL safety check (auth)
- `POST /api/abuse_report/v1/create` - Create abuse report (auth)
- `POST /api/abuse_report/v1/list` - List abuse reports (auth)
- `POST|GET|DELETE /api/brand_config/v1/*` - Brand config CRUD (auth)
- `GET /api/public/brand_config/:account_no` - Public brand config

### Data Service (:8002)

- `ANY /api/visit_stats/v1/page_record` - Paginated visit records (auth)
- `ANY /api/visit_stats/v1/region_day` - Regional daily stats (auth)
- `ANY /api/visit_stats/v1/trend` - Visit trend (auth)
- `ANY /api/visit_stats/v1/frequent_ip` - Top visitor IPs (auth)
- `ANY /api/visit_stats/v1/frequent_referer` - Top referrers (auth)
- `ANY /api/visit_stats/v1/device_info` - Device/OS breakdown (auth)
- `ANY /api/visit_stats/v1/dashboard` - Dashboard summary (auth)
- `ANY /api/visit_stats/v1/analysis` - Detailed analysis (auth)
- `ANY /api/visit_stats/v1/export` - Export CSV (auth)
- `ANY /api/visit_stats/v1/geo` - Geographic distribution (auth)

### Shop Service (:8005)

- `GET /api/product/v1/list` - List products
- `GET /api/product/v1/detail/:product_id` - Product detail
- `ANY /api/callback/order/v1/wechat` - WeChat Pay callback
- `ANY /api/callback/order/v1/alipay` - Alipay callback
- `GET /api/order/v1/get_token` - Get payment token (auth)
- `POST /api/order/v1/page` - Paginate orders (auth)
- `GET /api/order/v1/query_state` - Query order state (auth)
- `POST /api/order/v1/confirm` - Confirm order (auth)

### AI Service (:8006)

- `POST /api/ai/v1/recommend` - URL recommendation (title, group, tags) (auth)
- `POST /api/ai/v1/analytics` - Natural language to ClickHouse SQL (auth)
- `POST /api/ai/v1/check_safety` - URL safety check (phishing/malware/scam) (auth)

### Gateway (:8888)

Reverse proxy routing by path prefix:
- `/:shortLinkCode` -> link-service (302 redirect)
- `/link-server/*` -> link-service
- `/data-server/*` -> data-service
- `/account-server/*` -> account-service
- `/shop-server/*` -> shop-service
- `/ai-server/*` -> ai-service
- `/api/callback/*` -> shop-service (payment callbacks)
- `/registry/*` -> service registry

## Key Compatibility Points

- **MurmurHash3**: Guava-compatible UTF-16 LE encoding (`internal/common/util/hash.go`)
- **Java String.hashCode()**: Used for sharding routing (`internal/common/util/hash.go`)
- **JWT**: HS256, secret via `JWT_SECRET` env var, prefix=`dcloud-link`, 7-day expiry
- **MD5-crypt passwords**: `$1$` + 8-char salt, uses `GehirnInc/crypt`
- **Base62 charset**: `0-9a-zA-Z` (variable-length, not zero-padded)
- **MD5 output**: Uppercase 32-char hex

## Sharding Strategy

- **short_link**: DB by code[0], table by code[last] (3 DBs: 0/1/a, 2 tables: 0/a)
- **group_code_mapping**: DB by account_no%2, table by group_id%2
- **traffic**: DB by account_no%2 (tables: traffic_0, traffic_1)

## Streaming Pipeline (Streamer Service)

4-stage Kafka-to-ClickHouse ETL pipeline:

```
ODS (ods_link_visit_topic)
  -> DWD: device UDID generation (MD5), referer extraction, new/returning visitor detection (Redis)
    -> dwd_link_visit_topic
      -> DWM-Wide: User-Agent parsing (browser/OS/device), geo-lookup (Amap IP API)
        -> dwm_link_visit_topic
      -> DWM-UV: unique visitor dedup per day (Redis SETNX 24h TTL)
        -> dwm_unique_visitor_topic
          -> DWS: PV+UV join, 10s windowed aggregation by 9-dim key, batch insert to ClickHouse
```

## OAuth SSO

Supported providers (conditionally enabled via env vars):
- **Google**: `OAUTH_GOOGLE_CLIENT_ID` / `OAUTH_GOOGLE_CLIENT_SECRET`
- **GitHub**: `OAUTH_GITHUB_CLIENT_ID` / `OAUTH_GITHUB_CLIENT_SECRET`
- **WeChat**: `OAUTH_WECHAT_APP_ID` / `OAUTH_WECHAT_APP_SECRET`

Flow: login endpoint generates CSRF state -> redirect to provider -> callback validates state -> find/create account by `oidc_sub` + `oidc_provider` -> issue JWT.

## RabbitMQ Topology

- `short_link.event.exchange` - 6 queues (add/del/update x link/mapping) + error
- `traffic.event.exchange` - 3 queues (free_init, release delay/dead-letter, order_traffic) + error
- `order.event.exchange` - 4 queues (close delay/dead-letter, update, traffic) + error

## SQL Migrations

| File | Database | Purpose |
|------|----------|---------|
| `aqicloud_account.sql` | aqicloud_account | account, traffic_0/traffic_1, traffic_task |
| `aqicloud_link_0.sql` | aqicloud_link_0 | domain, group_code_mapping, link_group, short_link |
| `aqicloud_link_1.sql` | aqicloud_link_1 | Link shard 1 (same tables) |
| `aqicloud_link_a.sql` | aqicloud_link_a | Link shard A (same tables) |
| `aqicloud_shop.sql` | aqicloud_shop | product, product_order_0/_1 |
| `oidc_migration.sql` | aqicloud_account | oidc_sub + oidc_provider columns |
| `p0_password.sql` | link shards | password column (bcrypt) |
| `p2_migration.sql` | mixed | api_token, operation_log tables; domain.state column |
| `p3_migration.sql` | aqicloud_link_0 | abuse_report, brand_config tables |
| `ab_test.sql` | aqicloud_link_0 | ab_test, ab_test_variant tables |
| `clickhouse_visit_stats.sql` | ClickHouse default | visit_stats (ReplacingMergeTree, 12-dim key) |

## Environment Variables

| Variable | Default | Service |
|----------|---------|---------|
| JWT_SECRET | (required) | All (JWT signing) |
| PORT | 8001/8002/8003/8005/8006/8888 | All |
| MYSQL_HOST | 127.0.0.1 | account, link, shop |
| MYSQL_PORT | 3306 | account, link, shop |
| MYSQL_USER | root | account, link, shop |
| MYSQL_PWD | root | account, link, shop |
| REDIS_HOST | 127.0.0.1 | account, link, shop, streamer |
| REDIS_PORT | 6379 | account, link, shop, streamer |
| RABBITMQ_URL | amqp://guest:guest@localhost:5672/ | account, link, shop |
| KAFKA_BROKERS | localhost:9092 | link, streamer |
| CLICKHOUSE_HOST | 127.0.0.1 | data, streamer |
| CLICKHOUSE_PORT | 9000 | data, streamer |
| ACCOUNT_SERVICE | http://localhost:8001 | link |
| RPC_TOKEN | rpc-token-default | link |
| STORAGE_TYPE | local | account |
| MINIO_ENDPOINT | minio:9000 | account |
| MINIO_BUCKET | aqicloud | account |
| MINIO_ACCESS_KEY | minioadmin | account |
| MINIO_SECRET_KEY | minioadmin | account |
| MINIO_PUBLIC_URL | http://localhost:9000/aqicloud | account |
| SMS_PROVIDER | log | account |
| ALERT_TYPE | log | link |
| ALERT_WEBHOOK_URL | (empty) | link |
| AI_PROVIDER | doubao | ai |
| AI_API_KEY | (empty) | ai |
| AI_BASE_URL | (empty) | ai |
| AI_MODEL | doubao-pro-32k | ai |
| OAUTH_GOOGLE_CLIENT_ID | (empty) | account |
| OAUTH_GOOGLE_CLIENT_SECRET | (empty) | account |
| OAUTH_GITHUB_CLIENT_ID | (empty) | account |
| OAUTH_GITHUB_CLIENT_SECRET | (empty) | account |
| OAUTH_WECHAT_APP_ID | (empty) | account |
| OAUTH_WECHAT_APP_SECRET | (empty) | account |
| AMAP_API_KEY | (empty) | streamer (geo-lookup) |

## Databases

- `aqicloud_account` - account, traffic_0/traffic_1, traffic_task, api_token
- `aqicloud_link_0`, `aqicloud_link_1`, `aqicloud_link_a` - short_link, group_code_mapping, link_group, domain, ab_test, abuse_report, brand_config, operation_log
- `aqicloud_shop` - product, product_order_0/product_order_1
- ClickHouse `visit_stats` - ReplacingMergeTree, partitioned by day, 12-dim ORDER BY key

## Dependencies

- Go 1.26+, Gin, GORM, go-redis/v9, amqp091-go, kafka-go, golang-jwt/v5, sonyflake, murmur3, GehirnInc/crypt
- ClickHouse driver (`github.com/ClickHouse/clickhouse-go/v2`)
- OAuth2 (`golang.org/x/oauth2`) for Google/GitHub/WeChat SSO
- User-Agent parser (`github.com/mssola/user_agent`) for streamer DWM-Wide stage
- OpenAI-compatible LLM client for AI service (default: Ollama)
