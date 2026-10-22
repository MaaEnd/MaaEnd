#!/bin/bash
# 每天定时运行前：检查 MaaEnd 最新发行版 -> 有更新则 备份/替换/自检 -> 清理旧备份（保留最近 4 份）
#
# 设计要点：
#   * 只在版本变化时才下载（400MB 级），不变化直接退出，几乎不耗时。
#   * 备份与替换都不碰 web/ 与 config/ —— 我们的网页端覆盖层、脚本、预设都在那里。
#   * 替换后跑 post_upgrade_check.py（15 个关键节点必须在位），失败自动回滚到刚备份的版本。
#   * 任何异常都以"保持当前版本"收场，绝不让更新把日常跑挂。
#
# 用法: auto_update.sh [--check] [--force]
#   --check  只看版本，不动任何文件
#   --force  版本相同也重装一次
set -uo pipefail

ROOT="$HOME/maaend"
WEB="$ROOT/web"
BK="$ROOT/backups"
STATE="$ROOT/upgrade_state.json"
LOG="$ROOT/upgrade.log"
PROXY="http://127.0.0.1:7890"
KEEP=4
CHECK_PY="$WEB/linux/homesrv/post_upgrade_check.py"
AUDIT_PY="$WEB/linux/homesrv/option_audit.py"

MODE_CHECK=0
MODE_FORCE=0
for a in "$@"; do
  [ "$a" = "--check" ] && MODE_CHECK=1
  [ "$a" = "--force" ] && MODE_FORCE=1
done

log() { echo "[$(date -Is)] $*" >>"$LOG"; }

# 覆盖项基线检查：上游改了选项树（加深层级/新增 input 型/改类型）时当天可见。
# 失败会写 $ROOT/option_drift.json，供日报/下次开跑时读取。
run_audit() {
  [ -f "$AUDIT_PY" ] || return 0
  if (cd "$ROOT" && MAAEND_ROOT="$ROOT" timeout 180 python3 "$AUDIT_PY" --check \
        --json-out "$ROOT/option_drift.json" >>"$LOG" 2>&1); then
    rm -f "$ROOT/option_drift.json"
    log "覆盖项基线正常"
    return 0
  fi
  log "覆盖项基线异常（疑似上游改了选项树，详见 $ROOT/option_drift.json）"
  return 1
}

cur_version() {
  python3 - "$ROOT/interface.json" <<'PY' 2>/dev/null
import json, sys
try:
    d = json.load(open(sys.argv[1], encoding='utf-8-sig'))
    print(d.get('version') or 'unknown')
except Exception:
    print('unknown')
PY
}

CUR=$(cur_version)
log "---- 检查开始（当前 $CUR）----"

# 有任务在跑就不动，避免替换到一半被使用
if pgrep -f "run_daily_linux[.]py" >/dev/null 2>&1; then
  log "有任务正在运行，跳过本次更新"
  exit 0
fi

# 1) 查最新 release（含 prerelease，按发布时间取最新）
TMPJ=$(mktemp /tmp/maaend-rel-XXXXXX.json)
curl -s -x "$PROXY" --max-time 40 \
  "https://api.github.com/repos/MaaEnd/MaaEnd/releases?per_page=10" -o "$TMPJ" 2>/dev/null
# 注意：不能写成 `printf ... | python3 - <<'PY'`——heredoc 会顶掉管道，脚本读到的不是 JSON
PARSED=$(python3 - "$TMPJ" <<'PY' 2>/dev/null
import json, sys
try:
    rels = json.load(open(sys.argv[1], encoding='utf-8'))
except Exception:
    print(''); raise SystemExit
rels = [r for r in rels if not r.get('draft')]
if not rels:
    print(''); raise SystemExit
rels.sort(key=lambda r: r.get('published_at') or '', reverse=True)
r = rels[0]
url = ''
for a in r.get('assets') or []:
    n = a.get('name') or ''
    if n.startswith('MaaEnd-linux-x86_64-') and n.endswith('.tar.gz'):
        url = a.get('browser_download_url') or ''
        break
print('%s|%s|%s' % (r.get('tag_name', ''), (r.get('published_at') or '')[:19], url))
PY
)
rm -f "$TMPJ"

