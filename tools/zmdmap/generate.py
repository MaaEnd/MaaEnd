"""``zmdmap generate``：从本地游戏数据生成精简数据，再入库。

每份数据集读哪些目录（TableCfg / JsonData / LuaScripts / 解包资源）由它 ``build`` 的参数决定，
用不到的不检查也不索要。

各份数据一律先出原始形态（只带游戏世界坐标）进 ``--out-dir``，再入库到 ``--data-dir``：需要投影的按
BaseNav 的 zone 表补上底图像素，不需要投影的原样复制；``--raw-only`` 时只出原始形态。

``version`` 只在这里写，且必须是完整标签（客户端版本 + 构建号，如 ``1.6.4+11262943-8``）：取
``--version``（发布端显式传），其次
:func:`tools.zmdmap.utils.version.data_version`（兄弟仓库的 ledger 构建号 + 提交标题里的 ``Client:``），再次
:func:`tools.zmdmap.utils.version.data_version_from_path`（数据目录路径里的 ``initial_…_main_…``，只有构建号
那一半）。都拿不到、或只凑到一半，就直接报错，不生成没有版本或版本不全的数据。写到每份原始数据的最前面；
入库与 ``fetch`` 都只沿用，不改它。
"""

import time
from collections.abc import Sequence
from pathlib import Path
from typing import Any

import click

from .datasets import DATASET_NAMES, DATASETS, Dataset, files_of, select_datasets
from .materialize import copy_plain, report_data_changes, run_projection
from .utils.config import (
    ASSETS_DIR_ENV,
    DEFAULT_OUT_DIR,
    DEFAULT_WORKSPACE,
    JSON_DATA_DIR_ENV,
    LUA_DIR_ENV,
    MANIFEST_FILE,
    TABLE_CFG_DIR_ENV,
    WORKSPACE_ENV,
    ZMDMAP_DATA_DIR,
    assets_dir_in,
    json_data_dir_in,
    lua_dir_in,
    table_cfg_dir_in,
)
from .utils.datafile import (
    VERSION_KEY,
    is_unchanged,
    read_payload_hash,
    with_metadata,
    write_json,
)
from .utils.injection import SERVICE_OPTIONS, Injection, InjectionError, parse_injection
from .utils.inputs import Assets, JsonData, LuaScripts, TableCfg
from .utils.log import logger
from .utils.version import (
    data_version,
    data_version_from_path,
    is_complete_version,
    normalize_version,
    repo_data_version,
)
from .utils.workspace import (
    SYNC_MARK_UNKNOWN,
    SYNC_MARKS,
    preflight,
    sync_workspace,
)

# 路径是怎么定下来的，日志里说清楚（CI 上排查「读的是哪份数据」时最有用）。
_SOURCE_LABELS: dict[click.core.ParameterSource, str] = {
    click.core.ParameterSource.COMMANDLINE: "命令行",
    click.core.ParameterSource.ENVIRONMENT: "环境变量",
    click.core.ParameterSource.DEFAULT: "默认",
}


def parameter_source(ctx: click.Context, name: str) -> str:
    """这个参数的值是命令行给的、环境变量给的，还是没给走的默认值。"""
    source = ctx.get_parameter_source(name)
    if source is None:
        return "默认"
    return _SOURCE_LABELS.get(source, "默认")


def check_input_dir(label: str, path: Path, option: str, env_name: str) -> None:
    if not path.is_dir():
        raise click.ClickException(f"缺少{label}目录：{path}（用 {option} 或环境变量 {env_name} 指定）")


