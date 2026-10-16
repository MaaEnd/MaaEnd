"""MaaEnd 精简游戏数据（zmdmap 发布数据）的生成工具。

入口是 :mod:`tools.zmdmap.cli`（``uv run zmdmap``）：``build_*`` 输出各任务的
精简游戏数据，只带游戏世界坐标；底图像素由 MaaEnd 侧的投影步骤补上。

别的工具要读这些数据（``tools/zmdmap/data/*.json``）时，用 :func:`read_payload` 拿到数据本体
（元数据已经去掉），或 :func:`payload_of` 处理自己读进来的 JSON；不要自己解析元数据字段。
"""

from .project import payload_of, read_payload

__all__ = ["payload_of", "read_payload"]
