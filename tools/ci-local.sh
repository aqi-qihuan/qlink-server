#!/usr/bin/env bash
# qlink-server 本地 CI 门禁：不依赖 GitHub Actions。
# 用法:
#   bash tools/ci-local.sh pre-commit   # 轻量：gofmt / vet / 敏感信息
#   bash tools/ci-local.sh pre-push    # 完整：gofmt / vet / race 测试 / build / mod verify
#   bash tools/ci-local.sh nightly     # 定时：pre-push + compose 配置校验 + 可选 lint/vuln
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"
MODE="${1:-pre-push}"

log() { printf '\n[CI] %s\n' "$*"; }
ok() { printf '[OK] %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*"; }
fail() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

check_gofmt() {
  log "gofmt 检查"
  local files=()
  if [[ "${MODE}" == "pre-commit" ]]; then
    mapfile -t files < <(git diff --cached --name-only --diff-filter=ACM -- '*.go')
  else
    mapfile -t files < <(git ls-files '*.go')
  fi
  if ((${#files[@]} == 0)); then
    ok "无 Go 文件，跳过"
    return 0
  fi
  local unformatted
  unformatted="$(gofmt -l "${files[@]}" || true)"
  if [[ -n "${unformatted}" ]]; then
    printf '%s\n' "${unformatted}" >&2
    fail "以下 Go 文件未格式化"
  fi
  ok "gofmt 通过"
}

check_vet() {
  log "go vet"
  go vet ./...
  ok "go vet 通过"
}

check_mod() {
  log "go mod verify"
  go mod verify
  ok "依赖校验通过"
}

check_secrets() {
  log "敏感信息扫描"
  local pattern='(AKIA[0-9A-Z]{16}|BEGIN [A-Z ]*PRIVATE KEY|SECRET_KEY=.{8,}|PASSWORD=.{8,}|JWT_SECRET=.{8,})'
  local matches
  matches="$(git grep -nEI "${pattern}" -- ':!*.md' ':!.env.example' ':!go.sum' ':!*.lock' ':!tools/ci-local.sh' 2>/dev/null || true)"
  if [[ -n "${matches}" ]]; then
    printf '%s\n' "${matches}" | head -10 >&2
    fail "疑似硬编码凭据，请改用环境变量"
  fi
  ok "未发现疑似硬编码凭据"
}

check_tests() {
  log "go test -race ./..."
  go test -race ./... -count=1 -timeout=10m
  ok "race 测试通过"
}

check_build() {
  log "go build ./..."
  go build ./...
  ok "构建通过"
}

check_compose() {
  log "docker compose config -q"
  docker compose config -q
  ok "compose 配置通过"
}

optional_lint() {
  if command -v golangci-lint >/dev/null 2>&1; then
    log "golangci-lint"
    if golangci-lint run --timeout=2m ./...; then
      ok "golangci-lint 通过"
    else
      warn "golangci-lint 失败（可选门禁，未阻断）"
    fi
  else
    warn "golangci-lint 未安装，跳过"
  fi
}

optional_vuln() {
  if command -v govulncheck >/dev/null 2>&1; then
    log "govulncheck"
    if govulncheck ./...; then
      ok "govulncheck 通过"
    else
      warn "govulncheck 报告问题或无法联网（可选门禁，未阻断）"
    fi
  else
    warn "govulncheck 未安装，跳过"
  fi
}

case "${MODE}" in
  pre-commit)
    check_gofmt
    check_vet
    check_secrets
    ;;
  pre-push)
    check_gofmt
    check_vet
    check_secrets
    check_mod
    check_tests
    check_build
    ;;
  nightly)
    check_gofmt
    check_vet
    check_secrets
    check_mod
    check_tests
    check_build
    check_compose
    optional_lint
    optional_vuln
    ;;
  *)
    fail "未知模式: ${MODE}（可用: pre-commit | pre-push | nightly）"
    ;;
esac

log "本地 CI 完成: ${MODE}"