def write_manifest(out_dir: Path, file_names: Sequence[str], version: str) -> Path | None:
    """把发布清单写进 ``out_dir``：版本 + 这一批每份原始形态的本体指纹（按目录里那份文件重算）；内容没变时返回 ``None``。

    清单是发布端与 ``fetch`` 之间的约定：``fetch`` 拿本地文件里自称的 ``sha256``（:func:`read_hash`）跟它比，
    一样就不重下，也认得「同一个版本号下重新发布过」。收哪几份跟着这一批走：``generate --only`` 就只列那几份，
    ``out_dir`` 里其余的（上一次生成或 ``fetch`` 下载留下的旧文件）不算这一批，``fetch`` 会保持本地原样。指纹
    按目录里那份文件的内容重算，不照抄文件自称的值 —— 清单要说的是「发布的这批字节是什么」，这一批里内容没变、
    这次没重写的也照它现在的内容算。清单本身不入库，只跟着原始形态一起发布。

    ``version`` 必须有：``fetch`` 拿它跟本地那份的 ``version`` 比新旧，缺了清单就没法用（那边会直接报错）。
    :func:`generate_command` 已经把版本凑齐并校验过，这里再挡一次，避免有人绕开它写出没版本的清单。
    """
    if not version:
        raise ValueError("清单必须写版本：fetch 靠它判断本地那份比上游新还是旧")
    files = {
        file_name: digest
        for file_name in sorted(file_names)
        if (digest := read_payload_hash(out_dir / file_name)) is not None
    }
    manifest: dict[str, Any] = {VERSION_KEY: version, "files": files}

    path = out_dir / MANIFEST_FILE
    if is_unchanged(path, manifest):
        return None
    write_json(path, manifest)
    return path


def dataset_injection(dataset: type[Dataset]) -> Injection:
    """解析一份数据的 ``build`` 签名；参数写错了按输入错误报出来，不抛栈。"""
    try:
        return parse_injection(dataset.build)
    except InjectionError as error:
        raise click.ClickException(f"[{dataset.name}] {error}") from error


