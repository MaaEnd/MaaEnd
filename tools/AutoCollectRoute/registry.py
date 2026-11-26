"""AutoCollect 路线登记表 -> 调度节点、任务选项与五语文案。"""

import json
import math
import re
from collections import Counter
from collections.abc import Callable, Iterable
from dataclasses import dataclass, field
from itertools import groupby
from pathlib import Path
from typing import Any

import json5

PROJECT_ROOT = Path(__file__).resolve().parents[2]
REGISTRY_PATH = PROJECT_ROOT / "tools" / "AutoCollectRoute" / "routes.json"
COLLECT_POINTS_PATH = PROJECT_ROOT / "tools" / "zmdmap" / "data" / "collect_points.json"
ROUTE_DIR = PROJECT_ROOT / "assets" / "resource" / "pipeline" / "AutoCollect"
DISPATCH_PATH = ROUTE_DIR / "Dispatch.json"
TASK_PATH = PROJECT_ROOT / "assets" / "tasks" / "AutoCollect.json"
LOCALE_DIR = PROJECT_ROOT / "assets" / "locales" / "interface"
SCENE_INTERFACE_DIR = PROJECT_ROOT / "assets" / "resource" / "pipeline" / "Interface"

LOCALES = ("zh_cn", "zh_tw", "en_us", "ja_jp", "ko_kr")
GENERATED_FILES = (DISPATCH_PATH, TASK_PATH, *(LOCALE_DIR / f"{locale}.json" for locale in LOCALES))
KINDS = ("rare", "common")
KIND_DEFAULT_LIMITS = {"rare": 32, "common": 0}
RARE_SHOWING_TYPE = "Nurturance"

HAND_WRITTEN_OPTIONS = (
    "AutoCollectSchedule",
    "AutoCollectStashBackpackSubTask",
    "AutoCollectMidSweepStash",
    "AutoCollectMidSweepStashEvery",
    "AutoCollectMode",
    "AutoCollectTargetInventory",
    "AutoCollectSkipEmptyStock",
)
# 稀有采集物区（兔耳图标）与稀有矿物区（矿石图标）：大地图上带存量角标（1~10），采空后不显示角标。
# 点位类型前缀 -> (MapIcons 图标名, 图标重叠时选择列表项的模板)
STOCK_AREA_KINDS = {
    "int_doodad_core_plant_rabbithole": ("RareCollectArea", "AutoCollect/RareCollectListEntry.png"),
    "int_doodad_core_spcstone": ("RareMineralArea", "AutoCollect/RareMineralListEntry.png"),
}
STOCK_AREA_RADIUS = 15.0
REGION_MAPS = {"ValleyIV": "map01", "Wuling": "map02"}
STOCK_COLLECT_ACTIONS = ("COLLECT", "DIG")
STOCK_PANEL_COUNT_ROI = [1120, 160, 105, 40]
HAND_WRITTEN_KEYS = (
    "option.AutoCollectMode.",
    "option.AutoCollectTargetInventory.description",
    "option.AutoCollectTargetInventory.label",
    "option.AutoCollectTargetInventory.pattern_msg",
)
GENERATED_TASK_KEYS = ("task.AutoCollect.item.", "task.AutoCollect.route.")
REFERENCE_GLOBS = (
    "assets/tasks/**/*.json",
    "assets/resource*/pipeline/**/*.json",
    "tests/*/*.json",
    "tests/*/*/*.json",
)
NODE_NAME = re.compile(r"AutoCollect[A-Za-z0-9_]+")

