"""MaaEnd 精简游戏数据生成入口（``uv run zmdmap``）。

这一个是「统一入口」：默认只生成六份精简游戏数据，用 ``--sync`` 拉兄弟仓库、用 ``--project``
把结果投影进入库的 ``data/*.json``。生成部分与 endfield-workflow 的 ``maaend_game_data``
处理器同序跑同一批 ``build_*``（delivery_jobs -> environment_monitoring -> sell_product ->
delivery_destinations -> teleport_anchors -> collect_points），产物只带游戏世界坐标。

输入默认取与 MaaEnd 平级的兄弟仓库（由 ``--workspace`` 定位），三档优先级为
「命令行 > 环境变量 > 默认」：

| 输入 | CLI 参数 | 环境变量 | 默认 |
| --- | --- | --- | --- |
| 兄弟仓库目录 | ``--workspace`` | ``ZMDMAP_WORKSPACE`` | MaaEnd 的平级目录 |
| TableCfg | ``--table-cfg-dir`` | ``ZMDMAP_TABLE_CFG_DIR`` | ``<workspace>/BeyondTableCfg/TableCfg`` |
| JsonData | ``--json-data-dir`` | ``ZMDMAP_JSON_DATA_DIR`` | ``<workspace>/BeyondMemoryPack/JsonData`` |

用法::

    uv run zmdmap                                     # 只生成到 data/raw/
    uv run zmdmap --list                              # 列出数据集名（每行一个）
    uv run zmdmap --only collect_points               # 只生成一份（可重复给）
    uv run zmdmap --sync                              # 先同步兄弟仓库，再生成
    uv run zmdmap --sync --update-model --project     # 连 navmesh 子模块一起更新并投影入库
    uv run zmdmap --sync --dry-run                    # 只报告会做什么
"""

from __future__ import annotations

import json
import time
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any

import click

from .collect_points import build_collect_points
from .delivery_destinations import build_delivery_destinations
from .delivery_jobs import build_delivery_jobs
from .environment_monitoring import build_environment_monitoring
from .local_context import RunContext
from .paths import (
    DEFAULT_OUT_DIR,
    DEFAULT_WORKSPACE,
    JSON_DATA_DIR_ENV,
    TABLE_CFG_DIR_ENV,
    WORKSPACE_ENV,
    json_data_dir_in,
    table_cfg_dir_in,
)
from .project import Outcome, build_project_command, output_dir_label, run
from .sell_product import build_sell_product
from .sync import SYNC_MARK_UNKNOWN, SYNC_MARKS, changed_data_files, preflight, sync_workspace
from .teleport_anchors import build_teleport_anchors

Builder = Callable[[RunContext], dict[str, Any]]
# 与 workflow ``maaend_game_data`` 处理器的顺序、文件名一致。
DATASETS: tuple[tuple[str, str, Builder], ...] = (
    ("delivery_jobs", "delivery_jobs.json", build_delivery_jobs),
    ("environment_monitoring", "environment_monitoring.json", build_environment_monitoring),
    ("sell_product", "sell_product.json", build_sell_product),
    ("delivery_destinations", "delivery_destinations.json", build_delivery_destinations),
    ("teleport_anchors", "teleport_anchors.json", build_teleport_anchors),
    ("collect_points", "collect_points.json", build_collect_points),
)

DATASET_NAMES = tuple(name for name, _, _ in DATASETS)

# 路径是怎么定下来的，日志里说清楚（CI 上排查「读的是哪份数据」时最有用）。
_SOURCE_LABELS = {
    click.core.ParameterSource.COMMANDLINE: "命令行",
    click.core.ParameterSource.ENVIRONMENT: "环境变量",
    click.core.ParameterSource.DEFAULT: "默认",
}


def select_datasets(names: Sequence[str]) -> tuple[tuple[str, str, Builder], ...]:
    """按名字挑数据集；顺序始终跟随 :data:`DATASETS`，与 workflow 一致。"""
    if not names:
        return DATASETS
    wanted = set(names)
    return tuple(dataset for dataset in DATASETS if dataset[0] in wanted)


