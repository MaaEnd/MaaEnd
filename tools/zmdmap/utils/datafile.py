"""数据文件的形态与读写：最前面的 ``version`` / ``sha256`` 元数据、本体指纹、落盘。

一份数据文件 = 元数据 + 数据本体。两个元数据字段由写这份文件的 ``generate`` 一次定下：``version`` 是
给人看的版本标签，``sha256`` 是这份数据的**原始形态**（只带世界坐标的那一份）的内容指纹。原始形态一写进
``data/raw/`` 就定了，之后入库投影只换本体、两个字段都原样保留，所以 ``data/`` 与 ``data/raw/`` 里同名的
两份，``sha256`` 是一样的：它说明「这份数据是从哪一份原始数据来的」。

``fetch`` 就是拿本地文件里的 ``sha256`` 跟发布清单里的指纹比，决定要不要重下（:func:`read_hash`）；``version``
只给人看，不参与任何比较。别的工具读 ``tools/zmdmap/data/*.json`` 用 :func:`read_payload` 拿本体，不必自己
知道元数据是哪几个字段。

输出用 Python 的 ``json`` 序列化（``ensure_ascii=False``、缩进 4、LF 换行）：整型浮点会带小数点
（``520.0`` 而不是 ``520``），解析出来的值与键顺序不受影响。
"""

import hashlib
import json
from pathlib import Path
from typing import Any

from .shapes import is_object

#: 数据文件最前面的两个元数据字段：``version`` 是给人看的版本标签（「客户端版本 + 数据构建号」，
#: 如 ``1.6.3+11010254-4``，见 :func:`tools.zmdmap.utils.version.compose_version`），``sha256`` 是这份数据
#: 原始形态的内容指纹。
#: 两个都由 ``generate`` 写一次，``fetch`` 与入库都只照抄（见 :func:`keep_metadata`）。
VERSION_KEY = "version"
HASH_KEY = "sha256"
METADATA_KEYS = (VERSION_KEY, HASH_KEY)


def dump_json(value: Any) -> str:
    """数据文件的写法：缩进 4、非 ASCII 原样输出、结尾一个换行。"""
    return json.dumps(value, ensure_ascii=False, indent=4) + "\n"


def write_json(path: Path, value: Any) -> None:
    """按数据文件的写法落盘：父目录不存在就建，换行统一 LF（仓库的 ``.gitattributes`` 是 ``eol=lf``）。"""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(dump_json(value), encoding="utf-8", newline="\n")


def read_json(path: Path) -> Any:
    """读一份 JSON；超出 Number 安全范围的整数在这里天然是精确的 Python int，不会掉精度。"""
    return json.loads(path.read_text(encoding="utf-8"))


def payload_of(data: Any) -> Any:
    """去掉最前面的元数据字段之后的数据本体。"""
    if not is_object(data):
        return data
    return {key: value for key, value in data.items() if key not in METADATA_KEYS}


def metadata_of(data: Any) -> dict[str, Any]:
    """数据文件最前面的元数据字段（原键序、原样）。"""
    if not is_object(data):
        return {}
    return {key: value for key, value in data.items() if key in METADATA_KEYS}


def payload_hash(payload: Any) -> str:
    """数据本体的 sha256：键序无关（排序后紧凑序列化），数值写法与数组顺序照旧。

    与文件怎么排版无关，所以重新格式化不会换指纹；同一份内容无论谁生成，指纹都一样。它是这份数据
    **原始形态**的批次标识：``generate`` 写原始形态时算一次，入库投影之后不再重算。
    """
    text = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def version_of(data: Any) -> str | None:
    """解析好的数据里记的版本；没有、或不是非空字符串时返回 ``None``。"""
    value = data.get(VERSION_KEY) if is_object(data) else None
    return value if isinstance(value, str) and value else None


def read_version(path: Path) -> str | None:
    """读数据文件里记的版本；读不到（没文件 / 没字段 / 字段不是非空字符串）返回 ``None``。"""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    return version_of(data)


def read_hash(path: Path) -> str | None:
    """读数据文件里**自称的** ``sha256``；没有这个字段（或读不出来）返回 ``None``。

    ``fetch`` 用它跟发布清单里的指纹比：只读字段、不读本体 —— 指纹是这份数据原始形态的批次标识，采信
    写这份文件的人（上游 ``generate``）算的那一个。
    """
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    value = data.get(HASH_KEY) if is_object(data) else None
    return value if isinstance(value, str) and value else None


def read_payload_hash(path: Path) -> str | None:
    """按文件里的**内容**重算本体指纹；读不出来返回 ``None``。

    发布端（``generate`` 写发布清单）用它：清单说的是「发布出去的这份内容是什么」，所以按内容重算，而不是
    照抄文件里那个自称值 —— 那个值来自写这份文件的某一次运行，未必就是现在这份内容。
    """
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    return payload_hash(payload_of(data))


def read_payload(path: Path) -> Any:
    """读数据文件里的数据本体：去掉最前面的元数据字段，读不到就报错。

    别的工具读 ``tools/zmdmap/data/*.json`` 用这个（只要本体时也可以用 :func:`payload_of` 处理自己读进来
    的 JSON），不必自己知道元数据是哪几个字段。
    """
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except OSError as error:
        raise ValueError(
            f"缺少 zmdmap 数据 {path}（先跑 uv run zmdmap fetch，或 uv run zmdmap generate）"
        ) from error
    except json.JSONDecodeError as error:
        raise ValueError(f"{path} 不是合法 JSON：{error}") from error
    return payload_of(data)


def with_metadata(data: Any, version: str | None = None) -> Any:
    """把版本与内容指纹写在数据本体前面，指纹按传进来的本体重算；``version`` 不给时沿用数据里已有的版本。

    只有 ``generate`` 生成原始形态时走这里（它是这份数据的作者，指纹得新算）。入库与 ``fetch`` 都不重算，
    走 :func:`keep_metadata` 原样沿用。
    """
    payload = payload_of(data)
    if not is_object(payload):
        return payload
    metadata: dict[str, Any] = {}
    stamp = version or version_of(data)
    if stamp:
        metadata[VERSION_KEY] = stamp
    metadata[HASH_KEY] = payload_hash(payload)
    return {**metadata, **payload}


def keep_metadata(source: Any, body: Any) -> Any:
    """把 ``source`` 的元数据原样放到 ``body`` 前面：只换本体，``version`` / ``sha256`` 一个字不改。

    入库投影走这里 —— 投影之后本体与原始形态不同，但 ``sha256`` 仍然记原始形态那一份，于是 ``data/`` 与
    ``data/raw/`` 里同名的两份指纹一致，入库也不会把上游写的元数据改掉。
    """
    if not is_object(body):
        return body
    return {**metadata_of(source), **body}


def is_unchanged(path: Path, data: Any) -> bool:
    """``path`` 上已有的内容与这份数据逐字节相同时就不用重写。"""
    if not path.exists():
        return False
    try:
        return path.read_text(encoding="utf-8") == dump_json(data)
    except (OSError, UnicodeDecodeError):
        return False
