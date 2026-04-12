#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


DEFAULT_SOURCE_ROOT = Path("assets/resource/pipeline")
DEFAULT_TARGET_ROOT = Path("assets/resource_wlroots/pipeline")


VK_TO_EVDEV: dict[int, int] = {
    0x08: 14,
    0x09: 15,
    0x0D: 28,
    0x10: 42,
    0x11: 29,
    0x12: 56,
    0x13: 119,
    0x14: 58,
    0x1B: 1,
    0x20: 57,
    0x21: 104,
    0x22: 109,
    0x23: 107,
    0x24: 102,
    0x25: 105,
    0x26: 103,
    0x27: 106,
    0x28: 108,
    0x2C: 99,
    0x2D: 110,
    0x2E: 111,
    0x5B: 125,
    0x5C: 126,
    0x5D: 127,
    0xA0: 42,
    0xA1: 54,
    0xA2: 29,
    0xA3: 97,
    0xA4: 56,
    0xA5: 100,
    0x6A: 55,
    0x6B: 78,
    0x6C: 83,
    0x6D: 74,
    0x6E: 82,
    0x6F: 98,
    0xBA: 39,
    0xBB: 13,
    0xBC: 51,
    0xBD: 12,
    0xBE: 52,
    0xBF: 53,
    0xC0: 41,
    0xDB: 26,
    0xDC: 43,
    0xDD: 27,
    0xDE: 40,
}

# Number row (top row): VK '1'..'9' -> evdev 2..10, VK '0' -> evdev 11.
for offset, evdev_code in enumerate(range(2, 11), start=1):
    VK_TO_EVDEV[0x30 + offset] = evdev_code
VK_TO_EVDEV[0x30] = 11

VK_TO_EVDEV.update(
    {
        ord("A"): 30,
        ord("B"): 48,
        ord("C"): 46,
        ord("D"): 32,
        ord("E"): 18,
        ord("F"): 33,
        ord("G"): 34,
        ord("H"): 35,
        ord("I"): 23,
        ord("J"): 36,
        ord("K"): 37,
        ord("L"): 38,
        ord("M"): 50,
        ord("N"): 49,
        ord("O"): 24,
        ord("P"): 25,
        ord("Q"): 16,
        ord("R"): 19,
        ord("S"): 31,
        ord("T"): 20,
        ord("U"): 22,
        ord("V"): 47,
        ord("W"): 17,
        ord("X"): 45,
        ord("Y"): 21,
        ord("Z"): 44,
        0x70: 59,
        0x71: 60,
        0x72: 61,
        0x73: 62,
        0x74: 63,
        0x75: 64,
        0x76: 65,
        0x77: 66,
        0x78: 67,
        0x79: 68,
        0x7A: 87,
        0x7B: 88,
        0x7C: 183,
        0x7D: 184,
        0x7E: 185,
        0x7F: 186,
        0x80: 187,
        0x81: 188,
        0x82: 189,
        0x83: 190,
        0x84: 191,
        0x85: 192,
        0x86: 193,
        0x87: 194,
    }
)


def strip_jsonc_comments(text: str) -> str:
    result: list[str] = []
    index = 0
    in_string = False
    escape = False

    while index < len(text):
        char = text[index]

        if in_string:
            result.append(char)
            if escape:
                escape = False
            elif char == "\\":
                escape = True
            elif char == '"':
                in_string = False
            index += 1
            continue

        if char == '"':
            in_string = True
            result.append(char)
            index += 1
            continue

        if text.startswith("//", index):
            newline_index = text.find("\n", index)
            if newline_index == -1:
                break
            result.append("\n")
            index = newline_index + 1
            continue

        if text.startswith("/*", index):
            comment_end = text.find("*/", index + 2)
            if comment_end == -1:
                raise ValueError("Unterminated block comment")
            result.extend("\n" for char in text[index:comment_end] if char == "\n")
            index = comment_end + 2
            continue

        result.append(char)
        index += 1

    return "".join(result)


