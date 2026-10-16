"""MaaEnd 精简游戏数据入口（``uv run zmdmap``）。

子命令如下，**不带子命令时只打印帮助**：

- ``generate``：从本地 TableCfg / JsonData 生成精简数据。各份数据一律先出原始形态（只带游戏世界
  坐标）进 ``--out-dir``，再入库到 ``--data-dir``：需要投影的按 BaseNav 的 zone 表补上底图像素，
  不需要投影的原样复制；``--raw-only`` 时只出原始形态；
- ``fetch``：从 zmdmap 下载已发布的原始数据到 ``--out-dir``（下载和入库各由一个开关控制），再按
  同样的规则入库到 ``--data-dir``。

``data/`` 与 ``data/raw/`` 都是 zmdmap 自己的数据树（``tools/zmdmap/data/``）：``raw/`` 放原样
产物（不入库），``data/`` 放可以直接用的、入库的数据。

``generate`` 按固定顺序跑 ``datasets/`` 里的全部 ``Dataset`` 子类（需要投影的在前），产物只带游戏世界
坐标；底图像素由 ``project`` 按 BaseNav 的 zone 表补上。

输入默认取与 MaaEnd 平级的兄弟仓库（由 ``--workspace`` 定位），三档优先级为
「命令行 > 环境变量 > 默认」：

| 输入 | CLI 参数 | 环境变量 | 默认 |
| --- | --- | --- | --- |
| 兄弟仓库目录 | ``--workspace`` | ``ZMDMAP_WORKSPACE`` | MaaEnd 的平级目录 |
| TableCfg | ``--table-cfg-dir`` | ``ZMDMAP_TABLE_CFG_DIR`` | ``<workspace>/BeyondTableCfg/TableCfg`` |
| JsonData | ``--json-data-dir`` | ``ZMDMAP_JSON_DATA_DIR`` | ``<workspace>/BeyondMemoryPack/JsonData`` |

用法::

    uv run zmdmap                                     # 只打印帮助
    uv run zmdmap generate                            # 生成并入库到 tools/zmdmap/data/
    uv run zmdmap generate --raw-only                  # 只出原始形态（发布用的就是这一份）
    uv run zmdmap generate --list                     # 列出数据集名、文件名与入库方式
    uv run zmdmap generate --only collect_points       # 只生成一份（可重复给）
    uv run zmdmap generate --sync                     # 先同步兄弟仓库，再生成
    uv run zmdmap fetch                               # 下载已发布数据并入库
    uv run zmdmap fetch --force                       # 重新下载（带时间戳，不走远端缓存）
    uv run zmdmap fetch --only collect_points          # 只下载/入库这一份（各任务生成器都这么用）
    uv run zmdmap fetch --no-download --only collect_points  # 不下载，只用 data/raw/ 重新入库
"""

from __future__ import annotations

import json
import time
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any, TypeVar

import click

from .constants import MANIFEST_FILE
from .datasets import (
    DATASETS,
    DATASET_NAMES,
    Dataset,
    files_of,
    select_datasets,
)
from .fetch import download as download_published
from .paths import (
    DEFAULT_OUT_DIR,
    DEFAULT_WORKSPACE,
    JSON_DATA_DIR_ENV,
    TABLE_CFG_DIR_ENV,
    WORKSPACE_ENV,
    ZMDMAP_DATA_DIR,
    json_data_dir_in,
    table_cfg_dir_in,
)
from .project import copy_files, project_data, read_version, with_metadata, write_manifest
from .sync import (
    SYNC_MARK_UNKNOWN,
    SYNC_MARKS,
    changed_data_files,
    data_version,
    preflight,
    repo_data_version,
    sync_workspace,
)

#: click 选项装饰器的返回类型。
F = TypeVar("F", bound=Callable[..., Any])

# 路径是怎么定下来的，日志里说清楚（CI 上排查「读的是哪份数据」时最有用）。
_SOURCE_LABELS = {
    click.core.ParameterSource.COMMANDLINE: "命令行",
    click.core.ParameterSource.ENVIRONMENT: "环境变量",
    click.core.ParameterSource.DEFAULT: "默认",
}

Decorator = Callable[[F], F]


