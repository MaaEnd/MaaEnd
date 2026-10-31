#!/usr/bin/env bash
# homesrv 版日常：起浏览器 → 按预设跑 → 收尾。
# 收尾规则固定：跑完必须关掉 云·终末地(Chrome) + MaaEnd agent(go-service) + 合成器，
# 只留下 cron 这个定时器本身。trap 保证失败/中断也照样清理。
set -uo pipefail

ROOT="$HOME/maaend"
WEB="$ROOT/web"
CONF="$ROOT/daily.conf"
LOG="$ROOT/daily.log"
PROFILE="$HOME/.config/endfield-web"

TASKS=""
INSTANCE="hmuddz8"
[ -f "$CONF" ] && . "$CONF"
# 便于手动临时覆盖：MAAEND_TASKS=1,2 ./daily_run_homesrv.sh
TASKS="${MAAEND_TASKS:-${TASKS:-}}"

# cron 环境下补齐会话变量（缺 XDG_RUNTIME_DIR 会让点击不生效）
if [ -z "${XDG_RUNTIME_DIR:-}" ] && [ -d "/run/user/$(id -u)" ]; then
  export XDG_RUNTIME_DIR="/run/user/$(id -u)"
fi
[ -z "${XDG_SESSION_TYPE:-}" ] && export XDG_SESSION_TYPE=wayland
if [ -S "${XDG_RUNTIME_DIR:-/nonexistent}/bus" ]; then
  export DBUS_SESSION_BUS_ADDRESS="unix:path=$XDG_RUNTIME_DIR/bus"
fi
export MAAEND_DIST="$ROOT"
export MAAEND_ROOT="$ROOT"
export MAAEND_WEB="$WEB"
export MAAEND_RES_DIRS="$ROOT/resource:$ROOT/web/resource_web:$ROOT/web/linux/resource_web_linux:$ROOT/web/resource_web_homesrv"
export MAA_AGENT_BIN="$ROOT/agent/go-service"
export MAA_AGENT_LIB="$ROOT/maafw"

# 日志轮转：只保留最近 3 次运行（daily.log + daily.log.1 + daily.log.2）
if [ -s "$LOG" ]; then
  rm -f "$LOG.2"
  [ -f "$LOG.1" ] && mv -f "$LOG.1" "$LOG.2"
  mv -f "$LOG" "$LOG.1"
fi
: > "$LOG"

exec >>"$LOG" 2>&1

cleanup() {
  echo "[i] 收尾：先让页面离开云游戏（通知服务端结束会话），再关进程"
  python3 "$WEB/linux/leave_game.py" --port 9334 2>&1 | tail -2 || true
  echo "[i] 收尾：关闭 云·终末地 与 MaaEnd 相关进程"
  pkill -f "run_daily_linux.py" 2>/dev/null
  pkill -f "$ROOT/agent/go-service" 2>/dev/null
  pkill -f "hide_cloud_ball.py --watch" 2>/dev/null
  pkill -f "user-data-dir=$PROFILE" 2>/dev/null
  sleep 2
  pkill -f "user-data-dir=$PROFILE" 2>/dev/null
  pkill -x swaybg 2>/dev/null
  pkill -x sway 2>/dev/null
  sleep 1
  left_chrome=$(pgrep -fc "google-chrome.*user-data-dir=$PROFILE" 2>/dev/null || true)
  left_agent=$(pgrep -fc "$ROOT/agent/go-service" 2>/dev/null || true)
  left_sway=$(pgrep -fc "sway -c " 2>/dev/null || true)
  left_chrome=${left_chrome:-0}; left_agent=${left_agent:-0}; left_sway=${left_sway:-0}
  echo "[i] 残留：chrome=$left_chrome agent=$left_agent sway=$left_sway"
}
trap cleanup EXIT INT TERM

echo
echo "===== $(date -Is) 开始  预设=$INSTANCE 任务=[${TASKS:-全部}] ====="

bash "$WEB/linux/homesrv/chrome_homesrv.sh"
if [ $? -ne 0 ]; then echo "[X] 启动浏览器失败"; exit 10; fi

for i in $(seq 1 30); do
  curl -s --max-time 2 http://127.0.0.1:9334/json/version >/dev/null 2>&1 && break
  sleep 2
done
# 回到启动页并滚到顶，避免上次点击把页面带偏
python3 "$WEB/linux/reset_page.py" --port 9334 --url https://endfield.hypergryph.com/cloud --wait 12 2>&1 | tail -2

ARGS=(--instance "$INSTANCE")
[ -n "${TASKS:-}" ] && ARGS+=(--tasks "$TASKS")
python3 -u "$WEB/linux/run_daily_linux.py" "${ARGS[@]}" "$@"
RC=$?

# 验证云端悬浮球是否被隐藏（hide_cloud_ball.py 在 chrome 启动时注入）
if [ -f "$WEB/linux/homesrv/hide_cloud_ball.py" ]; then
  python3 "$WEB/linux/homesrv/hide_cloud_ball.py" --check 2>&1 | sed 's/^/[球] /'
fi

echo "===== $(date -Is) 结束 rc=$RC ====="
exit $RC
