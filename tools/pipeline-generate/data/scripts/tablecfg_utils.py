from __future__ import annotations

import argparse
import json
import sys
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any

DATA_DIR = Path(__file__).resolve().parent.parent
MAAEND_REPO_DIR = DATA_DIR.parents[2]
ENDFIELD_WORKSPACE_DIR = MAAEND_REPO_DIR.parent
DEFAULT_TABLE_CFG_DIR = ENDFIELD_WORKSPACE_DIR / "BeyondTableCfg" / "TableCfg"
DEFAULT_JSON_DATA_DIR = ENDFIELD_WORKSPACE_DIR / "BeyondMemoryPack" / "JsonData"

LOCALE_TABLE_SUFFIXES = {
    "zh_cn": "CN",
    "zh_tw": "TC",
    "en_us": "EN",
    "ja_jp": "JP",
    "ko_kr": "KR",
}
LOCALE_TABLES = tuple(
    f"I18nTextTable_{suffix}.json" for suffix in LOCALE_TABLE_SUFFIXES.values()
)


class TableCfgError(ValueError):
    pass


def assert_record(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise TableCfgError(f"{label} 不是对象")
    return value


def assert_list(value: Any, label: str) -> list[Any]:
    if not isinstance(value, list):
        raise TableCfgError(f"{label} 不是数组")
    return value


def sorted_entries(record: dict[str, Any]) -> list[tuple[str, Any]]:
    return sorted(record.items(), key=lambda entry: entry[0])


def unique_sorted_strings(values: Any, label: str) -> list[str]:
    items = assert_list(values, label)
    if not all(
        isinstance(value, (str, int, float)) and not isinstance(value, bool)
        for value in items
    ):
        raise TableCfgError(f"{label} 包含无法转为字符串的元素")
    return sorted({str(value) for value in items})


def build_locale_tables(tables: dict[str, Any]) -> dict[str, dict[str, Any]]:
    return {
        locale: assert_record(
            tables[f"I18nTextTable_{suffix}.json"],
            f"I18nTextTable_{suffix}",
        )
        for locale, suffix in LOCALE_TABLE_SUFFIXES.items()
    }


def build_localized_names(
    reference: Any,
    locale_tables: dict[str, dict[str, Any]],
    label: str,
) -> dict[str, str]:
    reference_record = assert_record(reference, f"{label} i18n 引用")
    text_id_value = reference_record.get("id")
    if text_id_value is None:
        raise TableCfgError(f"{label} 缺少 i18n id")
    text_id = str(text_id_value)

    names: dict[str, str] = {}
    for locale in LOCALE_TABLE_SUFFIXES:
        text = locale_tables[locale].get(text_id)
        if not isinstance(text, str) or not text:
            raise TableCfgError(f"{label} 在 {locale} 中缺少文本 {text_id}")
        names[locale] = text
    return names


def intersects(left: Sequence[Any], right: Sequence[Any]) -> bool:
    right_values = set(right)
    return any(value in right_values for value in left)


def resolve_json_paths(
    directory: Path, file_names: Sequence[str], label: str
) -> dict[str, Path]:
    resolved_directory = directory.expanduser().resolve()
    if not resolved_directory.is_dir():
        raise TableCfgError(f"{label} 目录不存在：{resolved_directory}")

    paths: dict[str, Path] = {}
    for file_name in file_names:
        path = resolved_directory / file_name
        if not path.is_file():
            raise TableCfgError(f"缺少 {label} 文件：{path}")
        paths[file_name] = path
    return paths


def resolve_table_paths(
    table_cfg_dir: Path, table_names: Sequence[str]
) -> dict[str, Path]:
    return resolve_json_paths(table_cfg_dir, table_names, "TableCfg")


def load_tables(table_paths: dict[str, Path]) -> dict[str, Any]:
    tables: dict[str, Any] = {}
    for table_name, path in table_paths.items():
        try:
            tables[table_name] = json.loads(path.read_text(encoding="utf-8-sig"))
        except json.JSONDecodeError as error:
            raise TableCfgError(
                f"{table_name} 不是有效 JSON（第 {error.lineno} 行，第 {error.colno} 列）"
            ) from error
    return tables


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


def parse_arguments(
    description: str,
    output_path: Path,
    args: Sequence[str] | None = None,
) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=description)
    parser.add_argument(
        "--table-cfg-dir",
        type=Path,
        default=DEFAULT_TABLE_CFG_DIR,
        help=f"TableCfg 目录（默认：{DEFAULT_TABLE_CFG_DIR}）",
    )
    parser.add_argument(
        "--output",
        type=Path,
        default=output_path,
        help=f"输出文件（默认：{output_path}）",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="即使生成结果未变化也强制重写输出文件",
    )
    return parser.parse_args(args)


def sync_dataset(
    *,
    label: str,
    table_names: Sequence[str],
    output_path: Path,
    build_data: Callable[[dict[str, Any]], dict[str, Any]],
    table_cfg_dir: Path,
    force: bool,
) -> bool:
    table_paths = resolve_table_paths(table_cfg_dir, table_names)
    data = assert_record(build_data(load_tables(table_paths)), f"{label} 生成结果")
    if should_skip(output_path, data, force):
        print(f"[{label}] 生成结果未变化，跳过写入；可使用 --force 强制重写")
        return False

    write_dataset(output_path, data)
    print(f"[{label}] 已从本地 TableCfg 生成数据：{output_path}")
    return True


def run_cli(
    *,
    label: str,
    description: str,
    table_names: Sequence[str],
    output_path: Path,
    build_data: Callable[[dict[str, Any]], dict[str, Any]],
    args: Sequence[str] | None = None,
) -> int:
    options = parse_arguments(description, output_path, args)
    try:
        sync_dataset(
            label=label,
            table_names=table_names,
            output_path=options.output,
            build_data=build_data,
            table_cfg_dir=options.table_cfg_dir,
            force=options.force,
        )
    except (OSError, ValueError) as error:
        print(f"[{label}] {error}", file=sys.stderr)
        return 1
    return 0