KIND_LABELS = {
    "rare": {
        "zh_cn": "稀有采集物",
        "zh_tw": "稀有採集物",
        "en_us": "Rare collectibles",
        "ja_jp": "レア採集物",
        "ko_kr": "희귀 채집물",
    },
    "common": {
        "zh_cn": "一般采集物",
        "zh_tw": "一般採集物",
        "en_us": "Common collectibles",
        "ja_jp": "一般採集物",
        "ko_kr": "일반 채집물",
    },
}
ROUTE_LABELS = {
    "zh_cn": "采集{name}",
    "zh_tw": "採集{name}",
    "en_us": "Collecting {name}",
    "ja_jp": "{name}を採集",
    "ko_kr": "{name} 채집",
}
ROUTE_NUMBERED = {
    "zh_cn": "{name}（路线{n}）",
    "zh_tw": "{name}（路線{n}）",
    "en_us": "{name} (route {n})",
    "ja_jp": "{name}（ルート{n}）",
    "ko_kr": "{name} (경로 {n})",
}
FAILED_TEXTS = {
    "zh_cn": "{name}采集失败",
    "zh_tw": "{name}採集失敗",
    "en_us": "{name} collection failed",
    "ja_jp": "{name}：採集失敗",
    "ko_kr": "{name} 채집 실패",
}
FAILED_STYLE = '<span style="color: red; font-weight: bold;">{}</span>'
SKIPPED_TEXTS = {
    "zh_cn": "{name} 库存已达目标，本次跳过",
    "zh_tw": "{name} 庫存已達目標，本次跳過",
    "en_us": "{name} — target inventory reached, skipped this run",
    "ja_jp": "{name} 在庫が目標に達したため、今回はスキップします",
    "ko_kr": "{name} 재고가 목표에 도달하여 이번에는 건너뜁니다",
}
STOCK_EMPTY_TEXTS = {
    "zh_cn": "{name} 存量为 0，本次跳过",
    "zh_tw": "{name} 存量為 0，本次跳過",
    "en_us": "{name} — area stock is 0, skipped this run",
    "ja_jp": "{name} エリアの在庫が0のため、今回はスキップします",
    "ko_kr": "{name} 구역 재고가 0이라 이번에는 건너뜁니다",
}


class RegistryError(ValueError):
    """登记表与采集物数据或路线文件对不上。"""


@dataclass(frozen=True)
class Item:
    id: str
    key: str
    icon: str
    kind: str
    names: dict[str, str]

    @property
    def series(self) -> str:
        """去掉末尾阶数/变体号：item_plant_mushroom_1_2 -> item_plant_mushroom_1"""
        return self.id.rsplit("_", 1)[0]

    @property
    def grade(self) -> int:
        tail = self.id.rsplit("_", 1)[-1]
        return int(tail) if tail.isdigit() else 0


@dataclass(frozen=True)
class Route:
    id: str
    region: str
    items: tuple[Item, ...]
    number: int

    @property
    def stem(self) -> str:
        return route_stem(self.region, self.items)


@dataclass
class Group:
    region: str
    item: Item
    routes: list[Route] = field(default_factory=list)

    @property
    def node(self) -> str:
        return f"__AutoCollect{self.region}{self.item.key}"


@dataclass(frozen=True)
class StockArea:
    """路线采的那片稀有采集物区或稀有矿物区：底图坐标、地图图标、列表项模板，以及传送前打开的子区域地图入口。"""

    at: tuple[float, float]
    icon: str
    list_entry: str
    scene: str


# zmdmap 数据与 locale 是机器写出的严格 JSON，体积大，用 json 读；手写的配置按 JSONC 读
def read_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def read_jsonc(path: Path) -> Any:
    return json5.loads(path.read_text(encoding="utf-8"))


def write_json(path: Path, data: Any) -> None:
    path.write_text(json.dumps(data, ensure_ascii=False, indent=4) + "\n", encoding="utf-8")


def to_pascal(text: str) -> str:
    return "".join(part[:1].upper() + part[1:] for part in re.split(r"[^A-Za-z0-9]+", text.replace("'s", "")) if part)


def route_kind(info: dict[str, Any]) -> str:
    """采集物归哪类：看主产物的展示分类。"""
    return "rare" if info["showing_type"] == RARE_SHOWING_TYPE else "common"


def route_stem(region: str, items: Iterable[Item]) -> str:
    return region + "".join(item.key for item in items)


def route_file(region: str, route_id: str) -> Path:
    return ROUTE_DIR / region / f"{route_id}.json"


def join_names(locale: str, names: list[str]) -> str:
    if len(names) == 1:
        return names[0]
    if locale == "en_us":
        return ", ".join(names[:-1]) + " and " + names[-1]
    if locale == "ja_jp":
        return "と".join(names)
    if locale == "ko_kr":
        head = ", ".join(names[:-1])
        last_char = head[-1]
        has_final = "가" <= last_char <= "힣" and (ord(last_char) - ord("가")) % 28 != 0
        return f"{head}{'과' if has_final else '와'} {names[-1]}"
    return "、".join(names)


def load_items() -> dict[str, Item]:
    return {
        info["item"]: Item(
            info["item"], to_pascal(info["names"]["en_us"]), info["icon"], route_kind(info), info["names"]
        )
        for info in read_json(COLLECT_POINTS_PATH)["types"].values()
    }