def should_skip(output_path: Path, data: Any, force: bool) -> bool:
    if force or not output_path.is_file():
        return False
    try:
        current = json.loads(output_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return False
    return current == data


def write_dataset(output_path: Path, data: Any) -> None:
    output_path.parent.mkdir(parents=True, exist_ok=True)
    # newline：仓库 .gitattributes 是 eol=lf，别让 Windows 把写入变成 CRLF。
    output_path.write_text(
        json.dumps(data, ensure_ascii=False, indent=4) + "\n",
        encoding="utf-8",
        newline="\n",
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
    datasets: Sequence[type[Dataset]],
    table_cfg_dir: Path,
    json_data_dir: Path,
    out_dir: Path,
    data_dir: Path,
    *,
    version: str | None,
    force: bool,
) -> int:
    """跑 ``build`` 并把原始形态落到 ``out_dir``；返回成功生成的份数。

    每份数据都在最前面写上元数据：版本取 ``--version`` / 从兄弟仓库读到的版本，都读不到就沿用已有文件里的
    版本（本地重算不会凭空改掉版本号）；``sha256`` 是数据本体的内容指纹，按这次生成的本体重算。
    """
    written = 0
    for dataset in datasets:
        output_path = out_dir / dataset.file_name
        try:
            data = dataset.build(table_cfg_dir, json_data_dir)
        except (OSError, ValueError) as error:
            raise click.ClickException(f"[{dataset.name}] {error}") from error
        stamp = version or read_version(output_path) or read_version(data_dir / dataset.file_name)
        data = with_metadata(data, stamp)
        if should_skip(output_path, data, force):
            click.echo(f"[{dataset.name}] 生成结果未变化，跳过写入：{output_path}")
            continue
        try:
            write_dataset(output_path, data)
        except OSError as error:
            raise click.ClickException(f"[{dataset.name}] 写入失败：{error}") from error
        click.echo(f"[{dataset.name}] 已生成：{output_path}")
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
    """投影 ``raw_dir`` 里需要投影的那几份到 ``data_dir``；返回写入份数。"""
    if not only:
        return 0
    try:
        result = project_data(raw_dir, data_dir, nav=nav, only=only, force=force)
    except (OSError, ValueError) as error:
        raise click.ClickException(str(error)) from error
    return int(result["written"])


def copy_plain(raw_dir: Path, data_dir: Path, file_names: Sequence[str], *, force: bool) -> int:
    """把不需要投影的那几份原样复制进 ``data_dir``；返回写入份数。"""
    if not file_names:
        return 0
    try:
        return copy_files(raw_dir, data_dir, list(file_names), force=force)
    except (OSError, ValueError) as error:
        raise click.ClickException(str(error)) from error


def report_data_changes(data_dir: Path, file_names: Sequence[str]) -> None:
    """入库目录的改动（只在入库目录就是默认的 data/ 时报）。"""
    if data_dir != ZMDMAP_DATA_DIR:
        return
    changed = changed_data_files(list(file_names))
    if changed:
        click.echo("data/*.json 改动：")
        for line in changed:
            click.echo(f"  {line}")
    else:
        click.echo("data/*.json：没有改动")


def only_option(names: Sequence[str] = DATASET_NAMES, verb: str = "处理") -> Decorator:
    return click.option(
        "--only",
        "only",
        type=click.Choice(names),
        multiple=True,
        help=f"只{verb}列出的数据集（可重复给；缺省全部）",
    )


def raw_dir_option() -> Decorator:
    return click.option(
        "--out-dir",
        type=click.Path(path_type=Path, file_okay=False),
        default=DEFAULT_OUT_DIR,
        show_default=True,
        help="原始数据目录（原样产物，只带世界坐标）",
    )


def data_dir_option() -> Decorator:
    return click.option(
        "--data-dir",
        type=click.Path(path_type=Path, file_okay=False),
        default=ZMDMAP_DATA_DIR,
        show_default=True,
        help="入库数据目录（需要投影的投影后落这里，其余原样复制进来）",
    )


def nav_option() -> Decorator:
    return click.option(
        "--nav",
        default=None,
        help="投影用的 nav 数据（本地路径或 URL；缺省用子模块里的 map/navmesh/base.nav.gz）",
    )


@click.group(context_settings={"help_option_names": ["-h", "--help"]})
def main() -> None:
    """生成 / 下载 / 入库 MaaEnd 的精简游戏数据。"""


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
@click.option(
    "--version",
    "data_version_option",
    default=None,
    help="写进每份数据的版本（默认从兄弟仓库读；都读不到就沿用原有的）",
)
@click.option(
    "--raw-only",
    is_flag=True,
    help="只出原始形态（只写 --out-dir），不做入库",
)
@nav_option()
@click.option("--dry-run", is_flag=True, help="只报告会做什么，不合并、不生成、不入库")
@click.option("--force", is_flag=True, help="结果未变化也重写输出文件")
@click.option("--list", "list_datasets", is_flag=True, help="只列出数据集名、文件名与入库方式后退出")
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
    data_version_option: str | None,
    raw_only: bool,
    nav: str | None,
    dry_run: bool,
    force: bool,
    list_datasets: bool,
) -> None:
    """从本地 TableCfg / JsonData 生成精简数据（带坐标的投影，其余原样复制）。"""
    if list_datasets:
        for dataset in DATASETS:
            how = "投影" if dataset.needs_projection else "复制"
            click.echo(f"{dataset.name}\t{dataset.file_name}\t{how}")
        return

    # click 的 envvar 已经把环境变量填进参数值了，这里只补最后的默认值。
    workspace = (workspace or DEFAULT_WORKSPACE).expanduser().resolve()
    table_cfg_dir = (table_cfg_dir or table_cfg_dir_in(workspace)).expanduser().resolve()
    json_data_dir = (json_data_dir or json_data_dir_in(workspace)).expanduser().resolve()
    out_dir = out_dir.expanduser().resolve()
    data_dir = data_dir.expanduser().resolve()
    datasets = select_datasets(only)
    pipeline_files = files_of(datasets, projected=True)
    plain_files = files_of(datasets, projected=False)

    click.echo(f"[zmdmap] 工作区（兄弟仓库）：{workspace}（{parameter_source(ctx, 'workspace')}）")
    click.echo(f"[zmdmap] TableCfg：{table_cfg_dir}（{parameter_source(ctx, 'table_cfg_dir')}）")
    click.echo(f"[zmdmap] JsonData：{json_data_dir}（{parameter_source(ctx, 'json_data_dir')}）")
    click.echo(f"[zmdmap] 数据集：{len(datasets)} 个（{'、'.join(d.name for d in datasets)}）")

    # 版本优先命令行；否则从兄弟仓库读 —— 工作区里没有兄弟仓库时（发布 CI 可能只给目录），
    # 退回 TableCfg 所在的那个仓库，它通常就是 BeyondTableCfg。
    detected = data_version(workspace) or repo_data_version(table_cfg_dir.parent)
    version = data_version_option or detected
    if version:
        origin = "命令行" if data_version_option else "兄弟仓库"
        click.echo(f"[zmdmap] 数据版本：{version}（{origin}）")
    else:
        click.echo("[zmdmap] 数据版本：读不到，沿用各份数据原有的版本")

    sync_failed = False
    if sync:
        click.echo("\n── 同步 ──────────────────────────────────────────")
        outcomes, sync_failed = sync_workspace(workspace, dry_run=dry_run)
        for outcome in outcomes:
            mark = SYNC_MARKS.get(outcome.state, SYNC_MARK_UNKNOWN)
            click.echo(f"  [{mark}] {outcome.name:<20} {outcome.detail}")

    problems = preflight(
        table_cfg_dir, json_data_dir, check_navmesh=not raw_only and bool(pipeline_files)
    )
    if problems:
        click.echo("\n── 预检提示 ─────────────────────────────────────")
        for problem in problems:
            # 只用 GBK 能编出来的字符：Windows 中文控制台编不出 ⚠ / ✘ 会直接抛 UnicodeEncodeError。
            click.echo(f"  ! {problem}")

    if dry_run:
        click.echo("\n── 将执行（--dry-run，未执行）────────────────────")
        click.echo(f"  原始数据目录：{out_dir}")
        click.echo(f"  入库数据目录：{data_dir}")
        if raw_only:
            click.echo("  入库    ：未启用（--raw-only）")
        else:
            click.echo(f"  投影    ：{len(pipeline_files)} 份 {out_dir} -> {data_dir}")
            click.echo(f"  复制    ：{len(plain_files)} 份 {out_dir} -> {data_dir}")
        click.echo("\n--dry-run：没有合并仓库，也没有写任何数据文件")
        return

    check_input_dir("TableCfg", table_cfg_dir, "--table-cfg-dir", TABLE_CFG_DIR_ENV)
    check_input_dir("JsonData", json_data_dir, "--json-data-dir", JSON_DATA_DIR_ENV)

    click.echo("\n── 生成（原始形态）──────────────────────────────")
    started = time.monotonic()
    generated_ok = True
    try:
        generate_datasets(
            datasets,
            table_cfg_dir,
            json_data_dir,
            out_dir,
            data_dir,
            version=version,
            force=force,
        )
        manifest_path = write_manifest(
            out_dir, [dataset.file_name for dataset in DATASETS], version
        )
        if manifest_path is not None:
            click.echo(f"  {MANIFEST_FILE} -> {manifest_path}")
    except click.ClickException as error:
        generated_ok = False
        click.echo(f"  x {error.message}")
    except OSError as error:
        generated_ok = False
        click.echo(f"  x 写 {MANIFEST_FILE} 失败：{error}")
    generate_seconds = time.monotonic() - started

    materialized_ok: bool | None = None
    if raw_only:
        materialized_ok = None
    elif not generated_ok:
        materialized_ok = False
        click.echo("\n── 入库：（跳过：生成失败）───────────────────────")
    else:
        click.echo("\n── 入库（原始形态 -> data/）─────────────────────")
        materialized_ok = True
        try:
            projected = run_projection(out_dir, data_dir, pipeline_files, nav=nav, force=force)
            copied = copy_plain(out_dir, data_dir, plain_files, force=force)
            click.echo(f"  投影 {projected} 份、复制 {copied} 份")
        except click.ClickException as error:
            click.echo(f"  x {error.message}")
            materialized_ok = False

    click.echo("\n── 汇总 ──────────────────────────────────────────")
    if sync:
        click.echo(f"同步：{'有失败' if sync_failed else '正常'}")
    else:
        click.echo("同步：未执行（--no-sync）")
    click.echo(f"生成：{'正常' if generated_ok else '失败'}（{generate_seconds:.1f}s）")
    if materialized_ok is None:
        click.echo("入库：未执行（--raw-only）")
    else:
        click.echo(f"入库：{'正常' if materialized_ok else '失败'}（{data_dir}）")
    report_data_changes(data_dir, [dataset.file_name for dataset in datasets])
    if data_dir == ZMDMAP_DATA_DIR:
        click.echo("提示：每份数据最前面写着 version（版本）与 sha256（内容指纹）；读不到兄弟仓库的版本时沿用原有的。")

    if sync_failed or not generated_ok or materialized_ok is False:
        ctx.exit(1)


