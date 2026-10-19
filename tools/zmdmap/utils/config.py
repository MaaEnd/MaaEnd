"""``tools/zmdmap`` 的静态配置：文件树与路径、语言 key 的映射、发布渠道的地址。

**路径**：输入（TableCfg / JsonData）默认取与 MaaEnd 仓库**平级**的兄弟仓库，可用 CLI 参数或环境变量
覆盖。三档优先级由 click 的 ``envvar`` 与 ``default`` 保证：命令行 > 环境变量 > 默认。``--workspace``
是放兄弟仓库的那个目录，既决定同步哪些仓库，也决定输入目录的默认位置；显式给了 ``--table-cfg-dir`` /
``--json-data-dir``（或对应环境变量）时以显式值为准。产物落在 zmdmap 自己的数据树里：生成或下载出来的
**原始形态**（只带世界坐标）先进不入库的 ``tools/zmdmap/data/raw/``，再由入库步骤进入库的
``tools/zmdmap/data/``——带坐标的由 :mod:`tools.zmdmap.utils.project` 按 BaseNav 的 zone 表换成底图像素。

**语言**是这里最容易写歪的一处：同一批语言，在几处地方用不同写法 ——

| 用处 | 语言 key | 例子 |
| --- | --- | --- |
| 文本表文件 ``I18nTextTable_<后缀>.json`` | 后缀 | ``CN`` |
| EssenceFilter / IconRecognition 读的数据（``weapons`` / ``item_mini_table``） | 后缀 | ``CN`` |
| 入 pipeline 的数据（MaaEnd 生成器读的） | locale 名 | ``zh_cn`` |

所以这里只写映射（:data:`I18N_TABLE_SUFFIXES`、:data:`LOCALE_NAMES`），其余由它们推出来。

**发布渠道**：数据与图标都从 voice.fz.wiki 取——它和 assets.fz.wiki 是同一个源站，但前置的 EdgeOne 有
海外节点，GitHub runner 不必再绕回国内 CDN（那边每个文件都要重新握手，几秒起步、偶发超时）。
"""

from pathlib import Path

# ── 路径与仓库布局 ──────────────────────────────────────────────────────────

# tools/zmdmap/utils/config.py -> utils -> tools/zmdmap -> tools -> 仓库根
UTILS_DIR = Path(__file__).resolve().parent
PACKAGE_DIR = UTILS_DIR.parent
REPO_ROOT = PACKAGE_DIR.parents[1]
# 兄弟仓库默认放在 MaaEnd 的平级目录里。
DEFAULT_WORKSPACE = REPO_ROOT.parent

WORKSPACE_ENV = "ZMDMAP_WORKSPACE"
TABLE_CFG_DIR_ENV = "ZMDMAP_TABLE_CFG_DIR"
JSON_DATA_DIR_ENV = "ZMDMAP_JSON_DATA_DIR"

# 兄弟仓库目录名与各自的输入子路径。
WORKSPACE_REPO_NAMES = ("BeyondMemoryPack", "BeyondTableCfg")
TABLE_CFG_SUBPATH = ("BeyondTableCfg", "TableCfg")
JSON_DATA_SUBPATH = ("BeyondMemoryPack", "JsonData")

# zmdmap 自己的数据树：``data/raw/`` 放原样产物（下载的或本地生成的，不入库），
# ``data/`` 放可以直接用的数据（投影后的，以及不需要投影的）。
ZMDMAP_DATA_DIR = REPO_ROOT / "tools" / "zmdmap" / "data"
DEFAULT_OUT_DIR = ZMDMAP_DATA_DIR / "raw"

# 投影参数（BaseNav 的 zone 表）来自 model 子模块：子路径按调用方给的仓库根拼，默认那份见下。
MODEL_SUBPATH = ("assets", "resource", "model")
NAVMESH_SUBPATH = ("map", "navmesh")
MODEL_SUBMODULE_PATH = REPO_ROOT.joinpath(*MODEL_SUBPATH)
NAVMESH_DIR = MODEL_SUBMODULE_PATH.joinpath(*NAVMESH_SUBPATH)
NAVMESH_FILES = ("base.nav.gz", "base.nav")

# ── 语言 key 映射 ──────────────────────────────────────────────────────────

#: 语言代码 -> ``I18nTextTable_<后缀>.json`` 的后缀；也就是 ``weapons`` / ``item_mini_table`` 里的语言 key。顺序固定。
I18N_TABLE_SUFFIXES: dict[str, str] = {
    "zh-CN": "CN",
    "zh-TW": "TC",
    "en-US": "EN",
    "ja-JP": "JP",
    "ko-KR": "KR",
}

#: 语言代码 -> MaaEnd 的 locale 名（``assets/locales/`` 里的文件名，也是数据文件的语言 key）。
LOCALE_NAMES: dict[str, str] = {
    "zh-CN": "zh_cn",
    "zh-TW": "zh_tw",
    "en-US": "en_us",
    "ja-JP": "ja_jp",
    "ko-KR": "ko_kr",
}

#: 入 pipeline 数据的 ``(语言 key, 语言代码)``。
MAAEND_LOCALES: tuple[tuple[str, str], ...] = tuple(
    (name, lang) for lang, name in LOCALE_NAMES.items()
)

#: 用文本表后缀那一套语言 key 的数据的 ``(语言 key, 语言代码)``。
UPSTREAM_LOCALES: tuple[tuple[str, str], ...] = tuple(
    (suffix, lang) for lang, suffix in I18N_TABLE_SUFFIXES.items()
)

# ── 发布渠道 ───────────────────────────────────────────────────────────────

DATA_BASE_URL = "https://voice.fz.wiki/output_maaend"

#: 发布目录里的清单（``<DATA_BASE_URL>/manifest.json``）：这批数据是哪一版、每份原始形态的内容指纹。
#: 有它就拿本地文件里的 ``sha256`` 跟它比，判断要不要重下；也能认出一份数据在同一个版本号下被重新发布过。
MANIFEST_FILE = "manifest.json"


def table_cfg_dir_in(workspace: Path) -> Path:
    """``<workspace>/BeyondTableCfg/TableCfg``。"""
    return workspace.joinpath(*TABLE_CFG_SUBPATH)


def json_data_dir_in(workspace: Path) -> Path:
    """``<workspace>/BeyondMemoryPack/JsonData``。"""
    return workspace.joinpath(*JSON_DATA_SUBPATH)