def load_routes(registry: dict[str, Any], items: dict[str, Item]) -> list[Route]:
    routes = []
    for entry in registry["routes"]:
        route_id = entry["id"]
        unknown = [item for item in entry["items"] if item not in items]
        if unknown:
            raise RegistryError(f"{route_id}: 未知采集物 {', '.join(unknown)}")
        route_items = tuple(items[item] for item in entry["items"])
        if len({item.kind for item in route_items}) > 1:
            raise RegistryError(f"{route_id}: 稀有与常规采集物需分别成线")
        stem = route_stem(entry["region"], route_items)
        match = re.fullmatch(rf"{stem}([1-9]\d*)", route_id)
        if match is None:
            raise RegistryError(f"{route_id}: 应命名为 {stem}<序号>")
        path = route_file(entry["region"], route_id)
        if not path.is_file() or f"AutoCollect{route_id}Start" not in read_jsonc(path):
            raise RegistryError(f"{route_id}: {path.relative_to(PROJECT_ROOT)} 缺少 AutoCollect{route_id}Start")
        routes.append(Route(route_id, entry["region"], route_items, int(match.group(1))))
    duplicated = [route_id for route_id, count in Counter(route.id for route in routes).items() if count > 1]
    if duplicated:
        raise RegistryError(f"路线 id 重复: {', '.join(duplicated)}")
    return routes


def route_collect_points(route: Route) -> list[tuple[float, float]] | None:
    """路线全部采集点的底图坐标；有采集点不在底图上（分层或对象写法）时返回 None。"""
    points = []
    for name, node in read_jsonc(route_file(route.region, route.id)).items():
        if not name.startswith(f"AutoCollect{route.id}Goto"):
            continue
        action = node.get("action")
        param = (action.get("param", {}) if isinstance(action, dict) else node).get("custom_action_param") or {}
        zone = ""
        for waypoint in param.get("path", []):
            if isinstance(waypoint, dict):
                if waypoint.get("action") == "ZONE":
                    zone = waypoint.get("zone_id") or ""
                elif waypoint.get("action") in STOCK_COLLECT_ACTIONS:
                    return None
                continue
            if len(waypoint) >= 3 and waypoint[2] in STOCK_COLLECT_ACTIONS:
                if not zone.endswith("_Base"):
                    return None
                points.append((waypoint[0], waypoint[1]))
    return points


def teleport_scene(route: Route, scenes: dict[str, Any]) -> str | None:
    """路线起点传送入口在传送前打开的子区域地图入口 SceneEnterMap<子区域>。"""
    start = read_jsonc(route_file(route.region, route.id))[f"AutoCollect{route.id}Start"]
    teleport = next(iter((start.get("custom_action_param") or {}).get("sub", [])), None)
    entries = scenes.get(teleport, {}).get("next", [])
    return next((entry.removeprefix("[JumpBack]") for entry in entries if "SceneEnterMap" in entry), None)


def load_stock_areas(routes: list[Route]) -> dict[str, StockArea]:
    """只给「全部采集点都落在同一片稀有采集物区或稀有矿物区里」的稀有路线做存量检查，其余路线照常执行。"""
    areas: dict[str, list[tuple[float, float, str]]] = {}
    for point in read_json(COLLECT_POINTS_PATH)["points"]:
        kind = next((prefix for prefix in STOCK_AREA_KINDS if point["detail_id"].startswith(prefix)), None)
        if kind is not None:
            areas.setdefault(point["map"], []).append((point["u"], point["v"], kind))
    scenes: dict[str, Any] = {}
    for path in sorted(SCENE_INTERFACE_DIR.glob("*.json")):
        scenes.update(read_jsonc(path))

    stocks = {}
    for route in routes:
        if any(item.kind != "rare" for item in route.items):
            continue
        points = route_collect_points(route)
        if not points:
            continue
        near = {
            area
            for point in points
            for area in areas.get(REGION_MAPS.get(route.region, ""), [])
            if math.dist(area[:2], point) <= STOCK_AREA_RADIUS
        }
        if len(near) != 1:
            continue
        u, v, kind = near.pop()
        if any(math.dist((u, v), point) > STOCK_AREA_RADIUS for point in points):
            continue
        scene = teleport_scene(route, scenes)
        if scene is not None:
            icon, list_entry = STOCK_AREA_KINDS[kind]
            stocks[route.id] = StockArea((round(u, 2), round(v, 2)), icon, list_entry, scene)
    return stocks


def validate_series_order(series_order: list[str], items: dict[str, Item]) -> None:
    """登记的系列名对不上任何采集物的 series 视为拼写错误，避免静默失效。"""
    known = {item.series for item in items.values()}
    invalid = [name for name in series_order if name not in known]
    if invalid:
        raise RegistryError(f"series_order 中的名字对不上任何采集物系列: {', '.join(invalid)}")


