"""中途停下：按 Ctrl+C 时打断当前 maa 任务，而不是干等它自己跑完。"""

from __future__ import annotations

import time
from collections.abc import Callable
from typing import Protocol

# 轮询间隔：够短，按下去几十毫秒内就有反应；够长，不至于空转占满一个核。
INTERVAL = 0.05

_requested = False


class Cancelled(Exception):
    """用户按 Ctrl+C 要求停下。"""


class JobLike(Protocol):
    """能查询是否跑完的作业；maa 的任务、截图、资源加载都满足。"""

    @property
    def done(self) -> bool: ...


def request() -> None:
    """记下「要停」，信号处理函数里只能做这个。"""
    global _requested
    _requested = True


def requested() -> bool:
    return _requested


def reset() -> None:
    """停下之后要接着跑（比如 --resume）时清掉标记。"""
    global _requested
    _requested = False


def wait_job[JobT: JobLike](job: JobT, *, stop: Callable[[], object] | None = None) -> JobT:
    """等作业完成；中途按了 Ctrl+C 就先停掉当前任务，再抛 Cancelled。

    不能直接 job.wait()：它底层是 C 的阻塞调用，不返回的话 Python 的信号处理函数
    根本没机会执行，看起来就是按了没反应。改成轮询 job.done，中间能跑信号处理，
    发现要停就调用 stop()（一般是 tasker.post_stop）把当前任务打断。
    """
    while not job.done:
        if _requested:
            if stop is not None:
                stop()
            raise Cancelled()
        time.sleep(INTERVAL)
    return job
