#!/usr/bin/env python3
"""同步兄弟数据仓库，并按本地数据重算 MaaEnd 的精简游戏数据。

入库的 ``tools/pipeline-generate/data/*.json`` 平时由 ``pnpm fetch:zmdmap`` 从 zmdmap
发布数据下载 + 投影得到；本脚本改用兄弟仓库的数据在本地重算一遍，做本地/CI 等价校验：

1. 对 ``BeyondMemoryPack`` / ``BeyondTableCfg`` / ``endfield-workflow`` 依次 ``git fetch``
   并只做快进合并到各自上游。工作区有未提交改动、处于分离头指针、没有配置上游时只提示并
   跳过该仓库 —— 绝不 stash / reset / 覆盖本地改动。另外会报告 MaaEnd 自己的
   ``assets/resource/model`` 子模块（BaseNav 数据包，投影用的 zone 表来源）是否落后于
   上游；加 ``--update-model`` 才会真的更新。
2. 运行 ``tools/pipeline-generate/data/scripts/generate.py``：与 endfield-workflow 的
   ``maaend_game_data`` 处理器同一批 ``build_*``，把原始形态（只有世界坐标）写到不入库的
   ``tools/pipeline-generate/data/raw/``。
3. 运行 ``node tools/pipeline-generate/fetch-data.mjs --project-only``：按 BaseNav 的 zone
   表把 ``data/raw/`` 投影/材料化到入库的 ``data/*.json``。navmesh 落后于数据版本时这一步
   会直接报错停下（``--nav`` 可以指定别的 nav 数据）。

注意：``data/version.txt`` 记录的是「已发布的 zmdmap 数据版本」，本地重算不会改它；
本脚本产出的是「按当前三个仓库的数据算出来的结果」，与发布版是否一致请自行核对。

用法::

    # MaaEnd 的 venv（Windows）
    .venv\\Scripts\\python.exe tools/pipeline-generate/sync_and_generate.py

    # 或从 MaaEnd 仓库根目录
    uv run python tools/pipeline-generate/sync_and_generate.py

    # 只看看有哪些更新：不合并、不生成
    uv run python tools/pipeline-generate/sync_and_generate.py --dry-run

    # 连 assets/resource/model 子模块（navmesh / zone 投影参数）一起更新
    uv run python tools/pipeline-generate/sync_and_generate.py --update-model

    # 离线重算（跳过同步）；只重算其中几个数据集
    uv run python tools/pipeline-generate/sync_and_generate.py --no-sync --only delivery_jobs sell_product

退出码：同步、生成或投影任一步失败 -> 1，否则 0。
"""

from __future__ import annotations

import argparse
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path

SCRIPT_PATH = Path(__file__).resolve()
PIPELINE_DIR = SCRIPT_PATH.parent
REPO_ROOT = PIPELINE_DIR.parents[1]
WORKSPACE_DEFAULT = REPO_ROOT.parent
SCRIPTS_DIR = PIPELINE_DIR / "data" / "scripts"
DATA_DIR = SCRIPTS_DIR.parent
RAW_DIR = DATA_DIR / "raw"
GENERATOR = SCRIPTS_DIR / "generate.py"
PROJECTOR = PIPELINE_DIR / "fetch-data.mjs"
DEFAULT_WORKSPACE_REPOS = ("BeyondMemoryPack", "BeyondTableCfg", "endfield-workflow")
TABLE_CFG_SUBPATH = ("BeyondTableCfg", "TableCfg")
JSON_DATA_SUBPATH = ("BeyondMemoryPack", "JsonData")
MODEL_SUBMODULE_PATH = "assets/resource/model"

SYNC_MARKS = {"updated": "↓", "current": "=", "pending": "→", "skipped": "·", "missing": "·"}


@dataclass
class Outcome:
    """一条命令的结果。"""

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
        return last_lines(self.stdout + self.stderr) or f"退出码 {self.code}"


@dataclass
class SyncOutcome:
    name: str
    state: str
    detail: str
    ok: bool = True


def last_lines(text: str, count: int = 1) -> str:
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