# GitHub API 常因共享出口 IP 被限流（403 rate limit），此时回退到 releases.atom：
# 该源无速率限制，且 Linux 包名可预测 -> MaaEnd-linux-x86_64-<tag>.tar.gz
if [ -z "$PARSED" ] || [ -z "${PARSED##*|}" ]; then
  ATOMJ=$(mktemp /tmp/maaend-atom-XXXXXX.xml)
  curl -s -x "$PROXY" --max-time 40 \
    "https://github.com/MaaEnd/MaaEnd/releases.atom" -o "$ATOMJ" 2>/dev/null
  PARSED=$(python3 - "$ATOMJ" <<'PY' 2>/dev/null
import re, sys
try:
    s = open(sys.argv[1], encoding='utf-8').read()
except Exception:
    print(''); raise SystemExit
blocks = re.findall(r'<entry>(.*?)</entry>', s, re.S)
if not blocks:
    print(''); raise SystemExit
entry = blocks[0]
mt = re.search(r'<title>([^<]+)</title>', entry)
mu = re.search(r'<updated>([^<]+)</updated>', entry)
if not mt:
    print(''); raise SystemExit
tag = mt.group(1).strip()
if not re.match(r'^v\d+\.', tag):
    print(''); raise SystemExit
pub = mu.group(1)[:19] if mu else ''
print('%s|%s|https://github.com/MaaEnd/MaaEnd/releases/download/%s/MaaEnd-linux-x86_64-%s.tar.gz'
      % (tag, pub, tag, tag))
PY
  )
  rm -f "$ATOMJ"
  [ -n "$PARSED" ] && log "API 不可用或缺少 Linux 资产，已回退 releases.atom"
fi

