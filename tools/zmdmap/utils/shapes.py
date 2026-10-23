"""读取没有建模的 JSON 时用的收窄助手。

表格与配置都走 :mod:`tools.zmdmap.utils.tables` 里的结构体，取值不必再判断类型。留在这里的只有两类
读不出结构的地方：

- **必须原样改写的任意载荷**：投影（:mod:`~tools.zmdmap.utils.project`）要在不碰其它字段的前提下换坐标，
  数据文件（:mod:`~tools.zmdmap.utils.datafile`）要算指纹、读 ``version`` / ``sha256``；
- **形状随游戏内容变化的属性**：交互模板的 ``componentProperties``、商店商品的 ``unlockConditions``、
  ``LevelData`` 里按组件名分的属性，以及发布清单这种上游随时可能加字段的 JSON。

这些地方用 ``is_object`` / ``is_array`` 收窄之后才能安全地 ``.get(...)``；有了结构体的地方就别再退回这里。
"""

from typing import Any, TypeGuard


def is_object(value: Any) -> TypeGuard[dict[str, Any]]:
    """这个值是不是 JSON 对象（键一定是字符串，值原样）。"""
    return isinstance(value, dict)


def is_array(value: Any) -> TypeGuard[list[Any]]:
    """这个值是不是 JSON 数组。"""
    return isinstance(value, list)


def as_object(value: Any) -> dict[str, Any]:
    """当成 JSON 对象用；不是对象时给个空对象（「没有这段」与「这段是空的」一起处理）。"""
    return value if is_object(value) else {}


def as_objects(value: Any) -> list[dict[str, Any]]:
    """当成「对象数组」用；不是数组时给空列表，数组里不是对象的元素丢掉。"""
    return [item for item in value if is_object(item)] if is_array(value) else []


def as_array(value: Any) -> list[Any]:
    """当成 JSON 数组用；不是数组时给空列表。

    pyright 在 strict 下把「直接迭代 ``Any``」当成 ``Any | Unknown`` 报「部分未知」，
    统一从这里取元素类型是 ``Any`` 的列表就没有这个噪音了。
    """
    return value if is_array(value) else []
