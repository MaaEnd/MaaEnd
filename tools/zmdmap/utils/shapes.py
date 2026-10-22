"""JSON 值的类型收窄。

``isinstance(value, dict)`` 在 pyright 眼里会把 ``value`` 收窄成没有参数的 ``dict``（也就是
``dict[Unknown, Unknown]``），strict 模式下接着就会在 ``.get()``、迭代、取值处报一串「部分未知」。

这里用 :class:`typing.TypeGuard` 把「确认是 JSON 对象/数组」这一步说明白：调用处写成
``if is_object(value): …`` 就能直接拿到 ``dict[str, Any]``，不必到处写 ``cast``。
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