def validate() -> None:
    registry = read_jsonc(REGISTRY_PATH)
    items = load_items()
    validate_series_order(registry.get("series_order", []), items)
    load_routes(registry, items)


def next_route_id(region: str, item_ids: list[str]) -> str:
    items = load_items()
    stem = route_stem(region, (items[item] for item in item_ids))
    taken = [
        int(match.group(1))
        for entry in read_jsonc(REGISTRY_PATH)["routes"]
        if (match := re.fullmatch(rf"{stem}(\d+)", entry["id"]))
    ]
    return f"{stem}{max(taken, default=0) + 1}"


def route_entry(route_id: str) -> dict[str, Any]:
    entry = next((entry for entry in read_jsonc(REGISTRY_PATH)["routes"] if entry["id"] == route_id), None)
    if entry is None:
        raise RegistryError(f"路线登记表中没有 {route_id}")
    return entry


def add_route(route_id: str, region: str, item_ids: list[str]) -> None:
    registry = read_jsonc(REGISTRY_PATH)
    registry["routes"].append({"id": route_id, "region": region, "items": item_ids})
    write_json(REGISTRY_PATH, registry)


def replace_route(old_id: str, entry: dict[str, Any] | None) -> None:
    """在原位置换掉一条登记，entry 为 None 时删去。"""
    registry = read_jsonc(REGISTRY_PATH)
    index = next(index for index, item in enumerate(registry["routes"]) if item["id"] == old_id)
    registry["routes"][index : index + 1] = [] if entry is None else [entry]
    write_json(REGISTRY_PATH, registry)


def node_references() -> dict[str, list[str]]:
    """生成的文件与路线文件以外，引用了 AutoCollect 节点的文件，按节点名索引。"""
    owned = {*GENERATED_FILES, *ROUTE_DIR.glob("*/*.json")}
    found: dict[str, list[str]] = {}
    for pattern in REFERENCE_GLOBS:
        for path in sorted(PROJECT_ROOT.glob(pattern)):
            if path in owned:
                continue
            for name in set(NODE_NAME.findall(path.read_text(encoding="utf-8"))):
                found.setdefault(name, []).append(path.relative_to(PROJECT_ROOT).as_posix())
    return found


def item_rank(routes: list[Route], series_order: list[str]) -> Callable[[Item], tuple[int, int, int]]:
    """排序键：登记了顺序的系列在前；未登记的系列按路线登记表首次出现排其后；同系列按阶数。"""
    appeared = (item.series for route in routes for item in route.items)
    ranks = {name: index for index, name in enumerate(dict.fromkeys([*series_order, *appeared]))}
    return lambda item: (KINDS.index(item.kind), ranks[item.series], item.grade)


def build_groups(routes: list[Route], rank: Callable[[Item], tuple[int, int, int]]) -> list[Group]:
    groups: dict[tuple[str, str], Group] = {}
    for route in routes:
        for item in route.items:
            groups.setdefault((route.region, item.id), Group(route.region, item)).routes.append(route)
    regions = list(dict.fromkeys(route.region for route in routes))
    return sorted(groups.values(), key=lambda group: (regions.index(group.region), rank(group.item)))


def route_names(routes: list[Route]) -> dict[str, dict[str, str]]:
    siblings = Counter(route.stem for route in routes)
    names = {}
    for route in routes:
        names[route.id] = {}
        for locale in LOCALES:
            name = join_names(locale, [item.names[locale] for item in route.items])
            names[route.id][locale] = (
                ROUTE_NUMBERED[locale].format(name=name, n=route.number) if siblings[route.stem] > 1 else name
            )
    return names


def limit_input(item: Item) -> str:
    return f"AutoCollectLimit_{item.id}"


def satisfied_expression(item: Item) -> str:
    limit = limit_input(item)
    return f"{{{limit}}}>0 && {{{item.id}}}>={{{limit}}}"


def quantity_check(expression: str) -> dict[str, Any]:
    return {
        "type": "Custom",
        "param": {
            "custom_recognition": "ItemQuantitySatisfied",
            "custom_recognition_param": {"expression": expression, "notify_ui": False},
        },
    }


def route_entries(route: Route, stocks: dict[str, StockArea]) -> list[str]:
    """调度入口里一条路线占的位置：有存量检查时先检查，再执行。"""
    check = [f"[JumpBack]__AutoCollect{route.id}StockCheck"] if route.id in stocks else []
    return [*check, f"[JumpBack]__AutoCollect{route.id}Run"]