def should_skip(output_path: Path, data: dict[str, Any], force: bool) -> bool:
    if force or not output_path.is_file():
        return False
    try:
        current = json.loads(output_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return False
    return current == data


def write_dataset(output_path: Path, data: dict[str, Any]) -> None:
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(
        json.dumps(data, ensure_ascii=False, indent=4) + "\n",
        encoding="utf-8",
    )


def parameter_source(ctx: click.Context, name: str) -> str:
    """这个参数的值是命令行给的、环境变量给的，还是没给走的默认值。"""
    source = ctx.get_parameter_source(name)
    return _SOURCE_LABELS.get(source, "默认")


def check_input_dir(label: str, path: Path, option: str, env_name: str) -> None:
    if not path.is_dir():
        raise click.ClickException(
            f"缺少{label}目录：{path}（用 {option} 或环境变量 {env_name} 指定）"
        )


def generate(
    datasets: Sequence[tuple[str, str, Builder]],
    context: RunContext,
    out_dir: Path,
    *,
    force: bool,
) -> int:
    """跑 ``build_*`` 并落盘；返回成功生成的份数。"""
    written = 0
    for name, file_name, build in datasets:
        output_path = out_dir / file_name
        try:
            data = build(context)
        except (OSError, ValueError) as error:
            raise click.ClickException(f"[{name}] {error}") from error
        if should_skip(output_path, data, force):
            click.echo(f"[{name}] 生成结果未变化，跳过写入：{output_path}")
            continue
        try:
            write_dataset(output_path, data)
        except OSError as error:
            raise click.ClickException(f"[{name}] 写入失败：{error}") from error
        click.echo(f"[{name}] 已生成：{output_path}")
        written += 1
    return written


@click.command(context_settings={"help_option_names": ["-h", "--help"]})
@click.option(
    "--workspace",
    type=click.Path(path_type=Path, file_okay=False),
    envvar=WORKSPACE_ENV,
    default=None,
    help=f"放 BeyondTableCfg / BeyondMemoryPack 等兄弟仓库的目录（默认：{DEFAULT_WORKSPACE}）",
)
@click.option(
    "--table-cfg-dir",
    type=click.Path(path_type=Path, file_okay=False),
    envvar=TABLE_CFG_DIR_ENV,
    default=None,
    help="TableCfg 目录（默认：<workspace>/BeyondTableCfg/TableCfg）",
)
@click.option(
    "--json-data-dir",
    type=click.Path(path_type=Path, file_okay=False),
    envvar=JSON_DATA_DIR_ENV,
    default=None,
    help="BeyondMemoryPack 的 JsonData 目录（默认：<workspace>/BeyondMemoryPack/JsonData）",
)
@click.option(
    "--out-dir",
    type=click.Path(path_type=Path, file_okay=False),
    default=DEFAULT_OUT_DIR,
    show_default=True,
    help="输出目录（只带世界坐标的原始形态）",
)
@click.option(
    "--only",
    "only",
    type=click.Choice(DATASET_NAMES),
    multiple=True,
    help="只生成列出的数据集（可重复给；缺省全部）",
)
@click.option("--sync/--no-sync", default=False, help="生成前先同步兄弟仓库（只做快进合并）")
@click.option("--update-model", is_flag=True, help="同步时顺带更新 assets/resource/model 子模块")
@click.option(
    "--project/--no-project",
    default=False,
    help="生成后调用 fetch-data.mjs 把 data/raw/ 投影进入库的 data/*.json",
)
@click.option("--nav", default=None, help="--project 用的 nav 数据（本地路径或 URL）")
@click.option("--dry-run", is_flag=True, help="只报告会做什么，不合并、不生成、不投影")
@click.option("--force", is_flag=True, help="结果未变化也重写输出文件")
@click.option("--list", "list_datasets", is_flag=True, help="只列出数据集名（每行一个）后退出")
@click.pass_context
def main(
    ctx: click.Context,
    workspace: Path | None,
    table_cfg_dir: Path | None,
    json_data_dir: Path | None,
    out_dir: Path,
    only: tuple[str, ...],
    sync: bool,
    update_model: bool,
    project: bool,
    nav: str | None,
    dry_run: bool,
    force: bool,
    list_datasets: bool,
) -> None:
    """生成 MaaEnd 的六份精简游戏数据（可选同步输入仓库、投影进入库数据）。"""
    if list_datasets:
        for name, file_name, _ in DATASETS:
            click.echo(f"{name}\t{file_name}")
        return

    # click 的 envvar 已经把环境变量填进参数值了，这里只补最后的默认值。
    workspace = (workspace or DEFAULT_WORKSPACE).expanduser().resolve()
    table_cfg_dir = (table_cfg_dir or table_cfg_dir_in(workspace)).expanduser().resolve()
    json_data_dir = (json_data_dir or json_data_dir_in(workspace)).expanduser().resolve()
    out_dir = out_dir.expanduser().resolve()
    datasets = select_datasets(only)

    click.echo(f"[zmdmap] 工作区（兄弟仓库）：{workspace}（{parameter_source(ctx, 'workspace')}）")
    click.echo(f"[zmdmap] TableCfg：{table_cfg_dir}（{parameter_source(ctx, 'table_cfg_dir')}）")
    click.echo(f"[zmdmap] JsonData：{json_data_dir}（{parameter_source(ctx, 'json_data_dir')}）")
    click.echo(f"[zmdmap] 数据集：{len(datasets)} 个（{'、'.join(name for name, _, _ in datasets)}）")

    sync_failed = False
    if sync:
        click.echo("\n── 同步 ──────────────────────────────────────────")
        outcomes, sync_failed = sync_workspace(
            workspace, dry_run=dry_run, update_model=update_model
        )
        for outcome in outcomes:
            mark = SYNC_MARKS.get(outcome.state, SYNC_MARK_UNKNOWN)
            click.echo(f"  [{mark}] {outcome.name:<20} {outcome.detail}")

    problems = preflight(table_cfg_dir, json_data_dir, check_navmesh=project)
    if problems:
        click.echo("\n── 预检提示 ─────────────────────────────────────")
        for problem in problems:
            # 只用 GBK 能编出来的字符：Windows 中文控制台编不出 ⚠ / ✘ 会直接抛 UnicodeEncodeError。
            click.echo(f"  ! {problem}")

    project_command = build_project_command([name for name, _, _ in datasets], nav)
    if dry_run:
        click.echo("\n── 将执行（--dry-run，未执行）────────────────────")
        click.echo(f"  输出目录：{out_dir}")
        if project:
            click.echo(f"  投影    ：{' '.join(project_command)}")
        else:
            click.echo("  投影    ：未启用（--project）")
        click.echo("\n--dry-run：没有合并仓库，也没有写任何 data 文件")
        return

    check_input_dir("TableCfg", table_cfg_dir, "--table-cfg-dir", TABLE_CFG_DIR_ENV)
    check_input_dir("JsonData", json_data_dir, "--json-data-dir", JSON_DATA_DIR_ENV)

    click.echo("\n── 生成（data/raw/）──────────────────────────────")
    started = time.monotonic()
    generated_ok = True
    try:
        generate(datasets, RunContext(table_cfg_dir, json_data_dir), out_dir, force=force)
    except click.ClickException as error:
        generated_ok = False
        click.echo(f"  x {error.message}")
    generate_seconds = time.monotonic() - started

    projected: Outcome | None = None
    if project:
        click.echo("\n── 投影（data/raw/ -> data/）─────────────────────")
        if generated_ok:
            click.echo(f"      $ {' '.join(project_command)}")
            projected = run(project_command)
            for line in (projected.stdout + projected.stderr).splitlines():
                if line.strip():
                    click.echo(f"      {line.strip()}")
        else:
            click.echo("      跳过（生成失败）")

    click.echo("\n── 汇总 ──────────────────────────────────────────")
    if sync:
        click.echo(f"同步：{'有失败' if sync_failed else '正常'}")
    else:
        click.echo("同步：未执行（--no-sync）")
    click.echo(f"生成：{'正常' if generated_ok else '失败'}（{generate_seconds:.1f}s）")
    if projected is None:
        click.echo("投影：未执行（--no-project）" if not project else "投影：跳过（生成失败）")
    else:
        click.echo(f"投影：{'正常' if projected.ok else '失败'}（{output_dir_label()}）")
    if out_dir == DEFAULT_OUT_DIR:
        changed = changed_data_files([name for name, _, _ in datasets])
        if changed:
            click.echo("data/*.json 改动：")
            for line in changed:
                click.echo(f"  {line}")
        else:
            click.echo("data/*.json：没有改动")
        click.echo("提示：data/version.txt 是「已发布数据版本」，本地重算不会改它。")

    if sync_failed or not generated_ok or (projected is not None and not projected.ok):
        ctx.exit(1)


if __name__ == "__main__":
    main()