def generate_datasets(
    datasets: Sequence[type[Dataset]],
    services: dict[str, Any],
    out_dir: Path,
    *,
    version: str,
    force: bool,
) -> int:
    """跑 ``build`` 并把原始形态落到 ``out_dir``；返回成功生成的份数。

    ``build`` 要哪些服务由它的参数决定（见 :func:`dataset_injection`），这里按参数名把服务传进去。
    每份数据都在最前面写上元数据：``version`` 是调用方定好的完整标签（``1.6.3+11010254-4``，客户端版本与
    构建号都在，调用前已校验）；``sha256`` 是这份数据原始形态的内容指纹，按这次生成的本体重算 —— 只有这里
    算它，入库与 ``fetch`` 都照抄。
    """
    written = 0
    for dataset in datasets:
        output_path = out_dir / dataset.file_name
        kwargs = dataset_injection(dataset).kwargs(services)
        try:
            data = dataset.build(**kwargs)
        except (OSError, ValueError) as error:
            raise click.ClickException(f"[{dataset.name}] {error}") from error
        data = with_metadata(data, version)
        if not force and is_unchanged(output_path, data):
            logger.info(f"{dataset.name} 生成结果未变化，跳过写入：{output_path}")
            continue
        try:
            write_json(output_path, data)
        except OSError as error:
            raise click.ClickException(f"[{dataset.name}] 写入失败：{error}") from error
        logger.info(f"{dataset.name} 已生成：{output_path}")
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
    "--lua-dir",
    type=click.Path(path_type=Path, file_okay=False),
    envvar=LUA_DIR_ENV,
    default=None,
    help="解包出的 LuaScripts 目录（默认：<workspace>/BeyondLua/LuaScripts；只有 region3d 数据集会读）",
)
@click.option(
    "--assets-dir",
    type=click.Path(path_type=Path, file_okay=False),
    envvar=ASSETS_DIR_ENV,
    default=None,
    help="解包出的 Unity 资源根，里面是 assets/ 那棵树（默认：<workspace>/BeyondImage/assets；只有 region3d 会读）",
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
@click.option(
    "--sync/--no-sync",
    default=False,
    help="生成前先同步兄弟仓库（表 / JsonData / Lua / 解包资源，只做快进合并；资源仓库很大）",
)
@click.option(
    "--version",
    "data_version_option",
    default=None,
    help="写进每份数据的版本，形如 1.6.4+11262943-8（客户端版本+数据构建号，两半都要有）；不给就从兄弟仓库读，读不到就报错",
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
    lua_dir: Path | None,
    assets_dir: Path | None,
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
            injection = dataset_injection(dataset)
            extra = "".join(
                f"，另需 {SERVICE_OPTIONS[service]}" for service in injection.services if service != "table_cfg"
            )
            logger.info(f"{dataset.name}\t{dataset.file_name}\t{how}{extra}")
        return

    # click 的 envvar 已经把环境变量填进参数值了，这里只补最后的默认值。
    workspace = (workspace or DEFAULT_WORKSPACE).expanduser().resolve()
    table_cfg_dir = (table_cfg_dir or table_cfg_dir_in(workspace)).expanduser().resolve()
    json_data_dir = (json_data_dir or json_data_dir_in(workspace)).expanduser().resolve()
    lua_dir = (lua_dir or lua_dir_in(workspace)).expanduser().resolve()
    assets_dir = (assets_dir or assets_dir_in(workspace)).expanduser().resolve()
    out_dir = out_dir.expanduser().resolve()
    data_dir = data_dir.expanduser().resolve()
    datasets = select_datasets(only)
    injections = {dataset: dataset_injection(dataset) for dataset in datasets}
    needed = {service for injection in injections.values() for service in injection.services}
    pipeline_files = files_of(datasets, projected=True)
    plain_files = files_of(datasets, projected=False)
    needs_json_data = "json_data" in needed
    needs_lua = "lua" in needed
    needs_assets = "assets" in needed

    logger.info(f"工作区（兄弟仓库）：{workspace}（{parameter_source(ctx, 'workspace')}）")
    logger.info(f"TableCfg：{table_cfg_dir}（{parameter_source(ctx, 'table_cfg_dir')}）")
    # 下面三样只有声明了对应参数的数据集才读；没选中它们时不打出来，免得让人以为每次生成都得挂着几十 GB 的解包资源。
    if needs_json_data:
        logger.info(f"JsonData：{json_data_dir}（{parameter_source(ctx, 'json_data_dir')}）")
    if needs_lua:
        logger.info(f"LuaScripts：{lua_dir}（{parameter_source(ctx, 'lua_dir')}）")
    if needs_assets:
        logger.info(f"解包资源：{assets_dir}（{parameter_source(ctx, 'assets_dir')}）")
    logger.info(f"数据集：{len(datasets)} 个（{'、'.join(d.name for d in datasets)}）")

    # 版本优先命令行；否则从兄弟仓库读（数据里的构建号 > 提交标题），工作区里没有兄弟仓库时退回
    # TableCfg 所在的那个仓库，它通常就是 BeyondTableCfg；两处都没有（发布端直接把解包目录递进来）
    # 就从数据目录路径里认构建号。
    detected, origin = data_version(workspace), "兄弟仓库"
    if not detected:
        detected, origin = repo_data_version(table_cfg_dir.parent), "数据仓库"
    if not detected:
        detected, origin = data_version_from_path(table_cfg_dir, json_data_dir), "数据目录路径"
    # ``--version`` 也认资源构建号的长写法（``1.6.4+initial_11262943-8_main_11262943-8``），统一缩成短号再写。
    version = normalize_version(data_version_option) if data_version_option else detected
    if version is None:
        raise click.ClickException(
            "拿不到版本：兄弟仓库里没有（ledger 与提交标题都读不到）、数据目录路径里也没有构建号，"
            "也没给 --version。版本必须写成 客户端版本+构建号（如 1.6.3+11010254-4），请用 --version 指定"
        )
    if not is_complete_version(version):
        raise click.ClickException(
            f"版本不完整：{version} —— 必须同时有客户端版本和构建号（如 1.6.3+11010254-4）。"
            "数据目录路径里只有构建号、提交标题里的 Client: 才有客户端版本；凑不齐就用 --version 指定"
        )
    logger.info(f"数据版本：{version}（{'命令行' if data_version_option else origin}）")

    sync_failed = False
    if sync:
        logger.section("同步")
        outcomes, sync_failed = sync_workspace(workspace, dry_run=dry_run)
        for outcome in outcomes:
            mark = SYNC_MARKS.get(outcome.state, SYNC_MARK_UNKNOWN)
            logger.info(f"[{mark}] {outcome.name:<20} {outcome.detail}")

    problems = preflight(
        table_cfg_dir,
        json_data_dir,
        lua_dir=lua_dir if needs_lua else None,
        assets_dir=assets_dir if needs_assets else None,
        check_navmesh=not raw_only and bool(pipeline_files),
        check_json_data=needs_json_data,
    )
    if problems:
        logger.section("预检提示")
        for problem in problems:
            # 只用 GBK 里有的字符：输出走 click，写不出的字符会被替换成问号而不是报错，但问号同样没法看。
            logger.warning(f"! {problem}")

    if dry_run:
        logger.section("将执行（--dry-run，未执行）")
        logger.info(f"原始数据目录：{out_dir}")
        logger.info(f"入库数据目录：{data_dir}")
        if raw_only:
            logger.info("入库    ：未启用（--raw-only）")
        else:
            logger.info(f"投影    ：{len(pipeline_files)} 份 {out_dir} -> {data_dir}")
            logger.info(f"复制    ：{len(plain_files)} 份 {out_dir} -> {data_dir}")
        logger.info("--dry-run：没有合并仓库，也没有写任何数据文件")
        return

    check_input_dir("TableCfg", table_cfg_dir, "--table-cfg-dir", TABLE_CFG_DIR_ENV)
    if needs_json_data:
        check_input_dir("JsonData", json_data_dir, "--json-data-dir", JSON_DATA_DIR_ENV)
    if needs_lua:
        check_input_dir("LuaScripts", lua_dir, "--lua-dir", LUA_DIR_ENV)
    if needs_assets:
        check_input_dir("解包资源", assets_dir, "--assets-dir", ASSETS_DIR_ENV)

    logger.section("生成（原始形态）")
    started = time.monotonic()
    generated_ok = True
    written = 0
    try:
        written = generate_datasets(
            datasets,
            {
                "table_cfg": TableCfg(table_cfg_dir),
                "json_data": JsonData(json_data_dir),
                "lua": LuaScripts(lua_dir),
                "assets": Assets(assets_dir),
            },
            out_dir,
            version=version,
            force=force,
        )
        manifest_path = write_manifest(out_dir, [dataset.file_name for dataset in datasets], version)
        if manifest_path is not None:
            logger.info(f"{MANIFEST_FILE} -> {manifest_path}")
    except click.ClickException as error:
        generated_ok = False
        logger.error(f"x {error.message}")
    except OSError as error:
        generated_ok = False
        logger.error(f"x 写 {MANIFEST_FILE} 失败：{error}")
    generate_seconds = time.monotonic() - started

    materialized_ok: bool | None = None
    if raw_only:
        materialized_ok = None
    elif not generated_ok:
        materialized_ok = False
        logger.section("入库：（跳过：生成失败）")
    else:
        logger.section("入库（原始形态 -> data/）")
        materialized_ok = True
        try:
            projected = run_projection(out_dir, data_dir, pipeline_files, nav=nav, force=force)
            copied = copy_plain(out_dir, data_dir, plain_files, force=force)
            logger.info(f"投影 {projected} 份、复制 {copied} 份")
        except click.ClickException as error:
            logger.error(f"x {error.message}")
            materialized_ok = False

    logger.section("汇总")
    if sync:
        logger.info(f"同步：{'有失败' if sync_failed else '正常'}")
    else:
        logger.info("同步：未执行（--no-sync）")
    if generated_ok:
        unchanged = len(datasets) - written
        logger.info(f"生成：正常（{generate_seconds:.1f}s，重写 {written} 份、未变化 {unchanged} 份）")
    else:
        logger.info(f"生成：失败（{generate_seconds:.1f}s）")
    if materialized_ok is None:
        logger.info("入库：未执行（--raw-only）")
    else:
        logger.info(f"入库：{'正常' if materialized_ok else '失败'}（{data_dir}）")
    report_data_changes(data_dir, [dataset.file_name for dataset in datasets])
    if data_dir == ZMDMAP_DATA_DIR:
        logger.info(
            "提示：每份数据最前面写着 version（版本标签）与 sha256（原始形态的内容指纹）。"
            "sha256 只在这里算一次，入库原样沿用、fetch 核对内容后照抄。"
        )

    if sync_failed or not generated_ok or materialized_ok is False:
        ctx.exit(1)
