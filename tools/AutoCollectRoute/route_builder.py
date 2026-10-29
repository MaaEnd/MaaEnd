"""AutoCollect 路线生成：选中的采集物 -> 采集顺序与路径 -> 路线文件与登记表。"""

import json
import math
import threading
from collections.abc import Callable, Iterable
from typing import Any

import registry
from registry import COLLECT_POINTS_PATH, PROJECT_ROOT, read_json, read_jsonc, route_kind

# (zone_id, 起点, 终点) -> 运行时规划出的步行长度（底图像素），不连通为 None
WalkFn = Callable[[str, dict[str, Any], dict[str, Any]], "float | None"]

TELEPORT_ANCHORS_PATH = PROJECT_ROOT / "tools" / "zmdmap" / "data" / "teleport_anchors.json"
ANCHOR_NAMES_PATH = PROJECT_ROOT / "tools" / "pipeline-generate" / "SceneTeleport" / "anchors.json"

# 相邻两点（含高度折算）超过此距离就补一个 NAVMESH，由运行时规划前往。
# 现有手写路线里采集点之间直走的段最长 13.1 px，更远的都靠录制的路点或 NAVMESH
STRAIGHT_LEG_MAX_PX = 15.0
# 步行长度超出直线这么多即需绕行，直走会被地形挡住，同样补 NAVMESH
DETOUR_RATIO = 1.15
DETOUR_SLACK_PX = 3.0
# 直线距离在这以内的点归成一簇：簇与簇之间按步行距离排，簇内按直线排
CLUSTER_PX = 20.0
# 直线距离小于此值的两点按直线计，不查询规划器
SHORT_LEG_PX = 12.0
ASSERT_HALF_PX = 10.0
ANCHOR_CANDIDATES = 3
TWO_OPT_ROUNDS = 50
TWO_OPT_NEIGHBORS = 6

KIND_NAMES = {"rare": "稀有", "common": "常规"}
# 任务物品只在任务里出现，不进采集路线
EXCLUDED_ITEM_TYPES = {"MissionItem"}
# 写入请求在线程池里并发执行，从分配路线 id 到生成完成须串行
APPLY_LOCK = threading.Lock()


class RouteError(ValueError):
    """选点或仓库现状不满足生成条件。"""


def straight_walk(zone_id: str, a: dict[str, Any], b: dict[str, Any]) -> float:
    """没有规划器时的步行长度：直线。"""
    return math.hypot(a["u"] - b["u"], a["v"] - b["v"])


class WalkDistances:
    """节点间的步行距离：按需向规划器要实走长度，直线距离是它的下界，用来剪掉不必问的点对。

    排序阶段把距离当作对称的、近处按直线算；最终路线逐段用 measure 按真实方向实测。
    连通关系随查询记录：已知不连通的一对，与两端各自连通的点之间不再查询。
    """

    def __init__(self, zone_id: str, nodes: list[dict[str, Any]], walk: WalkFn) -> None:
        self.zone_id = zone_id
        self.nodes = nodes
        self.walk = walk
        self._sorting: dict[tuple[int, int], float] = {}
        self._measured: dict[tuple[int, int], float] = {}
        self._parent = list(range(len(nodes)))
        self._apart: list[tuple[int, int]] = []

    def _root(self, i: int) -> int:
        while self._parent[i] != i:
            self._parent[i] = self._parent[self._parent[i]]
            i = self._parent[i]
        return i

    def _known_apart(self, i: int, j: int) -> bool:
        ri, rj = self._root(i), self._root(j)
        # 同一分量内的点彼此连通：分量内实测不通的一对，或之后被合并进同一分量的旧记录，都不作数
        return ri != rj and any({self._root(a), self._root(b)} == {ri, rj} for a, b in self._apart)

    def lower(self, i: int, j: int) -> float:
        a, b = self.nodes[i], self.nodes[j]
        return math.hypot(a["u"] - b["u"], a["v"] - b["v"])

    def exact(self, i: int, j: int) -> float:
        key = (min(i, j), max(i, j))
        if key not in self._sorting:
            straight = self.lower(i, j)
            if straight < SHORT_LEG_PX:
                length = straight
            elif self._known_apart(i, j):
                length = math.inf
            else:
                length = self.measure(i, j)
            if math.isinf(length):
                self._apart.append((i, j))
            else:
                self._parent[self._root(i)] = self._root(j)
            self._sorting[key] = length
        return self._sorting[key]

    def measure(self, i: int, j: int) -> float:
        """从 i 走到 j 的实走长度，不连通为 inf。"""
        if (i, j) not in self._measured:
            walked = self.walk(self.zone_id, self.nodes[i], self.nodes[j])
            self._measured[(i, j)] = math.inf if walked is None else walked
        return self._measured[(i, j)]


