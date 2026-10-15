"""MaaEnd 精简游戏数据入口（``uv run zmdmap``）。

两个子命令，**不带子命令时只打印帮助**：

- ``generate``：从本地 TableCfg / JsonData 生成六份精简数据到 ``data/raw/``；
- ``fetch``：从 zmdmap 下载已发布的精简数据到 ``data/raw/``，再投影进入库的 ``data/*.json``
  （下载和投影各由一个开关控制）。

``generate`` 按固定顺序跑同一批 ``build_*``
（delivery_jobs -> environment_monitoring -> sell_product -> delivery_destinations ->
teleport_anchors -> collect_points），产物只带游戏世界坐标；底图像素由 ``project`` 按 BaseNav
的 zone 表补上。

输入默认取与 MaaEnd 平级的兄弟仓库（由 ``--workspace`` 定位），三档优先级为
「命令行 > 环境变量 > 默认」：

| 输入 | CLI 参数 | 环境变量 | 默认 |
| --- | --- | --- | --- |
| 兄弟仓库目录 | ``--workspace`` | ``ZMDMAP_WORKSPACE`` | MaaEnd 的平级目录 |
| TableCfg | ``--table-cfg-dir`` | ``ZMDMAP_TABLE_CFG_DIR`` | ``<workspace>/BeyondTableCfg/TableCfg`` |
| JsonData | ``--json-data-dir`` | ``ZMDMAP_JSON_DATA_DIR`` | ``<workspace>/BeyondMemoryPack/JsonData`` |

用法::

    uv run zmdmap                                     # 只打印帮助
    uv run zmdmap generate                            # 生成到 data/raw/
    uv run zmdmap generate --list                     # 列出数据集名（每行一个）
    uv run zmdmap generate --only collect_points       # 只生成一份（可重复给）
    uv run zmdmap generate --sync                     # 先同步兄弟仓库，再生成
    uv run zmdmap generate --sync --update-model --project   # 连 navmesh 一起更新并投影入库
    uv run zmdmap fetch                               # 下载已发布数据并投影入库
    uv run zmdmap fetch --cache-bust                  # 强制重新下载（CI 上默认开启）
    uv run zmdmap fetch --no-download --only collect_points  # 不下载，只用 data/raw/ 重新投影
"""

from __future__ import annotations

import json
import time
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any, TypeVar

import click

from .datasets import DATASETS, DATASET_NAMES, Builder, select_datasets
from .fetch import download as download_published
from .paths import (
    DEFAULT_OUT_DIR,
    DEFAULT_WORKSPACE,
    JSON_DATA_DIR_ENV,
    PIPELINE_DATA_DIR,
    TABLE_CFG_DIR_ENV,
    WORKSPACE_ENV,
    json_data_dir_in,
    table_cfg_dir_in,
)
from .project import project_data
from .sync import SYNC_MARK_UNKNOWN, SYNC_MARKS, changed_data_files, preflight, sync_workspace

#: click 选项装饰器的返回类型。
F = TypeVar("F", bound=Callable[..., Any])

# 路径是怎么定下来的，日志里说清楚（CI 上排查「读的是哪份数据」时最有用）。
_SOURCE_LABELS = {
    click.core.ParameterSource.COMMANDLINE: "命令行",
    click.core.ParameterSource.ENVIRONMENT: "环境变量",
    click.core.ParameterSource.DEFAULT: "默认",
}

Decorator = Callable[[F], F]


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


def generate_datasets(
    datasets: Sequence[tuple[str, str, Builder]],
    table_cfg_dir: Path,
    json_data_dir: Path,
    out_dir: Path,
    *,
    force: bool,
) -> int:
    """跑 ``build_*`` 并落盘；返回成功生成的份数。"""
    written = 0
    for name, file_name, build in datasets:
        output_path = out_dir / file_name
        try:
            data = build(table_cfg_dir, json_data_dir)
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


def run_projection(
    raw_dir: Path,
    data_dir: Path,
    only: tuple[str, ...],
    *,
    nav: str | None,
    force: bool,
) -> int:
    """投影 ``raw_dir`` 到 ``data_dir``；返回写入份数。"""
    try:
        result = project_data(raw_dir, data_dir, nav=nav, only=only, force=force)
    except (OSError, ValueError) as error:
        raise click.ClickException(str(error)) from error
    return int(result["written"])


def report_data_changes(data_dir: Path, dataset_names: Sequence[str]) -> None:
    """入库目录的改动（只在入库目录就是默认的 data/ 时报）。"""
    if data_dir != PIPELINE_DATA_DIR:
        return
    changed = changed_data_files(list(dataset_names))
    if changed:
        click.echo("data/*.json 改动：")
        for line in changed:
            click.echo(f"  {line}")
    else:
        click.echo("data/*.json：没有改动")


def only_option() -> Decorator:
    return click.option(
        "--only",
        "only",
        type=click.Choice(DATASET_NAMES),
        multiple=True,
        help="只处理列出的数据集（可重复给；缺省全部）",
    )


def raw_dir_option() -> Decorator:
    return click.option(
        "--out-dir",
        type=click.Path(path_type=Path, file_okay=False),
        default=DEFAULT_OUT_DIR,
        show_default=True,
        help="原始数据目录（只带世界坐标）",
    )


def data_dir_option() -> Decorator:
    return click.option(
        "--data-dir",
        type=click.Path(path_type=Path, file_okay=False),
        default=PIPELINE_DATA_DIR,
        show_default=True,
        help="入库数据目录（投影输出）",
    )


def nav_option() -> Decorator:
    return click.option(
        "--nav",
        default=None,
        help="投影用的 nav 数据（本地路径或 URL；缺省用子模块里的 map/navmesh/base.nav.gz）",
    )