def group_nodes(group: Group, stocks: dict[str, StockArea]) -> dict[str, Any]:
    node = group.node
    name = group.item.names["zh_cn"]
    return {
        f"{node}Dispatch": {
            "desc": f"{name}调度入口：勾选后启用，依次执行名下路线",
            "enabled": False,
            "max_hit": 1,
            "recognition": "DirectHit",
            "pre_delay": 0,
            "action": "DoNothing",
            "post_delay": 0,
            "rate_limit": 0,
            "next": [*(entry for route in group.routes for entry in route_entries(route, stocks)), "AutoCollectEnd"],
        },
        f"{node}Skipped": {
            "desc": f"{name}目标库存预判：已达标时停用调度入口",
            "enabled": False,
            "max_hit": 1,
            # 默认不命中，目标库存输入覆盖为实际阈值
            "recognition": quantity_check(f"!({{{group.item.id}}}>=0)"),
            "pre_delay": 0,
            "action": {
                "type": "Custom",
                "param": {
                    "custom_action": "PipelineOverride",
                    "custom_action_param": {"patch": {f"{node}Dispatch": {"enabled": False}}},
                },
            },
            "post_delay": 0,
            "rate_limit": 0,
            "focus": {"Node.Recognition.Succeeded": f"$task.AutoCollect.item.{group.item.id}.skipped"},
        },
    }


def route_nodes(route: Route, name: str) -> dict[str, Any]:
    node = f"__AutoCollect{route.id}"
    return {
        f"{node}Run": {
            "desc": f"执行路线 {name}，同一轮只执行一次，失败计入汇总",
            "max_hit": 1,
            "recognition": "DirectHit",
            "pre_delay": 0,
            "action": {
                "type": "Custom",
                "param": {
                    "custom_action": "FailureCollectorRunTask",
                    "custom_action_param": {
                        "key": "AutoCollect",
                        "task": f"AutoCollect{route.id}Start",
                        "failure_task": f"{node}Failed",
                    },
                },
            },
            "post_delay": 0,
            "rate_limit": 0,
            # 检查未命中时同轮落到终止符，避免框架反复评估检查节点把计数当成评估次数
            "next": ["AutoCollectMidSweepStashCheck", "AutoCollectEnd"],
        },
        f"{node}Failed": {
            "desc": f"路线 {name} 失败提示",
            "pre_delay": 0,
            "post_delay": 0,
            "rate_limit": 0,
            "focus": {"Node.Recognition.Succeeded": f"$task.AutoCollect.route.{route.id}.failed"},
        },
    }


def stock_panel_count(expected: str) -> dict[str, Any]:
    """稀有采集物区 / 稀有矿物区详情面板右上角的「存量/上限」。"""
    return {"type": "OCR", "param": {"roi": STOCK_PANEL_COUNT_ROI, "expected": [expected]}}


def stock_panel_shared_nodes() -> dict[str, Any]:
    """各路线共用：确认详情面板仍开着再点面板右上角的 X 关闭，地图保持打开供随后的传送使用。"""
    return {
        "AutoCollectStockPanelCount": {
            "desc": "稀有采集物区 / 稀有矿物区详情面板：右上角的「存量/上限」",
            "recognition": stock_panel_count(r"^\d+/\d+$"),
        },
        "AutoCollectStockPanelClose": {
            "desc": "关闭稀有采集物区 / 稀有矿物区详情面板：面板仍开着时才点右上角 X，避免误关地图",
            "recognition": "And",
            "all_of": ["AutoCollectStockPanelCount", "CloseButtonType1"],
            "box_index": 1,
            "pre_delay": 0,
            "action": "Click",
            "post_wait_freezes": 300,
            "post_delay": 0,
            "rate_limit": 0,
        },
        "AutoCollectStockPanelLeave": {
            "desc": "详情面板未能确认关闭，交给随后的传送流程处理",
            "recognition": "DirectHit",
            "pre_delay": 0,
            "action": "DoNothing",
            "post_delay": 0,
            "rate_limit": 0,
        },
    }


def stock_digit_node(name: str, find_node: str, roi_offset: list[int], note: str) -> dict[str, Any]:
    """读到存量角标数字即命中，照常执行路线。"""
    return {
        "desc": f"路线 {name} 存量检查：读到存量角标，照常执行（{note}）",
        "recognition": {
            "type": "OCR",
            "param": {
                "roi": find_node,
                "roi_offset": roi_offset,
                "only_rec": True,
                "replace": [["\\D", ""]],
                "expected": ["^\\d+$"],
                # 只在读得很确定时直接放行；没读到或不够确定时交给详情面板确认。
                # 实测有角标时两个框的较高置信度通常在 0.95 以上，被滑索架压住时约 0.76，会改走详情面板
                "threshold": 0.9,
            },
        },
        "pre_delay": 0,
        "action": "DoNothing",
        "post_delay": 0,
        "rate_limit": 0,
    }


