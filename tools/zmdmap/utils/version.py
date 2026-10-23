"""游戏数据的版本标签：怎么解析、怎么拼、怎么比新旧、从哪儿读。

一个标签长这样 ``1.6.3+11010254-4`` —— 客户端版本 + 数据构建号（:func:`compose_version`）。构建号有短写法
``11010254-4``（:func:`res_short`）和完整写法 ``initial_11010254-4_main_11010254-4``。

发布端（数据 CI）从版本接口拿到客户端版本与构建号，生成时用 ``--version`` 显式传进来；本地没传
``--version`` 时按下面这些读：

1. 兄弟仓库数据里的构建号（``.beyond/ledger/*.json`` 的 ``last_res_version``，仓库停在 revert 提交上也准）；
2. 提交标题里的 ``Client:… Data:…``（客户端版本只有这里有，见 :func:`repo_data_version`）；
3. 数据目录路径里的 ``initial_…_main_…``（解包目录直接递进来时用），这时只拿得到构建号那一半
   （:func:`data_version_from_path`）。

客户端版本不唯一 —— 同一版客户端下会发多批数据 —— 带上构建号才分得清是哪一批；``+`` 用 semver 构建元数据
的写法，不参与优先级比较：数据是哪一份由 ``sha256`` 说了算，版本只是标签。
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


def normalize_version(label: str) -> str:
    """把命令行给的 ``--version`` 缩成标准写法：``1.6.4+initial_X_main_Y`` 取 ``1.6.4+X-Y``，
    只给 ``initial_X_main_Y`` 时取 ``X-Y``；认不出来的原样返回（允许随意自定标签）。

    版本接口返回的 ``res_version`` 就是长写法，调用方原样传进来即可，不必自己缩。
    """
    client, sep, build = label.partition("+")
    if not sep:
        return res_short(label)
    return f"{client}+{res_short(build)}"


_CLIENT_LEADING_DIGITS = re.compile(r"^(\d+)")
#: 客户端版本至少是「主版本.次版本」（``1.6``），后面可以跟任意后缀。
_CLIENT_RE = re.compile(r"^\d+\.\d+")


def is_complete_version(label: str) -> bool:
    """这个标签是不是完整形态：客户端版本与数据构建号都在，如 ``1.6.3+11010254-4``。

    只有构建号（``11010254-4``）、只有客户端版本（``1.6.3``）、空串都不算 —— 写进数据之后补不回来，
    与其发一份没有客户端版本的数据，不如在这里就报错。
    """
    client, sep, build = label.partition("+")
    if not sep or not client or not build:
        return False
    return bool(_CLIENT_RE.match(client)) and build_key(build) is not None


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


def data_version_from_path(*paths: Path) -> str | None:
    """数据目录自己带的版本：``…/initial_11262943-8_main_11262943-8/Data/TableCfg`` 里的构建号。

    发布端（zmdmap 的 CI）把解包结果放在 ``<存储根>/<构建号>/Data/…`` 直接生成，那里既没有兄弟仓库、也没有
    ``.beyond/ledger``，:func:`data_version` 与提交标题都读不到版本。构建号本来就在路径里，认它就行 —— 少了
    客户端那一半不影响「这份数据是哪一批」（:func:`compose_version` 缺哪半就写哪半），总比留空强。

    先从路径里找 ``initial_…_main_…``，找不到再往上两三层的 ``.beyond/ledger`` 里找。
    """
    for path in paths:
        # 先用原样的路径找：目录若是软链接，``resolve()`` 会把 ``initial_…_main_…`` 这几层也展开掉。
        for candidate in (str(path), str(path.resolve())):
            match = RES_VERSION_RE.search(candidate)
            if match:
                return compose_version(None, match.group(0))
        resolved = path.resolve()
        for ancestor in (resolved.parent, resolved.parent.parent):
            build = repo_data_build(ancestor)
            if build:
                return compose_version(None, build)
    return None


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

