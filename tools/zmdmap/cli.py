"""``uv run zmdmap`` 的入口：把两个子命令挂上。

- ``generate``：从本地游戏数据生成精简数据，再入库（:mod:`tools.zmdmap.generate`）；各数据集要哪些输入
  由它 ``build`` 的参数决定；
- ``fetch``：从 zmdmap 下载已发布的原始数据，再入库（:mod:`tools.zmdmap.fetch`）。

两个子命令各自的流程在自己的文件里，选项也各写各的；入库（投影 / 原样复制）共用一个
:mod:`tools.zmdmap.materialize`。``version`` 只由 ``generate`` 写进 ``data/raw/``，``fetch`` 与入库都只沿用；
细节见 ``tools/zmdmap/README.md``。

用法::

    uv run zmdmap generate                        # 生成并入库到 tools/zmdmap/data/
    uv run zmdmap generate --only collect_points   # 只生成一份
    uv run zmdmap generate --sync                  # 生成前先快进合并兄弟仓库
    uv run zmdmap generate --raw-only              # 只出原始形态，不入库
    uv run zmdmap fetch                            # 下载已发布数据并入库
    uv run zmdmap fetch --only delivery_jobs       # 只下、只入库这一份
"""

import click

from .fetch import fetch_command
from .generate import generate_command
from .utils.log import logger


@click.group(context_settings={"help_option_names": ["-h", "--help"]})
def _command() -> None:
    """生成 / 下载 / 入库 MaaEnd 的精简游戏数据。"""


_command.add_command(generate_command)
_command.add_command(fetch_command)


def main() -> None:
    """命令行入口。

    click 自己报错时会写一行 ``Error: …``（没有前缀），那样日志里就混进了一行不带标记的。这里接住它，
    改走 :func:`tools.zmdmap.utils.log.logger.error`，前缀、颜色、标准错误都和别处一致；``--help`` 的输出
    仍然交给 click（那是用法说明，不是日志）。
    """
    try:
        _command(standalone_mode=False)
    except click.ClickException as error:
        logger.error(f"x {error.format_message()}")
        raise SystemExit(error.exit_code) from error
    except click.Abort:
        logger.error("x 已中断")
        raise SystemExit(1) from None
    except click.exceptions.Exit as stopped:
        raise SystemExit(stopped.exit_code) from None


if __name__ == "__main__":
    main()