def stock_nodes(route: Route, name: str, region: str, stock: StockArea) -> dict[str, Any]:
    """传送前打开同一张子区域地图，读稀有采集物区 / 稀有矿物区的存量角标；只有详情面板确认为 0 才停用本轮的路线执行。"""
    node = f"__AutoCollect{route.id}"
    return {
        f"{node}StockCheck": {
            "desc": f"路线 {name} 存量检查：打开传送所在的子区域地图",
            "enabled": False,
            "max_hit": 1,
            "recognition": "DirectHit",
            "pre_delay": 0,
            "action": {
                "type": "Custom",
                "param": {"custom_action": "SubTask", "custom_action_param": {"sub": [stock.scene]}},
            },
            "post_delay": 0,
            "rate_limit": 0,
            "next": [f"{node}StockFind", f"{node}StockKept"],
            # 打不开地图时不判断存量，照常执行路线
            "on_error": [f"{node}StockKept"],
        },
        f"{node}StockFind": {
            "desc": f"路线 {name} 存量检查：在大地图上确认存量区域图标",
            "recognition": {
                "type": "Custom",
                "param": {
                    "custom_recognition": "MapFind",
                    "custom_recognition_param": {
                        "zone": region,
                        "icon": stock.icon,
                        "at": list(stock.at),
                        "max_attempts": 2,
                    },
                },
            },
            "pre_delay": 0,
            "action": "DoNothing",
            "post_delay": 0,
            "rate_limit": 0,
            "next": [f"{node}StockDigit", f"{node}StockDigitWide", f"{node}StockRecheck"],
        },
        # 存量为 0 时不显示角标。两个框任一读到数字就照常执行；都读不到时换一帧再看，两帧都读不到才停用
        f"{node}StockDigit": stock_digit_node(name, f"{node}StockFind", [9, -12, 10, 12], "只框角标本身"),
        f"{node}StockDigitWide": stock_digit_node(
            name, f"{node}StockFind", [9, -13, 13, 14], "向右与上下放宽，容忍遮挡与偏移；不向左放宽，避开图标本体"
        ),
        f"{node}StockRecheck": {
            "desc": f"路线 {name} 存量检查：首帧没读到角标，画面静止后再读一次",
            "recognition": "DirectHit",
            "pre_delay": 0,
            "action": "DoNothing",
            "post_wait_freezes": 300,
            "post_delay": 0,
            "rate_limit": 0,
            "next": [f"{node}StockDigit", f"{node}StockDigitWide", f"{node}StockPanelOpen"],
        },
        # 两帧都读不到角标时不直接跳过，点开区域详情面板读「存量/上限」，面板明确为 0 才停用
        f"{node}StockPanelOpen": {
            "desc": f"路线 {name} 存量检查：点开存量区域详情面板",
            "recognition": "DirectHit",
            "pre_delay": 0,
            "action": {"type": "Click", "param": {"target": f"{node}StockFind"}},
            "post_wait_freezes": 300,
            "post_delay": 0,
            "rate_limit": 0,
            "next": [f"{node}StockPanelEmpty", f"{node}StockPanelStocked", f"{node}StockPanelPick", f"{node}StockKept"],
        },
        f"{node}StockPanelPick": {
            "desc": f"路线 {name} 存量检查：图标重叠时在选择列表中点选存量区域",
            "recognition": {
                "type": "TemplateMatch",
                "param": {"roi": [860, 300, 140, 360], "template": [stock.list_entry], "threshold": 0.8},
            },
            "pre_delay": 0,
            "action": "Click",
            "post_wait_freezes": 300,
            "post_delay": 0,
            "rate_limit": 0,
            "next": [f"{node}StockPanelEmpty", f"{node}StockPanelStocked", f"{node}StockKept"],
        },
        f"{node}StockPanelEmpty": {
            "desc": f"路线 {name} 存量检查：详情面板存量为 0，停用本轮执行",
            "recognition": stock_panel_count(r"^0/\d+$"),
            "pre_delay": 0,
            "action": {
                "type": "Custom",
                "param": {
                    "custom_action": "PipelineOverride",
                    "custom_action_param": {"patch": {f"{node}Run": {"enabled": False}}},
                },
            },
            "post_delay": 0,
            "rate_limit": 0,
            "next": ["AutoCollectStockPanelClose", "AutoCollectStockPanelLeave"],
            "focus": {"Node.Recognition.Succeeded": f"$task.AutoCollect.route.{route.id}.stock_empty"},
        },
        f"{node}StockPanelStocked": {
            "desc": f"路线 {name} 存量检查：详情面板存量不为 0，照常执行",
            "recognition": stock_panel_count(r"^[1-9]\d*/\d+$"),
            "pre_delay": 0,
            "action": "DoNothing",
            "post_delay": 0,
            "rate_limit": 0,
            "next": ["AutoCollectStockPanelClose", "AutoCollectStockPanelLeave"],
        },
        f"{node}StockKept": {
            "desc": f"路线 {name} 存量检查：打不开地图、找不到图标或打不开详情面板，照常执行",
            "recognition": "DirectHit",
            "pre_delay": 0,
            "action": "DoNothing",
            "post_delay": 0,
            "rate_limit": 0,
        },
    }


