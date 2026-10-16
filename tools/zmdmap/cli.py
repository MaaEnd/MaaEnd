"""``uv run zmdmap`` 的入口：把两个子命令挂上。

- ``generate``：从本地 TableCfg / JsonData 生成精简数据，再入库（:mod:`tools.zmdmap.generate`）；
- ``fetch``：从 zmdmap 下载已发布的原始数据，再入库（:mod:`tools.zmdmap.fetch`）。

两个子命令各自的流程在自己的文件里，选项也各写各的（`--out-dir` / `--data-dir` / `--only` / `--nav` 一样，
但重复两份比多一个转手的中转文件更好读）；入库（投影 / 原样复制）共用一个
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

from __future__ import annotations

import click

from .fetch import fetch_command
from .generate import generate_command


@click.group(context_settings={"help_option_names": ["-h", "--help"]})
def main() -> None:
    """生成 / 下载 / 入库 MaaEnd 的精简游戏数据。"""


main.add_command(generate_command)
main.add_command(fetch_command)


if __name__ == "__main__":
    main()