def walk_order(dist: WalkDistances, start: int, targets: Iterable[int]) -> tuple[list[int], list[int]]:
    """从 start 出发、不回头的采集顺序：最近邻起手，2-opt 收短（起点固定）。

    返回 (顺序, 不连通的点)；顺序以 start 开头。已加入顺序的点彼此连通，与其中任一点不连通的点
    与整条路线均不连通，当即剔除，不再重复查询。
    """
    rest = set(targets)
    unreachable: set[int] = set()
    order = [start]
    while rest:
        best, best_length = None, math.inf
        for j in sorted(rest, key=lambda index: dist.lower(order[-1], index)):
            if dist.lower(order[-1], j) >= best_length:
                break
            length = dist.exact(order[-1], j)
            if math.isinf(length):
                unreachable.add(j)
            elif length < best_length:
                best, best_length = j, length
        rest -= unreachable
        if best is None:
            continue
        rest.remove(best)
        order.append(best)

    # 换边只在直线最近的几个点之间尝试：远处的换法几乎不会更短，每一对却都要查询规划器
    nearby = {
        a: set(sorted((c for c in order if c != a), key=lambda c: dist.lower(a, c))[:TWO_OPT_NEIGHBORS])
        for a in order
    }
    for _ in range(TWO_OPT_ROUNDS):
        improved = False
        for i in range(1, len(order) - 1):
            for j in range(i + 1, len(order)):
                a, b, c = order[i - 1], order[i], order[j]
                if c not in nearby[a]:
                    continue
                d = order[j + 1] if j + 1 < len(order) else None
                before = dist.exact(a, b) + (dist.exact(c, d) if d is not None else 0.0)
                bound = dist.lower(a, c) + (dist.lower(b, d) if d is not None else 0.0)
                if bound + 1e-6 >= before:
                    continue
                after = dist.exact(a, c) + (dist.exact(b, d) if d is not None else 0.0)
                if after + 1e-6 < before:
                    order[i : j + 1] = reversed(order[i : j + 1])
                    improved = True
        if not improved:
            break
    return order, sorted(unreachable)


def clusters_of(points: list[dict[str, Any]]) -> list[list[int]]:
    """单链聚类：直线距离在 CLUSTER_PX 以内的点归为一簇，返回各簇成员下标。"""
    parent = list(range(len(points)))

    def root(i: int) -> int:
        while parent[i] != i:
            parent[i] = parent[parent[i]]
            i = parent[i]
        return i

    for i in range(len(points)):
        for j in range(i + 1, len(points)):
            if straight_walk("", points[i], points[j]) < CLUSTER_PX:
                parent[root(i)] = root(j)
    groups: dict[int, list[int]] = {}
    for i in range(len(points)):
        groups.setdefault(root(i), []).append(i)
    return list(groups.values())


def sweep_cluster(points: list[dict[str, Any]], members: list[int], entry: dict[str, Any]) -> list[int]:
    """簇内顺序：从离入口最近的点开始，按直线最近邻走完。"""
    rest = set(members)
    order: list[int] = []
    here = entry
    while rest:
        nearest = min(rest, key=lambda index: straight_walk("", here, points[index]))
        rest.remove(nearest)
        order.append(nearest)
        here = points[nearest]
    return order


