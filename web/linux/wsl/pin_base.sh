#!/usr/bin/env bash
# 锁定当前 base 版本：把 MXU 更新会覆盖的东西存一份快照，版本一变就还原。
# 用法：pin_base.sh            → 建快照（若不存在）并检查
#       pin_base.sh --restore  → 强制还原
set -u
ROOT=/root/maaend
PIN=/root/maaend-pinned
LOG=/root/maaend/pin.log

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG"; }

ITEMS="resource resource_linux resource_adb resource_cloud_adb resource_macos resource_playcover locales interface.json agent maafw"

ver() { python3 -c "import json,sys;print(json.load(open('$1',encoding='utf-8-sig')).get('version','?'))" 2>/dev/null || echo "?"; }

CUR=$(ver "$ROOT/interface.json")
PINV=$(ver "$PIN/interface.json" 2>/dev/null || echo "")

if [ ! -f "$PIN/interface.json" ] || [ "${1:-}" = "--snapshot" ]; then
  log "建快照（base=$CUR）→ $PIN"
  mkdir -p "$PIN"
  for d in $ITEMS; do
    [ -e "$ROOT/$d" ] && cp -a "$ROOT/$d" "$PIN/" 2>/dev/null
  done
  log "快照完成：$PIN"
  exit 0
fi

if [ "$CUR" != "$PINV" ]; then
  log "!! 检测到 base 被改动：当前=$CUR 锁定=$PINV，开始还原"
  for d in $ITEMS; do
    if [ -e "$PIN/$d" ]; then
      rm -rf "$ROOT/$d"
      cp -a "$PIN/$d" "$ROOT/$d" 2>/dev/null && log "  还原 $d"
    fi
  done
  log "还原后版本=$(ver "$ROOT/interface.json")"
  python3 /root/maaend/web/linux/wsl/apply_wsl_setup.py 2>&1 | tail -3
  exit 0
fi

log "版本一致（$CUR），无需处理"
