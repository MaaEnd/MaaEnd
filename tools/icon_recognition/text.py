"""文本清洗和资源标识校验。"""

from __future__ import annotations

import re


_CONTROL_RE = re.compile(r"[\x00-\x1f\x7f]")
_SPACE_RE = re.compile(r"\s+")


def clean_text(value: object, *, field: str) -> str:
    if not isinstance(value, str):
        raise ValueError(f"{field} 必须是字符串")
    cleaned = _SPACE_RE.sub(" ", _CONTROL_RE.sub(" ", value)).strip()
    if not cleaned:
        raise ValueError(f"{field} 清洗后不能为空")
    return cleaned


def validate_identifier(value: object, *, field: str) -> str:
    if not isinstance(value, str) or not value:
        raise ValueError(f"{field} 必须是非空字符串")
    if _CONTROL_RE.search(value) or "/" in value or "\\" in value:
        raise ValueError(f"{field} 不能包含控制字符或路径分隔符")
    return value
