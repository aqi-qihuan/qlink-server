# ============================================================
#  Qlink Server (Go) �?Multi-stage Dockerfile
#  6 个微服务共享一�?builder，运行时使用最�?Alpine 镜像
# ============================================================

# ---------- Go 版本（与 go.mod 保持一致） ----------
ARG GO_VERSION=1.26

# ============================================================
#  Stage 1: 缓存依赖（仅�?go.mod/go.sum 变动时重新下载）
# ============================================================
FROM golang:${GO_VERSION}-alpine AS deps

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# ============================================================
#  Stage 2: 编译（静态链接，剥离调试信息�?# ============================================================
FROM deps AS builder

COPY . .

# 构建参数（可通过 --build-arg 注入�?ARG BUILD_VERSION=dev
ARG BUILD_TIME
ARG GIT_COMMIT

# 编译优化�?#   -s -w        去除符号表和 DWARF，减小约 30% 体积
#   -trimpath    移除编译路径信息，提升安全�?#   -tags netgo  使用�?Go DNS 解析器，避免依赖 libc
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64
ENV GOFLAGS="-trimpath -tags=netgo"

# 统一 ldflags
ARG LDFLAGS="-s -w -X main.version=${BUILD_VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT}"

RUN go build -ldflags="${LDFLAGS}" -o /out/gateway  ./cmd/gateway/  && \
    go build -ldflags="${LDFLAGS}" -o /out/account   ./cmd/account/   && \
    go build -ldflags="${LDFLAGS}" -o /out/link      ./cmd/link/      && \
    go build -ldflags="${LDFLAGS}" -o /out/data      ./cmd/data/      && \
    go build -ldflags="${LDFLAGS}" -o /out/shop      ./cmd/shop/      && \
    go build -ldflags="${LDFLAGS}" -o /out/ai        ./cmd/ai/        && \
    go build -ldflags="${LDFLAGS}" -o /out/streamer  ./cmd/streamer/

# ============================================================
#  Stage 3: 公共运行时基础（所有服务共享，减少重复层）
# ============================================================
FROM alpine:3.21 AS runtime-base

RUN apk --no-cache add \
        ca-certificates \
        tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone \
    && apk del tzdata

# �?root 用户运行
RUN addgroup -S app && adduser -S app -G app
USER app

# ============================================================
#  Stage 4: 各服务运行时镜像
# ============================================================

# --- Gateway (8888) ---
FROM runtime-base AS gateway
COPY --from=builder --chown=app:app /out/gateway /usr/local/bin/gateway
EXPOSE 8888
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD wget -qO- http://localhost:8888/health || exit 1
ENTRYPOINT ["gateway"]

# --- Account (8001) ---
FROM runtime-base AS account
COPY --from=builder --chown=app:app /out/account /usr/local/bin/account
EXPOSE 8001
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD wget -qO- http://localhost:8001/health || exit 1
ENTRYPOINT ["account"]

# --- Link (8003) ---
FROM runtime-base AS link
COPY --from=builder --chown=app:app /out/link /usr/local/bin/link
EXPOSE 8003
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD wget -qO- http://localhost:8003/health || exit 1
ENTRYPOINT ["link"]

# --- Data (8002) ---
FROM runtime-base AS data
COPY --from=builder --chown=app:app /out/data /usr/local/bin/data
EXPOSE 8002
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD wget -qO- http://localhost:8002/health || exit 1
ENTRYPOINT ["data"]

# --- Shop (8005) ---
FROM runtime-base AS shop
COPY --from=builder --chown=app:app /out/shop /usr/local/bin/shop
EXPOSE 8005
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD wget -qO- http://localhost:8005/health || exit 1
ENTRYPOINT ["shop"]

# --- AI (8006) ---
FROM runtime-base AS ai
COPY --from=builder --chown=app:app /out/ai /usr/local/bin/ai
EXPOSE 8006
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD wget -qO- http://localhost:8006/health || exit 1
ENTRYPOINT ["ai"]

# --- Streamer (no HTTP port, streaming pipeline) ---
FROM runtime-base AS streamer
COPY --from=builder --chown=app:app /out/streamer /usr/local/bin/streamer
ENTRYPOINT ["streamer"]