class Library:
    """采集物、营地与传送入口；构造时一次读齐，之后只读。"""

    def __init__(self) -> None:
        collect = read_json(COLLECT_POINTS_PATH)
        if "types" not in collect or any("showing_type" not in info for info in collect["types"].values()):
            raise RouteError(
                "tools/zmdmap/data/collect_points.json 缺少采集物类型表或产物分类，"
                "请执行 uv run zmdmap generate --only collect_points 重新生成"
            )
        self.types: dict[str, Any] = {
            detail_id: {**info, "kind": route_kind(info)}
            for detail_id, info in collect["types"].items()
            if info["item_type"] not in EXCLUDED_ITEM_TYPES
        }
        self.frames: dict[str, Any] = collect["maps"]
        config = read_jsonc(ANCHOR_NAMES_PATH)
        self.regions = {region["map"]: {"id": region_id, **region} for region_id, region in config["regions"].items()}
        self.points = {
            point["id"]: point
            for point in collect["points"]
            if point["detail_id"] in self.types and point["map"] in self.regions
        }

        entries: dict[str, str] = {}
        for level in config["levels"].values():
            prefix = f"{level['region']}{level['sub_region']}"
            for name, anchor_id in {**level["anchors"], **level.get("manual", {})}.items():
                entries[anchor_id] = f"SceneEnterWorld{prefix}{name}"
        teleport = read_json(TELEPORT_ANCHORS_PATH)
        self.anchors = [
            {**anchor, "entry": entries[anchor["id"]]}
            for anchor in teleport["anchors"]
            if anchor["id"] in entries and anchor["map"] in self.regions
        ]

    def catalog(self) -> dict[str, Any]:
        groups: dict[str, Any] = {}
        for detail_id, info in self.types.items():
            group = groups.setdefault(
                info["item"],
                {"item": info["item"], "icon": info["icon"], "action": info["action"], "kind": info["kind"],
                 "names": info["names"], "item_names": info["item_names"], "detail_ids": [], "counts": {}},
            )
            group["detail_ids"].append(detail_id)
        for point in self.points.values():
            counts = groups[self.types[point["detail_id"]]["item"]]["counts"]
            counts[point["map"]] = counts.get(point["map"], 0) + 1
        return {
            "maps": [
                {"map": map_id, "region": region["id"], "name": region["name"],
                 "image": f"MapLocator/{region['zone']}/Base.png"}
                for map_id, region in self.regions.items()
            ],
            "groups": sorted(groups.values(), key=lambda group: -sum(group["counts"].values())),
            "points": [
                {"id": point["id"], "item": self.types[point["detail_id"]]["item"], "map": point["map"],
                 "u": point["u"], "v": point["v"]}
                for point in self.points.values()
            ],
            "anchors": [
                {"id": anchor["id"], "entry": anchor["entry"], "map": anchor["map"], "u": anchor["u"],
                 "v": anchor["v"], "name": anchor["area_names"].get("zh_cn", "")}
                for anchor in self.anchors
            ],
        }

    def distance(self, a: dict[str, Any], b: dict[str, Any]) -> float:
        rise = (a.get("y", 0.0) - b.get("y", 0.0)) * self.frames[a["map"]]["sx"] if "y" in a and "y" in b else 0.0
        return math.sqrt((a["u"] - b["u"]) ** 2 + (a["v"] - b["v"]) ** 2 + rise**2)

    def plan(
        self, map_id: str, point_ids: list[str], walk: WalkFn = straight_walk, kind: str | None = None
    ) -> dict[str, Any]:
        region = self.regions.get(map_id)
        if region is None:
            raise RouteError(f"不支持的底图 {map_id}")
        points = [self.points[point_id] for point_id in dict.fromkeys(point_ids) if point_id in self.points]
        if not points:
            raise RouteError("未选择采集物")
        if any(point["map"] != map_id for point in points):
            raise RouteError("选中的采集物不在同一张底图上")
        if kind is not None:
            mixed = sum(self.types[point["detail_id"]]["kind"] != kind for point in points)
            if mixed:
                other = next(name for key, name in KIND_NAMES.items() if key != kind)
                raise RouteError(f"所选采集物中有 {mixed} 个属于{other}路线，稀有路线与常规路线需分别生成")
        anchors = [anchor for anchor in self.anchors if anchor["map"] == map_id]
        if not anchors:
            raise RouteError(f"{region['name']} 没有可用的传送点")

        def reach(anchor: dict[str, Any]) -> float:
            return min(straight_walk("", anchor, point) for point in points)

        zone_id = f"{region['zone']}_Base"
        groups = clusters_of(points)
        reps = []
        for members in groups:
            cu = sum(points[i]["u"] for i in members) / len(members)
            cv = sum(points[i]["v"] for i in members) / len(members)
            reps.append(points[min(members, key=lambda i: math.hypot(points[i]["u"] - cu, points[i]["v"] - cv))])
        candidates = sorted(anchors, key=reach)[:ANCHOR_CANDIDATES]
        dist = WalkDistances(zone_id, [*reps, *candidates], walk)

        def first_leg(start: int) -> float:
            """从传送点走到最近一簇的步行长度；直线下界剪掉不可能更近的簇。"""
            best = math.inf
            for c in sorted(range(len(reps)), key=lambda index: dist.lower(start, index)):
                if dist.lower(start, c) >= best:
                    break
                best = min(best, dist.exact(start, c))
            return best

        # 起点取首段步行最短的传送点，只排一次序
        start = min(range(len(reps), len(reps) + len(candidates)), key=first_leg)
        if math.isinf(first_leg(start)):
            raise RouteError("所选采集物与附近的传送点均不连通")
        order, cut_off = walk_order(dist, start, range(len(reps)))
        cluster_order = order[1:]
        anchor = dist.nodes[start]
        indices: list[int] = []
        here = anchor
        for c in cluster_order:
            swept = sweep_cluster(points, groups[c], here)
            indices += swept
            here = points[swept[-1]]
        unreachable = [i for c in cut_off for i in groups[c]]

        # 首点与超出直走上限的一段补 NAVMESH，由运行时规划；其余直走的一段按真实方向实测，
        # 需绕行时同样补 NAVMESH，实测不连通的点单独报出并排除在路线外
        legs = WalkDistances(zone_id, [*points, anchor], walk)
        origin = len(points)
        path: list[Any] = [{"action": "ZONE", "zone_id": zone_id}]
        order: list[dict[str, Any]] = []
        blocked: list[str] = []
        previous = origin
        for index in indices:
            point = points[index]
            straight = legs.lower(previous, index)
            navmesh = previous == origin or self.distance(legs.nodes[previous], point) > STRAIGHT_LEG_MAX_PX
            if not navmesh and straight >= SHORT_LEG_PX:
                walked = legs.measure(previous, index)
                if math.isinf(walked):
                    blocked.append(point["id"])
                    continue
                navmesh = walked > straight * DETOUR_RATIO + DETOUR_SLACK_PX
            target = [round(point["u"], 2), round(point["v"], 2)]
            if navmesh:
                path.append({"action": "NAVMESH", "target": target, "target_deck_y": round(point["y"], 2)})
            path.append([*target, self.types[point["detail_id"]]["action"], True])
            order.append(point)
            previous = index

        return {
            "map": map_id,
            "region": region["id"],
            "zone_id": zone_id,
            "anchor": {"id": anchor["id"], "entry": anchor["entry"], "u": anchor["u"], "v": anchor["v"],
                       "name": anchor["area_names"].get("zh_cn", "")},
            "assert_target": [
                round(anchor["u"] - ASSERT_HALF_PX, 2),
                round(anchor["v"] - ASSERT_HALF_PX, 2),
                ASSERT_HALF_PX * 2,
                ASSERT_HALF_PX * 2,
            ],
            "order": [point["id"] for point in order],
            "unreachable": [points[index]["id"] for index in unreachable],
            "blocked": blocked,
            "path": path,
            "items": self.route_items(order),
        }

    def route_items(self, order: list[dict[str, Any]]) -> list[dict[str, Any]]:
        """路线覆盖的产物，按点数从多到少；名字取该产物下点数最多的采集物类型。"""
        tally: dict[str, dict[str, int]] = {}
        for point in order:
            info = self.types[point["detail_id"]]
            per_type = tally.setdefault(info["item"], {})
            per_type[point["detail_id"]] = per_type.get(point["detail_id"], 0) + 1
        items = []
        for item, per_type in sorted(tally.items(), key=lambda kv: -sum(kv[1].values())):
            main_type = max(per_type, key=per_type.get)
            info = self.types[main_type]
            items.append({"item": item, "icon": info["icon"], "names": info["names"],
                          "item_names": info["item_names"], "count": sum(per_type.values())})
        return items


