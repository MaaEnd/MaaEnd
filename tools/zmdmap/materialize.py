"""入库：把 ``data/raw/`` 里的原始数据材料化进入库的 ``data/``。

需要投影的按 BaseNav 的 zone 表把世界坐标换成底图像素（:mod:`tools.zmdmap.utils.project`），其余原样复制。
两个元数据字段（``version`` / ``sha256``）都原样沿用原始形态那份：``sha256`` 是这份数据的批次标识，投影
之后也不重算 —— 于是 ``data/`` 与 ``data/raw/`` 里同名的两份指纹一致，``fetch`` 拿本地文件里的 ``sha256``
跟发布清单比就够了（见 :mod:`tools.zmdmap.utils.datafile`）。

``generate`` 与 ``fetch`` 都跑这一步，所以这里只认目录与文件名，不关心原始数据是本地生成来的还是
下载来的。
"""

from __future__ import annotations

from collections.abc import Sequence
from pathlib import Path
from typing import Any

import click

from .utils import datafile
from .utils.navmesh import load_nav_zones
from .utils.config import REPO_ROOT, ZMDMAP_DATA_DIR
from .utils.project import project_dataset
from .utils.sync import changed_data_files


def _missing_raw(source_path: Path) -> ValueError:
    return ValueError(
        f"缺少原始数据文件：{source_path}"
        "（先跑 uv run zmdmap fetch 下载，或 uv run zmdmap generate 本地生成）"
    )


def run_projection(
    raw_dir: Path,
    data_dir: Path,
    file_names: Sequence[str],
    *,
    nav: str | None = None,
    force: bool = False,
) -> int:
    """把这几份原始数据投影进 ``data_dir``；返回写入份数。"""
    if not file_names:
        return 0
    try:
        return _project(raw_dir, data_dir, file_names, nav=nav, force=force)
    except (OSError, ValueError) as error:
        raise click.ClickException(str(error)) from error


def _project(
    raw_dir: Path,
    data_dir: Path,
    file_names: Sequence[str],
    *,
    nav: str | None,
    force: bool,
) -> int:
    zones = load_nav_zones(nav, REPO_ROOT)
    skipped_maps: set[str] = set()
    pending: list[tuple[str, Path, Any]] = []
    for file_name in file_names:
        source_path = raw_dir / file_name
        if not source_path.exists():
            raise _missing_raw(source_path)
        raw = datafile.read_json(source_path)
        body = project_dataset(file_name, datafile.payload_of(raw), zones, skipped_maps)
        # 入库只换本体：``version`` 与 ``sha256`` 都原样沿用原始形态那份（指纹是这份数据的批次标识，投影
        # 之后也不重算），于是入库数据与原始形态的指纹一致，``fetch`` 拿它跟发布清单比就够了。
        result = datafile.keep_metadata(raw, body)
        output_path = data_dir / file_name
        if not force and datafile.is_unchanged(output_path, result):
            click.echo(f"[materialize] {file_name} 未变化，跳过写入")
            continue
        pending.append((file_name, output_path, result))

    data_dir.mkdir(parents=True, exist_ok=True)
    for file_name, output_path, result in pending:
        datafile.write_json(output_path, result)
        click.echo(f"[materialize] {file_name} -> {output_path}")
    if skipped_maps:
        names = ", ".join(sorted(skipped_maps))
        click.echo(f"[materialize] 以下底图没有对应的 BaseNav zone，其条目未纳入：{names}", err=True)
    return len(pending)


def copy_plain(
    raw_dir: Path,
    data_dir: Path,
    file_names: Sequence[str],
    *,
    force: bool = False,
) -> int:
    """不需要投影的数据：原样从 ``raw_dir`` 复制进 ``data_dir``；返回写入份数。"""
    if not file_names:
        return 0
    try:
        return _copy(raw_dir, data_dir, file_names, force=force)
    except (OSError, ValueError) as error:
        raise click.ClickException(str(error)) from error


def _copy(raw_dir: Path, data_dir: Path, file_names: Sequence[str], *, force: bool) -> int:
    data_dir.mkdir(parents=True, exist_ok=True)
    written = 0
    for file_name in file_names:
        source_path = raw_dir / file_name
        if not source_path.exists():
            raise _missing_raw(source_path)
        text = source_path.read_text(encoding="utf-8")
        output_path = data_dir / file_name
        if not force and output_path.exists() and output_path.read_text(encoding="utf-8") == text:
            click.echo(f"[materialize] {file_name} 未变化，跳过写入")
            continue
        output_path.write_text(text, encoding="utf-8", newline="\n")
        click.echo(f"[materialize] {file_name} -> {output_path}")
        written += 1
    return written


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
