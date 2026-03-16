#!/usr/bin/env python3
from __future__ import annotations

import argparse
import re
from pathlib import Path


DEFAULT_PATTERNS = [
    "assets/resource/pipeline/Common/*.json",
]
FIELD_NAMES = (
    "rate_limit",
    "pre_delay",
    "post_delay",
)


def read_string(text: str, start: int) -> tuple[str, int]:
    if text[start] != '"':
        raise ValueError(f"Expected string at index {start}")

    index = start + 1
    while index < len(text):
        char = text[index]
        if char == "\\":
            index += 2
            continue
        if char == '"':
            return text[start + 1 : index], index + 1
        index += 1

    raise ValueError("Unterminated string literal")


def skip_ws_and_comments(text: str, start: int) -> int:
    index = start
    while index < len(text):
        if text[index] in " \t\r\n":
            index += 1
            continue
        if text.startswith("//", index):
            newline_index = text.find("\n", index)
            if newline_index == -1:
                return len(text)
            index = newline_index + 1
            continue
        if text.startswith("/*", index):
            comment_end = text.find("*/", index + 2)
            if comment_end == -1:
                return len(text)
            index = comment_end + 2
            continue
        break
    return index


def find_matching_brace(text: str, start: int) -> int:
    if text[start] != "{":
        raise ValueError(f"Expected '{{' at index {start}")

    depth = 0
    index = start
    while index < len(text):
        if text.startswith("//", index):
            newline_index = text.find("\n", index)
            if newline_index == -1:
                raise ValueError("Unterminated line comment")
            index = newline_index + 1
            continue
        if text.startswith("/*", index):
            comment_end = text.find("*/", index + 2)
            if comment_end == -1:
                raise ValueError("Unterminated block comment")
            index = comment_end + 2
            continue

        char = text[index]
        if char == '"':
            _, index = read_string(text, index)
            continue
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return index
        index += 1

    raise ValueError("Unterminated object")


def iter_top_level_nodes(text: str) -> list[tuple[str, int, int]]:
    index = skip_ws_and_comments(text, 0)
    if index >= len(text) or text[index] != "{":
        raise ValueError("Top-level JSON value must be an object")

    nodes: list[tuple[str, int, int]] = []
    index += 1
    while index < len(text):
        index = skip_ws_and_comments(text, index)
        if index >= len(text) or text[index] == "}":
            break

        key, index = read_string(text, index)
        index = skip_ws_and_comments(text, index)
        if index >= len(text) or text[index] != ":":
            raise ValueError(f"Expected ':' after key {key}")

        index = skip_ws_and_comments(text, index + 1)
        if index >= len(text) or text[index] != "{":
            raise ValueError(f"Top-level key {key} must map to an object")

        object_start = index
        object_end = find_matching_brace(text, object_start)
        nodes.append((key, object_start, object_end))
        index = skip_ws_and_comments(text, object_end + 1)
        if index < len(text) and text[index] == ",":
            index += 1

    return nodes


def collect_top_level_keys(node_text: str) -> set[str]:
    keys: set[str] = set()
    nesting = 0
    index = 0
    while index < len(node_text):
        if node_text.startswith("//", index):
            newline_index = node_text.find("\n", index)
            if newline_index == -1:
                break
            index = newline_index + 1
            continue
        if node_text.startswith("/*", index):
            comment_end = node_text.find("*/", index + 2)
            if comment_end == -1:
                break
            index = comment_end + 2
            continue

        char = node_text[index]
        if char == '"':
            value, next_index = read_string(node_text, index)
            if nesting == 1:
                probe = skip_ws_and_comments(node_text, next_index)
                if probe < len(node_text) and node_text[probe] == ":":
                    keys.add(value)
            index = next_index
            continue
        if char in "{[":
            nesting += 1
        elif char in "}]":
            nesting -= 1
        index += 1

    return keys


