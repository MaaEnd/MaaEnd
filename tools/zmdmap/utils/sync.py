"""同步放数据的兄弟仓库，读出游戏数据版本，以及预检输入是否齐备。

只做 ``git fetch`` + ``--ff-only`` 快进合并：工作区有未提交改动、处于分离头指针、没有配置上游
时一律只提示并跳过该仓库 —— **绝不** stash / reset / 覆盖本地改动。

游戏数据版本也从这里读（:func:`data_version`）：优先兄弟仓库数据里的资源构建号
（``.beyond/ledger/*.json`` 的 ``last_res_version``，仓库处于 revert 提交时也仍然准），读不到再退回
提交标题（``Client:… Data:…``，客户端版本只有这里有）；拼法见 :func:`compose_version`。

这一层由 ``uv run zmdmap generate --sync`` 触发。
"""

import re
import subprocess
from dataclasses import dataclass
from pathlib import Path

import msgspec

from .config import (
    NAVMESH_DIR,
    NAVMESH_FILES,
    REPO_ROOT,
    WORKSPACE_REPO_NAMES,
    ZMDMAP_DATA_DIR,
)
from .log import logger

# 状态 -> 单字符标记，打印同步结果时用。标记只用 GBK 能编出来的字符：Windows 中文控制台
# 编不出 ✘ / ⚠ 这类符号，写出去就是 UnicodeEncodeError。
SYNC_MARKS = {"updated": "↓", "current": "=", "pending": "→", "skipped": "·", "missing": "·"}
SYNC_MARK_UNKNOWN = "x"

#: 资源构建号的长写法：游戏补丁接口的 ``res_version`` 与提交标题的 ``Data:`` 字段都是它，
#: 如 ``initial_11010254-4_main_11010254-4``。
RES_VERSION_RE = re.compile(r"initial_([0-9]+-[0-9]+)_main_([0-9]+-[0-9]+)")

#: 兄弟仓库提交标题里的游戏数据版本，如 ``Client:1.6.3 Data:initial_11010254-4_main_11010254-4``：
#: 客户端版本 + 可选的资源构建号（``Data:`` 字段）。它是数据里读不到构建号时的兜底，拼法见
#: :func:`compose_version`。
DATA_VERSION_RE = re.compile(r"Client:([0-9][0-9A-Za-z._-]*)(?:\s+Data:([0-9A-Za-z._-]+))?")
#: 往上翻多少个提交找版本号（合并 / revert 提交的标题里可能没有）。
DATA_VERSION_LOOKBACK = 20


def res_short(res_version: str) -> str:
    """资源构建号的短写法：``initial_X_main_Y`` 两半相同时缩成 ``X-Y``，否则原样返回。

    短号就是游戏补丁接口 ``resources[].version`` 里的那个号，也是资源包路径（``main/11095949-5_…``）
    里用的写法。
    """
    match = RES_VERSION_RE.fullmatch(res_version)
    if match and match.group(1) == match.group(2):
        return match.group(1)
    return res_version


def compose_version(client: str | None, res_version: str | None) -> str | None:
    """版本标签「客户端版本 + 数据构建号」，如 ``1.6.3+11010254-4``；缺哪半就只写有的那半。

    客户端版本不唯一（同一版客户端下会发布多批数据），带上构建号才分得清数据是哪一批。``+`` 用
    semver 构建元数据的写法：不参与优先级比较 —— 数据是哪一份由 ``sha256`` 说了算，版本只是标签。
    """
    short = res_short(res_version) if res_version else None
    if client and short:
        return f"{client}+{short}"
    return client or short


_CLIENT_LEADING_DIGITS = re.compile(r"^(\d+)")


def build_key(version: str) -> tuple[int, ...] | None:
    """版本标签里的数据构建号（``11010254-4``）化成可比较的数字段；没有构建号返回 ``None``。

    ``compose_version`` 写出来的形态是「客户端版本 + 构建号」（``1.6.3+11010254-4``），只有构建号时整串就是
    构建号（``11010254-4``）。
    """
    head, _, build = version.partition("+")
    parts = (build or head).split("-")
    if len(parts) < 2 or not all(part.isdigit() for part in parts):
        return None
    return tuple(int(part) for part in parts)


