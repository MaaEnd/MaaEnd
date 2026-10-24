"""工作区与兄弟仓库：同步它们、检查输入齐不齐、看入库目录有哪些改动。

同步只做 ``git fetch`` + ``--ff-only`` 快进合并：工作区有未提交改动、处于分离头指针、没有配置上游时一律
只提示并跳过该仓库 —— **绝不** stash / reset / 覆盖本地改动。这一层由 ``uv run zmdmap generate --sync``
触发。

版本标签怎么读、怎么比，在 :mod:`tools.zmdmap.utils.version`。
"""

import subprocess
from dataclasses import dataclass
from pathlib import Path

from .config import (
    NAVMESH_DIR,
    NAVMESH_FILES,
    REPO_ROOT,
    WORKSPACE_REPO_NAMES,
    ZMDMAP_DATA_DIR,
)

# 状态 -> 单字符标记，打印同步结果时用。标记只用 GBK 里有的字符：输出虽然走 click（写不了的字符它会替换成
# ``?`` 而不是抛错），但 Windows 中文控制台上显示成问号一样没法看。
SYNC_MARKS = {"updated": "↓", "current": "=", "pending": "→", "skipped": "·", "missing": "·"}
SYNC_MARK_UNKNOWN = "x"


@dataclass
class RepoOutcome:
    """一个仓库的同步结果。"""

    name: str
    state: str
    detail: str
    ok: bool = True


def last_lines(text: str, count: int = 1) -> str:
    """取一段命令输出最后几行、用 `` / `` 连起来：拼进一行日志里，不把多行输出铺开。"""
    lines = [line.strip() for line in text.splitlines() if line.strip()]
    return " / ".join(lines[-count:])


def git(repo: Path, *args: str) -> subprocess.CompletedProcess[str]:
    """在指定仓库里跑一条 git 命令，输出合并返回，不抛异常。"""
    try:
        return subprocess.run(
            ["git", "-C", str(repo), *args],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            check=False,
        )
    except OSError as error:  # git 不在 PATH
        return subprocess.CompletedProcess(
            args=["git", *args], returncode=127, stdout=f"无法执行 git：{error}", stderr=""
        )


def sync_repo(name: str, repo: Path, *, dry_run: bool) -> RepoOutcome:
    """fetch + 仅快进合并到上游；任何会动到本地改动的路径都只跳过。"""
    if not (repo / ".git").exists():
        return RepoOutcome(name, "missing", f"{repo} 不是 git 仓库，跳过同步")

    if git(repo, "status", "--porcelain").stdout.strip():
        return RepoOutcome(name, "skipped", "工作区有未提交改动，跳过同步（不动你的文件）")

    branch = git(repo, "rev-parse", "--abbrev-ref", "HEAD").stdout.strip()
    if not branch or branch == "HEAD":
        return RepoOutcome(name, "skipped", "处于分离头指针状态，跳过同步")

    upstream = git(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
    if upstream.returncode != 0 or not upstream.stdout.strip():
        return RepoOutcome(name, "skipped", f"{branch} 没有配置上游分支，跳过同步")
    upstream_ref = upstream.stdout.strip()

    fetched = git(repo, "fetch", "--prune", "origin")
    if fetched.returncode != 0:
        return RepoOutcome(name, "failed", f"git fetch 失败：{last_lines(fetched.stdout)}", ok=False)

    head = git(repo, "rev-parse", "--short", "HEAD").stdout.strip()
    target = git(repo, "rev-parse", "--short", upstream_ref).stdout.strip()
    behind = git(repo, "rev-list", "--count", f"HEAD..{upstream_ref}").stdout.strip() or "?"

    if head == target:
        return RepoOutcome(name, "current", f"已是最新（{branch} @ {head}）")

    if dry_run:
        subjects = git(repo, "log", "--oneline", f"HEAD..{upstream_ref}").stdout.strip()
        detail = f"可更新 {head} → {target}（{behind} 个提交）"
        if subjects:
            detail += f"：{last_lines(subjects, 3)}"
        return RepoOutcome(name, "pending", detail)

    merged = git(repo, "merge", "--ff-only", upstream_ref)
    if merged.returncode != 0:
        return RepoOutcome(
            name, "failed", f"无法快进到 {upstream_ref}（本地与远端分叉），需要人工处理", ok=False
        )
    after = git(repo, "rev-parse", "--short", "HEAD").stdout.strip()
    return RepoOutcome(name, "updated", f"{head} → {after}（{behind} 个提交，{branch}）")


def sync_workspace(workspace: Path, *, dry_run: bool) -> tuple[list[RepoOutcome], bool]:
    """同步 workspace 下的兄弟仓库。

    返回 ``(各仓库结果, 是否有失败)``。
    """
    outcomes: list[RepoOutcome] = []
    failed = False
    for name in WORKSPACE_REPO_NAMES:
        outcome = sync_repo(name, workspace / name, dry_run=dry_run)
        failed = failed or not outcome.ok
        outcomes.append(outcome)
    return outcomes, failed


def preflight(table_cfg_dir: Path, json_data_dir: Path, *, check_navmesh: bool, check_json_data: bool = True) -> list[str]:
    """生成/投影前的提示项；返回问题列表（只提示，不阻止）。"""
    problems: list[str] = []
    if not table_cfg_dir.is_dir():
        problems.append(f"缺少 TableCfg 目录：{table_cfg_dir}")
    if check_json_data and not json_data_dir.is_dir():
        problems.append(f"缺少 JsonData 目录：{json_data_dir}")
    elif check_json_data and not (json_data_dir / "GameplayConfig" / "LevelMapMark.json").is_file():
        problems.append(f"缺少 {json_data_dir / 'GameplayConfig' / 'LevelMapMark.json'}")
    if check_navmesh and not any((NAVMESH_DIR / name).exists() for name in NAVMESH_FILES):
        problems.append("本地没有 navmesh（投影会改用远端 nav 数据，需要联网）")
    return problems


def changed_data_files(file_names: list[str]) -> list[str]:
    """入库 data/ 目录里与本次生成相关的改动（git status 的原始行）。"""
    expected = set(file_names)
    changed = git(
        REPO_ROOT, "status", "--porcelain", "--", str(ZMDMAP_DATA_DIR)
    ).stdout.strip()
    return [
        line
        for line in changed.splitlines()
        if Path(line[3:].strip()).name in expected
    ]

