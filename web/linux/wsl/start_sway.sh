#!/usr/bin/env bash
# 只起 sway（幂等：已在跑直接退出）。供 pipeline 节点 WebLaunchSwayStart 调用。
set -uo pipefail
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/0}"
export WAYLAND_DISPLAY=wayland-1
[ -d "$XDG_RUNTIME_DIR" ] || mkdir -p "$XDG_RUNTIME_DIR"
[ -S "$XDG_RUNTIME_DIR/wayland-1" ] && { echo "[i] sway 已在运行"; exit 0; }
CONF=/tmp/endfield-sway.conf
printf '%s
' 'output HEADLESS-1 resolution 2560x1440 scale 1'   'output HEADLESS-1 bg #000000 solid_color' 'default_border none' > "$CONF"
WLR_BACKENDS=headless WLR_LIBINPUT_NO_DEVICES=1 WLR_RENDERER=pixman LIBSEAT_BACKEND=noop   setsid sway -c "$CONF" </dev/null >/tmp/sway-endfield.log 2>&1 &
for i in $(seq 1 15); do sleep 1; [ -S "$XDG_RUNTIME_DIR/wayland-1" ] && break; done
echo "[i] sway socket: $(ls "$XDG_RUNTIME_DIR"/wayland-1 2>/dev/null || echo 未就绪)"