def _client_key(client: str) -> tuple[int, ...]:
    """客户端版本化成可比较的数字段：``1.10.0`` 比 ``1.9.3`` 新，带字母后缀的取前面的数字。"""
    parts: list[int] = []
    for part in client.split("."):
        matched = _CLIENT_LEADING_DIGITS.match(part)
        parts.append(int(matched.group(1)) if matched else 0)
    return tuple(parts)


def version_order(version: str) -> tuple[tuple[int, ...], int, tuple[int, ...]]:
    """比较版本标签谁更新用的 key：先比客户端版本，再比有没有构建号，最后比构建号本身。

    客户端版本更新的一律算更新（``1.7.0`` 比 ``1.6.3+11095949-5`` 新）；客户端版本相同时比数据构建号，
    有构建号的比没有的具体。所以两个兄弟仓库落后/超前时，取的是数据最新的那一份，而不是列表里排前面的那个。
    """
    client = version.partition("+")[0]
    key = build_key(version)
    return (_client_key(client), 1 if key is not None else 0, key or ())


@dataclass
class RepoOutcome:
    """一个仓库的同步结果。"""

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


class LedgerState(msgspec.Struct, frozen=True):
    """解包工具记在这份数据里的状态。"""

    last_res_version: str | None = None
    """这批数据来自哪个资源构建，如 initial_11010254-4_main_11010254-4；
    这个文件用蛇形命名，所以这里不加 ``rename="camel"``"""


class Ledger(msgspec.Struct, frozen=True):
    """``.beyond/ledger/*.json``：解包工具留下的记录。"""

    state: LedgerState = LedgerState()
    """这份数据的状态"""


def repo_data_build(repo: Path) -> str | None:
    """这个仓库数据里的资源构建号（``initial_11010254-4_main_11010254-4``），读不到返回 ``None``。

    读 ``.beyond/ledger/*.json`` 的 ``state.last_res_version``：解包工具记在数据里的「这批数据来自哪个
    资源构建」，比提交标题可靠 —— revert / 手动提交的标题会说谎，数据不会。
    """
    for ledger in sorted(repo.glob(".beyond/ledger/*.json")):
        try:
            data = msgspec.json.decode(ledger.read_bytes(), type=Ledger)
        except (OSError, ValueError):
            continue
        if data.state.last_res_version:
            return data.state.last_res_version
    return None


def repo_data_version(repo: Path) -> str | None:
    """这个仓库的游戏数据版本（``1.6.3+11010254-4``），读不到返回 ``None``。

    优先用数据仓库里的构建号（:func:`repo_data_build`）；客户端版本只在提交标题里有，读不到时
    也用标题兜底。合并 / revert 提交会盖住标题，所以往上翻几个提交找，优先带 ``Data:`` 构建号的那条。
    """
    matched = None
    if (repo / ".git").exists():
        result = git(repo, "log", f"-{DATA_VERSION_LOOKBACK}", "--format=%s")
        found = [
            item
            for line in result.stdout.splitlines()
            if (item := DATA_VERSION_RE.search(line)) is not None
        ]
        matched = next((item for item in found if item.group(2) is not None), None)
        if matched is None and found:
            matched = found[0]
    client = matched.group(1) if matched else None
    res_version = repo_data_build(repo) or (matched.group(2) if matched else None)
    return compose_version(client, res_version)


def data_version(workspace: Path) -> str | None:
    """工作区下兄弟仓库的游戏数据版本（``1.6.3+11010254-4``），读不到返回 ``None``。

    每个仓库都先看**数据**里的构建号、再退回提交标题（见 :func:`repo_data_version`），几个仓库
    **取最新的那一份**（:func:`version_order`）—— 两个仓库通常一起解包，落后的一方不该把版本标签拉回去。
    数据文件里的 ``version`` 字段就是这个值：给 ``zmdmap generate`` 用，免得每份数据各写各的版本，
    也免得另外维护一份「当前是哪一版」的记录。
    """
    found: list[tuple[str, str]] = []
    for name in WORKSPACE_REPO_NAMES:
        version = repo_data_version(workspace / name)
        if version:
            found.append((name, version))
    if not found:
        return None
    newest = max(found, key=lambda item: version_order(item[1]))
    if len({version for _, version in found}) > 1:
        detail = "、".join(f"{name} {version}" for name, version in found)
        logger.warning(
            f"兄弟仓库数据版本不一致：{detail}；取最新的 {newest[1]}（{newest[0]}）。"
            "两侧停在不同批次时可以先跑 uv run zmdmap generate --sync"
        )
    return newest[1]


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
