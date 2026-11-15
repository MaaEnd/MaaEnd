"""去掉自动做任务数据里不需要的高度。

assets/data/AutoMission/missions.json.gz 里的坐标点可以带 height（世界高度），寻路时会写成
NAVMESH 的 target_deck_y，把终点钉在那一层可走面上。只有目标点底下压着多层可走面（走廊 /
天桥 / 屋顶）时才需要钉层；单层的点钉了没有好处，navmesh 更新后高度对不上反而会让这一段
直接失败。

规则：对每个带 height 的点跑 navmesh 的 deck_probe，查出的层数 <= 1 就去掉 height，
>= 2 层保留；navmesh 里没有的 zone 也去掉 height。

每次更新任务数据后都要跑一次，先不加 --write 看统计，确认后再写回：

    .venv/Scripts/python.exe tools/AutoMission/strip_deck_height.py          # 只看统计
    .venv/Scripts/python.exe tools/AutoMission/strip_deck_height.py --write  # 写回文件

依赖：项目 .venv 里装好 maa，并已构建 install/agent/cpp-algo.exe（navmesh 查询走 cpp-algo agent）。
统计里列出的「多层但高度对不上任何一层」的点，寻路时可能失败或停错层，需要人工看数据来源。
"""

import argparse
import collections
import gzip
import json
import sys
from pathlib import Path
from typing import Any

PROJECT_ROOT = Path(__file__).resolve().parents[2]
DATA_PATH = PROJECT_ROOT / "assets" / "data" / "AutoMission" / "missions.json.gz"
sys.path.insert(0, str(PROJECT_ROOT / "tools" / "MapNavigator"))

from navmesh_backend import NavmeshBackend, default_pack_path  # noqa: E402

# 与 cpp-algo 的 kDeckBand 一致：高度相差在此范围内算同一层。
DECK_BAND = 2.0


def dump(missions: Any) -> bytes:
    return json.dumps(missions, ensure_ascii=False, separators=(",", ":")).encode()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--write", action="store_true", help="把结果写回 missions.json.gz")
    args = parser.parse_args()

    raw = gzip.decompress(DATA_PATH.read_bytes())
    missions = json.loads(raw)
    if dump(missions) != raw:
        print("missions.json.gz 不是紧凑 JSON，重新序列化会改动无关内容，先检查数据格式", file=sys.stderr)
        return 1

    backend = NavmeshBackend(default_pack_path())
    try:
        zones = backend.query("zones")
        if not zones.get("ok"):
            print(f"navmesh 查询失败: {zones}", file=sys.stderr)
            return 1
        zone_ids = {zone["name"]: zone["zone_id"] for zone in zones["zones"]}

        stats: collections.Counter[str] = collections.Counter()
        deck_counts: collections.Counter[int] = collections.Counter()
        unknown_zones: collections.Counter[str] = collections.Counter()
        off_band: list[tuple[str, str, float, float, float, list[float]]] = []
        cache: dict[tuple[str, float, float], list[dict[str, Any]]] = {}

        def probe(zone: str, x: float, y: float) -> list[dict[str, Any]]:
            key = (zone, x, y)
            if key not in cache:
                result = backend.query("deck_probe", zone_id=zone_ids[zone], point=[x, y])
                if not result.get("ok"):
                    raise RuntimeError(f"deck_probe 失败: {zone} {x} {y} {result}")
                cache[key] = result["decks"]
            return cache[key]

        def visit(obj: Any, path: str) -> None:
            if isinstance(obj, dict):
                if all(k in obj for k in ("height", "zone_id", "x", "y")):
                    stats["points"] += 1
                    zone = obj["zone_id"]
                    if zone not in zone_ids:
                        # navmesh 里没有这个 zone，寻路不会用到这层高度。
                        del obj["height"]
                        unknown_zones[zone] += 1
                    else:
                        decks = probe(zone, obj["x"], obj["y"])
                        deck_counts[len(decks)] += 1
                        if len(decks) <= 1:
                            del obj["height"]
                            stats["stripped"] += 1
                        else:
                            stats["kept"] += 1
                            if not any(abs(deck["height"] - obj["height"]) <= DECK_BAND for deck in decks):
                                off_band.append(
                                    (
                                        path,
                                        zone,
                                        obj["x"],
                                        obj["y"],
                                        obj["height"],
                                        [round(deck["height"], 2) for deck in decks],
                                    )
                                )
                for key, value in obj.items():
                    visit(value, f"{path}/{key}")
            elif isinstance(obj, list):
                for i, value in enumerate(obj):
                    visit(value, f"{path}/{i}")

        visit(missions, "")
    finally:
        backend.close()

    print(f"带高度的点: {stats['points']}")
    print(f"  去掉高度（<=1 层）: {stats['stripped']}")
    print(f"  保留高度（>=2 层）: {stats['kept']}")
    print(f"  去掉高度（navmesh 里没有的 zone）: {dict(unknown_zones)}")
    print(f"层数分布: {dict(sorted(deck_counts.items()))}")
    print(f"多层但高度对不上任何一层: {len(off_band)}")
    for path, zone, x, y, height, decks in off_band:
        print(f"  {path} {zone} ({x}, {y}) height={height} decks={decks}")

    if args.write:
        DATA_PATH.write_bytes(gzip.compress(dump(missions), compresslevel=9, mtime=0))
        print(f"已写回 {DATA_PATH.relative_to(PROJECT_ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
