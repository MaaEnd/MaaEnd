"""读「必须原样保留未知字段」的 JSON 时用的收窄助手。

游戏内容都有结构：表格、关卡数据、交互模板都在 :mod:`tools.zmdmap.utils.tables` 里声明成 ``msgspec.Struct``，
形状一变解码就报错并指出是哪个文件、哪个字段。留在这里的只有**不能建模**的三处，它们的共同点是必须把没声明
的字段原样带走：

- 投影（:mod:`~tools.zmdmap.utils.project`）要在不改动其它字段的前提下换坐标；
- 数据文件（:mod:`~tools.zmdmap.utils.datafile`）要按内容算指纹、读 ``version`` / ``sha256``；
- 下载回来的内容校验（:mod:`tools.zmdmap.fetch` 里的 ``content_problem``）要先看清它自称的字段。

一旦建模就会丢掉没声明的字段，正好破坏「别的字段一个字都不动」这个前提，所以只能按原始 JSON 走。
"""

from typing import Any, TypeGuard


def is_object(value: Any) -> TypeGuard[dict[str, Any]]:
    """这个值是不是 JSON 对象（键一定是字符串，值原样）。"""
    return isinstance(value, dict)


def is_array(value: Any) -> TypeGuard[list[Any]]:
    """这个值是不是 JSON 数组。"""
    return isinstance(value, list)
