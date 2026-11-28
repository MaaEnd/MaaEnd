"""``uv run zmdmap`` 的入口：把三个子命令挂上。

- ``generate``：从本地游戏数据生成精简数据，再入库（:mod:`tools.zmdmap.generate`）；各数据集要哪些输入
  由它 ``build`` 的参数决定；
- ``fetch``：从 zmdmap 下载已发布的原始数据，再入库（:mod:`tools.zmdmap.fetch`）；
- ``version``：打印上游清单里这一批数据的版本号（同步作业的 PR 标题用它）。

generate 与 fetch 的流程各自在文件里，选项也各写各的；version 只是读一次清单。入库（投影 / 原样复制）共用一个
:mod:`tools.zmdmap.materialize`。``version`` 由 ``generate`` 写进 ``data/raw/``；``fetch`` 在内容没变、标签却还是
旧的时候只改这一个字段，入库则原样沿用；
细节见 ``tools/zmdmap/README.md``。

用法::

    uv run zmdmap generate                        # 生成并入库到 tools/zmdmap/data/
    uv run zmdmap generate --only collect_points   # 只生成一份
    uv run zmdmap generate --sync                  # 生成前先快进合并兄弟仓库
    uv run zmdmap generate --raw-only              # 只出原始形态，不入库
    uv run zmdmap fetch                            # 下载已发布数据并入库
    uv run zmdmap fetch --only delivery_jobs       # 只下、只入库这一份
    uv run zmdmap version                          # 打印上游清单里这一批的版本号
"""

import click

from .fetch import fetch_command, version_command
from .generate import generate_command
from .utils.log import logger


@click.group(context_settings={"help_option_names": ["-h", "--help"]})
def _command() -> None:
    """生成 / 下载 / 入库 MaaEnd 的精简游戏数据。"""


_command.add_command(generate_command)
_command.add_command(fetch_command)
_command.add_command(version_command)


def main() -> None:
    """命令行入口。

    click 自己报错时会写一行 ``Error: …``（没有前缀），那样日志里就混进了一行不带标记的。这里接住它，
    改走 :func:`tools.zmdmap.utils.log.logger.error`，前缀、颜色、标准错误都和别处一致；``--help`` 的输出
    仍然交给 click（那是用法说明，不是日志）。
    """
    try:
        exit_code = _command(standalone_mode=False)
    except click.ClickException as error:
        logger.error(f"x {error.format_message()}")
        raise SystemExit(error.exit_code) from error
    except click.Abort:
        logger.error("x 已中断")
        raise SystemExit(1) from None
    except click.exceptions.Exit as stopped:
        raise SystemExit(stopped.exit_code) from None
    # standalone_mode=False 时 click 不再自己 sys.exit：命令里的 ctx.exit(1)（生成或入库失败走的就是
    # 这条）变成一个返回值，没人接就等于成功退出（退出码 0）。这里照它的意思退出，别让失败悄悄过。
    if exit_code:
        raise SystemExit(exit_code)


if __name__ == "__main__":
    main()
