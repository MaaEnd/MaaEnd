from __future__ import annotations

import argparse
import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path, PurePosixPath
from typing import Any


SOURCE_PREFIX = "docs/zh_cn/"
TARGET_PREFIX = "docs/en_us/"
DEFAULT_STATE_PATH = Path("docs/en_us/.docs-sync-state.json")
SUPPORTED_SUFFIXES = {
    ".md",
    ".markdown",
    ".mdx",
    ".txt",
}


def normalize_repo_path(path: str) -> str:
    return PurePosixPath(path.replace("\\", "/")).as_posix()


def source_hash(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def classify_source_doc(path: Path) -> dict[str, str] | None:
    normalized = normalize_repo_path(path.as_posix())
    if not normalized.startswith(SOURCE_PREFIX):
        return None

    relative = normalized[len(SOURCE_PREFIX):]
    if not relative:
        return None

    suffix = PurePosixPath(relative).suffix.lower()
    if suffix not in SUPPORTED_SUFFIXES:
        return None

    return {
        "source_path": normalized,
        "relative_path": relative,
        "target_path": f"{TARGET_PREFIX}{relative}",
        "source_sha256": source_hash(path),
    }


def read_state(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {}
    state = json.loads(path.read_text(encoding="utf-8-sig"))
    if not isinstance(state, dict):
        return {}
    return state


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Build docs translation tasks by comparing zh_cn source hashes with sync state.",
    )
    parser.add_argument("--state-path", default=DEFAULT_STATE_PATH, type=Path)
    parser.add_argument("--output-manifest", required=True, type=Path)
    parser.add_argument("--output-summary", required=True, type=Path)
    return parser.parse_args()


def current_source_docs() -> dict[str, dict[str, str]]:
    docs: dict[str, dict[str, str]] = {}
    for path in sorted(Path("docs/zh_cn").rglob("*")):
        if not path.is_file():
            continue
        info = classify_source_doc(path)
        if info:
            docs[info["source_path"]] = info
    return docs


def build_tasks(
    docs: dict[str, dict[str, str]],
    synced_sources: dict[str, Any],
) -> list[dict[str, Any]]:
    tasks: list[dict[str, Any]] = []
    for source_path, info in docs.items():
        synced = synced_sources.get(source_path, {})
        if not isinstance(synced, dict):
            synced = {}
        if synced.get("source_sha256") == info["source_sha256"]:
            continue
        tasks.append(
            {
                "mode": "translate_file",
                "status": "changed" if synced else "added",
                "relative_path": info["relative_path"],
                "source_path": info["source_path"],
                "target_path": info["target_path"],
                "source_sha256": info["source_sha256"],
            }
        )

    for source_path, synced in sorted(synced_sources.items()):
        if source_path in docs or not isinstance(synced, dict):
            continue
        target_path = synced.get("target_path")
        if not isinstance(target_path, str) or not target_path.startswith(TARGET_PREFIX):
            continue
        tasks.append(
            {
                "mode": "delete_target",
                "status": "deleted",
                "source_path": source_path,
                "target_path": target_path,
            }
        )

    return tasks


def build_summary(task: dict[str, Any]) -> str:
    mode = task["mode"]
    if mode == "translate_file":
        return f"- translate {task['source_path']} -> {task['target_path']} ({task['status']})"
    if mode == "delete_target":
        return f"- delete {task['target_path']} because {task['source_path']} was removed"
    return f"- unknown task mode: {mode}"


def main() -> int:
    args = parse_args()
    state = read_state(args.state_path)
    synced_sources = state.get("sources", {})
    if not isinstance(synced_sources, dict):
        synced_sources = {}

    docs = current_source_docs()
    tasks = build_tasks(docs, synced_sources)
    manifest = {
        "source_lang": "zh_cn",
        "target_lang": "en_us",
        "generated_at_utc": datetime.now(timezone.utc).isoformat(),
        "source_count": len(docs),
        "task_count": len(tasks),
        "tasks": tasks,
        "sources": docs,
    }

    args.output_manifest.parent.mkdir(parents=True, exist_ok=True)
    args.output_summary.parent.mkdir(parents=True, exist_ok=True)
    args.output_manifest.write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )

    summary_lines = [
        f"Docs source files: {len(docs)}",
        f"Docs translation tasks: {len(tasks)}",
        f"State: {args.state_path.as_posix()}",
    ]
    if tasks:
        summary_lines.append("")
        summary_lines.extend(build_summary(task) for task in tasks)

    args.output_summary.write_text("\n".join(summary_lines) + "\n", encoding="utf-8")

    print(f"Collected {len(tasks)} docs translation task(s) from {len(docs)} source file(s).")
    for task in tasks:
        print(build_summary(task))

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
