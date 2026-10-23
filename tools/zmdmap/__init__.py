"""MaaEnd 精简游戏数据（zmdmap 发布数据）的生成工具。

入口是 :mod:`tools.zmdmap.cli`（``uv run zmdmap``），两个子命令都在同一个入口下：``generate`` 从本地
TableCfg / JsonData 生成，``fetch`` 下载 zmdmap 已发布的数据。两边都是「先出**原始形态**（只带游戏世界
坐标）进 ``data/raw/``，再入库进 ``data/``」——带坐标的按 BaseNav 的 zone 表把 x/z 原位换成底图像素，
其余原样复制；各份数据由 ``datasets/`` 里一个模块一份定义。

每份数据最前面写着 ``version``（版本标签）与 ``sha256``（原始形态的内容指纹）：``sha256`` 只在生成原始
形态时算一次，入库原样沿用，下载则会按内容核对一遍再照抄。

别的工具要读这些数据（``tools/zmdmap/data/*.json``）时，用 :func:`read_payload` 拿到数据本体
（``version`` / ``sha256`` 两个元数据字段已经去掉），或 :func:`payload_of` 处理自己读进来的 JSON；
不要自己解析元数据字段。
"""

from .utils.datafile import payload_of, read_payload

__all__ = ["payload_of", "read_payload"]