@click.group(context_settings={"help_option_names": ["-h", "--help"]})
def main() -> None:
    """生成 / 下载 / 投影 MaaEnd 的六份精简游戏数据。"""


@main.command(name="generate")
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
@raw_dir_option()
@data_dir_option()
@only_option()
@click.option("--sync/--no-sync", default=False, help="生成前先同步兄弟仓库（只做快进合并）")
@click.option("--update-model", is_flag=True, help="同步时顺带更新 assets/resource/model 子模块")
@click.option(
    "--project/--no-project",
    default=False,
    help="生成后把 data/raw/ 投影进入库的 data/*.json",
)
@nav_option()
@click.option("--dry-run", is_flag=True, help="只报告会做什么，不合并、不生成、不投影")
@click.option("--force", is_flag=True, help="结果未变化也重写输出文件")
@click.option("--list", "list_datasets", is_flag=True, help="只列出数据集名（每行一个）后退出")
@click.pass_context
def generate_command(
    ctx: click.Context,
    workspace: Path | None,
    table_cfg_dir: Path | None,
    json_data_dir: Path | None,
    out_dir: Path,
    data_dir: Path,
    only: tuple[str, ...],
    sync: bool,
    update_model: bool,
    project: bool,
    nav: str | None,
    dry_run: bool,
    force: bool,
    list_datasets: bool,
) -> None:
    """从本地 TableCfg / JsonData 生成六份精简数据。"""
    if list_datasets:
        for name, file_name, _ in DATASETS:
            click.echo(f"{name}\t{file_name}")
        return

    # click 的 envvar 已经把环境变量填进参数值了，这里只补最后的默认值。
    workspace = (workspace or DEFAULT_WORKSPACE).expanduser().resolve()
    table_cfg_dir = (table_cfg_dir or table_cfg_dir_in(workspace)).expanduser().resolve()
    json_data_dir = (json_data_dir or json_data_dir_in(workspace)).expanduser().resolve()
    out_dir = out_dir.expanduser().resolve()
    data_dir = data_dir.expanduser().resolve()
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

    if dry_run:
        click.echo("\n── 将执行（--dry-run，未执行）────────────────────")
        click.echo(f"  输出目录：{out_dir}")
        if project:
            click.echo(f"  投影    ：{out_dir} -> {data_dir}")
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
        generate_datasets(datasets, table_cfg_dir, json_data_dir, out_dir, force=force)
    except click.ClickException as error:
        generated_ok = False
        click.echo(f"  x {error.message}")
    generate_seconds = time.monotonic() - started

    projected_ok: bool | None = None
    if project:
        click.echo("\n── 投影（data/raw/ -> data/）─────────────────────")
        if generated_ok:
            try:
                run_projection(out_dir, data_dir, only, nav=nav, force=force)
                projected_ok = True
            except click.ClickException as error:
                click.echo(f"  x {error.message}")
                projected_ok = False
        else:
            click.echo("      跳过（生成失败）")

    click.echo("\n── 汇总 ──────────────────────────────────────────")
    if sync:
        click.echo(f"同步：{'有失败' if sync_failed else '正常'}")
    else:
        click.echo("同步：未执行（--no-sync）")
    click.echo(f"生成：{'正常' if generated_ok else '失败'}（{generate_seconds:.1f}s）")
    if projected_ok is None:
        click.echo("投影：未执行（--no-project）" if not project else "投影：跳过（生成失败）")
    else:
        click.echo(f"投影：{'正常' if projected_ok else '失败'}（{data_dir}）")
    report_data_changes(data_dir, [name for name, _, _ in datasets])
    if data_dir == PIPELINE_DATA_DIR:
        click.echo("提示：data/version.txt 是「已发布数据版本」，本地重算不会改它。")

    if sync_failed or not generated_ok or projected_ok is False:
        ctx.exit(1)


@main.command(name="fetch")
@raw_dir_option()
@data_dir_option()
@only_option()
@nav_option()
@click.option("--force", is_flag=True, help="重新下载，并重写未变化的输出文件")
@click.option(
    "--cache-bust",
    is_flag=True,
    help="请求带时间戳绕开远端缓存（CI 上自动开启，同时意味着强制下载）",
)
@click.option(
    "--download/--no-download",
    default=True,
    help="先下载已发布数据（--no-download 时只用本地 data/raw/ 重新投影）",
)
@click.option("--project/--no-project", default=True, help="把 data/raw/ 投影进入库的 data/*.json")
def fetch_command(
    out_dir: Path,
    data_dir: Path,
    only: tuple[str, ...],
    nav: str | None,
    force: bool,
    cache_bust: bool,
    download: bool,
    project: bool,
) -> None:
    """下载 zmdmap 已发布的精简数据，并投影进入库的 data/*.json。"""
    raw_dir = out_dir.expanduser().resolve()
    data_dir = data_dir.expanduser().resolve()

    if download:
        click.echo("── 下载（data/raw/）──────────────────────────────")
        try:
            download_published(raw_dir, force=force, cache_bust=cache_bust)
        except (OSError, ValueError) as error:
            raise click.ClickException(str(error)) from error
    else:
        click.echo("── 下载：跳过（--no-download），直接用现有 data/raw/")

    if not project:
        click.echo("投影：未执行（--no-project）")
        return

    click.echo("\n── 投影（data/raw/ -> data/）─────────────────────")
    written = run_projection(raw_dir, data_dir, only, nav=nav, force=force)
    click.echo("\n── 汇总 ──────────────────────────────────────────")
    click.echo(f"投影：正常（写入 {written} 份，输出到 {data_dir}）")
    report_data_changes(data_dir, only or DATASET_NAMES)


if __name__ == "__main__":
    main()
