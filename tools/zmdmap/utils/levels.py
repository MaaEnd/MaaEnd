"""MaaEnd 精简游戏数据共用的关卡归属：ID 前缀 -> 关卡 -> 底图。

地图标记的分组 key 与世界实体 ID，除以 10^8 就是 ``LevelBasicInfoTable`` 的
``idNum``；再由关卡 ID 推出它挂在哪张底图上。``teleport_anchors`` 与
``collect_points`` 共用这一套。
"""

from .game_data import Record

ID_LEVEL_FACTOR = 10**8
# indie_dgXXX 各自一张底图，其余关卡按 mapXX / base01 / dung01 归图。
INDIE_LEVEL_HEAD = "indie"

class LevelIndexError(ValueError):
    """关卡编号表有问题。"""

def map_of_level(level_id: str) -> str:
    """关卡 ID -> 底图 key。"""
    head = level_id.split("_")[0]
    return level_id if head == INDIE_LEVEL_HEAD else head

def build_level_index(level_basic_info: dict[str, Record]) -> dict[int, str]:
    """``idNum -> levelId``。"""
    levels: dict[int, str] = {}
    for level_id in sorted(level_basic_info):
        id_num = level_basic_info[level_id].idNum
        if id_num in levels:
            raise LevelIndexError(f"idNum {id_num} 同时属于 {levels[id_num]} 和 {level_id}")
        levels[id_num] = level_id
    if not levels:
        raise LevelIndexError("LevelBasicInfoTable 里一个关卡都没有")
    return levels

def level_of(levels: dict[int, str], raw_id: str) -> str | None:
    """按 ID 前缀定位关卡；非数字 ID 或未登记的前缀都返回 None。"""
    if not raw_id.isdigit():
        return None
    return levels.get(int(raw_id) // ID_LEVEL_FACTOR)
