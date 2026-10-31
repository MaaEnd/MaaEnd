#!/usr/bin/env bash
# 启动 MXU，并把它看到的 wayland 名字「对调」，让自动匹配必然选中 sway。
#
# 问题：MXU 的设备扫描优先 wayland-0，而 wayland-0 是 WSLg 的 Weston（不支持 wlr_screencopy），
#       所以自动连接必然失败；手选 wayland-1 又会被 MXU 退出时的内存状态覆盖回 wayland-0。
# 做法：给 MXU 一个私有 XDG_RUNTIME_DIR，里面把两个 socket 的名字对调：
#       wayland-0 -> /run/user/0/wayland-1       （我们的 sway，带 wlr_screencopy）
#       wayland-1 -> /mnt/wslg/runtime-dir/wayland-0（WSLg，给 GUI 渲染用）
#       于是「默认 wayland-0」正好命中 sway，GUI 仍然走 WSLg。
set -uo pipefail
RUN=/root/mxu-run
SWAY_SOCK=/run/user/0/wayland-1
WSLG_SOCK=/mnt/wslg/runtime-dir/wayland-0

mkdir -p "$RUN"
rm -f "$RUN"/wayland-0 "$RUN"/wayland-0.lock "$RUN"/wayland-1 "$RUN"/wayland-1.lock
[ -S "$SWAY_SOCK" ] && ln -s "$SWAY_SOCK" "$RUN/wayland-0"
[ -S "$WSLG_SOCK" ] && ln -s "$WSLG_SOCK" "$RUN/wayland-1"
echo "[i] $RUN: $(ls -1 "$RUN" | tr '\n' ' ')"
if [ ! -S "$SWAY_SOCK" ]; then
  echo "[!] sway socket 不存在：先跑 start_sway.sh，或让 WebLaunchSwayStart 节点拉起"
fi

cd /root/maaend || exit 1
exec env XDG_RUNTIME_DIR="$RUN" WAYLAND_DISPLAY=wayland-1 \
  WEBKIT_DISABLE_COMPOSITING_MODE=1 ./MaaEnd
