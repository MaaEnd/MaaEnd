"""命令行输出：进度写标准输出，提示与失败写标准错误。

产物都是文件，标准输出上没有别的东西，所以进度可以放心写在那里；提示和失败走标准错误，CI 里会标红，
通过管道读结果的地方也不会把它们当成结果。

用法是 ``logger.info(...)`` / ``logger.warning(...)`` / ``logger.debug(...)``，前缀统一由这里加，调用处
不必自己写 ``[xxx]``。排查时设 ``ZMDMAP_DEBUG=1`` 再跑，就能看到 ``debug`` 那几条。
"""

import os
import sys
import unicodedata

#: 每条输出前面的标记：命令行里只有这一个工具，具体是哪一步看消息本身。
PREFIX = "[zmdmap]"

#: 是否打印 ``debug``：默认关掉，排查时设 ``ZMDMAP_DEBUG=1``。
DEBUG_ENABLED = os.environ.get("ZMDMAP_DEBUG", "") not in ("", "0")

#: 分节横幅的总显示宽度；中文按两格算，见 :func:`_display_width`。
SECTION_WIDTH = 50


def _display_width(text: str) -> int:
    """终端里占几格：全角/宽字符算两格，其余一格。"""
    return sum(2 if unicodedata.east_asian_width(char) in ("W", "F") else 1 for char in text)


class _Logger:
    """把进度、提示、细节分别送到该去的地方。"""

    def info(self, message: str) -> None:
        """正常进度。"""
        print(f"{PREFIX} {message}", flush=True)

    def section(self, title: str) -> None:
        """一条分节横幅，如 ``[zmdmap] ── 汇总 ────…``。"""
        head = f"── {title} "
        print(f"{PREFIX} {head}{'─' * max(0, SECTION_WIDTH - _display_width(head))}", flush=True)

    def warning(self, message: str) -> None:
        """还能继续、但要让人知道的情况。"""
        print(f"{PREFIX} {message}", file=sys.stderr, flush=True)

    def debug(self, message: str) -> None:
        """排查用的细节；``ZMDMAP_DEBUG`` 没开就什么都不做。"""
        if DEBUG_ENABLED:
            print(f"{PREFIX} {message}", file=sys.stderr, flush=True)


logger = _Logger()