def run(command: list[str]) -> Outcome:
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
        result = Outcome(
            name=Path(command[1]).name if len(command) > 1 else command[0],
            code=completed.returncode,
            seconds=time.monotonic() - started,
            stdout=completed.stdout,
            stderr=completed.stderr,
        )
    except OSError as error:
        result = Outcome(
            name=Path(command[0]).name,
            code=127,
            seconds=time.monotonic() - started,
            stdout=f"无法执行 {command[0]}：{error}",
            stderr="",
        )
    return result


def sync_repo(name: str, repo: Path, *, dry_run: bool) -> SyncOutcome:
    """fetch + 仅快进合并到上游；任何会动到本地改动的路径都只跳过。"""
    if not (repo / ".git").exists():
        return SyncOutcome(name, "missing", f"{repo} 不是 git 仓库，跳过同步")

    if git(repo, "status", "--porcelain").stdout.strip():
        return SyncOutcome(name, "skipped", "工作区有未提交改动，跳过同步（不动你的文件）")

    branch = git(repo, "rev-parse", "--abbrev-ref", "HEAD").stdout.strip()
    if not branch or branch == "HEAD":
        return SyncOutcome(name, "skipped", "处于分离头指针状态，跳过同步")

    upstream = git(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
    if upstream.returncode != 0 or not upstream.stdout.strip():
        return SyncOutcome(name, "skipped", f"{branch} 没有配置上游分支，跳过同步")
    upstream_ref = upstream.stdout.strip()

    fetched = git(repo, "fetch", "--prune", "origin")
    if fetched.returncode != 0:
        return SyncOutcome(name, "failed", f"git fetch 失败：{last_lines(fetched.stdout)}", ok=False)

    head = git(repo, "rev-parse", "--short", "HEAD").stdout.strip()
    target = git(repo, "rev-parse", "--short", upstream_ref).stdout.strip()
    behind = git(repo, "rev-list", "--count", f"HEAD..{upstream_ref}").stdout.strip() or "?"

    if head == target:
        return SyncOutcome(name, "current", f"已是最新（{branch} @ {head}）")

    if dry_run:
        subjects = git(repo, "log", "--oneline", f"HEAD..{upstream_ref}").stdout.strip()
        detail = f"可更新 {head} → {target}（{behind} 个提交）"
        if subjects:
            detail += f"：{last_lines(subjects, 3)}"
        return SyncOutcome(name, "pending", detail)

    merged = git(repo, "merge", "--ff-only", upstream_ref)
    if merged.returncode != 0:
        return SyncOutcome(
            name, "failed", f"无法快进到 {upstream_ref}（本地与远端分叉），需要人工处理", ok=False
        )
    after = git(repo, "rev-parse", "--short", "HEAD").stdout.strip()
    return SyncOutcome(name, "updated", f"{head} → {after}（{behind} 个提交，{branch}）")


def submodule_upstream(repo: Path) -> str | None:
    """子模块处于分离头指针，按 origin/HEAD → origin/main → origin/master 找可比较的上游。"""
    default = git(repo, "rev-parse", "--abbrev-ref", "origin/HEAD").stdout.strip()
    candidates = [default] if default and default != "origin/HEAD" else []
    candidates += ["origin/main", "origin/master"]
    for candidate in candidates:
        if git(repo, "rev-parse", "--verify", "--quiet", candidate).returncode == 0:
            return candidate
    return None


def sync_model_submodule(*, update: bool) -> SyncOutcome:
    """报告（或更新）assets/resource/model：BaseNav 数据包，投影用的 zone 表来源。"""
    name = "assets/resource/model"
    repo = REPO_ROOT / MODEL_SUBMODULE_PATH
    if not (repo / ".git").exists():
        return SyncOutcome(name, "missing", f"{repo} 尚未初始化；投影会改用远端 navmesh")

    fetched = git(repo, "fetch", "--prune", "origin")
    if fetched.returncode != 0:
        return SyncOutcome(name, "failed", f"git fetch 失败：{last_lines(fetched.stdout)}", ok=False)

    upstream = submodule_upstream(repo)
    head = git(repo, "rev-parse", "--short", "HEAD").stdout.strip()
    if upstream is None:
        return SyncOutcome(name, "current", f"停在 {head}（找不到可比较的上游）")

    target = git(repo, "rev-parse", "--short", upstream).stdout.strip()
    behind = git(repo, "rev-list", "--count", f"HEAD..{upstream}").stdout.strip() or "0"
    if head == target:
        return SyncOutcome(name, "current", f"已是最新（{head}）")
    if not update:
        return SyncOutcome(name, "pending", f"{head} → {target}（落后 {behind} 个提交；--update-model 才会更新）")

    merged = git(repo, "merge", "--ff-only", upstream)
    if merged.returncode != 0:
        return SyncOutcome(name, "failed", f"无法快进到 {upstream}，需要人工处理", ok=False)
    return SyncOutcome(name, "updated", f"{head} → {target}（{behind} 个提交）")


def dataset_names() -> list[str]:
    """六个数据集名，问 generate.py 自己（--list），免得两边各写一份。"""
    listed = subprocess.run(
        [sys.executable, str(GENERATOR), "--list"],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    names = [line.split("\t")[0].strip() for line in listed.stdout.splitlines() if line.strip()]
    if listed.returncode != 0 or not names:
        raise ValueError(f"没能从 {GENERATOR} 读出数据集名：{last_lines(listed.stdout + listed.stderr)}")
    return names


def select_datasets(only: list[str] | None) -> list[str]:
    known = dataset_names()
    if not only:
        return known
    unknown = [name for name in only if name not in known]
    if unknown:
        raise ValueError(f"未知数据集：{', '.join(unknown)}（可选：{', '.join(known)}）")
    return [name for name in known if name in set(only)]


def build_generate_command(workspace: Path, datasets: list[str], *, force: bool) -> list[str]:
    command = [
        sys.executable,
        str(GENERATOR),
        "--table-cfg-dir",
        str(workspace.joinpath(*TABLE_CFG_SUBPATH)),
        "--json-data-dir",
        str(workspace.joinpath(*JSON_DATA_SUBPATH)),
        "--out-dir",
        str(RAW_DIR),
        "--only",
        *datasets,
    ]
    if force:
        command.append("--force")
    return command


def build_project_command(datasets: list[str], nav: str | None) -> list[str]:
    command = ["node", str(PROJECTOR), "--project-only", "--only", *datasets]
    if nav:
        command += ["--nav", nav]
    return command


def preflight(workspace: Path) -> list[str]:
    problems: list[str] = []
    table_cfg = workspace.joinpath(*TABLE_CFG_SUBPATH)
    json_data = workspace.joinpath(*JSON_DATA_SUBPATH)
    if not table_cfg.is_dir():
        problems.append(f"缺少 TableCfg 目录：{table_cfg}")
    if not json_data.is_dir():
        problems.append(f"缺少 JsonData 目录：{json_data}")
    if not (json_data / "GameplayConfig" / "LevelMapMark.json").is_file():
        problems.append(f"缺少 {json_data / 'GameplayConfig' / 'LevelMapMark.json'}")
    if not any((REPO_ROOT / MODEL_SUBMODULE_PATH / "map" / "navmesh" / name).exists() for name in ("base.nav.gz", "base.nav")):
        problems.append("本地没有 navmesh（投影会改用远端 nav 数据，需要联网）")
    return problems


def print_changed_data_files() -> None:
    """只看入库的那几份 data 文件（data/raw/ 与脚本自身的改动不在这里报）。"""
    expected = {f"{name}.json" for name in dataset_names()} | {"version.txt"}
    changed = git(REPO_ROOT, "status", "--porcelain", "--", str(DATA_DIR)).stdout.strip()
    lines = [line for line in changed.splitlines() if Path(line[3:].strip()).name in expected]
    if not lines:
        print("data/*.json：没有改动")
        return
    print("data/*.json 改动：")
    for line in lines:
        print(f"  {line}")


def parse_arguments(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="同步兄弟仓库并在本地重算精简游戏数据")
    parser.add_argument(
        "--workspace",
        type=Path,
        default=WORKSPACE_DEFAULT,
        help=f"放兄弟仓库的目录（默认：{WORKSPACE_DEFAULT}）",
    )
    parser.add_argument("--no-sync", action="store_true", help="跳过同步，直接用本地数据重算")
    parser.add_argument("--dry-run", action="store_true", help="只报告会做什么，不合并也不生成")
    parser.add_argument(
        "--only",
        nargs="+",
        default=None,
        metavar="数据集",
        help="只重算列出的数据集（缺省全部）",
    )
    parser.add_argument("--update-model", action="store_true", help="顺带更新 assets/resource/model 子模块")
    parser.add_argument("--force", action="store_true", help="结果未变化也重写 data/raw/ 与 data/*.json")
    parser.add_argument("--nav", default=None, help="投影用的 nav 数据（本地路径或 URL）")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    options = parse_arguments(argv)
    workspace = options.workspace.expanduser().resolve()

    try:
        datasets = select_datasets(options.only)
    except (OSError, ValueError) as error:
        print(f"✘ {error}", file=sys.stderr)
        return 1

    print(f"MaaEnd 仓库  ：{REPO_ROOT}")
    print(f"兄弟仓库目录 ：{workspace}")
    print(f"数据集       ：{len(datasets)} 个（{'、'.join(datasets)}）")

    sync_failed = False
    if options.no_sync:
        print("\n── 同步（--no-sync，跳过）────────────────────────")
    else:
        print("\n── 同步 ──────────────────────────────────────────")
        for name in DEFAULT_WORKSPACE_REPOS:
            outcome = sync_repo(name, workspace / name, dry_run=options.dry_run)
            sync_failed = sync_failed or not outcome.ok
            print(f"  [{SYNC_MARKS.get(outcome.state, '✘')}] {name:<20} {outcome.detail}")
        model = sync_model_submodule(update=options.update_model and not options.dry_run)
        sync_failed = sync_failed or not model.ok
        print(f"  [{SYNC_MARKS.get(model.state, '✘')}] {'assets/resource/model':<20} {model.detail}")

    problems = preflight(workspace)
    if problems:
        print("\n── 预检提示 ─────────────────────────────────────")
        for problem in problems:
            print(f"  ⚠ {problem}")

    generate_command = build_generate_command(workspace, datasets, force=options.force)
    project_command = build_project_command(datasets, options.nav)

    if options.dry_run:
        print("\n── 将运行（--dry-run，未执行）────────────────────")
        print("  " + " ".join(generate_command))
        print("  " + " ".join(project_command))
        print("\n--dry-run：没有合并仓库，也没有写任何 data 文件")
        return 0

    print("\n── 生成（data/raw/）──────────────────────────────")
    print(f"      $ {' '.join(generate_command)}")
    generated = run(generate_command)
    for line in (generated.stdout + generated.stderr).splitlines():
        if line.strip():
            print(f"      {line.strip()}")
    if not generated.ok:
        print(f"  ✘ 生成失败：{generated.detail}")

    print("\n── 投影（data/raw/ -> data/）─────────────────────")
    print(f"      $ {' '.join(project_command)}")
    if generated.ok:
        projected = run(project_command)
        for line in (projected.stdout + projected.stderr).splitlines():
            if line.strip():
                print(f"      {line.strip()}")
    else:
        projected = Outcome("fetch-data.mjs", 1, 0.0, "上一步失败，跳过投影", "")
        print("      跳过（生成失败）")

    print("\n── 汇总 ──────────────────────────────────────────")
    print(f"同步：{'有失败' if sync_failed else '正常'}")
    print(f"生成：{'正常' if generated.ok else '失败'}（{generated.seconds:.1f}s）")
    print(f"投影：{'正常' if projected.ok else '失败'}（{RAW_DIR} -> {DATA_DIR}）")
    print_changed_data_files()
    print("提示：data/version.txt 是「已发布数据版本」，本地重算不会改它。")

    return 0 if (generated.ok and projected.ok and not sync_failed) else 1


if __name__ == "__main__":
    raise SystemExit(main())
