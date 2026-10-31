#!/usr/bin/env bash
# 只起 sway(headless) + Chrome，不跑任务。
# 用途：MXU 的「运行前动作」调它，这样在 MXU 里点「运行」时会先把网页端拉起来。
#
# 默认 headless（不占屏）；要可见窗口时设 MAAEND_HEADLESS=0
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

export MAAEND_PROFILE="${MAAEND_PROFILE:-/root/chrome-profile}"
export MAAEND_CHROME="${MAAEND_CHROME:-/usr/bin/google-chrome}"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp/xdg-run}"
export XDG_SESSION_TYPE=wayland

# 已经起过就不重复拉起（preAction 可能在每次运行前都会执行）
if pgrep -x sway >/dev/null 2>&1 && [ -S "$XDG_RUNTIME_DIR/wayland-1" ] \
   && pgrep -f "user-data-dir=$MAAEND_PROFILE" >/dev/null 2>&1; then
  echo "[i] 网页端已在运行（sway + Chrome），跳过"
  exit 0
fi

exec bash "$HERE/../homesrv/chrome_homesrv.sh"
