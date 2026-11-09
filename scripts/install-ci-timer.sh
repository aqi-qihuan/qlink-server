#!/usr/bin/env bash
# 安装 qlink-server 的 systemd --user 定时门禁。
# 用法: bash scripts/install-ci-timer.sh
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user"
mkdir -p "${TARGET}"

cp "${ROOT}/deployments/dev/systemd/qlink-ci.service" "${TARGET}/qlink-ci.service"
cp "${ROOT}/deployments/dev/systemd/qlink-ci.timer" "${TARGET}/qlink-ci.timer"

systemctl --user daemon-reload
systemctl --user enable --now qlink-ci.timer

echo "✅ 已安装并启用 qlink-ci.timer（每天 03:00）"
systemctl --user list-timers qlink-ci.timer --no-pager
