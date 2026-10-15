"""MaaEnd 精简游戏数据（zmdmap 发布数据）的生成工具。

入口是 :mod:`tools.zmdmap.cli`（``uv run zmdmap``）：``build_*`` 输出各任务的
精简游戏数据，只带游戏世界坐标；底图像素由 MaaEnd 侧的投影步骤补上。
"""
