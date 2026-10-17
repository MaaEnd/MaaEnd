"""``zmdmap generate``：从本地 TableCfg / JsonData 生成精简数据，再入库。

各份数据一律先出原始形态（只带游戏世界坐标）进 ``--out-dir``，再入库到 ``--data-dir``：需要投影的按
BaseNav 的 zone 表补上底图像素，不需要投影的原样复制；``--raw-only`` 时只出原始形态。

``version`` 只在这里写：取 :func:`tools.zmdmap.utils.sync.data_version`（兄弟仓库数据里的构建号 >
提交标题）或 ``--version``，写到每份原始数据的最前面；入库与 ``fetch`` 都只沿用，不改它。
"""

from __future__ import annotations

import json
import time
from collections.abc import Sequence
from pathlib import Path
from typing import Any

import click

from .datasets import DATASET_NAMES, DATASETS, Dataset, files_of, select_datasets
from .materialize import copy_plain, report_data_changes, run_projection
from .utils.config import (
    DEFAULT_OUT_DIR,
    DEFAULT_WORKSPACE,
    JSON_DATA_DIR_ENV,
    MANIFEST_FILE,
    TABLE_CFG_DIR_ENV,
    WORKSPACE_ENV,
    ZMDMAP_DATA_DIR,
    json_data_dir_in,
    table_cfg_dir_in,
)
from .utils.datafile import (
    VERSION_KEY,
    is_unchanged,
    read_payload_hash,
    read_version,
    with_metadata,
    write_json,
)
from .utils.sync import (
    SYNC_MARK_UNKNOWN,
    SYNC_MARKS,
    data_version,
    preflight,
    repo_data_version,
    sync_workspace,
)

# 路径是怎么定下来的，日志里说清楚（CI 上排查「读的是哪份数据」时最有用）。
_SOURCE_LABELS = {
    click.core.ParameterSource.COMMANDLINE: "命令行",
    click.core.ParameterSource.ENVIRONMENT: "环境变量",
    click.core.ParameterSource.DEFAULT: "默认",
}


def should_skip(output_path: Path, data: Any, force: bool) -> bool:
    if force or not output_path.is_file():
        return False
    try:
        current = json.loads(output_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return False
    return current == data


def parameter_source(ctx: click.Context, name: str) -> str:
    """这个参数的值是命令行给的、环境变量给的，还是没给走的默认值。"""
    source = ctx.get_parameter_source(name)
    return _SOURCE_LABELS.get(source, "默认")


def check_input_dir(label: str, path: Path, option: str, env_name: str) -> None:
    if not path.is_dir():
        raise click.ClickException(
            f"缺少{label}目录：{path}（用 {option} 或环境变量 {env_name} 指定）"
        )


def write_manifest(out_dir: Path, file_names: Sequence[str], version: str | None) -> Path | None:
    """把发布清单写进 ``out_dir``：版本 + 这一批每份原始形态的本体指纹（按目录里那份文件重算）；内容没变时返回 ``None``。

    清单是发布端与 ``fetch`` 之间的约定：``fetch`` 拿本地文件里自称的 ``sha256``（:func:`read_hash`）跟它比，
    一样就不重下，也认得「同一个版本号下重新发布过」。收哪几份跟着这一批走：``generate --only`` 就只列那几份，
    ``out_dir`` 里其余的（上一次生成或 ``fetch`` 下载留下的旧文件）不算这一批，``fetch`` 会保持本地原样。指纹
    按目录里那份文件的内容重算，不照抄文件自称的值 —— 清单要说的是「发布的这批字节是什么」，这一批里内容没变、
    这次没重写的也照它现在的内容算。清单本身不入库，只跟着原始形态一起发布。
    """
    files = {
        file_name: digest
        for file_name in sorted(file_names)
        if (digest := read_payload_hash(out_dir / file_name)) is not None
    }
    manifest: dict[str, Any] = {}
    if version:
        manifest[VERSION_KEY] = version
    manifest["files"] = files

    path = out_dir / MANIFEST_FILE
    if is_unchanged(path, manifest):
        return None
    write_json(path, manifest)
    return path


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

    每份数据都在最前面写上元数据：版本取 ``--version`` / 从兄弟仓库读到的版本（形如
    ``1.6.3+11010254-4``：客户端版本 + 数据构建号），都读不到就沿用已有文件里的版本（本地重算不会
    凭空改掉版本号）；``sha256`` 是这份数据原始形态的内容指纹，按这次生成的本体重算 —— 只有这里算它，
    入库与 ``fetch`` 都照抄。
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
            write_json(output_path, data)
        except OSError as error:
            raise click.ClickException(f"[{dataset.name}] 写入失败：{error}") from error
        click.echo(f"[{dataset.name}] 已生成：{output_path}")
        written += 1
    return written


@click.command(name="generate")
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
    help="原始数据目录（原样产物，只带世界坐标）",
)
@click.option(
    "--data-dir",
    type=click.Path(path_type=Path, file_okay=False),
    default=ZMDMAP_DATA_DIR,
    show_default=True,
    help="入库数据目录（需要投影的投影后落这里，其余原样复制进来）",
)
@click.option(
    "--only",
    type=click.Choice(DATASET_NAMES),
    multiple=True,
    help="只处理列出的数据集（可重复给；缺省全部）",
)
@click.option("--sync/--no-sync", default=False, help="生成前先同步兄弟仓库（只做快进合并）")
@click.option(
    "--version",
    "data_version_option",
    default=None,
    help="写进每份数据的版本，形如 1.6.3+11010254-4（客户端版本+数据构建号；默认从兄弟仓库读，都读不到就沿用原有的）",
)
@click.option(
    "--raw-only",
    is_flag=True,
    help="只出原始形态（只写 --out-dir），不做入库",
)
@click.option(
    "--nav",
    default=None,
    help="投影用的 nav 数据（本地路径或 URL；缺省用子模块里的 map/navmesh/base.nav.gz）",
)
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

    # 版本优先命令行；否则从兄弟仓库读（数据里的构建号 > 提交标题），工作区里没有兄弟仓库时退回
    # TableCfg 所在的那个仓库，它通常就是 BeyondTableCfg。
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
            out_dir, [dataset.file_name for dataset in datasets], version
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
        click.echo("提示：每份数据最前面写着 version（版本标签）与 sha256（原始形态的内容指纹）；两个字段都只在这里写，入库与 fetch 只沿用。")

    if sync_failed or not generated_ok or materialized_ok is False:
        ctx.exit(1)
