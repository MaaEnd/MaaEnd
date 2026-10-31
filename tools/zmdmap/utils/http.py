"""HTTP 请求与重试：下载数据、下载 nav、下载图标都走这里。

两层：

- :func:`get` 只负责一次带重试的 GET —— 网络故障与 5xx 重试，4xx 直接返回给调用方判断（例如 404 = 上游
  还没发布这一份）。超时、UA、重试次数与间隔都在这里定，别处写请求时不必自己再套一层重试。
- :func:`download` 在它上面补一件常做的事：把内容落到文件。带条件的请求（``ETag`` / ``Last-Modified``）
  记住上次的响应，来源没变时服务端回 304，就不必重新下。
"""

import hashlib
import json
import time
from collections.abc import Callable, Mapping
from pathlib import Path
from typing import Any

import requests

#: 请求头里的 UA：数据由 MaaEnd 自己取，标清楚来源。
USER_AGENT = "MaaEnd-pipeline"

#: 单次请求的超时秒数；连接与读响应各按这个值限时。
REQUEST_TIMEOUT = 60.0
#: 总尝试次数：网络故障与 5xx 都算失败，到这个次数为止。
REQUEST_ATTEMPTS = 3
#: 重试前等待的秒数，按第几次重试递增（第一次失败等 2 秒，第二次等 4 秒）。
RETRY_BACKOFF = 2.0


def get(
    url: str, headers: Mapping[str, str] | None = None, *, timeout: float = REQUEST_TIMEOUT
) -> requests.Response:
    """带重试的 GET：网络故障与 5xx 会重试，返回可用的响应。

    4xx 立刻返回，不算失败，由调用方决定怎么办（``fetch`` 把 404 当成「上游还没发布这份」）。``headers``
    是这次请求额外的头，例如 ``Range`` / ``If-None-Match``；UA 在这里统一加。重试用完仍失败时抛最后一次
    的异常，都是 :class:`requests.RequestException` 的子类。
    """
    merged = {**(headers or {}), "User-Agent": USER_AGENT}
    failure: Exception = RuntimeError(f"没有发起请求：{url}")
    for attempt in range(REQUEST_ATTEMPTS):
        if attempt:
            time.sleep(RETRY_BACKOFF * attempt)
        try:
            response = requests.get(url, headers=merged, timeout=timeout)
        except requests.RequestException as error:
            failure = error
        else:
            if response.status_code < 500:
                return response
            failure = requests.HTTPError(f"{response.status_code} {response.reason}", response=response)
    raise failure


def metadata_path(destination: Path) -> Path:
    """某个已下载文件的元数据放在它旁边的 ``.meta.json``，换目录搬家时要连它一起搬。"""
    return destination.with_suffix(destination.suffix + ".meta.json")


def download(
    url: str,
    destination: Path,
    *,
    timeout: float = REQUEST_TIMEOUT,
    validate: Callable[[bytes], Any] | None = None,
    reuse_existing: Callable[[Path], bool] | None = None,
) -> dict[str, Any]:
    """把 ``url`` 的内容下载到 ``destination``，返回这次的元数据。

    上次下过同一地址时带上 ``If-None-Match`` / ``If-Modified-Since``，服务端回 304 就直接返回上次的元数据，
    不重下也不改文件。``validate`` 在落盘前检查内容（例如核对 PNG 头），抛异常即视为这份不可用；
    ``reuse_existing`` 判断本地那份还能不能用 —— 说不能就跳过条件请求、重新下，免得 304 把坏文件留在原地。
    下载失败抛 :class:`requests.HTTPError`（404 之类由调用方决定怎么算）。
    """
    destination.parent.mkdir(parents=True, exist_ok=True)
    meta_path = metadata_path(destination)
    metadata: dict[str, Any] = (
        json.loads(meta_path.read_text(encoding="utf-8")) if meta_path.is_file() else {}
    )
    headers: dict[str, str] = {}
    reusable = destination.is_file() and metadata.get("url") == url
    if reusable and reuse_existing is not None:
        reusable = reuse_existing(destination)
    if reusable:
        for source_key, header in (
            ("etag", "If-None-Match"),
            ("lastModified", "If-Modified-Since"),
        ):
            if isinstance(metadata.get(source_key), str):
                headers[header] = metadata[source_key]
    response = get(url, headers, timeout=timeout)
    if response.status_code == 304 and destination.is_file():
        return metadata
    response.raise_for_status()
    content = response.content
    if validate is not None:
        validate(content)
    result: dict[str, Any] = {
        "url": url,
        "bytes": len(content),
        "sha256": hashlib.sha256(content).hexdigest(),
        "updatedAt": int(time.time()),
    }
    for source_key, output_key in (("ETag", "etag"), ("Last-Modified", "lastModified")):
        if response.headers.get(source_key):
            result[output_key] = response.headers[source_key]

    temporary = destination.with_suffix(destination.suffix + ".part")
    try:
        temporary.write_bytes(content)
        temporary.replace(destination)
        meta_path.write_text(json.dumps(result, ensure_ascii=False, indent=4) + "\n", encoding="utf-8")
    finally:
        temporary.unlink(missing_ok=True)
    return result