def dispatch_document(
    groups: list[Group], routes: list[Route], names: dict[str, dict[str, str]], stocks: dict[str, StockArea]
) -> dict[str, Any]:
    document = {
        "AutoCollectLoop": {
            "desc": "依次调度已勾选的采集物，未勾选的不进入库存判断",
            "pre_delay": 0,
            "post_delay": 0,
            "rate_limit": 0,
            "next": [*(f"[JumpBack]{group.node}Dispatch" for group in groups), "AutoCollectAfterStashBackpack"],
        },
        "AutoCollectInventoryPreflight": {
            "desc": "库存同步后、调度循环前，逐个停用已达目标库存的采集物",
            "recognition": "DirectHit",
            "pre_delay": 0,
            "action": "DoNothing",
            "post_delay": 0,
            "rate_limit": 0,
            "next": [*(f"[JumpBack]{group.node}Skipped" for group in groups), "AutoCollectInventoryReleaseSnapshot"],
        },
        "AutoCollectMidSweepStashCheck": {
            "desc": "每完成 every 条采集路线后存放一次背包",
            "enabled": False,
            "recognition": {
                "type": "Custom",
                "param": {
                    "custom_recognition": "EveryNthHit",
                    "custom_recognition_param": {"key": "AutoCollect", "every": 6},
                },
            },
            "pre_delay": 0,
            "action": {
                "type": "Custom",
                "param": {"custom_action": "SubTask", "custom_action_param": {"sub": ["StashBackpackSubTask"]}},
            },
            "post_delay": 0,
            "rate_limit": 0,
            # 存放失败时落到终止符，避免带错终止导致后续路线与失败汇总不执行
            "on_error": ["AutoCollectEnd"],
            "focus": {"Node.Recognition.Succeeded": "$task.AutoCollect.focus.mid_sweep_stash"},
        },
    }
    for group in groups:
        document.update(group_nodes(group, stocks))
    for route in routes:
        document.update(route_nodes(route, names[route.id]["zh_cn"]))
        if route.id in stocks:
            document.update(stock_nodes(route, names[route.id]["zh_cn"], route.region, stocks[route.id]))
    if stocks:
        document.update(stock_panel_shared_nodes())
    return document


def item_case(group: Group) -> dict[str, Any]:
    return {
        "name": group.item.id,
        "label": f"$option.AutoCollectItem.{group.item.id}.label",
        "icon": f"resource/image/UI/Item/{group.item.icon}.png",
        "pipeline_override": {
            f"{group.node}Dispatch": {"enabled": True},
            f"{group.node}Skipped": {"enabled": True},
            **{f"AutoCollect{route.id}Start": {"enabled": True} for route in group.routes},
        },
    }


def region_options(groups: list[Group]) -> dict[str, Any]:
    options = {}
    for region, in_region in groupby(groups, key=lambda group: group.region):
        kinds = {}
        for kind, of_kind in groupby(in_region, key=lambda group: group.item.kind):
            cases = [item_case(group) for group in of_kind]
            kinds[f"AutoCollect{region}{kind.capitalize()}Items"] = {
                "type": "checkbox",
                "label": f"$option.AutoCollect{kind.capitalize()}Items.label",
                "cases": cases,
                "default_case": [case["name"] for case in cases],
            }
        options[f"AutoCollect{region}"] = {
            "type": "switch",
            "label": f"$global.region.{region}",
            "cases": [{"name": "Yes", "option": list(kinds)}, {"name": "No"}],
            "default_case": "Yes",
        }
        options.update(kinds)
    return options


