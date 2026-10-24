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
  # 选「真正有登录态」的那个 profile：按 Local Storage 体积比，
  # 只看目录是否存在会选到空 profile → 页面弹登录框，整批任务空转。
  _A="$HOME/.config/endfield-web"
  _B="$HOME/chrome-profile"
  _SA=$(du -sb "$_A/Default/Local Storage/leveldb" 2>/dev/null | cut -f1); _SA=${_SA:-0}
  _SB=$(du -sb "$_B/Default/Local Storage/leveldb" 2>/dev/null | cut -f1); _SB=${_SB:-0}
  if [ "$_SB" -ge "$_SA" ] && [ -d "$_B" ]; then
    MAAEND_PROFILE="$_B"
  else
    MAAEND_PROFILE="$_A"
  fi
  echo "[i] profile: $MAAEND_PROFILE (endfield-web=${_SA}B chrome-profile=${_SB}B)"
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

# MXU 自更新会覆盖 resource/interface.json 并冲掉 agent/maafw 链接 → 每次运行前自愈一次
# 先锁版本（MXU 自更新会把 base 换掉），再补装配
PIN="$WEB/linux/wsl/pin_base.sh"
if [ -f "$PIN" ]; then
  bash "$PIN" 2>&1 | sed 's/^/  [锁版] /'
fi

SELFHEAL="$WEB/linux/wsl/apply_wsl_setup.py"
if [ -f "$SELFHEAL" ]; then
  python3 "$SELFHEAL" 2>&1 | sed 's/^/  [自愈] /'
fi

bash "$WEB/linux/homesrv/chrome_homesrv.sh" || { echo "[X] 启动浏览器失败"; exit 10; }

# 浏览器原生对话框自动接受（Leave site? 等），必须 nohup+setsid 才能脱离本会话存活
if [ -f "$WEB/linux/auto_dismiss_dialog.py" ]; then
  pkill -f "auto_dismiss_dialo[g].py" 2>/dev/null
  nohup setsid python3 "$WEB/linux/auto_dismiss_dialog.py" --port 9334 </dev/null >/tmp/auto_dialog.log 2>&1 &
  sleep 2
  echo "[i] 对话框自动接受: $(pgrep -fc "auto_dismiss_dialo[g].py" || echo 0) 个进程"
fi

python3 "$WEB/linux/reset_page.py" --port 9334 --url https://endfield.hypergryph.com/cloud --wait 12 2>&1 | tail -2

ARGS=(--instance "$INSTANCE")
[ -n "$TASKS" ] && ARGS+=(--tasks "$TASKS")
python3 -u "$WEB/linux/run_daily_linux.py" "${ARGS[@]}" "$@"
RC=$?
echo "===== $(date -Is) WSL 结束 rc=$RC ====="
exit $RC
