"""已登记的 AutoCollect 路线：按执行顺序读出起点传送、各段导航与路线采集的点。"""

import math
import re
from collections.abc import Callable
from typing import Any

import registry
from json_import import discover_path_routes
from model import ActionType
from registry import read_jsonc
from route_builder import Library, RouteError, references, relative

# (区域, x, y) -> 底图像素
ToBase = Callable[[str, float, float], tuple[float, float]]

# 路线里的采集点与采集物点位相差在此以内视为同一个点
MATCH_PX = 5.0
# 两段之间没有定位节点时，上一段终点离本段首点在此以内就接着展开，更远说明中间乘过滑索
CONTINUE_PX = 30.0
COLLECT_ACTIONS = {int(ActionType.COLLECT), int(ActionType.DIG)}
GENERATED_ACTIONS = {int(ActionType.NAVMESH), *COLLECT_ACTIONS}


def custom_action(node: dict[str, Any]) -> tuple[str, dict[str, Any]]:
    action = node.get("action")
    if isinstance(action, dict):
        param = action.get("param", {})
        return param.get("custom_action", ""), param.get("custom_action_param", {})
    return node.get("custom_action", ""), node.get("custom_action_param", {})


def custom_recognition(node: dict[str, Any]) -> tuple[str, dict[str, Any]]:
    recognition = node.get("recognition")
    if isinstance(recognition, dict):
        param = recognition.get("param", {})
        return param.get("custom_recognition", ""), param.get("custom_recognition_param", {})
    return node.get("custom_recognition", ""), node.get("custom_recognition_param", {})


def next_nodes(node: dict[str, Any]) -> list[str]:
    value = node.get("next", [])
    entries = value if isinstance(value, list) else [value]
    return [entry["name"] if isinstance(entry, dict) else re.sub(r"^(\[\w+\])+", "", entry) for entry in entries]


def native_position(point: dict[str, Any]) -> dict[str, Any]:
    return {"position": [point["x"], point["y"]], "zone": point.get("target_tier") or point["zone"]}


def execution_order(document: dict[str, Any], start: str) -> list[str]:
    """从入口沿子任务与 next 能走到的本文件节点，按首次到达排序。"""
    order: list[str] = []

    def visit(name: str) -> None:
        if name in order or not isinstance(document.get(name), dict):
            return
        order.append(name)
        action, param = custom_action(document[name])
        if action == "SubTask":
            for sub in param.get("sub", []):
                visit(sub)
        for following in next_nodes(document[name]):
            visit(following)

    visit(start)
    return order


