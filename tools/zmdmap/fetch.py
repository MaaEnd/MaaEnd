"""从 zmdmap 下载已发布的精简游戏数据。

上游发布的清单（``manifest.json``）给出这批数据是哪一版、每份原始形态的内容指纹，数据按
``?ver=<版本>&t=<指纹前 8 位>`` 从发布 CDN 拉取；清单还没发布时退回版本接口，按文件里的 ``version``
逐份比（版本接口也查不到、而本地那几份齐时，就先用本地的）。下哪几份由调用方给出 —— 各任务生成器只要自己用的那几份，就只下那几份，不必被整包牵着走。

本地已是最新时跳过下载：逐份比内容指纹，一样的跳过（版本号变了就按清单改写文件里的 ``version``），
不一样的才下，下完核对一遍拿到的内容（不符只提示、不当失败）；本地与 CI 跑的是同一套判断。清单本身每次
都带时间戳取 —— 它只有几百字节，却决定了整批判断，被远端缓存住就会拿旧索引判断。无论下载与否都会跑一次
入库。上游还没发布的数据只提示、不当失败：本地可以用 ``uv run zmdmap generate --only <名字>`` 生成。

发布的都是**原始形态**（只带游戏世界坐标），一律先落不入库的 ``data/raw/``，再由
:mod:`tools.zmdmap.project` 材料化进入库的 ``data/``：需要投影的补上底图像素，不需要投影的原样复制。

下载走 httpx 的共享连接池并发拉取：数据文件各自独立，串行等一个文件会把整包耗时乘上文件数。
"""

from __future__ import annotations

import json
import sys
import time
from collections.abc import Sequence
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlencode

import httpx

