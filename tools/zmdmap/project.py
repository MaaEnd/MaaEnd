"""调用 ``tools/pipeline-generate/fetch-data.mjs`` 把原始形态投影进入库数据。

repo 里负责换算的是 Node 那份（BaseNav zone 表、navmesh 解析都在 JS 侧），这里只当调用方：
``uv run zmdmap --project`` 生成完 ``data/raw/`` 之后再跑一遍 ``--project-only``。
"""

from __future__ import annotations

import subprocess
import time
from dataclasses import dataclass
from pathlib import Path

from .paths import PIPELINE_DATA_DIR, PROJECTOR, REPO_ROOT


@dataclass
class Outcome:
    """一条外部命令的结果。"""

    name: str
    code: int
    seconds: float
    stdout: str
    stderr: str

    @property
    def ok(self) -> bool:
        return self.code == 0

    @property
    def detail(self) -> str:
        lines = [line.strip() for line in (self.stdout + self.stderr).splitlines() if line.strip()]
        return " / ".join(lines[-1:]) or f"退出码 {self.code}"


def run(command: list[str]) -> Outcome:
    """在仓库根跑一条命令，捕获输出。"""
    started = time.monotonic()
    try:
        completed = subprocess.run(
            command,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            check=False,
            cwd=REPO_ROOT,
        )
        return Outcome(
            name=Path(command[1]).name if len(command) > 1 else command[0],
            code=completed.returncode,
            seconds=time.monotonic() - started,
            stdout=completed.stdout,
            stderr=completed.stderr,
        )
    except OSError as error:
        return Outcome(
            name=Path(command[0]).name,
            code=127,
            seconds=time.monotonic() - started,
            stdout=f"无法执行 {command[0]}：{error}",
            stderr="",
        )


def build_project_command(datasets: list[str], nav: str | None) -> list[str]:
    """``fetch-data.mjs --project-only`` 的命令行。

    JS 侧的 ``--only`` 是空格分隔的多值选项（不是 click 的重复选项），这里照它的写法来。
    """
    command = ["node", str(PROJECTOR), "--project-only", "--only", *datasets]
    if nav:
        command += ["--nav", nav]
    return command


def output_dir_label() -> str:
    return f"{PIPELINE_DATA_DIR / 'raw'} -> {PIPELINE_DATA_DIR}"
