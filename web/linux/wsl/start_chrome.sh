#!/usr/bin/env bash
# 只起 Chrome + 对话框守护（幂等：已在跑直接退出）。供节点 WebLaunchChromeStart 调用。
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if pgrep -f "user-data-dir=/root/chrome-profile" >/dev/null 2>&1; then
  echo "[i] Chrome 已在运行"; exit 0
fi
exec bash "$HERE/../homesrv/chrome_homesrv.sh"