from .constants import DATA_BASE_URL, MANIFEST_FILE, USER_AGENT, VERSION_API
from .project import (
    HASH_KEY,
    dump_json,
    payload_hash,
    payload_of,
    read_payload_hash,
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


@dataclass
class Manifest:
    """上游发布的清单：这批数据是哪一版，以及每份原始形态的内容指纹。"""

    version: str | None
    files: dict[str, str]


def parse_manifest(text: str) -> Manifest | None:
    """解析清单；形状不对就当没清单（上游还是老形态，或者拿到的是错误页）。"""
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return None
    if not isinstance(data, dict):
        return None
    files = data.get("files")
    if not isinstance(files, dict):
        return None
    return Manifest(version_of(data), {name: value for name, value in files.items() if isinstance(value, str)})


def read_manifest() -> Manifest | None:
    """取上游清单；没发布（404）或形状不对时返回 ``None``，由调用方退回版本接口。

    清单决定了「后面下哪几份」，被远端缓存住就等于拿旧索引判断，所以每次带一个时间戳当 URL 的一部分。
    实测这个 CDN 只看 URL：请求头带 ``Cache-Control: no-cache`` 照样命中缓存（``Age`` 不变），换 URL 才真的
    回源。只有它需要这样 —— 数据文件的 URL 自带清单给的内容指纹（``t=<指纹前 8 位>``），本来就随内容变。
    """
    # 清单只有几百字节，每次都回源不心疼；它错了会连带整批判断都错。
    token = urlencode({"t": int(time.time() * 1000)})
    try:
        with _new_client() as client:
            response = _request(client, f"{DATA_BASE_URL}/{MANIFEST_FILE}?{token}")
    except httpx.HTTPError as error:
        # 5xx 重试完仍失败也走这条路：取不到清单就退回版本接口，不让清单的问题挡住整次 fetch。
        print(f"[fetch-data] 取清单失败 ({error})", file=sys.stderr)
        return None
    if response.status_code >= 400:
        return None
    return parse_manifest(response.text)


def manifest_plan(
    raw_dir: Path, file_names: Sequence[str], manifest: Manifest
) -> tuple[list[str], list[str]]:
    """按清单把这批文件分成「内容变了，要重下」与「内容一样，只需按清单改写版本」两组。

    本地那份算不算一样，比的是**原始形态的本体指纹**（清单记的就是它）：没有文件、读不出来、清单里没有
    这一份、指纹对不上的，都算要重下。
    """
    stale: list[str] = []
    restamp: list[str] = []
    for file_name in file_names:
        expected = manifest.files.get(file_name)
        local = read_payload_hash(raw_dir / file_name)
        if expected is None or local is None or local != expected:
            stale.append(file_name)
        else:
            restamp.append(file_name)
    return stale, restamp


def _download_json(
    client: httpx.Client,
    file_name: str,
    destination: Path,
    version: str,
    token: str | None,
    expect: str | None = None,
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

    # 元数据该写在数据文件里：版本按这次发布的版本写上，内容指纹按收到的本体重算。
    # 清单或文件自称的指纹都对一遍，不符说明拿到的不是它该有的那一份（远端缓存串了 / 上游算错了）。
    payload = payload_of(data)
    digest = payload_hash(payload)
    if expect and digest != expect:
        print(
            f"[fetch-data] {file_name} 与清单不符：清单 {expect}，拿到 {digest}"
            "（远端缓存串了？可以加 --force 再试）",
            file=sys.stderr,
        )
    if isinstance(data, dict):
        if not version_of(data):
            print(f"[fetch-data] {file_name} 没带 version 字段，按 v{version} 补上", file=sys.stderr)
        known = data.get(HASH_KEY)
        if known and known != digest:
            print(
                f"[fetch-data] {file_name} 自称的 sha256 与内容不符：{known} != {digest}",
                file=sys.stderr,
            )
        data = with_metadata(payload, version)
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
    hashes: dict[str, str] | None = None,
) -> list[str]:
    """并发把这几份发布的原始数据下载到 ``raw_dir``；返回上游还没发布、被跳过的文件名。

    ``hashes`` 是清单给的每份内容指纹：有指纹的用指纹前 8 位当 ``t`` 参数 —— 同一份内容的 URL 稳定、内容
    一变就是新 URL，绕开远端缓存。``token`` 给了就替掉指纹（``--force`` 时是时间戳），让这几份一律回源。
    """
    hashes = hashes or {}
    raw_dir.mkdir(parents=True, exist_ok=True)
    failures: dict[str, DownloadError] = {}
    with _new_client() as client, ThreadPoolExecutor(max_workers=DOWNLOAD_WORKERS) as executor:
        pending = {
            file_name: executor.submit(
                _download_json,
                client,
                file_name,
                raw_dir / file_name,
                version,
                token or hashes.get(file_name, "")[:8],
                hashes.get(file_name),
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


def _restamp(path: Path, version: str) -> None:
    """内容与清单一致、只有版本号变了：只改写文件里的元数据，不用重下。"""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return
    if version_of(data) == version:
        return
    path.write_text(dump_json(with_metadata(data, version)), encoding="utf-8", newline="\n")
    print(f"[fetch-data] {path.name} 内容一致，版本改写为 v{version}")


def download(
    raw_dir: Path,
    projected_files: Sequence[str],
    plain_files: Sequence[str],
    *,
    force: bool = False,
) -> str | None:
    """按需下载最新版本里这几份数据到 ``raw_dir``；本地那几份都已经是这一版时返回 ``None``。

    有清单时以清单为准：逐份比内容指纹，一样的跳过下载（只是版本号变了就按清单改写 ``version``），不一样
    的才下 —— 这样不必问版本接口，也认得「同一个版本号下重新发布过」。清单还没发布时退回版本接口，按文件
    里的 ``version`` 逐份比（版本检查失败就用本地已有的那几份）。

    ``force`` 是不比对、直接重下这几份，并且请求 URL 上的 ``t`` 用时间戳而不是清单指纹 —— 「怀疑远端说辞」
    时的唯一手段（这个 CDN 只看 URL，请求头说了不算）。CI 上没有额外开关：本地与 CI 跑的是同一套判断。
    """
    todo = [*projected_files, *plain_files]
    token = str(int(time.time() * 1000)) if force else None

    manifest = read_manifest()
    version = manifest.version if manifest is not None else None
    if version is None:
        # 清单还没发布、或清单里没写版本：退回版本接口；查不到而本地那几份又齐，就先用本地的。
        try:
            version = fetch_latest_version()
        except (OSError, ValueError) as error:
            if not force and is_data_cached(raw_dir, todo):
                print(f"[fetch-data] 版本查询失败 ({error})，使用本地缓存", file=sys.stderr)
                return None
            raise
        if manifest is None:
            print(
                f"[fetch-data] 上游还没发布 {MANIFEST_FILE}，退回版本接口 (v{version})",
                file=sys.stderr,
            )

    hashes = manifest.files if manifest is not None else {}
    restamp: list[str] = []
    if force:
        print("[fetch-data] 强制下载（--force），不比对本地内容")
    elif manifest is not None:
        todo, restamp = manifest_plan(raw_dir, todo, manifest)
        if todo:
            print(f"[fetch-data] 清单 v{version}：{len(todo)} 份内容有变化")
        else:
            print(f"[fetch-data] 本地内容与清单一致 (v{version})，跳过下载")
            if not restamp:
                return None
    else:
        stale = [
            file_name for file_name in todo if read_version(raw_dir / file_name) != version
        ]
        if not stale:
            print(f"[fetch-data] 本地已是最新版本 (v{version})，跳过下载")
            return None
        if len(stale) < len(todo):
            current = ", ".join(name for name in todo if name not in stale)
            print(f"[fetch-data] 已是最新，跳过：{current}")
        todo = stale

    for file_name in restamp:
        _restamp(raw_dir / file_name, version)

    missing: list[str] = []
    if todo:
        pending = set(todo)
        missing = fetch_and_cache(
            raw_dir,
            version,
            [file_name for file_name in projected_files if file_name in pending],
            [file_name for file_name in plain_files if file_name in pending],
            token=token,
            hashes=hashes,
        )
        if missing:
            print(f"[fetch-data] 上游还没发布：{', '.join(missing)}", file=sys.stderr)
    print(f"[fetch-data] 完成，当前版本: v{version}")
    return version
