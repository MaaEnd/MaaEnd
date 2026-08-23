"""
检查 agent/cpp-algo/MaaUtils submodule 是否与 MaaFramework 发布版锁定的 commit 一致。

cpp-algo 从源码编译 MaaUtils（agent/cpp-algo/MaaUtils），并链接 install 工作流下载的
MaaFramework 预编译包。MaaFramework 自身通过其 source/MaaUtils submodule 锁定了一个
精确的 MaaUtils commit，两边的 pin 一旦脱节就会导致构建失败。

本脚本将本地 gitlink SHA 与指定 MaaFramework ref 锁定的 commit 比较，
并通过 git 祖先关系归类：

    synced    与上游锁定 commit 完全一致
    ahead     是上游锁定 commit 的后代（有意的提前 bump，允许）
    behind    是上游锁定 commit 的祖先 -> 需要更新
    diverged  互不为祖先 -> 按 behind 同样处理（正常流程不会出现）
    unknown   基础设施瞬时故障（网络/API），永不作为拦截构建的依据

退出码：
    0  synced / ahead
    1  behind
    2  diverged
    3  上游布局异常（tag 不存在、submodule 路径改名等）-> 需人工介入
    4  unknown（重试后仍失败）-> 调用方只告警不拦截

所有网络与 git-fetch 操作均带指数退避重试；重试耗尽后归类为 unknown（退出码 4），
仅 404 等确定性 API 错误映射为退出码 3。

用法：
    python tools/check_maautils_sync.py --maafw-ref v5.12.3
    python tools/check_maautils_sync.py --resolve-latest
"""

from __future__ import annotations

import argparse
import json
import os
import random
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

MAAFW_REPO = "MaaXYZ/MaaFramework"
MAAFW_SUBMODULE_PATH = "source/MaaUtils"
DEFAULT_API_BASE = "https://api.github.com"
DEFAULT_MAAUTILS_URL = "https://github.com/MaaXYZ/MaaUtils.git"

EXIT_OK = 0  # synced 或 ahead
EXIT_BEHIND = 1
EXIT_DIVERGED = 2
EXIT_UPSTREAM_BROKEN = 3
EXIT_UNKNOWN = 4


class FatalUpstreamError(Exception):
    """确定性的上游侧问题（404、响应结构不符合预期）。"""


class InfrastructureError(Exception):
    """重试后仍然存在的瞬态问题。"""


def log(msg: str) -> None:
    try:
        sys.stdout.buffer.write((msg + "\n").encode("utf-8"))
        sys.stdout.buffer.flush()
    except OSError:
        # 下游管道提前关闭导致 stdout 不可写；日志失败不能掩盖真实结论
        pass


def github_token() -> str:
    return os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN") or ""


