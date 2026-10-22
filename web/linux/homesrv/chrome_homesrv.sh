#!/usr/bin/env bash
# homesrv 版：起 sway（headless）+ Chrome（复用 M7A 自带的 Chrome for Testing）。
# 与 WSL 版等价，只是路径/浏览器不同；用户级运行，不需要 root。
set -uo pipefail

if [ -n "${MAAEND_CHROME:-}" ]; then
  CHROME="$MAAEND_CHROME"
elif [ -x "$HOME/opt/chrome/opt/google/chrome/google-chrome" ]; then
  CHROME="$HOME/opt/chrome/opt/google/chrome/google-chrome"   # 正式版，无 "for Testing" 提示条
else
  CHROME="$HOME/m7a/3rdparty/WebBrowser/chrome/linux64/140.0.7339.207/chrome"
fi
PROFILE="${MAAEND_PROFILE:-$HOME/.config/endfield-web}"
URL="${1:-https://endfield.hypergryph.com/cloud}"
# cron 里没有 XDG_RUNTIME_DIR，会落到 /tmp/xdg-run；实测那样点击不生效。
# 统一到用户 session 的运行时目录（存在才用），与手动运行保持一致。
if [ -z "${XDG_RUNTIME_DIR:-}" ] || [ ! -d "${XDG_RUNTIME_DIR:-}" ]; then
  if [ -d "/run/user/$(id -u)" ]; then
    export XDG_RUNTIME_DIR="/run/user/$(id -u)"
  else
    export XDG_RUNTIME_DIR="/tmp/xdg-run"
  fi
fi
RUNTIME="$XDG_RUNTIME_DIR"
SWAY_CONF=/tmp/endfield-sway.conf

export XDG_RUNTIME_DIR="$RUNTIME"
export XDG_SESSION_TYPE=wayland
export WAYLAND_DISPLAY=wayland-1
mkdir -p "$RUNTIME" "$PROFILE"; chmod 700 "$RUNTIME"

# 物理 2560x1440（整数倍），缩放交给 Chrome 的 DSF 1.5 → CSS 1706x960
printf '%s\n' \
  'output HEADLESS-1 resolution 2560x1440 scale 1' \
  'output HEADLESS-1 bg #000000 solid_color' \
  'default_border none' > "$SWAY_CONF"

if pgrep -x sway >/dev/null 2>&1 && [ ! -S "$RUNTIME/wayland-1" ]; then
  echo "[i] 检测到 sway 跑在别的 socket 上，重启到 $RUNTIME"
  pkill -x swaybg 2>/dev/null; pkill -x sway 2>/dev/null; sleep 2
fi

if ! pgrep -x sway >/dev/null 2>&1; then
  echo "[i] 起 sway"
  WLR_BACKENDS=headless WLR_LIBINPUT_NO_DEVICES=1 WLR_RENDERER=pixman LIBSEAT_BACKEND=noop \
    setsid sway -c "$SWAY_CONF" </dev/null >/tmp/sway-endfield.log 2>&1 &
  for i in $(seq 1 15); do sleep 1; [ -S "$RUNTIME/wayland-1" ] && break; done
fi

pkill -f "user-data-dir=$PROFILE" 2>/dev/null
sleep 2
# 强杀会留下 Singleton 锁，下一次 Chrome 会立刻退出（表现为"启动浏览器失败"）
rm -f "$PROFILE"/SingletonLock "$PROFILE"/SingletonCookie "$PROFILE"/SingletonSocket "$PROFILE"/lockfile 2>/dev/null
echo "[i] 起 Chrome: $CHROME"
setsid "$CHROME" \
  --ozone-platform=wayland \
  --test-type \
  --force-device-scale-factor=1.5 \
  --no-sandbox --test-type \
  --window-size=1706,960 --kiosk \
  --use-angle=swiftshader --enable-unsafe-swiftshader \
  --disable-features=Translate,TranslateUI,msEdgeTranslate,CalculateNativeWinOcclusion,WebContentsOcclusion \
  --disable-backgrounding-occluded-windows \
  --disable-renderer-backgrounding \
  --disable-background-timer-throttling \
  --deny-permission-prompts \
  --lang=zh-CN --accept-lang=zh-CN \
  --autoplay-policy=no-user-gesture-required \
  --remote-debugging-port=9334 --remote-debugging-address=127.0.0.1 \
  --user-data-dir="$PROFILE" \
  --no-first-run --no-default-browser-check --hide-crash-restore-bubble \
  "$URL" </dev/null >/tmp/chrome-endfield.log 2>&1 &
sleep 16

# 自动接受浏览器原生对话框（云页面注册了 beforeunload，会弹 "Leave site?" 遮住标题页）
_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$_HERE/../auto_dismiss_dialog.py" ]; then
  pkill -f "auto_dismiss_dialo[g].py" 2>/dev/null
  setsid python3 "$_HERE/../auto_dismiss_dialog.py" --port 9334 </dev/null >/tmp/auto_dialog.log 2>&1 &
fi

# 隐藏云端悬浮球：它浮在 canvas 上、默认停在左上角，会遮住游戏标题区（OCR/模板都会受它影响）。
# 脚本对当前页面立即生效，并注册到之后的每次页面加载。
HIDE_PY="$(dirname "$0")/hide_cloud_ball.py"
if [ -f "$HIDE_PY" ]; then
  pkill -f "hide_cloud_ball.py --watch" 2>/dev/null
  setsid python3 "$HIDE_PY" --watch </dev/null >/tmp/hermes-hide-ball.log 2>&1 &
fi

echo "  socket: $RUNTIME/wayland-1"
echo "  进程: $(ps -eo args | grep -c '[e]ndfield-web')"

curl -s --max-time 5 http://127.0.0.1:9334/json/version | head -2