def route_document(route_id: str, plan: dict[str, Any]) -> dict[str, Any]:
    node = f"AutoCollect{route_id}"
    return {
        f"{node}Start": {
            "enabled": False,
            "action": "Custom",
            "custom_action": "SubTask",
            "custom_action_param": {"sub": [plan["anchor"]["entry"]]},
            "next": [f"{node}AssertLocation"],
            "focus": {"Node.Recognition.Succeeded": f"$task.AutoCollect.route.{route_id}.label"},
        },
        f"{node}End": {"pre_delay": 0, "post_delay": 0},
        f"{node}AssertLocation": {
            "desc": "传送到采集点",
            "recognition": "Custom",
            "custom_recognition": "MapLocateAssertLocation",
            "custom_recognition_param": {"zone_id": plan["zone_id"], "target": plan["assert_target"]},
            "action": "DoNothing",
            "next": [f"{node}Goto"],
        },
        f"{node}Goto": {
            "desc": "前往采集点",
            "action": {
                "type": "Custom",
                "param": {"custom_action": "MapNavigateAction", "custom_action_param": {"path": plan["path"]}},
            },
            "next": [f"{node}End"],
            "focus": {"Node.Action.Succeeded": {"trace": True}, "Node.Action.Failed": {"trace": True}},
        },
    }


