#!/usr/bin/env bash
# WSL（Ubuntu-24.04）本地测试入口：起 sway(headless) + Chrome → 按预设跑任务 → 收尾。
#
# 与 homesrv 版逻辑一致，只改了三处路径默认值：
#   * Chrome 用系统包 /usr/bin/google-chrome（homesrv 是解包的 ~/opt/chrome）
#   * XDG_RUNTIME_DIR 默认 /tmp/xdg-run（WSL 里通常没有 /run/user/<uid> 的 logind 会话）
#   * 资源层目录取自仓库布局：web/resource_web、web/linux/resource_web_linux、web/resource_web_homesrv
#
# 用法:
#   ./daily_run_wsl.sh                      # 跑预设里全部任务
#   MAAEND_TASKS=5 ./daily_run_wsl.sh       # 只跑第 5 项（AutoStockpile）
#   MAA_SAVE_DRAW=1 MAAEND_TASKS=5 ./daily_run_wsl.sh   # 同时保存识别可视化截图
set -uo pipefail

ROOT="${MAAEND_DIST:-$HOME/maaend}"
WEB="$ROOT/web"
CONF="$ROOT/daily.conf"
LOG="$ROOT/daily.log"
# 旧会话的 profile 在 ~/chrome-profile；新部署用 ~/.config/endfield-web。两者都认。
if [ -z "${MAAEND_PROFILE:-}" ]; then
  if [ -d "$HOME/.config/endfield-web" ]; then
    MAAEND_PROFILE="$HOME/.config/endfield-web"
  elif [ -d "$HOME/chrome-profile" ]; then
    MAAEND_PROFILE="$HOME/chrome-profile"
  else
    MAAEND_PROFILE="$HOME/.config/endfield-web"
  fi
fi
PROFILE="$MAAEND_PROFILE"
INSTANCE="hmuddz8"
TASKS=""
[ -f "$CONF" ] && . "$CONF"
TASKS="${MAAEND_TASKS:-${TASKS:-}}"

export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp/xdg-run}"
export XDG_SESSION_TYPE=wayland
export WAYLAND_DISPLAY=wayland-1
mkdir -p "$XDG_RUNTIME_DIR" "$PROFILE"
chmod 700 "$XDG_RUNTIME_DIR" 2>/dev/null

export MAAEND_DIST="$ROOT"
export MAAEND_ROOT="$ROOT"
export MAAEND_WEB="$WEB"
export MAAEND_RES_DIRS="$ROOT/resource:$WEB/resource_web:$WEB/linux/resource_web_linux:$WEB/resource_web_homesrv"
export MAA_AGENT_BIN="$ROOT/agent/go-service"
export MAA_AGENT_LIB="$ROOT/maafw"
export MAAEND_CHROME="${MAAEND_CHROME:-/usr/bin/google-chrome}"
export MAAEND_PROFILE="$PROFILE"

exec >>"$LOG" 2>&1

cleanup() {
  echo "[i] 收尾：先让页面离开云游戏，再关进程"
  python3 "$WEB/linux/leave_game.py" --port 9334 2>&1 | tail -2 || true
  pkill -f "run_daily_linux.py" 2>/dev/null
  pkill -f "$ROOT/agent/go-service" 2>/dev/null
  pkill -f "user-data-dir=$PROFILE" 2>/dev/null
  sleep 2
  pkill -f "hide_cloud_ball.py" 2>/dev/null
  pkill -x swaybg 2>/dev/null
  pkill -x sway 2>/dev/null
  sleep 1
  echo "[i] 残留: chrome=$(pgrep -fc "user-data-dir=$PROFILE" || true) agent=$(pgrep -fc "$ROOT/agent/go-service" || true) sway=$(pgrep -xc sway || true)"
}
trap cleanup EXIT INT TERM

echo
echo "===== $(date -Is) WSL 开始  预设=$INSTANCE 任务=[${TASKS:-全部}] ====="

bash "$WEB/linux/homesrv/chrome_homesrv.sh" || { echo "[X] 启动浏览器失败"; exit 10; }

python3 "$WEB/linux/reset_page.py" --port 9334 --url https://endfield.hypergryph.com/cloud --wait 12 2>&1 | tail -2

ARGS=(--instance "$INSTANCE")
[ -n "$TASKS" ] && ARGS+=(--tasks "$TASKS")
python3 -u "$WEB/linux/run_daily_linux.py" "${ARGS[@]}" "$@"
RC=$?
echo "===== $(date -Is) WSL 结束 rc=$RC ====="
exit $RC
