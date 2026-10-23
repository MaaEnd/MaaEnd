"""游戏数据的版本标签：怎么认、怎么比新旧、从哪儿读。

标签就是游戏数据的构建号，形如 ``11262943-8`` —— 也就是资源包的 ``res_version``
（``initial_11262943-8_main_11262943-8``）的短写法：两半相同时缩成一段，不同时原样保留（:func:`res_short`）。
标构建号而不是客户端版本号，是因为同一版客户端下会发多批数据，客户端版本分不清是哪一批；「数据是哪一批」
最终由 ``sha256`` 说了算，版本只是给人看的标签。

读过的地方，按可靠性排：

1. 兄弟仓库数据里的构建号（``.beyond/ledger/*.json`` 的 ``last_res_version``，仓库停在 revert 提交上也准）；
2. 兄弟仓库的提交标题（``Client:1.6.4 Data:initial_11262943-8_main_11262943-8`` 里的 ``Data:`` 那半）；
3. 数据目录路径里的 ``initial_…_main_…``（发布端把解包目录直接递进来，既没兄弟仓库也没 ledger）。
"""

import re
from pathlib import Path

import msgspec

from .config import WORKSPACE_REPO_NAMES
from .log import logger
from .workspace import git

#: 资源构建号的长写法：游戏补丁接口的 ``res_version`` 与提交标题的 ``Data:`` 字段都是它，
#: 如 ``initial_11010254-4_main_11010254-4``。
RES_VERSION_RE = re.compile(r"initial_([0-9]+-[0-9]+)_main_([0-9]+-[0-9]+)")


#: 兄弟仓库提交标题里的资源构建号，如 ``… Data:initial_11262943-8_main_11262943-8`` 里的那一段。
DATA_VERSION_RE = re.compile(r"Data:([0-9A-Za-z._-]+)")
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


def build_key(version: str) -> tuple[int, ...] | None:
    """把构建号化成可比较的数字段（``11262943-8`` -> ``(11262943, 8)``）；认不出来返回 ``None``。

    长写法（``initial_X_main_Y``）先取前一半再切，所以两种写法都能比。
    """
    match = RES_VERSION_RE.fullmatch(version)
    text = match.group(1) if match else version
    parts = text.split("-")
    if len(parts) < 2 or not all(part.isdigit() for part in parts):
        return None
    return tuple(int(part) for part in parts)


def version_order(version: str) -> tuple[int, ...]:
    """比较两个版本谁更新用的 key：按构建号的数字段比，认不出来的排最前（当最旧）。"""
    return build_key(version) or ()


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
    """这个仓库的游戏数据版本（``11262943-8``），读不到返回 ``None``。

    优先用数据里的构建号（:func:`repo_data_build`）；没有 ledger 时退回提交标题里的 ``Data:`` 字段。
    合并 / revert 提交会盖住标题，所以往上翻几个提交找。
    """
    build = repo_data_build(repo)
    if build:
        return res_short(build)
    if not (repo / ".git").exists():
        return None
    result = git(repo, "log", f"-{DATA_VERSION_LOOKBACK}", "--format=%s")
    for line in result.stdout.splitlines():
        matched = DATA_VERSION_RE.search(line)
        if matched:
            return res_short(matched.group(1))
    return None


def data_version_from_path(*paths: Path) -> str | None:
    """数据目录自己带的版本：``…/initial_11262943-8_main_11262943-8/Data/TableCfg`` 里的构建号。

    发布端（zmdmap 的 CI）把解包结果放在 ``<存储根>/<构建号>/Data/…`` 直接生成，那里既没有兄弟仓库、也没有
    ``.beyond/ledger``，提交标题同样读不到 —— 构建号本来就在路径里，认它就行。发布端手里只有这一份数据，
    所以这是它唯一能自己拿到的来源；要别的标签就在命令行用 ``--version`` 给。

    先从路径里找 ``initial_…_main_…``，找不到再往上两三层的 ``.beyond/ledger`` 里找。
    """
    for path in paths:
        # 先用原样的路径找：目录若是软链接，``resolve()`` 会把 ``initial_…_main_…`` 这几层也展开掉。
        for candidate in (str(path), str(path.resolve())):
            match = RES_VERSION_RE.search(candidate)
            if match:
                return res_short(match.group(0))
        resolved = path.resolve()
        for ancestor in (resolved.parent, resolved.parent.parent):
            build = repo_data_build(ancestor)
            if build:
                return res_short(build)
    return None


def data_version(workspace: Path) -> str | None:
    """工作区下兄弟仓库的游戏数据版本（``11262943-8``），读不到返回 ``None``。

    每个仓库都先看**数据**里的构建号、再退回提交标题（见 :func:`repo_data_version`），几个仓库取**最新的
    那一份**（:func:`version_order`）—— 两个仓库通常一起解包，落后的一方不该把版本标签拉回去。数据文件里的
    ``version`` 字段就是这个值：给 ``zmdmap generate`` 用，免得每份数据各写各的版本，也免得另外维护一份
    「当前是哪一版」的记录。
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
