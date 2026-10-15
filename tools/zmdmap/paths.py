"""``tools/zmdmap`` 的输入/输出路径。

输入（TableCfg / JsonData）默认取与 MaaEnd 仓库**平级**的兄弟仓库，可用 CLI 参数或环境变量
覆盖。三档优先级由 click 的 ``envvar`` 与 ``default`` 保证：命令行 > 环境变量 > 默认。

``--workspace`` 是放兄弟仓库的那个目录，既决定同步哪些仓库，也决定输入目录的默认位置；显式
给了 ``--table-cfg-dir`` / ``--json-data-dir``（或对应环境变量）时以显式值为准。

产物落在 zmdmap 自己的数据树里：生成或下载出来的**原始形态**（只带世界坐标）先进不入库的
``tools/zmdmap/data/raw/``，再由入库步骤进入库的 ``tools/zmdmap/data/``——带坐标的由
``tools/zmdmap/project.py`` 按 BaseNav 的 zone 表换成底图像素，其余原样复制。
"""

from __future__ import annotations

from pathlib import Path

# tools/zmdmap/paths.py -> tools/zmdmap -> tools -> 仓库根
PACKAGE_DIR = Path(__file__).resolve().parent
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
MODEL_SUBMODULE_PATH = REPO_ROOT / "assets" / "resource" / "model"
NAVMESH_DIR = MODEL_SUBMODULE_PATH / "map" / "navmesh"
NAVMESH_FILES = ("base.nav.gz", "base.nav")


def table_cfg_dir_in(workspace: Path) -> Path:
    """``<workspace>/BeyondTableCfg/TableCfg``。"""
    return workspace.joinpath(*TABLE_CFG_SUBPATH)


def json_data_dir_in(workspace: Path) -> Path:
    """``<workspace>/BeyondMemoryPack/JsonData``。"""
    return workspace.joinpath(*JSON_DATA_SUBPATH)
