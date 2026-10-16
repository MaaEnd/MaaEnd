"""从 zmdmap 下载已发布的精简游戏数据（原 ``fetch-data.mjs``）。

版本接口给出当前数据版本，数据从发布 CDN 按 ``?ver=<版本>`` 拉取；下哪几份由调用方给出——各任务生成器
只要自己用的那几份，就只下那几份，不必被整包牵着走。发布的都是**原始形态**（只带游戏世界坐标），一律
先落不入库的 ``data/raw/``，再由 :mod:`tools.zmdmap.project` 材料化进入库的 ``data/``：需要投影的补上
底图像素，不需要投影的原样复制。

本地已是最新时跳过下载：每份数据的 ``version`` 字段就是「本地是哪一版」，逐份和版本接口给的当前版本比，
一致就不下（版本检查失败时用本地已有的那几份继续）。下载到的文件里若带着 ``sha256``，顺手核对一遍内容
指纹，不符只提示、不当失败（上游算法不一致或缓存串了）。无论下载与否都会跑一次入库。上游还没发布的
数据只提示、不当失败：本地可以用 ``uv run zmdmap generate --only <名字>`` 生成。

下载走 httpx 的共享连接池并发拉取：数据文件各自独立，串行等一个文件会把整包耗时乘上文件数。
CI 上一次运行里同一份文件只会真的下载一次（见 :func:`download`）。
"""

from __future__ import annotations

import json
import os
import sys
import time
from collections.abc import Sequence
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from urllib.parse import urlencode

import httpx

from .constants import DATA_BASE_URL, USER_AGENT, VERSION_API
from .project import (
    HASH_KEY,
    dump_json,
    payload_hash,
    payload_of,
    read_version,
    version_of,
    with_metadata,
)

#: 单次请求超时；连接（含 TLS 握手）单独限时，握手卡住时不必把整个超时耗满。
REQUEST_TIMEOUT = httpx.Timeout(60.0, connect=20.0)
#: 网络层瞬时失败的重试次数（httpx 自带的 retries 只覆盖建连失败）。
REQUEST_ATTEMPTS = 3
#: 重试前的等待秒数，按次数递增。
RETRY_BACKOFF = 2.0
#: 并发下载的连接数；共用同一个连接池。
DOWNLOAD_WORKERS = 4
#: CI 上标记「本次运行已经下过」的文件名前缀，写在 `--out-dir`（默认 ``data/raw/``，不入库）里。
RUN_MARKER_PREFIX = ".fetched-"


class DownloadError(ValueError):
    """一份数据下载失败；``status_code`` 为 ``None`` 表示网络层错误。"""

    def __init__(self, file_name: str, message: str, *, status_code: int | None = None) -> None:
        super().__init__(f"下载 {file_name} 失败: {message}")
        self.file_name = file_name
        self.status_code = status_code


def _new_client() -> httpx.Client:
    """共享连接池的客户端：并发下载复用连接，不必每份数据重新握手。"""
    return httpx.Client(
        headers={"User-Agent": USER_AGENT},
        timeout=REQUEST_TIMEOUT,
        follow_redirects=True,
        transport=httpx.HTTPTransport(retries=2),
    )


def _request(client: httpx.Client, url: str) -> httpx.Response:
    """带瞬时失败重试的 GET：网络层错误与 5xx 都会重试，返回可用的响应。"""
    for attempt in range(REQUEST_ATTEMPTS):
        if attempt:
            time.sleep(RETRY_BACKOFF * attempt)
        try:
            response = client.get(url)
        except httpx.TransportError as error:
            failure: Exception = error
        else:
            if response.status_code < 500:
                return response
            failure = httpx.HTTPStatusError(
                f"{response.status_code} {response.reason_phrase}",
                request=response.request,
                response=response,
            )
    raise failure