def preview(
    library: Library,
    map_id: str,
    point_ids: list[str],
    kind: str,
    walk: WalkFn = straight_walk,
) -> dict[str, Any]:
    if kind not in KIND_NAMES:
        raise RouteError(f"未知的路线类别 {kind}")
    plan = library.plan(map_id, point_ids, walk, kind)
    item_ids = [item["item"] for item in plan["items"]]
    route_id = registry.next_route_id(plan["region"], item_ids)
    route_path = registry.route_file(plan["region"], route_id)
    files = [route_path, registry.REGISTRY_PATH, *registry.GENERATED_FILES]
    return {
        **plan,
        "kind": kind,
        "id": route_id,
        "route_file": str(route_path.relative_to(PROJECT_ROOT)),
        "files": [str(path.relative_to(PROJECT_ROOT)) for path in files],
        "route_text": json.dumps(route_document(route_id, plan), ensure_ascii=False, indent=4),
        "path_text": json.dumps(plan["path"], ensure_ascii=False, indent=4),
    }


def apply(
    library: Library,
    map_id: str,
    point_ids: list[str],
    kind: str,
    walk: WalkFn = straight_walk,
) -> dict[str, Any]:
    with APPLY_LOCK:
        result = preview(library, map_id, point_ids, kind, walk)
        route_path = PROJECT_ROOT / result["route_file"]
        if route_path.exists():
            raise RouteError(f"{route_path.name} 已存在")

        registry.validate()
        route_path.parent.mkdir(parents=True, exist_ok=True)
        route_path.write_text(result["route_text"] + "\n", encoding="utf-8")
        registry.add_route(result["id"], result["region"], [item["item"] for item in result["items"]])
        registry.generate()
    return {**result, "written": result["files"]}
