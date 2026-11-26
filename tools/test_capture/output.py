"""输出目录：每次运行一个时间戳目录，只保留最近若干份。"""

from __future__ import annotations

import re
import shutil
from datetime import datetime
from pathlib import Path

DEFAULT_KEEP = 10
RUN_DIR_RE = re.compile(r"^\d{8}-\d{6}(-\d+)?$")


def new_run_dir(root: Path) -> Path:
    """建一个以本次开始时间命名的目录；同一秒重复启动时加后缀。"""
    root.mkdir(parents=True, exist_ok=True)
    base = datetime.now().strftime("%Y%m%d-%H%M%S")
    candidate = root / base
    suffix = 1
    while candidate.exists():
        candidate = root / f"{base}-{suffix}"
        suffix += 1
    candidate.mkdir()
    return candidate


def prune_runs(root: Path, keep: int = DEFAULT_KEEP) -> list[Path]:
    """只留最近 keep 次运行的目录，返回删掉的目录。"""
    if not root.is_dir():
        return []
    runs = sorted(
        (path for path in root.iterdir() if path.is_dir() and RUN_DIR_RE.match(path.name)),
        reverse=True,
    )
    removed: list[Path] = []
    for stale in runs[keep:]:
        shutil.rmtree(stale, ignore_errors=True)
        removed.append(stale)
    return removed
