"""从 zmdmap 下载已发布的精简游戏数据（原 ``fetch-data.mjs``）。

版本接口给出当前数据版本，数据从发布 CDN 按 ``?ver=<版本>`` 拉取。发布的都是**原始形态**
（只带游戏世界坐标），一律先落不入库的 ``data/raw/``，再由 :mod:`tools.zmdmap.project` 材料化进
入库的 ``data/``：需要投影的补上底图像素，不需要投影的原样复制。

本地已是最新版本且文件齐全时跳过下载（版本检查失败也用本地缓存继续），无论下载与否都会跑一次
入库。上游还没发布的数据只提示、不当失败：本地可以用 ``uv run zmdmap generate --only <名字>``
生成。
"""

from __future__ import annotations

import json
import os
import sys
import time
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen

from .constants import DATA_BASE_URL, USER_AGENT, VERSION_API
from .datasets import COPY_FILES, PROJECT_FILES
from .project import VERSION_FILE, dump_json

#: 发布的数据文件：需要投影的 + 原样复制的。
DATA_FILES: tuple[str, ...] = (*PROJECT_FILES, *COPY_FILES)


def _get(url: str) -> bytes:
    request = Request(url, headers={"User-Agent": USER_AGENT})
    with urlopen(request, timeout=60) as response:
        return response.read()


def fetch_latest_version() -> str:
    """版本接口返回的当前数据版本。"""
    try:
        body = json.loads(_get(VERSION_API).decode("utf-8"))
    except HTTPError as error:
        raise ValueError(f"版本查询失败: {error.code} {error.reason}") from error
    except URLError as error:
        raise ValueError(f"版本查询失败: {error.reason}") from error
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
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


def _download_json(file_name: str, destination: Path, version: str, token: str | None) -> None:
    params = {"ver": version}
    if token:
        params["t"] = token
    url = f"{DATA_BASE_URL}/{file_name}?{urlencode(params)}"
    print(f"[fetch-data] 下载 {url}")
    text = _get(url).decode("utf-8")
    try:
        data = json.loads(text)
    except json.JSONDecodeError as error:
        raise ValueError(f"{file_name} 不是合法 JSON：{error}") from error

    formatted = dump_json(data)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(formatted, encoding="utf-8", newline="\n")
    size_kb = len(formatted.encode("utf-8")) / 1024
    print(f"[fetch-data] 已缓存 {destination} ({size_kb:.0f} KB)")


def fetch_and_cache(raw_dir: Path, version: str, *, token: str | None = None) -> list[str]:
    """按版本把发布的原始数据下载到 ``raw_dir``；返回上游还没发布、被跳过的文件名。"""
    raw_dir.mkdir(parents=True, exist_ok=True)
    for file_name in PROJECT_FILES:
        try:
            _download_json(file_name, raw_dir / file_name, version, token)
        except HTTPError as error:
            raise ValueError(f"下载 {file_name} 失败: {error.code} {error.reason}") from error
        except URLError as error:
            raise ValueError(f"下载 {file_name} 失败: {error.reason}") from error

    missing: list[str] = []
    for file_name in COPY_FILES:
        try:
            _download_json(file_name, raw_dir / file_name, version, token)
        except HTTPError as error:
            if error.code != 404:
                raise ValueError(f"下载 {file_name} 失败: {error.code} {error.reason}") from error
            print(
                f"[fetch-data] 上游还没发布 {file_name}，跳过"
                f"（本地可用 uv run zmdmap generate 生成）",
                file=sys.stderr,
            )
            missing.append(file_name)
        except URLError as error:
            raise ValueError(f"下载 {file_name} 失败: {error.reason}") from error

    (raw_dir / VERSION_FILE).write_text(version, encoding="utf-8", newline="\n")
    return missing


def download(raw_dir: Path, *, force: bool = False, cache_bust: bool = False) -> str | None:
    """按需下载最新版本到 ``raw_dir``；本地已是最新时返回 ``None``。

    ``cache_bust``（CI 上自动开启）同时意味着强制下载，并在请求上追加时间戳绕开远端缓存。
    """
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
    return version
