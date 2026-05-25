from __future__ import annotations

import argparse
import json
import subprocess
from pathlib import Path, PurePosixPath


def normalize_repo_path(path: str | Path) -> str:
    normalized = str(path).replace("\\", "/")
    while normalized.startswith("./"):
        normalized = normalized[2:]
    return PurePosixPath(normalized).as_posix()


def run_git(args: list[str]) -> str:
    repo_root = Path.cwd().resolve()
    command = [
        "git",
        "-c",
        f"safe.directory={repo_root.as_posix()}",
        *args,
    ]
    completed = subprocess.run(
        command,
        check=True,
        capture_output=True,
        text=True,
    )
    return completed.stdout


def git_changed_files(base_ref: str) -> list[str]:
    tracked = run_git(["diff", "--name-only", base_ref, "--"])
    untracked = run_git(["ls-files", "--others", "--exclude-standard", "--"])
    paths = {
        line.strip()
        for output in (tracked, untracked)
        for line in output.splitlines()
        if line.strip()
    }
    return sorted(paths)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Verify that only allowed translated docs files were changed.",
    )
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--base-ref", required=True)
    parser.add_argument(
        "--state-path",
        default=Path("docs/en_us/.docs-sync-state.json"),
        type=Path,
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    manifest = json.loads(args.manifest.read_text(encoding="utf-8"))
    tasks = manifest.get("tasks")
    if not isinstance(tasks, list):
        print("Invalid manifest: expected a 'tasks' list.")
        return 1

    allowed_paths: set[str] = set()
    allowed_paths.add(normalize_repo_path(args.state_path))
    for task in tasks:
        mode = task["mode"]
        if mode in {"translate_file", "delete_target"}:
            allowed_paths.add(normalize_repo_path(task["target_path"]))
        elif mode == "rename_target":
            allowed_paths.add(normalize_repo_path(task["target_path_before"]))
            allowed_paths.add(normalize_repo_path(task["target_path_after"]))

    changed = git_changed_files(args.base_ref)
    disallowed = [path for path in changed if path not in allowed_paths]

    if disallowed:
        print("Disallowed changes detected:")
        for path in disallowed:
            print(path)
        return 1

    source_side_changes = [path for path in changed if path.startswith("docs/zh_cn/")]
    if source_side_changes:
        print("Source-side docs were modified, which is forbidden:")
        for path in source_side_changes:
            print(path)
        return 1

    non_docs_changes = [path for path in changed if not path.startswith("docs/en_us/")]
    if non_docs_changes:
        print("Non-target files were modified, which is forbidden:")
        for path in non_docs_changes:
            print(path)
        return 1

    print("Allowed-changes check passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
