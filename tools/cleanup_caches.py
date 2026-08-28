"""GitHub Actions 缓存自动清理脚本。

规则:
  - merge ref 中与 v2 同 key 的 maadeps 冗余副本 -> 全删
  - 已关闭/合并 PR 的 merge ref 缓存 -> 全删
  - 同 (ref, key 前缀) 保留最新 keep 份(默认2), 其余删除
  - 删除前检查 last_accessed_at: 最近 access_window 秒内被访问过则跳过(并发保护)
  - v2 分支 maadeps-* 永不删 (共享依赖库存)
  - 可选水位保护: 超 watermark 时删到 target

默认 dry-run, 加 --apply 才真删。用法: python tools/cleanup_caches.py [--apply] [--ref REF]
环境变量: GITHUB_TOKEN 或 GH_TOKEN (需 actions: write)。
"""

from __future__ import annotations

import argparse
import calendar
import json
import os
import sys
import time
import urllib.error
import urllib.request

API_BASE = "https://api.github.com"
REPO = os.environ.get("GITHUB_REPOSITORY") or "MaaEnd/MaaEnd"
CACHE_PREFIX_PART_COUNT = {
    # 前缀取前 N 个 "-" 分隔段 (去掉末尾 hash)
    "ccache-v4": 4,
    "ccache": 3,
    "maadeps": 3,
}


class CleanupError(Exception):
    pass


def log(msg: str) -> None:
    try:
        sys.stdout.buffer.write((msg + "\n").encode("utf-8"))
        sys.stdout.buffer.flush()
    except OSError:
        pass


def github_token() -> str:
    return os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN") or ""