def unique_items(groups: list[Group]) -> list[Item]:
    return list({group.item.id: group.item for group in groups}.values())


def update_task(
    task: dict[str, Any],
    groups: list[Group],
    rank: Callable[[Item], tuple[int, int, int]],
    default_limits: dict[str, int],
    stocks: dict[str, StockArea],
) -> dict[str, Any]:
    generated = region_options(groups)
    options = {name: option for name, option in task["option"].items() if name in HAND_WRITTEN_OPTIONS}

    skip_empty = next(case for case in options["AutoCollectSkipEmptyStock"]["cases"] if case["name"] == "Yes")
    skip_empty["pipeline_override"] = {f"__AutoCollect{route_id}StockCheck": {"enabled": True} for route_id in stocks}

    inventory = options["AutoCollectTargetInventory"]
    inventory["inputs"] = [
        {
            "name": limit_input(item),
            "label": f"$option.AutoCollectTargetInventory.inputs.{item.id}.label",
            "default": str(default_limits.get(item.id, KIND_DEFAULT_LIMITS[item.kind])),
            "pipeline_type": "int",
            "verify": "^\\d+$",
            "pattern_msg": "$option.AutoCollectTargetInventory.pattern_msg",
        }
        # 目标库存输入是跨地区的扁平列表，按系列/阶数全局排序，不让地区打乱三阶连续
        for item in sorted(unique_items(groups), key=rank)
    ]
    inventory["pipeline_override"] = {
        f"{group.node}Skipped": {
            "recognition": {"param": {"custom_recognition_param": {"expression": satisfied_expression(group.item)}}}
        }
        for group in groups
    }

    entry = next(item for item in task["task"] if item["name"] == "AutoCollect")
    kept = [name for name in entry["option"] if name in HAND_WRITTEN_OPTIONS]
    after_schedule = kept.index("AutoCollectSchedule") + 1
    switches = [name for name, option in generated.items() if option["type"] == "switch"]
    entry["option"] = [*kept[:after_schedule], *switches, *kept[after_schedule:]]
    task["option"] = {**options, **generated}
    return task


def owned_key(key: str) -> bool:
    if key.startswith(GENERATED_TASK_KEYS):
        return True
    return key.startswith("option.AutoCollect") and not key.startswith(HAND_WRITTEN_KEYS)


def locale_texts(
    locale: str,
    groups: list[Group],
    routes: list[Route],
    names: dict[str, dict[str, str]],
    stocks: dict[str, StockArea],
) -> dict[str, str]:
    texts = {f"option.AutoCollect{kind.capitalize()}Items.label": KIND_LABELS[kind][locale] for kind in KINDS}
    for item in unique_items(groups):
        name = item.names[locale]
        texts[f"option.AutoCollectItem.{item.id}.label"] = name
        texts[f"option.AutoCollectTargetInventory.inputs.{item.id}.label"] = name
        texts[f"task.AutoCollect.item.{item.id}.skipped"] = SKIPPED_TEXTS[locale].format(name=name)
    for route in routes:
        name = names[route.id][locale]
        texts[f"task.AutoCollect.route.{route.id}.label"] = ROUTE_LABELS[locale].format(name=name)
        texts[f"task.AutoCollect.route.{route.id}.failed"] = FAILED_STYLE.format(FAILED_TEXTS[locale].format(name=name))
        if route.id in stocks:
            texts[f"task.AutoCollect.route.{route.id}.stock_empty"] = STOCK_EMPTY_TEXTS[locale].format(name=name)
    return texts


def generate() -> list[Path]:
    registry = read_jsonc(REGISTRY_PATH)
    items = load_items()
    series_order = registry.get("series_order", [])
    validate_series_order(series_order, items)
    routes = load_routes(registry, items)
    rank = item_rank(routes, series_order)
    groups = build_groups(routes, rank)
    names = route_names(routes)
    stocks = load_stock_areas(routes)

    write_json(DISPATCH_PATH, dispatch_document(groups, routes, names, stocks))
    task = update_task(read_jsonc(TASK_PATH), groups, rank, registry.get("default_limits", {}), stocks)
    write_json(TASK_PATH, task)
    for locale in LOCALES:
        path = LOCALE_DIR / f"{locale}.json"
        messages = {key: value for key, value in read_json(path).items() if not owned_key(key)}
        texts = locale_texts(locale, groups, routes, names, stocks)
        write_json(path, dict(sorted({**messages, **texts}.items())))
    return list(GENERATED_FILES)