def request_json(url: str, attempts: int, base_delay: float) -> "dict | list":
    """从 GitHub API GET 一个 JSON 文档，带重试与指数退避。"""
    headers = {
        "Accept": "application/vnd.github+json",
        "User-Agent": "maaend-maautils-sync-check",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    token = github_token()
    if token:
        headers["Authorization"] = f"Bearer {token}"

    last_error: Exception | None = None
    for attempt in range(1, attempts + 1):
        try:
            req = urllib.request.Request(url, headers=headers)
            with urllib.request.urlopen(req, timeout=30) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            if e.code == 404:
                raise FatalUpstreamError(f"404 Not Found: {url}") from None
            retry_after = e.headers.get("Retry-After") if e.headers else None
            rate_limited = e.code == 429 or (
                e.code == 403 and e.headers and e.headers.get("x-ratelimit-remaining") == "0"
            )
            if rate_limited:
                # 限流时优先遵循 Retry-After 头，等待时间不计入常规退避节奏
                wait = float(retry_after) if retry_after else base_delay * (2 ** attempt)
                log(f"[retry] rate limited (HTTP {e.code}), waiting {wait:.0f}s before attempt {attempt + 1}/{attempts}")
                time.sleep(wait)
                last_error = e
                continue
            if 500 <= e.code < 600:
                last_error = e
            else:
                raise FatalUpstreamError(f"HTTP {e.code}: {url}") from None
        except (urllib.error.URLError, TimeoutError, ConnectionError, json.JSONDecodeError, OSError) as e:
            last_error = e

        delay = base_delay * (2 ** (attempt - 1)) + random.uniform(0, 1)
        log(f"[retry] attempt {attempt}/{attempts} failed: {last_error}; retrying in {delay:.1f}s")
        time.sleep(delay)

    raise InfrastructureError(f"GitHub API unreachable after {attempts} attempts: {url} ({last_error})")


def resolve_latest_maafw(api_base: str, attempts: int, base_delay: float) -> str:
    """取最新的非 draft release tag（语义与 install.yml 的版本解析器一致）。"""
    data = request_json(f"{api_base}/repos/{MAAFW_REPO}/releases?per_page=30", attempts, base_delay)
    for rel in data:
        if not rel.get("draft") and rel.get("tag_name"):
            return str(rel["tag_name"])
    raise FatalUpstreamError(f"No non-draft release found for {MAAFW_REPO}")


def get_expected_sha(api_base: str, maafw_ref: str, attempts: int, base_delay: float) -> str:
    """获取指定 MaaFramework ref 锁定的 MaaUtils commit。"""
    url = f"{api_base}/repos/{MAAFW_REPO}/contents/{MAAFW_SUBMODULE_PATH}?ref={urllib.parse.quote(maafw_ref)}"
    try:
        data = request_json(url, attempts, base_delay)
    except FatalUpstreamError as e:
        raise FatalUpstreamError(
            f"{e}; check that MaaFramework ref '{maafw_ref}' exists and that its "
            f"'{MAAFW_SUBMODULE_PATH}' submodule path has not been renamed"
        ) from None
    if data.get("type") != "submodule" or not data.get("sha"):
        raise FatalUpstreamError(
            f"Unexpected payload for '{MAAFW_SUBMODULE_PATH}' at '{maafw_ref}': type={data.get('type')!r}"
        )
    return str(data["sha"])


def run_git(args: list[str], cwd: str | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(["git", *args], cwd=cwd, capture_output=True, text=True)


def get_local_gitlink(submodule_path: str) -> str:
    """读取 HEAD 中记录的 gitlink SHA，无需初始化 submodule。"""
    proc = run_git(["ls-tree", "HEAD", "--", submodule_path])
    if proc.returncode != 0:
        raise InfrastructureError(f"git ls-tree failed: {proc.stderr.strip()}")
    m = re.match(r"^[0-9]+ commit ([0-9a-f]{40})\b", proc.stdout.strip())
    if not m:
        raise FatalUpstreamError(f"'{submodule_path}' is not a gitlink in HEAD; is the path still correct?")
    return m.group(1)


def fetch_commits(maautils_url: str, shas: list[str], attempts: int, base_delay: float) -> None:
    """把任意 commit 从 MaaUtils 远端拉取到本地对象库（GitHub 支持按可达 SHA fetch）。"""
    last_error = ""
    for attempt in range(1, attempts + 1):
        proc = run_git(["fetch", "--no-tags", maautils_url, *shas])
        if proc.returncode == 0:
            return
        last_error = proc.stderr.strip().splitlines()[-1] if proc.stderr.strip() else f"exit {proc.returncode}"
        delay = base_delay * (2 ** (attempt - 1)) + random.uniform(0, 1)
        log(f"[retry] git fetch attempt {attempt}/{attempts} failed: {last_error}; retrying in {delay:.1f}s")
        time.sleep(delay)
    raise InfrastructureError(f"git fetch failed after {attempts} attempts: {last_error}")


def is_ancestor(a: str, b: str) -> bool:
    """当且仅当 commit a 是 commit b 的祖先（或相等）时返回 True。"""
    proc = run_git(["merge-base", "--is-ancestor", a, b])
    if proc.returncode == 0:
        return True
    if proc.returncode == 1:
        return False
    raise InfrastructureError(f"git merge-base --is-ancestor failed: {proc.stderr.strip()}")


def classify(local_sha: str, expected_sha: str) -> str:
    if local_sha == expected_sha:
        return "synced"
    if is_ancestor(local_sha, expected_sha):
        return "behind"
    if is_ancestor(expected_sha, local_sha):
        return "ahead"
    return "diverged"


def validate_sha(sha: str, label: str) -> str:
    # SHA 会写入 GITHUB_OUTPUT 并作为 git 参数，必须确保是合法的 40 位十六进制，
    # 防止异常/恶意响应通过换行符向输出文件注入伪造键值对
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise FatalUpstreamError(f"Illegal {label} (expect 40-hex): {sha[:80]!r}")
    return sha


def emit_outputs(mapping: dict[str, str]) -> None:
    lines = [f"{key}={value}" for key, value in mapping.items()]
    for line in lines:
        log(line)
    output_file = os.environ.get("GITHUB_OUTPUT")
    if output_file:
        with open(output_file, "a", encoding="utf-8") as f:
            f.write("\n".join(lines) + "\n")


def _build_parser() -> _SafeArgumentParser:
    parser = _SafeArgumentParser(description=__doc__.splitlines()[1])
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--maafw-ref", help="MaaFramework release tag to compare against")
    group.add_argument("--resolve-latest", action="store_true", help="Resolve the latest MaaFramework release")
    parser.add_argument("--github-api", default=DEFAULT_API_BASE, help="GitHub API base URL")
    parser.add_argument("--maautils-url", default=DEFAULT_MAAUTILS_URL, help="MaaUtils git remote URL")
    parser.add_argument("--submodule-path", default="agent/cpp-algo/MaaUtils", help="Local submodule path")
    parser.add_argument("--local-sha", help="Override local gitlink SHA (for testing)")
    parser.add_argument("--attempts", type=int, default=4, help="Retry attempts for network operations")
    parser.add_argument("--base-delay", type=float, default=2.0, help="Base delay in seconds for exponential backoff")
    return parser

class _SafeArgumentParser(argparse.ArgumentParser):
    """把用法错误转为异常而非直接退出。

    argparse 默认在参数错误时以 exit 2 终止，会与 EXIT_DIVERGED 撞码，
    导致调用方误判为"分叉"。这里改为抛异常，由入口统一归类为 unknown。
    """

    def error(self, message: str) -> None:
        raise InfrastructureError(f"argument error: {message}")


def main() -> int:
    args = _build_parser().parse_args()

    try:
        maafw_ref = args.maafw_ref or resolve_latest_maafw(args.github_api, args.attempts, args.base_delay)
        log(f"MaaFramework ref: {maafw_ref}")

        expected_sha = validate_sha(
            get_expected_sha(args.github_api, maafw_ref, args.attempts, args.base_delay), "upstream pin"
        )
        local_sha = validate_sha(args.local_sha or get_local_gitlink(args.submodule_path), "local gitlink")
        log(f"Expected MaaUtils commit: {expected_sha}")
        log(f"Current MaaUtils commit:  {local_sha}")

        # fetch 失败时无法判断祖先关系；归为 unknown，保证调用方绝不因基础设施抖动误拦构建
        try:
            fetch_commits(args.maautils_url, [local_sha, expected_sha], args.attempts, args.base_delay)
        except InfrastructureError as e:
            log(f"[warn] cannot verify ancestry: {e}")
            emit_outputs(
                {
                    "status": "unknown",
                    "maafw_ref": maafw_ref,
                    "expected_sha": expected_sha,
                    "current_sha": local_sha,
                }
            )
            return EXIT_UNKNOWN

        status = classify(local_sha, expected_sha)

        if status == "synced":
            log(f"OK: MaaUtils pin matches the pin of MaaFramework {maafw_ref}")
        elif status == "ahead":
            log(f"OK: MaaUtils pin is ahead of the pin of MaaFramework {maafw_ref} (intentional forward bump)")
        elif status == "behind":
            log(f"OUT OF SYNC: MaaUtils pin is BEHIND the pin of MaaFramework {maafw_ref}; update required")
        else:
            log(f"OUT OF SYNC: MaaUtils pin DIVERGED from the pin of MaaFramework {maafw_ref}; human review required")

        emit_outputs(
            {
                "status": status,
                "maafw_ref": maafw_ref,
                "expected_sha": expected_sha,
                "current_sha": local_sha,
            }
        )
        return {
            "synced": EXIT_OK,
            "ahead": EXIT_OK,
            "behind": EXIT_BEHIND,
            "diverged": EXIT_DIVERGED,
        }[status]

    except FatalUpstreamError as e:
        log(f"[error] upstream layout problem: {e}")
        emit_outputs({"status": "upstream-broken"})
        return EXIT_UPSTREAM_BROKEN
    except InfrastructureError as e:
        log(f"[warn] transient infrastructure failure, status unknown: {e}")
        emit_outputs({"status": "unknown"})
        return EXIT_UNKNOWN


if __name__ == "__main__":
    try:
        rc = main()
    except Exception as e:  # noqa: BLE001 - 入口兜底：未捕获异常的默认退出码会撞 EXIT_BEHIND，必须归类为 unknown
        log(f"[warn] unexpected internal error: {e!r}")
        rc = EXIT_UNKNOWN
    sys.exit(rc)
