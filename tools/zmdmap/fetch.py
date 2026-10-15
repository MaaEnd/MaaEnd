"""从 zmdmap 下载已发布的精简游戏数据（原 ``fetch-data.mjs``）。

版本接口给出当前数据版本，数据从发布 CDN 按 ``?ver=<版本>`` 拉取。发布的都是**原始形态**
（只带游戏世界坐标），一律先落不入库的 ``data/raw/``，再由 :mod:`tools.zmdmap.project` 材料化进
入库的 ``data/``：需要投影的补上底图像素，不需要投影的原样复制。

本地已是最新版本且文件齐全时跳过下载（版本检查失败也用本地缓存继续），无论下载与否都会跑一次
入库。上游还没发布的数据只提示、不当失败：本地可以用 ``uv run zmdmap generate --only <名字>``
生成。

下载走 httpx 的共享连接池并发拉取：数据文件各自独立，串行等一个文件会把整包耗时乘上文件数。
CI 上一次运行只会真的下载一次（见 :func:`download`）。
"""

from __future__ import annotations

import json
import os
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from urllib.parse import urlencode

import httpx

from .constants import DATA_BASE_URL, USER_AGENT, VERSION_API
from .datasets import COPY_FILES, PROJECT_FILES
from .project import VERSION_FILE, dump_json

#: 发布的数据文件：需要投影的 + 原样复制的。
DATA_FILES: tuple[str, ...] = (*PROJECT_FILES, *COPY_FILES)

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


def read_cached_version(raw_dir: Path) -> str | None:
    try:
        return (raw_dir / VERSION_FILE).read_text(encoding="utf-8").strip()
    except OSError:
        return None


def is_data_cached(raw_dir: Path) -> bool:
    """要下载的数据是不是都在本地 ``raw_dir`` 里（下载的都是原始形态）。"""
    for file_name in DATA_FILES:
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

    formatted = dump_json(data)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(formatted, encoding="utf-8", newline="\n")
    size_kb = len(formatted.encode("utf-8")) / 1024
    print(f"[fetch-data] 已缓存 {destination} ({size_kb:.0f} KB)", flush=True)


def fetch_and_cache(raw_dir: Path, version: str, *, token: str | None = None) -> list[str]:
    """并发按版本把发布的原始数据下载到 ``raw_dir``；返回上游还没发布、被跳过的文件名。"""
    raw_dir.mkdir(parents=True, exist_ok=True)
    failures: dict[str, DownloadError] = {}
    with _new_client() as client, ThreadPoolExecutor(max_workers=DOWNLOAD_WORKERS) as executor:
        pending = {
            file_name: executor.submit(
                _download_json, client, file_name, raw_dir / file_name, version, token
            )
            for file_name in DATA_FILES
        }
        for file_name, future in pending.items():
            try:
                future.result()
            except DownloadError as error:
                failures[file_name] = error

    # 需要投影的数据必须齐全；原样复制的允许上游还没发布（404 时跳过）。
    for file_name in PROJECT_FILES:
        if file_name in failures:
            raise failures[file_name]

    missing: list[str] = []
    for file_name in COPY_FILES:
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

    (raw_dir / VERSION_FILE).write_text(version, encoding="utf-8", newline="\n")
    return missing


def _run_marker(raw_dir: Path) -> Path | None:
    """CI 上同一次运行的下载标记；本地没有 ``GITHUB_RUN_ID``，始终返回 ``None``。

    一个 job 会多次调用 fetch（每个 ``generate:*`` 脚本各自一次），标记用来让同一个 job 里只真的
    下载一次。标记写在 ``raw_dir``（不入库）里，各 job 各自一份工作副本，互不影响。
    """
    if os.environ.get("GITHUB_ACTIONS") != "true":
        return None
    run_id = os.environ.get("GITHUB_RUN_ID")
    if not run_id:
        return None
    return raw_dir / f"{RUN_MARKER_PREFIX}{run_id}"


def download(raw_dir: Path, *, force: bool = False, cache_bust: bool = False) -> str | None:
    """按需下载最新版本到 ``raw_dir``；本地已是最新时返回 ``None``。

    ``cache_bust``（CI 上自动开启）同时意味着强制下载，并在请求上追加时间戳绕开远端缓存。CI 上
    同一个 job 已经下载过时直接跳过：省掉重复的整包下载，也让同一次运行里的各步读到同一份数据。
    """
    marker = _run_marker(raw_dir)
    if marker is not None and marker.is_file():
        print(f"[fetch-data] 本次运行已下载过（{marker.name}），跳过下载")
        return None

    should_cache_bust = cache_bust or os.environ.get("GITHUB_ACTIONS") == "true"
    force = force or should_cache_bust
    token = str(int(time.time() * 1000)) if should_cache_bust else None

    cached_version = read_cached_version(raw_dir)
    data_cached = is_data_cached(raw_dir)
    latest_version: str | None = None

    if cached_version and data_cached and not force:
        try:
            latest_version = fetch_latest_version()
            if latest_version == cached_version:
                print(f"[fetch-data] 本地已是最新版本 (v{cached_version})，跳过下载")
                return None
            print(f"[fetch-data] 发现新版本: v{cached_version} → v{latest_version}")
        except (OSError, ValueError) as error:
            print(f"[fetch-data] 版本检查失败 ({error})，使用本地缓存 v{cached_version}", file=sys.stderr)
            return None
    elif not data_cached:
        print("[fetch-data] 本地缓存不完整，开始下载...")
    elif force:
        print("[fetch-data] 强制模式，重新下载...")
    else:
        print("[fetch-data] 本地无缓存，开始下载...")

    version = latest_version or fetch_latest_version()
    missing = fetch_and_cache(raw_dir, version, token=token)
    if missing:
        print(f"[fetch-data] 上游还没发布：{', '.join(missing)}", file=sys.stderr)
    print(f"[fetch-data] 完成，当前版本: v{version}")
    if marker is not None:
        marker.write_text(version, encoding="utf-8", newline="\n")
    return version