TAG=${PARSED%%|*}
REST=${PARSED#*|}
PUB=${REST%%|*}
URL=${REST#*|}

if [ -z "${TAG:-}" ] || [ -z "${URL:-}" ]; then
  log "取最新版本失败（网络/接口），保持当前 $CUR"
  exit 0
fi
log "上游最新: $TAG ($PUB)"

if [ "$MODE_CHECK" = "1" ]; then
  echo "当前: $CUR"
  echo "最新: $TAG ($PUB)"
  echo "包:   $URL"
  exit 0
fi

if [ "$TAG" = "$CUR" ] && [ "$MODE_FORCE" != "1" ]; then
  log "已是最新（$CUR），无需更新"
  # 不升级的日子也要检查一次基线（约 3 秒），否则告警只在升级当天出现
  run_audit || true
  exit 0
fi

FREE_KB=$(df -Pk "$ROOT" | awk 'NR==2{print $4}')
if [ "${FREE_KB:-0}" -lt 2097152 ]; then
  log "磁盘不足 2GB（${FREE_KB}KB），跳过更新"
  exit 0
fi

TS=$(date +%Y%m%d-%H%M%S)
DEST="$BK/$TS"
mkdir -p "$DEST" "$BK" || { log "建备份目录失败"; exit 0; }

rollback() {
  log "!! 回滚到 $CUR"
  rm -rf "$ROOT/resource" "$ROOT/maafw"
  [ -d "$DEST/resource" ] && mv "$DEST/resource" "$ROOT/resource"
  [ -d "$DEST/maafw" ] && mv "$DEST/maafw" "$ROOT/maafw"
  [ -f "$DEST/interface.json" ] && cp -a "$DEST/interface.json" "$ROOT/interface.json"
  [ -d "$DEST/agent" ] && cp -a "$DEST/agent/." "$ROOT/agent/" 2>/dev/null
  # 平台层 / 语言包也一起回滚（存在才回，旧的部署里可能没有）
  for d in locales resource_linux resource_adb resource_cloud_adb resource_macos resource_playcover; do
    if [ -e "$DEST/$d" ]; then
      rm -rf "$ROOT/$d"
      cp -a "$DEST/$d" "$ROOT/$d" 2>/dev/null
    fi
  done
  ln -sfn "$ROOT/maafw" "$ROOT/agent/maafw"
  log "已回滚；当前版本 $(cur_version)"
}

echo "$CUR" > "$DEST/VERSION"

# 2) 备份当前：直接 mv（快，也保证备份是完整的）
[ -d "$ROOT/resource" ] && mv "$ROOT/resource" "$DEST/resource"
cp -a "$ROOT/interface.json" "$DEST/interface.json" 2>/dev/null
mkdir -p "$DEST/agent"
mv "$ROOT/agent/go-service" "$DEST/agent/" 2>/dev/null
mv "$ROOT/agent/cpp-algo"   "$DEST/agent/" 2>/dev/null
[ -d "$ROOT/maafw" ] && mv "$ROOT/maafw" "$DEST/maafw"
# 平台层 / 语言包也备份一份（都很小，几 MB）
for d in locales resource_linux resource_adb resource_cloud_adb resource_macos resource_playcover; do
  [ -e "$ROOT/$d" ] && cp -a "$ROOT/$d" "$DEST/$d" 2>/dev/null
done
log "已备份到 $DEST"

# 3) 下载 + 解包
TMP=$(mktemp -d /tmp/maaend-up-XXXXXX)
if ! curl -sL -x "$PROXY" --max-time 1800 -o "$TMP/pkg.tar.gz" "$URL"; then
  log "下载失败"; rollback; rm -rf "$TMP"; exit 0
fi
if ! tar xzf "$TMP/pkg.tar.gz" -C "$TMP" 2>/dev/null; then
  log "解包失败"; rollback; rm -rf "$TMP"; exit 0
fi
SRC="$TMP"
[ -d "$TMP/x" ] && SRC="$TMP/x"

if [ ! -f "$SRC/resource/pipeline/nodes.json" ] || [ ! -f "$SRC/interface.json" ]; then
  log "包内容不完整"; rollback; rm -rf "$TMP"; exit 0
fi

# 4) 替换（只换 resource / interface / agent 二进制 / maafw）
cp -a "$SRC/resource" "$ROOT/resource" || { log "拷贝 resource 失败"; rollback; rm -rf "$TMP"; exit 0; }
cp -a "$SRC/interface.json" "$ROOT/interface.json" || { log "拷贝 interface 失败"; rollback; rm -rf "$TMP"; exit 0; }
# tasks/ 也要一起换：interface.json 每次都会更新，而它 import 的任务清单跟着变
# （v2.35 新增 LaunchGameLinux / pretasks/LaunchGamePC，不同步就会「任务不存在于接口」）。
# 只覆盖同名文件，不删除本地自建任务。
if [ -d "$SRC/tasks" ]; then
  mkdir -p "$BK/tasks-prev"
  cp -a "$ROOT/tasks/." "$BK/tasks-prev/" 2>/dev/null
  cp -a "$SRC/tasks/." "$ROOT/tasks/" || { log "拷贝 tasks 失败"; rollback; rm -rf "$TMP"; exit 0; }
  log "已同步 tasks/"
fi

if [ -f "$SRC/agent/go-service" ]; then
  cp -a "$SRC/agent/go-service" "$ROOT/agent/go-service"
  cp -a "$SRC/agent/cpp-algo" "$ROOT/agent/cpp-algo" 2>/dev/null
  chmod +x "$ROOT/agent/go-service" "$ROOT/agent/cpp-algo" 2>/dev/null
fi
[ -d "$SRC/maafw" ] && cp -a "$SRC/maafw" "$ROOT/maafw"
# 平台层与语言包：MXU 用 Linux 控制器会自动追加 resource_linux —— 版本不一致时
# PipelineChecker 整包校验失败（悬空 next），而且会连带后面所有覆盖层 load 失败；
# locales 落后则新任务名显示成 task.Xxx.label 原始键。
for d in locales resource_linux resource_adb resource_cloud_adb resource_macos resource_playcover; do
  if [ -e "$SRC/$d" ]; then
    rm -rf "$ROOT/$d"
    cp -a "$SRC/$d" "$ROOT/$d" || { log "拷贝 $d 失败"; rollback; rm -rf "$TMP"; exit 0; }
    log "已同步 $d"
  fi
done
ln -sfn "$ROOT/maafw" "$ROOT/agent/maafw"
rm -rf "$TMP"
log "已替换为 $TAG"

# 5) 自检：15 个关键节点必须都在（覆盖层 + base）
if [ -f "$CHECK_PY" ]; then
  if ! (cd "$ROOT" && timeout 180 python3 "$CHECK_PY" >>"$LOG" 2>&1); then
    log "自检未通过，回滚"
    rollback
    exit 0
  fi
  log "自检通过"
else
  log "缺少自检脚本 $CHECK_PY（跳过自检）"
fi

# 5b) 覆盖项基线：放在自检之外——回滚后的运行时同样值得对照基线
AUDIT_FAILED=0
run_audit || AUDIT_FAILED=1

# 6) 只留最近 KEEP 份备份
PRUNED=$(ls -1dt "$BK"/*/ 2>/dev/null | tail -n +$((KEEP + 1)))
if [ -n "$PRUNED" ]; then
  echo "$PRUNED" | while read -r d; do
    [ -n "$d" ] && rm -rf "$d" && log "清理旧备份 $(basename "$d")"
  done
fi

python3 - "$TAG" "$PUB" <<'PY' >"$STATE" 2>/dev/null
import json, sys, time
print(json.dumps({'version': sys.argv[1], 'published_at': sys.argv[2],
                  'installed_at': time.strftime('%Y-%m-%dT%H:%M:%S')}, ensure_ascii=False))
PY
if [ "$AUDIT_FAILED" = "1" ]; then
  log "完成：$CUR -> $TAG（覆盖项基线异常，见 option_drift.json）"
else
  log "完成：$CUR -> $TAG"
fi
exit 0