def remove_trailing_commas(text: str) -> str:
    result: list[str] = []
    index = 0
    in_string = False
    escape = False

    while index < len(text):
        char = text[index]

        if in_string:
            result.append(char)
            if escape:
                escape = False
            elif char == "\\":
                escape = True
            elif char == '"':
                in_string = False
            index += 1
            continue

        if char == '"':
            in_string = True
            result.append(char)
            index += 1
            continue

        if char == ",":
            probe = index + 1
            while probe < len(text) and text[probe] in " \t\r\n":
                probe += 1
            if probe < len(text) and text[probe] in "}]":
                index += 1
                continue

        result.append(char)
        index += 1

    return "".join(result)


def load_jsonc(path: Path) -> Any:
    raw_text = path.read_text(encoding="utf-8")
    clean_text = remove_trailing_commas(strip_jsonc_comments(raw_text))
    return json.loads(clean_text)


def contains_key_field(value: Any) -> bool:
    if isinstance(value, dict):
        if "key" in value:
            return True
        return any(contains_key_field(child) for child in value.values())
    if isinstance(value, list):
        return any(contains_key_field(item) for item in value)
    return False


def map_win32_key(value: Any, file_path: Path, json_path: str) -> Any:
    if isinstance(value, bool):
        return value
    if isinstance(value, int):
        try:
            return VK_TO_EVDEV[value]
        except KeyError as exc:
            raise KeyError(
                f"Unsupported Win32 key code {value} at {file_path}::{json_path}"
            ) from exc
    if isinstance(value, list):
        return [map_win32_key(item, file_path, json_path) for item in value]
    raise TypeError(
        f"Unsupported key value type {type(value).__name__} at {file_path}::{json_path}"
    )


def transform_value(value: Any, file_path: Path, json_path: str = "") -> Any:
    if isinstance(value, dict):
        transformed: dict[str, Any] = {}
        for key, child in value.items():
            child_path = f"{json_path}/{key}" if json_path else f"/{key}"
            if key == "key":
                transformed[key] = map_win32_key(child, file_path, child_path)
            else:
                transformed[key] = transform_value(child, file_path, child_path)
        return transformed
    if isinstance(value, list):
        return [
            transform_value(item, file_path, f"{json_path}[{index}]")
            for index, item in enumerate(value)
        ]
    return value


def filter_top_level_entries(data: dict[str, Any]) -> dict[str, Any]:
    return {
        key: value
        for key, value in data.items()
        if contains_key_field(value)
    }


def convert_file(source_path: Path, source_root: Path, target_root: Path) -> bool:
    raw_text = source_path.read_text(encoding="utf-8")
    if "key" not in raw_text:
        return False

    data = load_jsonc(source_path)
    if not isinstance(data, dict):
        return False

    filtered = filter_top_level_entries(data)
    if not filtered:
        return False

    transformed = transform_value(filtered, source_path)
    target_path = target_root / source_path.relative_to(source_root)
    target_path.parent.mkdir(parents=True, exist_ok=True)

    output = json.dumps(transformed, ensure_ascii=False, indent=4) + "\n"

    if target_path.exists():
        existing = target_path.read_text(encoding="utf-8")
        if existing == output:
            return False

    target_path.write_text(output, encoding="utf-8")
    return True


def iter_source_files(source_root: Path) -> list[Path]:
    return sorted(path for path in source_root.rglob("*.json") if path.is_file())


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Generate wlroots pipeline JSON files from Win32 pipeline resources."
    )
    parser.add_argument(
        "--source-root",
        type=Path,
        default=DEFAULT_SOURCE_ROOT,
        help="Source pipeline root (default: assets/resource/pipeline).",
    )
    parser.add_argument(
        "--target-root",
        type=Path,
        default=DEFAULT_TARGET_ROOT,
        help="Target pipeline root (default: assets/resource_wlroots/pipeline).",
    )
    args = parser.parse_args()

    repo_root = Path(__file__).resolve().parent.parent
    source_root = (repo_root / args.source_root).resolve()
    target_root = (repo_root / args.target_root).resolve()

    if not source_root.exists():
        raise SystemExit(f"Source root does not exist: {source_root}")

    converted_count = 0
    scanned_count = 0

    for source_path in iter_source_files(source_root):
        scanned_count += 1
        try:
            if convert_file(source_path, source_root, target_root):
                converted_count += 1
                print(f"[OK] {source_path.relative_to(repo_root)}")
        except Exception as exc:
            raise SystemExit(f"Failed to convert {source_path}: {exc}") from exc

    print(f"Scanned {scanned_count} JSON files, wrote {converted_count} files.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
