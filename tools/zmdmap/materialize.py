"""入库：把 ``data/raw/`` 里的原始数据材料化进入库的 ``data/``。

需要投影的按 BaseNav 的 zone 表把世界坐标换成底图像素（:mod:`tools.zmdmap.utils.project`），其余原样复制。
两个元数据字段（``version`` / ``sha256``）都原样沿用原始形态那份：``sha256`` 是这份数据的批次标识，投影
之后也不重算 —— 于是 ``data/`` 与 ``data/raw/`` 里同名的两份指纹一致，``fetch`` 拿本地文件里的 ``sha256``
跟发布清单比就够了（见 :mod:`tools.zmdmap.utils.datafile`）。

``generate`` 与 ``fetch`` 都跑这一步，所以这里只认目录与文件名，不关心原始数据是本地生成来的还是
下载来的。
"""

import json
import re
from collections.abc import Sequence
from pathlib import Path
from typing import Any

import click

from .utils import datafile
from .utils.config import REPO_ROOT, ZMDMAP_DATA_DIR
from .utils.log import logger
from .utils.navmesh import load_nav_zones
from .utils.project import project_dataset
from .utils.workspace import changed_data_files


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
            logger.info(f"{file_name} 未变化，跳过写入")
            continue
        pending.append((file_name, output_path, result))

    data_dir.mkdir(parents=True, exist_ok=True)
    for file_name, output_path, result in pending:
        datafile.write_json(output_path, result)
        logger.info(f"{file_name} -> {output_path}")
    if skipped_maps:
        names = ", ".join(sorted(skipped_maps))
        logger.warning(f"以下底图没有对应的 BaseNav zone，其条目未纳入：{names}")
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
        written = _copy(raw_dir, data_dir, file_names, force=force)
        if "usable_items.json" in file_names:
            sync_usable_items_task_options(data_dir)
        return written
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
        try:
            unchanged = not force and output_path.read_text(encoding="utf-8") == text
        except (OSError, UnicodeError):
            unchanged = False
        if unchanged:
            logger.info(f"{file_name} 未变化，跳过写入")
            continue
        output_path.write_text(text, encoding="utf-8", newline="\n")
        logger.info(f"{file_name} -> {output_path}")
        written += 1
    return written


def sync_usable_items_task_options(data_dir: Path) -> None:
    """把生成的主动可用道具表放入资源目录并更新协议空间选项。"""
    source_path = data_dir / "usable_items.json"
    data_path = REPO_ROOT / "assets" / "data" / "ProtocolSpace" / "usable_items.json"
    task_path = REPO_ROOT / "assets" / "tasks" / "ProtocolSpace.json"
    try:
        data_text = source_path.read_text(encoding="utf-8")
        data = json.loads(data_text)
        task_text = task_path.read_text(encoding="utf-8")
    except (OSError, ValueError) as error:
        raise click.ClickException(f"同步协议空间可用道具失败：{error}") from error

    items = data.get("items") if isinstance(data, dict) else None
    if not isinstance(items, dict):
        raise click.ClickException(f"{source_path} 的 items 必须是对象")

    cases: list[dict[str, Any]] = []
    for item_id, item in sorted(items.items()):
        if not isinstance(item, dict):
            raise click.ClickException(f"{source_path} 中 {item_id} 的记录必须是对象")
        name = item.get("name")
        if not isinstance(name, str) or not name:
            raise click.ClickException(f"{source_path} 中 {item_id} 缺少物品名称")
        icon_params = {
            "grid_type": "transfer",
            "item_ids": [item_id],
            "item_recheck_filters": ["Normal:Usable"],
            "deduplicate": True,
        }
        cases.append(
            {
                "name": item_id,
                "label": f"$iconRecognition.name.{item_id}",
                "pipeline_override": {
                    "ProtocolSpaceBattleConsumableHasItem": {"enabled": True},
                    **{
                        node: {
                            "recognition": {
                                "param": {"custom_recognition_param": icon_params}
                            }
                        }
                        for node in (
                            "ProtocolSpaceBattleConsumableStockWulingFindItem",
                            "ProtocolSpaceBattleConsumableStockValleyIVFindItem",
                            "ProtocolSpaceBattleConsumableUseFindItem",
                        )
                    }
                },
            }
        )

    if not cases:
        raise click.ClickException(f"{source_path} 中没有可供选择的主动可用道具")

    option = re.search(r'"ProtocolSpaceBattleConsumableItem"\s*:\s*\{', task_text)
    cases_match = re.search(r'"cases"\s*:\s*\[', task_text[option.end() :]) if option else None
    if option is None or cases_match is None:
        raise click.ClickException(f"{task_path} 缺少 ProtocolSpaceBattleConsumableItem")
    cases_start = option.end() + cases_match.start()
    array_start = task_text.index("[", cases_start)

    # 任务文件是 JSONC；只定位并替换 cases 数组，保留其他任务注释。
    depth = 0
    in_string = False
    escaped = False
    array_end = -1
    for index in range(array_start, len(task_text)):
        char = task_text[index]
        if in_string:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                in_string = False
        elif char == '"':
            in_string = True
        elif char == "[":
            depth += 1
        elif char == "]":
            depth -= 1
            if depth == 0:
                array_end = index
                break
    if array_end < 0:
        raise click.ClickException(f"{task_path} 中可用道具 cases 数组没有闭合")

    original_task_text = task_text
    current_cases = task_text[array_start : array_end + 1]
    try:
        parsed_cases = json.loads(current_cases)
    except ValueError:
        parsed_cases = None
    if parsed_cases != cases:
        property_start = task_text.rfind("\n", 0, cases_start) + 1
        property_indent = task_text[property_start:cases_start]
        formatted_cases = json.dumps(cases, ensure_ascii=False, indent=4).splitlines()
        replacement = formatted_cases[0] + "\n" + "\n".join(
            property_indent + line for line in formatted_cases[1:]
        )
        task_text = task_text[:array_start] + replacement + task_text[array_end + 1 :]
        default_case_search_start = array_start + len(replacement)
    else:
        default_case_search_start = array_end + 1

    default_case = cases[0]["name"]
    default_case_match = re.search(
        r'\s*,\s*"default_case"\s*:\s*"([^"]*)"',
        task_text[default_case_search_start:],
    )
    if default_case_match is None:
        raise click.ClickException(
            f"{task_path} 缺少 ProtocolSpaceBattleConsumableItem.default_case"
        )
    if default_case_match.group(1) != default_case:
        default_case_start = default_case_search_start + default_case_match.start(1)
        default_case_end = default_case_search_start + default_case_match.end(1)
        task_text = (
            task_text[:default_case_start]
            + json.dumps(default_case, ensure_ascii=False)[1:-1]
            + task_text[default_case_end:]
        )

    if task_text != original_task_text:
        task_path.write_text(task_text, encoding="utf-8", newline="\n")

    if source_path.resolve() != data_path.resolve():
        data_path.parent.mkdir(parents=True, exist_ok=True)
        data_path.write_text(data_text, encoding="utf-8", newline="\n")


def report_data_changes(data_dir: Path, file_names: Sequence[str]) -> None:
    """入库目录的改动（只在入库目录就是默认的 data/ 时报）。"""
    if data_dir != ZMDMAP_DATA_DIR:
        return
    changed = changed_data_files(list(file_names))
    if changed:
        logger.info("data/*.json 改动：")
        for line in changed:
            logger.info(line)
    else:
        logger.info("data/*.json：没有改动")
