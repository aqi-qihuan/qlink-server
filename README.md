# qlink-server

短链 link 系统的 Go 实现，替换原 Java 9 个宿主服务单元，复用现有 `link-stack` 的 14 个中间件/APM 容器。

## 当前状态（2026-11-09）

- 目标运行形态：**宿主 systemd 二进制**，由 `deployments/dev/resources/go-stack.sh` 管理。
- 容器编排唯一入口仍是 `aqicloud-short-link/deployments/dev/stack/stack.yml`，14 容器全保留、全 healthy。
- Go 侧 7 个长运行服务：`gateway / account / link / data / shop / streamer / ai`，其中 `ai` 默认不启用（本机无 Ollama LLM）。
- 可观测性：标准 OTel SDK → OTLP/HTTP → 现有 SkyWalking OAP（12800）；每个服务暴露 `/metrics`。
- CI：本地三层门禁（pre-commit / pre-push / systemd timer），不依赖 GitHub Actions。

## 目录结构

```text
cmd/                     7 个服务入口 + initschema 一次性 CLI
internal/                按服务分包的业务实现
internal/common/telemetry OTel + Prometheus 公共包
deployments/dev/resources/go-stack.sh  宿主 systemd 起停脚本
deployments/dev/systemd/  定时 CI service/timer 模板
sql/                     MySQL DDL；sql/clickhouse/ 为 ClickHouse DDL（MySQL init 不执行）
tools/                   本地 CI、全链路联调、PV 回归脚本
tests/integration/       Go 集成测试
scripts/githooks/        pre-commit / pre-push / commit-msg
```

## 环境准备

```bash
cd ~/Projects/qlink-server
cp .env.example .env
# 按需填写 JWT_SECRET、DB/Redis/MQ/CH/RustFS 凭据；本机已有对齐 link-stack 的 .env（gitignored）
bash deployments/dev/resources/go-stack.sh build
```

关键变量：

| 变量 | 说明 |
|---|---|
| `JWT_SECRET` | account/link/data/shop 启动强校验，必须非默认；用环境变量，不入库 |
| `MYSQL_HOST/PORT` | 现有 `shortlink-mysql`，宿主 `127.0.0.1:3307` |
| `REDIS_HOST/PORT/PWD` | 现有 `shortlink-redis` |
| `RABBITMQ_URL` | 现有 `shortlink-rabbitmq`，vhost `dev` |
| `KAFKA_BROKERS` | 现有 `shortlink-kafka`，宿主 `127.0.0.1:9092` |
| `CLICKHOUSE_ADDR/DB` | `127.0.0.1:8123` / `visit_stats`（Go 版新库） |
| `MINIO_ENDPOINT/BUCKET` | `127.0.0.1:9010` / `aqicloud-link` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://127.0.0.1:12800/v1/traces` |
| `STREAMER_METRICS_PORT` | 默认 `8007` |

> `PORT` 不写入 `.env`：7 个服务共用同一个键，`go-stack.sh` 按单元注入 `Environment=PORT=<port>` 覆盖。

## 运行与验收

```bash
bash deployments/dev/resources/go-stack.sh start
bash deployments/dev/resources/go-stack.sh status

for p in 8888 8001 8002 8003 8005 8007; do
  curl -fsS "http://127.0.0.1:${p}/health" >/dev/null
  curl -fsS "http://127.0.0.1:${p}/metrics" >/dev/null
done

go test -race ./... -count=1 -timeout=10m
python3 tools/full_integration_test.py
python3 tools/pv_chain_regression.py --no-start
```

预期：

- 5 个业务 HTTP 服务 + gateway + streamer 均 `/health` 200；streamer 的 health/metrics 在 `8007`。
- `/metrics` 包含 `qlink_http_requests_total`、`go_goroutines`、`process_resident_memory_bytes`。
- 全量联调 `PASS=44 / FAIL=0 / SKIP=2`（AI 两项因未启 LLM 跳过）。
- PV 回归 6 条断言全通过，Kafka 4 段 ETL 写入 `visit_stats.visit_stats`。

## 数据面

- MySQL 继承 Java 版库表；Go 版增量表由 `sql/30~34` 补齐。
- ClickHouse 不复用 Java 的 `default.visit_stats`（`DateTime` / `MergeTree`），Go 使用 `visit_stats.visit_stats`（`UInt64` 毫秒 / `ReplacingMergeTree`）。DDL 在 `sql/clickhouse/visit_stats.sql`，需手工执行：
  ```bash
  curl -sS -u "$CLICKHOUSE_USER:$CLICKHOUSE_PWD" \
    'http://127.0.0.1:8123/' --data-binary 'CREATE DATABASE IF NOT EXISTS visit_stats'
  curl -sS -u "$CLICKHOUSE_USER:$CLICKHOUSE_PWD" \
    'http://127.0.0.1:8123/' --data-binary @sql/clickhouse/visit_stats.sql
  ```
- Redis 跳转缓存前缀为 `sl:redirect:`；从 Java 切换时应清理旧的 `link:redirect:*`。
- Kafka 必须独占消费：Go `streamer` 与 Java 4 个 Flink 作业不能同时运行。

## 可观测性

- OAP 已开启 `SW_TRACEQL_ENABLE_DATASOURCE_OTLP=true`；Go 启动 span 与 HTTP span 写入 `sw_otlp_span-YYYYMMDD` 索引。
- `status_code` 为 OTLP 语义：`0=UNSET`、`1=OK`、`2=ERROR`；当前验收错误 span 为 0。
- `OTEL_SDK_DISABLED=true` 可关闭 trace，仅保留 `/metrics`。

## 本地 CI

```bash
bash tools/ci-local.sh pre-commit   # gofmt / vet / 敏感信息
bash tools/ci-local.sh pre-push    # + go mod verify / race 测试 / build
bash tools/ci-local.sh nightly     # + compose config / 可选 lint/vuln
make ci                            # 等价于 pre-push
make ci-nightly                    # 等价于 nightly
make install-hooks                 # 安装 Git Hooks
make install-ci-timer              # 安装每日 03:00 systemd --user timer
```

GitHub Actions 不启用：本地门禁已覆盖，且避免 artifacts/Packages 配额问题。

## 与 Java 栈切换 / 回滚

端口与 Java 完全重叠，采用停机切换，不并行启动。

```bash
# Go -> Java 回滚
bash ~/Projects/qlink-server/deployments/dev/resources/go-stack.sh stop
bash ~/Projects/aqicloud-short-link/deployments/dev/resources/host-stack.sh start

# Java -> Go 切换
bash ~/Projects/aqicloud-short-link/deployments/dev/resources/host-stack.sh stop
bash ~/Projects/qlink-server/deployments/dev/resources/go-stack.sh start
```

回滚演练已验证：Java 9 单元 active、gateway 302 正常、3 次访问使 `default.visit_stats` 行数 +1；随后停 Java、恢复 Go 栈，PV 回归再次通过。

## 已知延后项

- OIDC workspace / 多工作区仍 deferred，不阻塞本次切换。
- `sql/01~05` 基础 DDL 含 `DROP TABLE IF EXISTS`，重复执行 `cmd/initschema` 会清库；后续应替换为版本化 migration。
- `ai:8006` 需要真实 LLM endpoint 才启用；默认不启动。