def fetch_latest_version() -> str:
    """版本接口返回的当前数据版本。"""
    try:
        with _new_client() as client:
            response = _request(client, VERSION_API)
    except httpx.TransportError as error:
        raise ValueError(f"版本查询失败: {error}") from error
    if response.status_code >= 400:
        raise ValueError(f"版本查询失败: {response.status_code} {response.reason_phrase}")

    try:
        body = json.loads(response.text)
    except json.JSONDecodeError as error:
        raise ValueError(f"无法解析版本接口响应：{error}") from error

    try:
        version = body["data"]["list"][0]["version"]
    except (KeyError, IndexError, TypeError) as error:
        raise ValueError("无法解析版本号，API 返回结构可能已变更") from error
    if not isinstance(version, str) or not version:
        raise ValueError("版本接口没有返回有效版本号")
    return version


def is_data_cached(raw_dir: Path, files: Sequence[str]) -> bool:
    """这几份数据是不是都在本地 ``raw_dir`` 里（下载的都是原始形态）。"""
    for file_name in files:
        try:
            (raw_dir / file_name).read_text(encoding="utf-8")
        except OSError:
            return False
    return True


def _download_json(
    client: httpx.Client,
    file_name: str,
    destination: Path,
    version: str,
    token: str | None,
) -> None:
    params = {"ver": version}
    if token:
        params["t"] = token
    url = f"{DATA_BASE_URL}/{file_name}?{urlencode(params)}"
    print(f"[fetch-data] 下载 {url}", flush=True)
    try:
        response = _request(client, url)
    except httpx.TransportError as error:
        raise DownloadError(file_name, str(error)) from error
    if response.status_code >= 400:
        raise DownloadError(
            file_name,
            f"{response.status_code} {response.reason_phrase}",
            status_code=response.status_code,
        )
    try:
        data = json.loads(response.text)
    except json.JSONDecodeError as error:
        raise DownloadError(file_name, f"不是合法 JSON：{error}") from error

    # 元数据该写在数据文件里：上游还没跟上时（老形态的文件）按这次要下的版本补上，免得每次都得重下；
    # 带了的就核对一下内容指纹，不符说明拿到的不是它自称的那一份（缓存串了 / 上游算错了）。
    payload = payload_of(data)
    if isinstance(data, dict):
        if not version_of(data):
            print(f"[fetch-data] {file_name} 没带 version 字段，按 v{version} 补上", file=sys.stderr)
        digest = payload_hash(payload)
        known = data.get(HASH_KEY)
        if known and known != digest:
            print(
                f"[fetch-data] {file_name} 的 sha256 与内容不符：{known} != {digest}",
                file=sys.stderr,
            )
        data = with_metadata(payload, version_of(data) or version)
    formatted = dump_json(data)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(formatted, encoding="utf-8", newline="\n")
    size_kb = len(formatted.encode("utf-8")) / 1024
    print(f"[fetch-data] 已缓存 {destination} ({size_kb:.0f} KB)", flush=True)


def fetch_and_cache(
    raw_dir: Path,
    version: str,
    projected_files: Sequence[str],
    plain_files: Sequence[str],
    *,
    token: str | None = None,
) -> list[str]:
    """并发按版本把这几份发布的原始数据下载到 ``raw_dir``；返回上游还没发布、被跳过的文件名。"""
    raw_dir.mkdir(parents=True, exist_ok=True)
    failures: dict[str, DownloadError] = {}
    with _new_client() as client, ThreadPoolExecutor(max_workers=DOWNLOAD_WORKERS) as executor:
        pending = {
            file_name: executor.submit(
                _download_json, client, file_name, raw_dir / file_name, version, token
            )
            for file_name in (*projected_files, *plain_files)
        }
        for file_name, future in pending.items():
            try:
                future.result()
            except DownloadError as error:
                failures[file_name] = error

    # 需要投影的数据必须齐全；原样复制的允许上游还没发布（404 时跳过）。
    for file_name in projected_files:
        if file_name in failures:
            raise failures[file_name]

    missing: list[str] = []
    for file_name in plain_files:
        error = failures.get(file_name)
        if error is None:
            continue
        if error.status_code != 404:
            raise error
        print(
            f"[fetch-data] 上游还没发布 {file_name}，跳过"
            f"（本地可用 uv run zmdmap generate 生成）",
            file=sys.stderr,
        )
        missing.append(file_name)

    return missing


