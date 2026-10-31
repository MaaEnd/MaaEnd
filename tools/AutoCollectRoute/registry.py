"""AutoCollect 路线登记表 -> 调度节点、任务选项与五语文案。"""

import json
import re
from collections import Counter
from collections.abc import Iterable
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

LOCALES = ("zh_cn", "zh_tw", "en_us", "ja_jp", "ko_kr")
GENERATED_FILES = (DISPATCH_PATH, TASK_PATH, *(LOCALE_DIR / f"{locale}.json" for locale in LOCALES))
KINDS = ("rare", "common")
KIND_DEFAULT_LIMITS = {"rare": 32, "common": 0}
RARE_SHOWING_TYPE = "Nurturance"

HAND_WRITTEN_OPTIONS = (
    "AutoCollectSchedule",
    "AutoCollectStashBackpackSubTask",
    "AutoCollectMode",
    "AutoCollectTargetInventory",
)
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


class RegistryError(ValueError):
    """登记表与采集物数据或路线文件对不上。"""


@dataclass(frozen=True)
class Item:
    id: str
    key: str
    icon: str
    kind: str
    names: dict[str, str]


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
            info["item"], to_pascal(info["item_names"]["en_us"]), info["icon"], route_kind(info), info["item_names"]
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


def validate() -> None:
    load_routes(read_jsonc(REGISTRY_PATH), load_items())


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


def build_groups(routes: list[Route]) -> list[Group]:
    groups: dict[tuple[str, str], Group] = {}
    for route in routes:
        for item in route.items:
            groups.setdefault((route.region, item.id), Group(route.region, item)).routes.append(route)
    regions = list(dict.fromkeys(route.region for route in routes))
    return sorted(groups.values(), key=lambda group: (regions.index(group.region), KINDS.index(group.item.kind)))


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


def group_nodes(group: Group) -> dict[str, Any]:
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
            "next": [*(f"[JumpBack]__AutoCollect{route.id}Run" for route in group.routes), "AutoCollectEnd"],
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
        },
        f"{node}Failed": {
            "desc": f"路线 {name} 失败提示",
            "pre_delay": 0,
            "post_delay": 0,
            "rate_limit": 0,
            "focus": {"Node.Recognition.Succeeded": f"$task.AutoCollect.route.{route.id}.failed"},
        },
    }


def dispatch_document(groups: list[Group], routes: list[Route], names: dict[str, dict[str, str]]) -> dict[str, Any]:
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
    }
    for group in groups:
        document.update(group_nodes(group))
    for route in routes:
        document.update(route_nodes(route, names[route.id]["zh_cn"]))
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


def update_task(task: dict[str, Any], groups: list[Group], default_limits: dict[str, int]) -> dict[str, Any]:
    generated = region_options(groups)
    options = {name: option for name, option in task["option"].items() if name in HAND_WRITTEN_OPTIONS}

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
        for item in unique_items(groups)
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


def locale_texts(locale: str, groups: list[Group], routes: list[Route], names: dict[str, dict[str, str]]) -> dict[str, str]:
    texts = {f"option.AutoCollect{kind.capitalize()}Items.label": KIND_LABELS[kind][locale] for kind in KINDS}
    for item in unique_items(groups):
        name = item.names[locale]
        texts[f"option.AutoCollectItem.{item.id}.label"] = name
        texts[f"option.AutoCollectTargetInventory.inputs.{item.id}.label"] = (
            f"![](resource/image/UI/Item/{item.icon}.png) {name}"
        )
        texts[f"task.AutoCollect.item.{item.id}.skipped"] = SKIPPED_TEXTS[locale].format(name=name)
    for route in routes:
        name = names[route.id][locale]
        texts[f"task.AutoCollect.route.{route.id}.label"] = ROUTE_LABELS[locale].format(name=name)
        texts[f"task.AutoCollect.route.{route.id}.failed"] = FAILED_STYLE.format(FAILED_TEXTS[locale].format(name=name))
    return texts


def generate() -> list[Path]:
    registry = read_jsonc(REGISTRY_PATH)
    routes = load_routes(registry, load_items())
    groups = build_groups(routes)
    names = route_names(routes)

    write_json(DISPATCH_PATH, dispatch_document(groups, routes, names))
    write_json(TASK_PATH, update_task(read_jsonc(TASK_PATH), groups, registry.get("default_limits", {})))
    for locale in LOCALES:
        path = LOCALE_DIR / f"{locale}.json"
        messages = {key: value for key, value in read_json(path).items() if not owned_key(key)}
        write_json(path, dict(sorted({**messages, **locale_texts(locale, groups, routes, names)}.items())))
    return list(GENERATED_FILES)
