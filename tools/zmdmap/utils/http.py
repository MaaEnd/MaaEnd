"""HTTP 客户端：连接池、超时与瞬时失败重试（数据下载与 nav 取数共用）。

要发请求的模块都用这里：一次会话复用连接（:func:`new_client`），请求失败按 :data:`REQUEST_ATTEMPTS`
重试（:func:`get`）。超时、重试、UA 这些策略只在这里维护，别处不必自己碰 httpx 的构造参数。

4xx 不重试、直接交给调用方判断（例如 404 = 上游还没发布这一份）；网络层错误与 5xx 重试用完仍失败时抛
最后一次的异常，调用方按 ``httpx.TransportError`` / ``httpx.HTTPStatusError`` 接。
"""

from __future__ import annotations

import time
from collections.abc import Mapping

import httpx

#: 请求头里的 UA：数据由 MaaEnd 自己取，标清楚来源。
USER_AGENT = "MaaEnd-pipeline"

#: 单次请求超时；连接（含 TLS 握手）单独限时，握手卡住时不必把整个超时耗满。
REQUEST_TIMEOUT = httpx.Timeout(60.0, connect=20.0)
#: 应用层的重试次数：网络层错误与 5xx 都重试。
REQUEST_ATTEMPTS = 3
#: 重试前的等待秒数，按第几次重试递增。
RETRY_BACKOFF = 2.0
#: 传输层（建连）重试次数，交给 httpx 自己做；与应用层的 :data:`REQUEST_ATTEMPTS` 不是一回事。
TRANSPORT_RETRIES = 2


def new_client() -> httpx.Client:
    """共享连接池的客户端：并发下载复用连接，不必每份数据重新握手。"""
    return httpx.Client(
        headers={"User-Agent": USER_AGENT},
        timeout=REQUEST_TIMEOUT,
        follow_redirects=True,
        transport=httpx.HTTPTransport(retries=TRANSPORT_RETRIES),
    )


def get(
    client: httpx.Client, url: str, headers: Mapping[str, str] | None = None
) -> httpx.Response:
    """带瞬时失败重试的 GET：网络层错误与 5xx 都会重试，返回可用的响应。

    4xx 立刻返回（不算失败，由调用方决定怎么办）；重试用完仍失败时抛最后一次的异常。``headers``
    是这次请求额外的头（例如 ``Range`` / ``Accept``）；UA 之类共用的那份由 :func:`new_client` 统一设。
    """
    for attempt in range(REQUEST_ATTEMPTS):
        if attempt:
            time.sleep(RETRY_BACKOFF * attempt)
        try:
            response = client.get(url, headers=headers)
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