def _run_marker(raw_dir: Path, file_name: str) -> Path | None:
    """CI 上同一次运行、单份数据的下载标记；本地没有 ``GITHUB_RUN_ID``，始终返回 ``None``。

    一个 job 会多次调用 fetch（每个 ``generate:*`` 脚本各自一次），而各脚本只要自己那几份数据；标记按
    文件粒度，既让同一个 job 里同一份数据只真的下载一次，也不会让先跑的脚本把后跑脚本要的数据一并跳过。
    标记写在 ``raw_dir``（不入库）里，各 job 各自一份工作副本，互不影响。
    """
    if os.environ.get("GITHUB_ACTIONS") != "true":
        return None
    run_id = os.environ.get("GITHUB_RUN_ID")
    if not run_id:
        return None
    return raw_dir / f"{RUN_MARKER_PREFIX}{run_id}-{file_name}"


def _downloaded_this_run(raw_dir: Path, file_name: str) -> bool:
    """同一个 job 里已经下过这份文件（本地没有 CI 标记，永远是 ``False``）。"""
    marker = _run_marker(raw_dir, file_name)
    return marker is not None and marker.is_file()


def download(
    raw_dir: Path,
    projected_files: Sequence[str],
    plain_files: Sequence[str],
    *,
    force: bool = False,
    cache_bust: bool = False,
) -> str | None:
    """按需下载最新版本里这几份数据到 ``raw_dir``；本地那几份都已经是这一版时返回 ``None``。

    版本就写在数据文件里（``version`` 字段），所以不必另外记一份「本地是哪一版」：文件缺了、没写版本、
    或版本与最新不一致的才真的下载，逐份判断。``cache_bust``（CI 上自动开启）同时意味着强制下载，并在
    请求上追加时间戳绕开远端缓存；CI 上同一个 job 里同一份文件已经下载过时直接跳过：省掉重复下载，也让
    同一次运行里的各步读到同一份数据。
    """
    requested = [*projected_files, *plain_files]
    todo = [file_name for file_name in requested if not _downloaded_this_run(raw_dir, file_name)]
    if requested and not todo:
        print(f"[fetch-data] 本次运行已下载过这 {len(requested)} 份数据，跳过下载")
        return None

    should_cache_bust = cache_bust or os.environ.get("GITHUB_ACTIONS") == "true"
    force = force or should_cache_bust
    token = str(int(time.time() * 1000)) if should_cache_bust else None

    latest_version: str | None = None
    if force:
        print("[fetch-data] 强制下载（--force 或 CI），不比对本地版本")
    else:
        try:
            latest_version = fetch_latest_version()
        except (OSError, ValueError) as error:
            if is_data_cached(raw_dir, todo):
                print(f"[fetch-data] 版本检查失败 ({error})，使用本地缓存", file=sys.stderr)
                return None
            raise
        stale = [
            file_name
            for file_name in todo
            if read_version(raw_dir / file_name) != latest_version
        ]
        if not stale:
            print(f"[fetch-data] 本地已是最新版本 (v{latest_version})，跳过下载")
            return None
        if len(stale) < len(todo):
            current = ", ".join(name for name in todo if name not in stale)
            print(f"[fetch-data] 已是最新，跳过：{current}")
        todo = stale

    version = latest_version or fetch_latest_version()
    pending = set(todo)
    missing = fetch_and_cache(
        raw_dir,
        version,
        [file_name for file_name in projected_files if file_name in pending],
        [file_name for file_name in plain_files if file_name in pending],
        token=token,
    )
    if missing:
        print(f"[fetch-data] 上游还没发布：{', '.join(missing)}", file=sys.stderr)
    print(f"[fetch-data] 完成，当前版本: v{version}")
    for file_name in todo:
        marker = _run_marker(raw_dir, file_name)
        if marker is not None:
            marker.write_text(version, encoding="utf-8", newline="\n")
    return version