class RouteReader:
    """把路线文件读成地图上要画的样子：各段路点换算到底图坐标，采集点对到采集物点位。"""

    def __init__(self, library: Library, to_base: ToBase) -> None:
        self.library = library
        self.to_base = to_base
        self.base_zones = {f"{region['zone']}_Base" for region in library.regions.values()}
        self.maps = {region["id"]: map_id for map_id, region in library.regions.items()}
        self.entries = {anchor["entry"]: anchor for anchor in library.anchors}
        self.points: dict[tuple[str, str], list[dict[str, Any]]] = {}
        for point in library.points.values():
            item = library.types[point["detail_id"]]["item"]
            self.points.setdefault((point["map"], item), []).append(point)

    def display_point(self, point: dict[str, Any]) -> dict[str, Any]:
        zone = point.get("target_tier") or point["zone"]
        x, y = point["x"], point["y"]
        if zone and zone not in self.base_zones:
            x, y = self.to_base(zone, x, y)
        return {
            "x": round(x, 2),
            "y": round(y, 2),
            "action": point["action"],
            "actions": point["actions"],
            "strict": point["strict"],
        }

    def anchor_view(self, entry: str) -> dict[str, Any]:
        anchor = self.entries.get(entry)
        if anchor is None:
            return {"entry": entry, "name": ""}
        return {
            "id": anchor["id"],
            "entry": entry,
            "u": anchor["u"],
            "v": anchor["v"],
            "name": anchor["area_names"].get("zh_cn", ""),
        }

    def nearest(self, candidates: list[dict[str, Any]], point: dict[str, Any]) -> str | None:
        best = min(candidates, key=lambda c: math.hypot(c["u"] - point["x"], c["v"] - point["y"]), default=None)
        if best is None or math.hypot(best["u"] - point["x"], best["v"] - point["y"]) > MATCH_PX:
            return None
        return best["id"]

    def read(self, route: registry.Route, document: dict[str, Any]) -> dict[str, Any]:
        map_id = self.maps[route.region]
        zone_id = f"{self.library.regions[map_id]['zone']}_Base"
        start_node = f"AutoCollect{route.id}Start"
        anchor: dict[str, Any] | None = None
        start: dict[str, Any] | None = None
        previous_end: dict[str, Any] | None = None
        segments: list[dict[str, Any]] = []
        known = {start_node, f"AutoCollect{route.id}End"}
        for name in execution_order(document, start_node):
            action, param = custom_action(document[name])
            recognition, target = custom_recognition(document[name])
            if recognition == "MapLocateAssertLocation":
                x, y, w, h = target["target"]
                start = {"position": [x + w / 2, y + h / 2], "zone": target["zone_id"]}
                known.add(name)
            elif action == "SubTask" and anchor is None:
                scene = next((sub for sub in param.get("sub", []) if sub.startswith("SceneEnterWorld")), None)
                if scene is None:
                    continue
                anchor = self.anchor_view(scene)
                if "u" in anchor:
                    start = {"position": [anchor["u"], anchor["v"]], "zone": zone_id}
                known.add(name)
            elif action == "MapNavigateAction":
                known.add(name)
                parsed = discover_path_routes({"path": param.get("path", [])})
                points = parsed[0] if parsed else []
                if not points:
                    continue
                shown = [self.display_point(point) for point in points]
                if start is None and segments:
                    last = segments[-1]["points"][-1]
                    if math.hypot(shown[0]["x"] - last["x"], shown[0]["y"] - last["y"]) <= CONTINUE_PX:
                        start = previous_end
                if start is None:
                    start = native_position(points[0])
                segments.append({"node": name, "start": start, "path": param["path"], "points": shown})
                previous_end = native_position(points[-1])
                start = None

        candidates = [point for item in route.items for point in self.points.get((map_id, item.id), [])]
        visits = [point for segment in segments for point in segment["points"] if COLLECT_ACTIONS & set(point["actions"])]
        matched = [found for point in visits if (found := self.nearest(candidates, point)) is not None]
        point_ids = list(dict.fromkeys(matched))

        notes = []
        if len(segments) > 1:
            notes.append(f"{len(segments)} 段导航")
        actions = {action for segment in segments for point in segment["points"] for action in point["actions"]}
        others = sorted(actions - GENERATED_ACTIONS)
        if others:
            notes.append(f"含 {'、'.join(ActionType(action).name for action in others)} 路点")
        if len(matched) > len(point_ids):
            notes.append(f"重复采集 {len(matched) - len(point_ids)} 次")
        if len(visits) > len(matched):
            notes.append(f"{len(visits) - len(matched)} 个采集点对不上采集物点位")
        extra = [name for name, node in document.items() if isinstance(node, dict) and name not in known]
        if extra:
            notes.append(f"另有 {len(extra)} 个其他节点")
        return {"anchor": anchor, "segments": segments, "point_ids": point_ids, "notes": notes}


def registered_routes(library: Library, to_base: ToBase) -> dict[str, Any]:
    items = registry.load_items()
    routes = registry.load_routes(read_jsonc(registry.REGISTRY_PATH), items)
    names = registry.route_names(routes)
    index = registry.node_references()
    reader = RouteReader(library, to_base)
    views = []
    for route in routes:
        path = registry.route_file(route.region, route.id)
        document = read_jsonc(path)
        view: dict[str, Any] = {
            "id": route.id,
            "region": route.region,
            "map": reader.maps[route.region],
            "kind": route.items[0].kind,
            "name": names[route.id]["zh_cn"],
            "file": relative(path),
            "items": [{"item": item.id, "icon": item.icon, "names": item.names} for item in route.items],
            "references": references(document, index),
        }
        try:
            view.update(reader.read(route, document))
        except RouteError as exc:
            view.update({"error": str(exc), "anchor": None, "segments": [], "point_ids": [], "notes": []})
        views.append(view)
    files = [registry.REGISTRY_PATH, *registry.GENERATED_FILES]
    return {"routes": views, "files": [relative(path) for path in files]}