def _headers(extra: dict | None = None) -> dict:
    h = {
        "Accept": "application/vnd.github+json",
        "User-Agent": "maaend-cleanup-caches",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    token = github_token()
    if token:
        h["Authorization"] = f"Bearer {token}"
    if extra:
        h.update(extra)
    return h


def request_json(url: str) -> "dict | list":
    # 5 次指数退避重试 (5xx/429/网络瞬态)
    import random
    import time as _time
    last: Exception | None = None
    for attempt in range(1, 6):
        req = urllib.request.Request(url, headers=_headers())
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            if e.code == 404:
                return {}
            if 500 <= e.code < 600 or e.code == 429:
                last = e
                _time.sleep(min(2 ** attempt, 15) + random.uniform(0, 1))
                continue
            raise CleanupError(f"HTTP {e.code}: {url}") from None
        except (urllib.error.URLError, TimeoutError, ConnectionError, json.JSONDecodeError, OSError) as e:
            last = e
            _time.sleep(min(2 ** attempt, 15) + random.uniform(0, 1))
    raise CleanupError(f"GitHub API unreachable: {url} ({last})") from None


def delete_cache(cache_id: int) -> bool:
    """删除一条缓存。返回 True 表示删除成功（含已不存在）。"""
    url = f"{API_BASE}/repos/{REPO}/actions/caches/{cache_id}"
    req = urllib.request.Request(url, method="DELETE", headers=_headers())
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.status in (200, 204)
    except urllib.error.HTTPError as e:
        # 404：已被删 / 已被 LRU 清掉 -> 视为成功
        if e.code == 404:
            return True
        log(f"  [warn] delete cache #{cache_id} failed HTTP {e.code}")
        return False


def list_all_caches() -> list[dict]:
    """分页拉全量缓存。"""
    caches: list[dict] = []
    per_page = 100
    page = 1
    while True:
        url = f"{API_BASE}/repos/{REPO}/actions/caches?per_page={per_page}&page={page}"
        data = request_json(url)
        if not isinstance(data, dict):
            break
        items = data.get("actions_caches") or []
        caches.extend(items)
        if len(items) < per_page:
            break
        page += 1
    return caches


def key_prefix(key: str) -> str | None:
    """取缓存 key 前缀 (去掉 hash); 只处理 ccache/maadeps, 其余返回 None。"""
    for base, count in CACHE_PREFIX_PART_COUNT.items():
        if key.startswith(base + "-"):
            parts = key.split("-")
            if len(parts) > count:
                return "-".join(parts[:count])
            return key
    return None


def pr_state(pr_number: int) -> str | None:
    """查询 PR 状态，返回 'open'/'closed' 或 None（查询失败）。"""
    url = f"{API_BASE}/repos/{REPO}/pulls/{pr_number}"
    data = request_json(url)
    if isinstance(data, dict) and "state" in data:
        return data["state"]
    return None


def plan(caches: list[dict], keep: int, access_window_s: int,
         watermark_gb: float, target_gb: float, ref_filter: str | None) -> tuple[list[dict], list[dict]]:
    """计算删除计划。返回 (to_delete, protected)。

    to_delete: 应删除的缓存条目（含 reason）
    protected: 因并发/白名单跳过的条目（含 reason）
    """
    now = time.time()
    to_delete: list[dict] = []
    protected: list[dict] = []
    remaining = list(caches)

    def age_last_access(cache: dict) -> float:
        la = cache.get("last_accessed_at") or cache.get("created_at") or ""
        try:
            # GitHub API 的时间戳是 UTC (带 Z), 用 calendar.timegm 按 UTC 解析,
            # 避免 time.mktime 按本地时区解释导致本机/非 UTC 环境并发保护窗口偏移。
            t = calendar.timegm(time.strptime(la[:19], "%Y-%m-%dT%H:%M:%S"))
            return now - t
        except ValueError:
            return 0.0

    # ---- 规则 0: merge ref 中与 v2 同 key 的 maadeps 冗余副本全删 (PR 走 v2 恢复) ----
    v2_maadeps_keys = {
        c.get("key")
        for c in caches
        if (c.get("ref") == "refs/heads/v2") and (c.get("key") or "").startswith("maadeps-")
    }
    for c in list(caches):
        ref = c.get("ref") or ""
        key = c.get("key") or ""
        if ref.startswith("refs/pull/") and key in v2_maadeps_keys:
            c["_reason"] = f"redundant maadeps duplicate of v2 (ref {ref})"
            to_delete.append(c)
            remaining.remove(c)

    # ---- 规则 1：已关闭/合并 PR 的 merge ref 全删 ----
    # 遍历 remaining: 规则 0 已移除的项不再重复 remove (否则 ValueError)
    pr_cache: dict[str, list[dict]] = {}
    for c in remaining:
        ref = c.get("ref") or ""
        if ref.startswith("refs/pull/") and ref.endswith("/merge"):
            pr_cache.setdefault(ref, []).append(c)
    for ref, entries in pr_cache.items():
        num = ref.split("/")[2]
        state = pr_state(num)
        if state in ("closed",):
            for c in entries:
                c["_reason"] = f"PR {num} closed (ref {ref})"
                to_delete.append(c)
                remaining.remove(c)

    # ---- 规则 2: 同 (ref, key 前缀) 保留最新 keep 份, 其余删除 ----
    groups: dict[tuple[str, str], list[dict]] = {}
    for c in remaining:
        ref = c.get("ref") or ""
        pref = key_prefix(c.get("key") or "")
        if not pref:
            continue
        groups.setdefault((ref, pref), []).append(c)
    for (ref, pref), entries in groups.items():
        entries.sort(key=lambda c: (c.get("created_at") or ""))
        for idx, c in enumerate(entries):
            if idx < len(entries) - keep:
                c["_reason"] = f"orphan old (ref {ref}, prefix {pref}), keep latest {keep}"
                to_delete.append(c)
                remaining.remove(c)

    # ---- 规则 3：并发保护 + v2 maadeps 白名单 ----
    final_to_delete: list[dict] = []
    for c in to_delete:
        ref = c.get("ref") or ""
        key = c.get("key") or ""
        # 白名单：v2 分支 maadeps 永不删
        if ref == "refs/heads/v2" and key.startswith("maadeps-"):
            c["_reason"] = "PROTECTED: v2 maadeps shared stock"
            protected.append(c)
            continue
        # 并发保护：最近 access_window 秒内被访问过 -> 跳过
        if age_last_access(c) < access_window_s:
            c["_reason"] = f"skip: last access within {access_window_s}s (possible in-flight restore)"
            protected.append(c)
            continue
        final_to_delete.append(c)

    # ---- 规则 4: 总量水位保护 (从完整 caches 容量计, 只减 final_to_delete, 避免双扣) ----
    remaining_size = sum(c.get("size_in_bytes", 0) for c in caches)
    deleted_size = sum(c.get("size_in_bytes", 0) for c in final_to_delete)
    if watermark_gb and remaining_size - deleted_size > watermark_gb * 1024**3:
        # 从旧到新删，直到低于 target
        overshoot = (remaining_size - deleted_size) - target_gb * 1024**3
        sorted_by_access = sorted(
            [c for c in remaining if not (
                c.get("ref") == "refs/heads/v2" and (c.get("key") or "").startswith("maadeps-")
            )],
            key=lambda c: age_last_access(c), reverse=True,
        )
        for c in sorted_by_access:
            if overshoot <= 0:
                break
            if age_last_access(c) < access_window_s:
                continue  # 并发保护
            c["_reason"] = f"watermark: reduce below {target_gb}GB"
            final_to_delete.append(c)
            deleted_size += c.get("size_in_bytes", 0)
            overshoot -= c.get("size_in_bytes", 0)

    return final_to_delete, protected


def main() -> int:
    parser = argparse.ArgumentParser(description="Clean up GitHub Actions caches")
    parser.add_argument("--apply", action="store_true", help="actually delete (default is dry-run)")
    parser.add_argument("--ref", default=None, help="only consider caches under this ref")
    parser.add_argument("--keep", type=int, default=2, help="keep N newest per (ref, prefix)")
    parser.add_argument("--access-window", type=int, default=600, help="skip delete if last access within N seconds")
    parser.add_argument("--watermark-gb", type=float, default=8.0, help="watermark threshold in GB (0=off)")
    parser.add_argument("--target-gb", type=float, default=7.0, help="target total after watermark trim in GB")
    args = parser.parse_args()

    if not github_token():
        log("error: GITHUB_TOKEN / GH_TOKEN not set (needs actions: write)")
        return 2

    log(f"repo: {REPO}")
    caches = list_all_caches()
    if args.ref:
        caches = [c for c in caches if (c.get("ref") or "") == args.ref]
    total = sum(c.get("size_in_bytes", 0) for c in caches)
    log(f"caches: {len(caches)} entries, {total / 1024**3:.2f} GB (filter ref={args.ref or 'all'})")

    to_delete, protected = plan(caches, args.keep, args.access_window,
                                args.watermark_gb, args.target_gb, args.ref)

    del_size = sum(c.get("size_in_bytes", 0) for c in to_delete)
    log(f"\n=== 计划删除 {len(to_delete)} 条 ({del_size / 1024**3:.2f} GB) ===")
    for c in sorted(to_delete, key=lambda c: (c.get("ref") or "")):
        log(f"  [del] #{c.get('id')} {((c.get('size_in_bytes') or 0) / 1024**2):.1f}MB | {(c.get('ref') or '')} | {(c.get('key') or '')[:60]} | {c.get('_reason')}")

    if protected:
        log(f"\n=== 跳过 {len(protected)} 条 (并发保护/白名单) ===")
        for c in protected[:20]:
            log(f"  [keep] #{c.get('id')} {((c.get('size_in_bytes') or 0) / 1024**2):.1f}MB | {(c.get('ref') or '')} | {(c.get('key') or '')[:50]} | {c.get('_reason')}")
        if len(protected) > 20:
            log(f"  ... 等 {len(protected) - 20} 条")

    if not args.apply:
        log("\n[dry-run] 未执行删除。加 --apply 执行。")
        return 0

    log("\n=== 执行删除 ===")
    ok = 0
    for c in to_delete:
        if delete_cache(c.get("id")):
            ok += 1
            log(f"  [ok] #{c.get('id')} deleted")
        else:
            log(f"  [fail] #{c.get('id')} delete failed")
    log(f"deleted {ok}/{len(to_delete)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