@main.command(name="fetch")
@raw_dir_option()
@data_dir_option()
@only_option(verb="下载并入库")
@nav_option()
@click.option(
    "--force",
    is_flag=True,
    help="重新下载（请求带时间戳，不走远端缓存），并重写未变化的输出文件",
)
@click.option(
    "--download/--no-download",
    default=True,
    help="先下载已发布数据（--no-download 时只用本地 data/raw/ 重新入库）",
)
@click.option("--raw-only", is_flag=True, help="只下载原始形态（只写 --out-dir），不做入库")
def fetch_command(
    out_dir: Path,
    data_dir: Path,
    only: tuple[str, ...],
    nav: str | None,
    force: bool,
    download: bool,
    raw_only: bool,
) -> None:
    """下载 zmdmap 已发布的原始数据，并入库到 data/。"""
    raw_dir = out_dir.expanduser().resolve()
    data_dir = data_dir.expanduser().resolve()
    datasets = select_datasets(only)
    pipeline_files = files_of(datasets, projected=True)
    plain_files = files_of(datasets, projected=False)

    if download:
        click.echo("── 下载 ──────────────────────────────────────────")
        click.echo(f"   {len(pipeline_files) + len(plain_files)} 份：{'、'.join((*pipeline_files, *plain_files))}")
        try:
            download_published(raw_dir, pipeline_files, plain_files, force=force)
        except (OSError, ValueError) as error:
            raise click.ClickException(str(error)) from error
    else:
        click.echo("── 下载：跳过（--no-download），直接用现有 data/raw/")

    if raw_only:
        click.echo("入库：未执行（--raw-only）")
        return

    click.echo("\n── 入库（原始形态 -> data/）─────────────────────")
    projected = run_projection(raw_dir, data_dir, pipeline_files, nav=nav, force=force)
    copied = copy_plain(raw_dir, data_dir, plain_files, force=force)
    click.echo("\n── 汇总 ──────────────────────────────────────────")
    click.echo(f"入库：正常（投影 {projected} 份、复制 {copied} 份，输出到 {data_dir}）")
    report_data_changes(data_dir, [dataset.file_name for dataset in datasets])


if __name__ == "__main__":
    main()
