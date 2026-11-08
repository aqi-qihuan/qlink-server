#!/usr/bin/env bash
# qlink-server Go 服务宿主起停与资源约束（对标 Java host-stack.sh）。
# 用法: bash deployments/dev/resources/go-stack.sh {start|stop|restart|status|build|start_ai}
#
# 设计要点:
#   - 7 个长运行服务（gateway/account/link/data/shop/streamer）+ 可选 ai
#   - PORT 单键冲突用单元级 Environment=PORT=<port> 覆盖，不改 Go 源码
#   - 内存约束对齐 8C16G 预算：普通服务 256M/512M，streamer 512M/768M
#   - .env 为唯一连接配置来源；WorkingDirectory 指向仓库根，godotenv 可回退加载
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
ENV_FILE="${ENV_FILE:-${ROOT}/.env}"
ACTION="${1:-status}"
UNIT_PREFIX="qlink"
GO_BIN="${GO_BIN:-$(command -v go || true)}"
BIN_DIR="${ROOT}/bin"

# name port binary
# ai 默认不启：本机无 Ollama，需要时执行 start_ai
APP_NAMES=(gateway account link data shop streamer)
AI_NAMES=(ai)

PORT_MAP=(
  [gateway]="8888"
  [account]="8001"
  [link]="8003"
  [data]="8002"
  [shop]="8005"
  [ai]="8006"
  [streamer]=""
)

# Go 进程实测内存远低于 Java，但仍设硬顶防失控
UNIT_MEMORY_HIGH="256M"
UNIT_MEMORY_MAX="512M"
STREAMER_MEMORY_HIGH="512M"
STREAMER_MEMORY_MAX="768M"

unit_name() {
  printf '%s-%s.service' "${UNIT_PREFIX}" "$1"
}

env_val() {
  local key="$1" def="${2:-}" val
  val="$(grep -E "^${key}=" "${ENV_FILE}" 2>/dev/null | tail -1 | cut -d= -f2-)" || true
  printf '%s' "${val:-${def}}"
}

require_files() {
  [[ -r "${ENV_FILE}" ]] || { printf '[FAIL] 环境文件不可读: %s\n' "${ENV_FILE}" >&2; exit 1; }
  [[ -n "${GO_BIN}" && -x "${GO_BIN}" ]] || { printf '[FAIL] Go 工具链不可用\n' >&2; exit 1; }
  mkdir -p "${BIN_DIR}"
}

build_all() {
  require_files
  local name
  printf '[BUILD] go build -> %s\n' "${BIN_DIR}"
  for name in gateway account link data shop ai streamer; do
    printf '  building %-10s ' "${name}"
    "${GO_BIN}" build -trimpath -ldflags "-s -w" -o "${BIN_DIR}/${name}" "./cmd/${name}"
    printf 'OK\n'
  done
}

stop_unit() {
  local name="$1"
  local unit
  unit="$(unit_name "${name}")"
  if systemctl --user cat "${unit}" >/dev/null 2>&1; then
    printf '[STOP] %s\n' "${unit}"
    systemctl --user stop "${unit}"
    systemctl --user reset-failed "${unit}" >/dev/null 2>&1 || true
  fi
}

start_service() {
  local name="$1"
  local port="${PORT_MAP[${name}]:-}"
  local mem_high="${UNIT_MEMORY_HIGH}"
  local mem_max="${UNIT_MEMORY_MAX}"
  local unit
  unit="$(unit_name "${name}")"

  if [[ "${name}" == "streamer" ]]; then
    mem_high="${STREAMER_MEMORY_HIGH}"
    mem_max="${STREAMER_MEMORY_MAX}"
  fi

  if systemctl --user is-active --quiet "${unit}"; then
    printf '[SKIP] %s 已运行；如需应用新参数请使用 restart\n' "${unit}"
    return 0
  fi
  stop_unit "${name}"

  printf '[START] %s (port=%s, mem=%s/%s)\n' "${unit}" "${port:-无}" "${mem_high}" "${mem_max}"

  local props=(
    --property=WorkingDirectory="${ROOT}"
    --property=EnvironmentFile="${ENV_FILE}"
    --property=Restart=on-failure
    --property=RestartSec=5
    --property=TimeoutStopSec=35
    --property=MemoryHigh="${mem_high}"
    --property=MemoryMax="${mem_max}"
    --property=MemorySwapMax=0
  )
  if [[ -n "${port}" ]]; then
    props+=(--property=Environment=PORT="${port}")
  fi

  systemd-run --user --unit="${unit%.service}" \
    --description="qlink ${name} (Go, 8C16G tuned)" \
    "${props[@]}" \
    "${BIN_DIR}/${name}"
}

start_all() {
  require_files
  build_all

  local name
  for name in "${APP_NAMES[@]}"; do
    start_service "${name}"
  done
}

start_ai_unit() {
  require_files
  if [[ ! -x "${BIN_DIR}/ai" ]]; then
    printf '[BUILD] ai\n'
    "${GO_BIN}" build -trimpath -ldflags "-s -w" -o "${BIN_DIR}/ai" ./cmd/ai
  fi
  start_service "ai"
}

stop_all() {
  local name
  local all_names=("${APP_NAMES[@]}" "${AI_NAMES[@]}")
  for name in "${all_names[@]}"; do
    stop_unit "${name}"
  done
}

status_all() {
  local name unit active sub pid rss cap
  printf '%-28s %-10s %-10s %-9s %-10s %-10s\n' UNIT ACTIVE SUB PID RSS_MiB MEM_CAP
  local all_names=("${APP_NAMES[@]}" "${AI_NAMES[@]}")
  for name in "${all_names[@]}"; do
    unit="$(unit_name "${name}")"
    active="$(systemctl --user show "${unit}" -p ActiveState --value 2>/dev/null || printf 'not-found')"
    sub="$(systemctl --user show "${unit}" -p SubState --value 2>/dev/null || printf 'not-found')"
    pid="$(systemctl --user show "${unit}" -p MainPID --value 2>/dev/null || printf '0')"
    rss='-'
    if [[ "${pid:-0}" =~ ^[1-9][0-9]*$ ]]; then
      rss="$(ps -p "${pid}" -o rss= 2>/dev/null | awk '{printf "%.1f", $1/1024}')"
    fi
    cap="$(systemctl --user show "${unit}" -p MemoryMax --value 2>/dev/null || printf 'infinity')"
    [[ -z "${cap}" || "${cap}" = infinity ]] && cap='无' || cap="$((cap / 1024 / 1024))MiB"
    printf '%-28s %-10s %-10s %-9s %-10s %-10s\n' "${unit}" "${active:-not-found}" "${sub:-not-found}" "${pid:-0}" "${rss:--}" "${cap}"
  done
}

case "${ACTION}" in
  start) start_all ;;
  stop) stop_all ;;
  restart) stop_all; start_all ;;
  status) status_all ;;
  build) build_all ;;
  start_ai) start_ai_unit ;;
  *) printf '用法: %s {start|stop|restart|status|build|start_ai}\n' "$0" >&2; exit 2 ;;
esac
