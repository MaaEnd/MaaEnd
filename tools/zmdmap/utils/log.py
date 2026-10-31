"""命令行输出：进度写标准输出，提示与失败写标准错误；彩色只给真终端。

产物都是文件，标准输出上没有别的东西，所以进度可以放心写在那里；提示和失败走标准错误，CI 里会标红，
通过管道读结果的地方也不会把它们当成结果。**每一行都带 ``[zmdmap]`` 前缀**，CI 日志里混着解包、投影等
别的输出时按前缀就能筛出来。

彩色由 click 负责：输出到管道 / 文件 / CI 时自动去掉颜色码，只剩纯文本；``ZMDMAP_COLOR=1`` 强制彩色、
``ZMDMAP_COLOR=0`` 或环境变量 ``NO_COLOR`` 关掉。Windows 上的编码问题也交给 click（它按控制台的编码写出，
写不了的字符会替换，而不是像 ``print`` 那样直接抛 UnicodeEncodeError）。

用法是 ``logger.info(...)`` / ``logger.section(...)`` / ``logger.warning(...)`` / ``logger.error(...)`` /
``logger.debug(...)``，前缀由这里加，调用处不必自己写 ``[xxx]``。排查时设 ``ZMDMAP_DEBUG=1`` 再跑，就能看到
``debug`` 那几条。
"""

import contextlib
import os
import sys
import unicodedata

import click

#: 每条输出前面的标记：命令行里只有这一个工具，具体是哪一步看消息本身。
PREFIX = "[zmdmap]"

#: 是否打印 ``debug``：默认关掉，排查时设 ``ZMDMAP_DEBUG=1``。
DEBUG_ENABLED = os.environ.get("ZMDMAP_DEBUG", "") not in ("", "0")

#: 分节横幅的总显示宽度；中文按两格算，见 :func:`_display_width`。
SECTION_WIDTH = 50


def _display_width(text: str) -> int:
    """终端里占几格：全角/宽字符算两格，其余一格。"""
    return sum(2 if unicodedata.east_asian_width(char) in ("W", "F") else 1 for char in text)


def _color_choice() -> bool | None:
    """要不要彩色：``ZMDMAP_COLOR`` 显式指定，``NO_COLOR`` 按通用约定关掉，其余交给 click 看是不是终端。"""
    forced = os.environ.get("ZMDMAP_COLOR", "")
    if forced == "1":
        return True
    if forced == "0" or os.environ.get("NO_COLOR"):
        return False
    return None


def _echo(message: str, *, fg: str | None = None, bold: bool = False, err: bool = False) -> None:
    """写一行：前缀淡一点，正文按级别上色；非终端输出时 click 会把颜色码去掉。

    写完就 flush：输出接进管道、被 CI 收走时是块缓冲，不主动 flush 就要等攒满才出现，长任务看起来像卡住。
    """
    prefix = click.style(PREFIX, fg="bright_black")
    body = click.style(message, fg=fg, bold=bold) if fg or bold else message
    click.echo(f"{prefix} {body}", err=err, color=_color_choice())
    with contextlib.suppress(OSError, ValueError):
        (sys.stderr if err else sys.stdout).flush()


class _Logger:
    """把进度、提示、失败、细节分别送到该去的地方。"""

    def info(self, message: str) -> None:
        """正常进度。"""
        _echo(message)

    def section(self, title: str) -> None:
        """一条分节横幅，如 ``[zmdmap] ── 汇总 ────…``。"""
        head = f"── {title} "
        banner = head + "─" * max(0, SECTION_WIDTH - _display_width(head))
        _echo(banner, fg="cyan", bold=True)

    def warning(self, message: str) -> None:
        """还能继续、但要让人知道的情况。"""
        _echo(message, fg="yellow", err=True)

    def error(self, message: str) -> None:
        """这一步失败。"""
        _echo(message, fg="red", bold=True, err=True)

    def debug(self, message: str) -> None:
        """排查用的细节；``ZMDMAP_DEBUG`` 没开就什么都不做。"""
        if DEBUG_ENABLED:
            _echo(message, fg="bright_black", err=True)


logger = _Logger()