def detect_newline(text: str) -> str:
    return "\r\n" if "\r\n" in text else "\n"


def detect_inner_indent(text: str, object_start: int) -> str:
    line_start = text.rfind("\n", 0, object_start) + 1
    line_text = text[line_start:object_start]
    base_indent = re.match(r"[ \t]*", line_text).group(0)
    indent_unit = "\t" if "\t" in base_indent else "    "
    return base_indent + indent_unit


def build_insertion(
    node_text: str, inner_indent: str, newline: str, missing_fields: list[str]
) -> str:
    has_existing_members = bool(node_text[1:-1].strip())
    if has_existing_members:
        lines = [f'{inner_indent}"{name}": 0,' for name in missing_fields]
        return newline + newline.join(lines)

    lines = []
    for index, name in enumerate(missing_fields):
        suffix = "," if index < len(missing_fields) - 1 else ""
        lines.append(f'{inner_indent}"{name}": 0{suffix}')
    closing_indent = (
        inner_indent[:-4] if inner_indent.endswith("    ") else inner_indent[:-1]
    )
    return newline + newline.join(lines) + newline + closing_indent


def update_file(path: Path) -> tuple[bool, int]:
    original = path.read_text(encoding="utf-8")
    newline = detect_newline(original)
    nodes = iter_top_level_nodes(original)
    insertions: list[tuple[int, str]] = []
    updated_nodes = 0

    for _, object_start, object_end in nodes:
        node_text = original[object_start : object_end + 1]
        existing_keys = collect_top_level_keys(node_text)
        missing_fields = [name for name in FIELD_NAMES if name not in existing_keys]
        if not missing_fields:
            continue

        inner_indent = detect_inner_indent(original, object_start)
        insertions.append(
            (
                object_start + 1,
                build_insertion(node_text, inner_indent, newline, missing_fields),
            )
        )
        updated_nodes += 1

    if not insertions:
        return False, 0

    updated = original
    for position, insertion in reversed(insertions):
        updated = updated[:position] + insertion + updated[position:]

    path.write_text(updated, encoding="utf-8")
    return True, updated_nodes


def count_nodes_missing_fields(text: str) -> int:
    return sum(
        1
        for _, object_start, object_end in iter_top_level_nodes(text)
        if any(
            name not in collect_top_level_keys(text[object_start : object_end + 1])
            for name in FIELD_NAMES
        )
    )


def resolve_targets(patterns: list[str]) -> list[Path]:
    repo_root = Path(__file__).resolve().parent.parent
    files: set[Path] = set()
    for pattern in patterns:
        files.update(repo_root.glob(pattern))
    return sorted(path for path in files if path.is_file())


def main() -> int:
    repo_root = Path(__file__).resolve().parent.parent
    parser = argparse.ArgumentParser(
        description="Add zero-valued rate_limit/pre_delay/post_delay fields to Common pipeline nodes."
    )
    parser.add_argument(
        "paths",
        nargs="*",
        help="Optional glob patterns relative to the repository root.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Only report the files that would be updated.",
    )
    args = parser.parse_args()

    targets = resolve_targets(args.paths or DEFAULT_PATTERNS)
    if not targets:
        print("No matching files found.")
        return 1

    changed_files = 0
    changed_nodes = 0
    for path in targets:
        changed, nodes = update_file(path) if not args.dry_run else (False, 0)
        if args.dry_run:
            original = path.read_text(encoding="utf-8")
            nodes = count_nodes_missing_fields(original)
            changed = nodes > 0

        status = "would update" if args.dry_run else "updated"
        relative_path = path.relative_to(repo_root)
        if changed:
            print(f"{status}: {relative_path} ({nodes} nodes)")
            changed_files += 1
            changed_nodes += nodes
        else:
            print(f"unchanged: {relative_path}")

    summary_prefix = "Would update" if args.dry_run else "Updated"
    print(f"{summary_prefix} {changed_files} files, {changed_nodes} nodes.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
