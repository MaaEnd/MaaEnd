#!/usr/bin/env python3
"""从基质数据生成基质刷取的目标模式和地区模式选项。

武器来自 weapons_output.json：只收录 5、6 星，6 星作为默认勾选。
图标路径按物品 ID 写成 resource/image/UI/Item/<id>.png，不使用站点 JSON 里的 icon_path。
地区来自 locations.json，中文名到节点 key 的对照读取 locations.go。
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[2]
DATA_DIR = REPO_ROOT / "assets" / "data" / "EssenceFilter"
TASK_DIR = REPO_ROOT / "assets" / "tasks" / "AutoEssence"
LOCATION_DIR = TASK_DIR / "Location"
TARGET_PATH = TASK_DIR / "Target" / "Target.json"
AUTO_ESSENCE_TASK = TASK_DIR / "AutoEssence.json"
INTERFACE_PATH = REPO_ROOT / "assets" / "interface.json"
PIPELINE_PATH = (
    REPO_ROOT / "assets" / "resource" / "pipeline" / "AutoEssence" / "CommonNodes" / "AutoEssence.json"
)
REGION_NODE_DIR = REPO_ROOT / "assets" / "resource" / "pipeline" / "AutoEssence" / "RegionNodes"
UI_ITEM_DIR = REPO_ROOT / "assets" / "resource" / "image" / "UI" / "Item"
LOCATION_KEYS_GO = REPO_ROOT / "agent" / "go-service" / "autoessence" / "locations.go"
LOCALE_DIR = REPO_ROOT / "assets" / "locales" / "interface"

# 界面展示顺序，与 AutoEssenceMenu 的目标模式选项一致。
WEAPON_GROUPS = (
    ("Sword", "Sword"),
    ("Claymores", "Claymore"),
    ("Pistol", "Pistol"),
    ("Wand", "Wand"),
    ("Lance", "Lance"),
)
MIN_RARITY = 5
DEFAULT_RARITY = 6
OCR_ROI = [43, 125, 904, 540]
# slot2 的现有识别文本把英文放在繁体前面，slot1/slot3 则按简繁英日韩。
OCR_LANG_ORDER = {
    "slot1": ("cn", "tc", "en", "jp", "kr"),
    "slot2": ("cn", "en", "tc", "jp", "kr"),
    "slot3": ("cn", "tc", "en", "jp", "kr"),
}
LOCALE_LANG = {
    "zh_cn": "CN",
    "zh_tw": "TC",
    "en_us": "EN",
    "ja_jp": "JP",
    "ko_kr": "KR",
}
SLOT1_DEFAULT = ["s1_2", "s1_3", "s1_4"]
FIXED_LOCATION_FILES = ("ChooseLocation.json", "SelectLocation.json", "Slot1.json")

# 刻写界面 OCR 和技能池基名不一致的词条。列出的语言整段替换技能池文本。
OCR_TEXT_OVERRIDE = {
    ("slot2", 6): {
        "cn": ["源石技艺提升", "源石技艺强度"],
        "tc": ["源石技藝提升", "源石技藝強度"],
        "en": ["Arts Intensity"],
        "jp": ["アーツ強度UP", "アーツ強度"],
        "kr": ["오리지늄 아츠 강도 증가", "오리지늄 아츠 강도"],
    },
    ("slot2", 11): {
        "en": ["Ultimate Gain"],
    },
}

LOCATION_KEY_RE = re.compile(r'"((?:[^"\\]|\\.)*)"\s*:\s*"([A-Za-z0-9]+)"')
ANCHOR_BLOCK_RE = re.compile(
    r'(?P<indent>[ \t]*)"GotoTriggerPointSetAnchor_[A-Za-z0-9]+",?\n'
    r'(?:[ \t]*"GotoTriggerPointSetAnchor_[A-Za-z0-9]+",?\n)*'
)
IMPORT_BLOCK_RE = re.compile(
    r'(?P<indent>[ \t]*)"tasks/AutoEssence/AutoEssence\.json",\n'
    r'(?:[ \t]*"tasks/AutoEssence/Location/[A-Za-z0-9]+\.json",\n)*'
    r'[ \t]*"tasks/AutoEssence/Target/Target\.json",\n'
)


def parse_location_keys(source: str) -> dict[str, str]:
    start = source.find("locationNameToKey")
    if start < 0:
        raise ValueError("locations.go 里没有 locationNameToKey")
    body = source[start : source.find("}", start)]
    keys = {name: key for name, key in LOCATION_KEY_RE.findall(body)}
    if not keys:
        raise ValueError("locationNameToKey 为空")
    return keys


def ocr_expected(slot: str, entry: dict[str, Any]) -> list[str]:
    override = OCR_TEXT_OVERRIDE.get((slot, int(entry["id"])), {})
    values: list[str] = []
    for lang in OCR_LANG_ORDER[slot]:
        if lang in override:
            values.extend(override[lang])
        else:
            values.append(str(entry[lang]))
    expected: list[str] = []
    for value in values:
        if value and value not in expected:
            expected.append(value)
    return expected


def weapon_icon(weapon_id: str) -> str:
    if not (UI_ITEM_DIR / f"{weapon_id}.png").is_file():
        raise FileNotFoundError(weapon_id)
    return f"resource/image/UI/Item/{weapon_id}.png"


def select_weapons(weapons: dict[str, Any]) -> dict[str, list[dict[str, Any]]]:
    grouped: dict[str, list[dict[str, Any]]] = {weapon_type: [] for weapon_type, _ in WEAPON_GROUPS}
    unknown: list[str] = []
    for weapon_id, weapon in weapons.items():
        if not isinstance(weapon, dict):
            continue
        rarity = weapon.get("rarity")
        if not isinstance(rarity, int) or rarity < MIN_RARITY:
            continue
        weapon_type = weapon.get("weapon_type")
        if weapon_type not in grouped:
            unknown.append(f"{weapon_id} ({weapon_type})")
            continue
        grouped[weapon_type].append({**weapon, "id": weapon_id})
    if unknown:
        raise ValueError("未登记的武器类型: " + ", ".join(unknown))
    for items in grouped.values():
        items.sort(key=lambda item: (-int(item["rarity"]), str(item["id"])))
    return grouped


def region_label(location_key: str) -> str:
    if not location_key.startswith(("VF", "WL")):
        raise ValueError(f"地区 key 必须以 VF 或 WL 开头: {location_key}")
    return f"$global.region.{location_key[2:]}"


def _ocr_override(expected: list[str]) -> dict[str, Any]:
    return {
        "AutoEssenceEngraveCondition2OCR": {
            "recognition": {"param": {"expected": expected}},
        },
        "AutoEssenceSelectEngraveBonusCondition": {
            "recognition": {
                "type": "OCR",
                "param": {"roi": OCR_ROI, "expected": expected},
            },
        },
    }


def build_slot1(slot1: list[dict[str, Any]]) -> dict[str, Any]:
    cases = []
    for entry in slot1:
        name = f"s1_{int(entry['id'])}"
        expected = ocr_expected("slot1", entry)
        cases.append(
            {
                "name": name,
                "label": f"$option.AutoEssenceSkill.{name}",
                "pipeline_override": {
                    f"AutoEssenceSelectEngraveBase_{name}": {
                        "recognition": {
                            "type": "OCR",
                            "param": {"roi": OCR_ROI, "expected": expected},
                        },
                        "action": {"type": "Click"},
                    },
                    f"AutoEssenceEngraveCondition1Base_{name}": {
                        "recognition": {"param": {"expected": expected}},
                    },
                },
            }
        )
    names = {case["name"] for case in cases}
    missing = [name for name in SLOT1_DEFAULT if name not in names]
    if missing:
        raise ValueError("基础属性缺少默认项: " + ", ".join(missing))
    return {
        "option": {
            "AutoEssenceLocationSlot1": {
                "type": "checkbox",
                "label": "$option.AutoEssenceLocationSlot1.label",
                "description": "$option.AutoEssenceLocationSlot1.description",
                "cases": cases,
                "default_case": list(SLOT1_DEFAULT),
                "min_count": 3,
                "max_count": 3,
            }
        }
    }


def build_secondary(location_key: str, location: dict[str, Any]) -> dict[str, Any]:
    cases = []
    for slot, entries in (("slot2", location["slot2"]), ("slot3", location["slot3"])):
        prefix = "s2" if slot == "slot2" else "s3"
        for entry in entries:
            name = f"{prefix}_{int(entry['id'])}"
            cases.append(
                {
                    "name": name,
                    "label": f"$option.AutoEssenceSkill.{name}",
                    "pipeline_override": _ocr_override(ocr_expected(slot, entry)),
                }
            )
    if not cases:
        raise ValueError(f"{location_key} 没有附加或技能属性")
    return {
        "option": {
            f"AutoEssenceLocationSecondary_{location_key}": {
                "type": "select",
                "label": "$option.AutoEssenceLocationSecondary.label",
                "description": "$option.AutoEssenceLocationSecondary.description",
                "cases": cases,
                "default_case": cases[0]["name"],
            }
        }
    }


def _location_case(location_key: str, *, with_options: bool) -> dict[str, Any]:
    case: dict[str, Any] = {
        "name": location_key,
        "label": region_label(location_key),
    }
    if with_options:
        case["option"] = [
            "AutoEssenceLocationSlot1",
            f"AutoEssenceLocationSecondary_{location_key}",
        ]
    case["pipeline_override"] = {
        f"GotoTriggerPointSetAnchor_{location_key}": {"enabled": True},
    }
    return case


def build_select_location(keys: list[str]) -> dict[str, Any]:
    return {
        "option": {
            "AutoEssenceSelectLocation": {
                "type": "select",
                "label": "$option.AutoEssenceSelectLocation.label",
                "description": "$option.AutoEssenceSelectLocation.description",
                "cases": [_location_case(key, with_options=True) for key in keys],
                "default_case": keys[0],
            }
        }
    }


def build_choose_location(keys: list[str]) -> dict[str, Any]:
    return {
        "option": {
            "AutoEssenceChooseLocation": {
                "type": "checkbox",
                "label": "$option.AutoEssenceChooseLocation.label",
                "description": "$option.AutoEssenceChooseLocation.description",
                "cases": [_location_case(key, with_options=False) for key in keys],
                "default_case": list(keys),
            }
        }
    }


def build_target(grouped: dict[str, list[dict[str, Any]]]) -> dict[str, Any]:
    options: dict[str, Any] = {}
    missing_icons: list[str] = []
    for weapon_type, suffix in WEAPON_GROUPS:
        weapons = grouped[weapon_type]
        if not weapons:
            raise ValueError(f"{weapon_type} 没有 {MIN_RARITY} 星及以上武器")
        cases = []
        for weapon in weapons:
            weapon_id = str(weapon["id"])
            try:
                icon = weapon_icon(weapon_id)
            except FileNotFoundError:
                missing_icons.append(weapon_id)
                continue
            cases.append(
                {
                    "name": weapon_id,
                    "label": f"$weapon.{weapon_id}",
                    "icon": icon,
                    "pipeline_override": {
                        "AutoEssenceTargetPlan": {"attach": {weapon_id: True}},
                    },
                }
            )
        if missing_icons:
            continue
        type_name = f"AutoEssenceWeaponType{suffix}"
        list_name = f"AutoEssenceWeapons{suffix}"
        options[type_name] = {
            "type": "switch",
            "label": f"$option.{type_name}.label",
            "cases": [
                {"name": "Yes", "option": [list_name]},
                {"name": "No"},
            ],
            "default_case": "Yes",
        }
        options[list_name] = {
            "type": "checkbox",
            "label": "$option.AutoEssenceSelectWeapons.label",
            "cases": cases,
            "default_case": [
                str(weapon["id"]) for weapon in weapons if int(weapon["rarity"]) == DEFAULT_RARITY
            ],
        }
    if missing_icons:
        raise FileNotFoundError(
            "缺少武器 UI 图标，请先发布到 assets/resource/image/UI/Item/: "
            + ", ".join(missing_icons)
        )
    return {"option": options}


def resolve_locations(
    locations: list[Any],
    location_keys: dict[str, str],
    locale_keys: set[str],
) -> list[tuple[str, dict[str, Any]]]:
    resolved: list[tuple[str, dict[str, Any]]] = []
    seen: set[str] = set()
    for location in locations:
        name = location.get("name")
        key = location_keys.get(name)
        if not key:
            raise ValueError(
                f"locations.json 的 {name!r} 没有地区 key。"
                "请先在 locations.go 的 locationNameToKey 和 RegionNodes 中补上该地区"
            )
        if key in seen:
            raise ValueError(f"地区 key 重复: {key}")
        seen.add(key)
        label_key = region_label(key).removeprefix("$")
        if label_key not in locale_keys:
            raise ValueError(f"缺少地区文案 {label_key}")
        node = REGION_NODE_DIR / f"{key}.json"
        if not node.is_file():
            raise ValueError(f"缺少地区节点 {node.relative_to(REPO_ROOT)}")
        resolved.append((key, location))
    if not resolved:
        raise ValueError("locations.json 没有可用地区")
    return resolved


def skill_labels(locations: list[dict[str, Any]], slot1: list[dict[str, Any]]) -> set[str]:
    labels = {f"option.AutoEssenceSkill.s1_{int(entry['id'])}" for entry in slot1}
    for location in locations:
        for entry in location["slot2"]:
            labels.add(f"option.AutoEssenceSkill.s2_{int(entry['id'])}")
        for entry in location["slot3"]:
            labels.add(f"option.AutoEssenceSkill.s3_{int(entry['id'])}")
    return labels


def dumps(data: Any) -> str:
    return json.dumps(data, ensure_ascii=False, indent=4) + "\n"


def write_if_changed(path: Path, text: str) -> bool:
    newline = "\n"
    if path.is_file():
        current, newline = _normalize(path.read_text(encoding="utf-8"))
        if current == text:
            return False
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text.replace("\n", newline), encoding="utf-8", newline="")
    return True


def _normalize(text: str) -> tuple[str, str]:
    newline = "\r\n" if "\r\n" in text else "\n"
    return text.replace("\r\n", "\n"), newline


def strip_choose_location(text: str) -> str:
    marker = '"AutoEssenceChooseLocation":'
    start = text.find(marker)
    if start < 0:
        return text
    line_start = text.rfind("\n", 0, start) + 1
    brace = text.find("{", start)
    depth = 0
    end = -1
    for index in range(brace, len(text)):
        if text[index] == "{":
            depth += 1
        elif text[index] == "}":
            depth -= 1
            if depth == 0:
                end = index + 1
                break
    if end < 0:
        raise ValueError("AutoEssenceChooseLocation 对象没有闭合")
    rest = text[end:]
    if rest.startswith(","):
        rest = rest[1:]
    if rest.startswith("\n"):
        rest = rest[1:]
    return text[:line_start] + rest


def sync_anchor_list(text: str, keys: list[str]) -> str:
    matches = list(ANCHOR_BLOCK_RE.finditer(text))
    if len(matches) != 1:
        raise ValueError(f"淤积点 sub 列表应只有 1 处，实际 {len(matches)}")
    indent = matches[0].group("indent")
    lines = [f'{indent}"GotoTriggerPointSetAnchor_{key}"' for key in keys]
    block = ",\n".join(lines) + "\n"
    match = matches[0]
    return text[: match.start()] + block + text[match.end() :]


def sync_interface_imports(text: str, keys: list[str]) -> str:
    match = IMPORT_BLOCK_RE.search(text)
    if match is None:
        raise ValueError("interface.json 里没有 AutoEssence 导入块")
    indent = match.group("indent")
    files = [
        "tasks/AutoEssence/AutoEssence.json",
        "tasks/AutoEssence/Location/ChooseLocation.json",
        "tasks/AutoEssence/Location/SelectLocation.json",
        "tasks/AutoEssence/Location/Slot1.json",
        *[f"tasks/AutoEssence/Location/{key}.json" for key in keys],
        "tasks/AutoEssence/Target/Target.json",
    ]
    block = "".join(f'{indent}"{name}",\n' for name in files)
    return text[: match.start()] + block + text[match.end() :]


def _replace_file(path: Path, updated: str, newline: str) -> bool:
    current, _ = _normalize(path.read_text(encoding="utf-8"))
    if current == updated:
        return False
    path.write_text(updated.replace("\n", newline), encoding="utf-8", newline="")
    return True


def sync_weapon_locales(weapons: dict[str, Any]) -> list[str]:
    changed: list[str] = []
    ordered = sorted(weapons, key=str)
    for locale_name, lang in LOCALE_LANG.items():
        path = LOCALE_DIR / f"{locale_name}.json"
        raw = path.read_text(encoding="utf-8")
        text, newline = _normalize(raw)
        data = json.loads(text)
        keys = list(data)
        weapon_indexes = [index for index, key in enumerate(keys) if key.startswith("weapon.wpn_")]
        if weapon_indexes and any(
            not key.startswith("weapon.wpn_") for key in keys[weapon_indexes[0] :]
        ):
            raise ValueError(f"{locale_name}.json 的 weapon.* 不是文件末尾的连续字段")
        desired: dict[str, str] = {}
        for weapon_id in ordered:
            weapon = weapons[weapon_id]
            name = weapon.get("names", {}).get(lang)
            if not isinstance(name, str) or not name.strip():
                raise ValueError(f"{weapon_id} 缺少 {lang} 名称")
            desired[f"weapon.{weapon_id}"] = f"★{weapon['rarity']} {name}"
        current = {key: data[key] for key in keys if key.startswith("weapon.wpn_")}
        if current == desired:
            continue
        kept = [line for line in text.splitlines() if not re.match(r'\s*"weapon\.wpn_', line)]
        if kept and kept[-1].strip() == "}":
            body = kept[:-1]
        else:
            raise ValueError(f"{locale_name}.json 无法定位结尾")
        if body and not body[-1].rstrip().endswith(","):
            body[-1] = body[-1].rstrip() + ","
        weapon_lines = [
            f'    "{key}": {json.dumps(value, ensure_ascii=False)}{"," if index < len(desired) - 1 else ""}'
            for index, (key, value) in enumerate(desired.items())
        ]
        updated = "\n".join([*body, *weapon_lines, "}"]) + "\n"
        path.write_text(updated.replace("\n", newline), encoding="utf-8", newline="")
        changed.append(locale_name)
    return changed


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--weapons", type=Path, default=DATA_DIR / "weapons_output.json")
    parser.add_argument("--locations", type=Path, default=DATA_DIR / "locations.json")
    parser.add_argument("--skill-pools", type=Path, default=DATA_DIR / "skill_pools.json")
    parser.add_argument("--location-keys", type=Path, default=LOCATION_KEYS_GO)
    args = parser.parse_args()

    weapons = json.loads(args.weapons.read_text(encoding="utf-8"))
    locations = json.loads(args.locations.read_text(encoding="utf-8"))
    pools = json.loads(args.skill_pools.read_text(encoding="utf-8"))
    location_keys = parse_location_keys(args.location_keys.read_text(encoding="utf-8"))
    zh_cn = json.loads((LOCALE_DIR / "zh_cn.json").read_text(encoding="utf-8"))

    resolved = resolve_locations(locations, location_keys, set(zh_cn))
    grouped = select_weapons(weapons)
    labels = skill_labels([location for _, location in resolved], pools["slot1"])
    for locale_name in LOCALE_LANG:
        locale = json.loads((LOCALE_DIR / f"{locale_name}.json").read_text(encoding="utf-8"))
        missing = sorted(label for label in labels if label not in locale)
        if missing:
            raise ValueError(f"{locale_name}.json 缺少词条文案: " + ", ".join(missing))

    keys = [key for key, _ in resolved]
    written: list[Path] = []
    outputs = {
        LOCATION_DIR / "Slot1.json": build_slot1(pools["slot1"]),
        LOCATION_DIR / "SelectLocation.json": build_select_location(keys),
        LOCATION_DIR / "ChooseLocation.json": build_choose_location(keys),
        TARGET_PATH: build_target(grouped),
    }
    for key, location in resolved:
        outputs[LOCATION_DIR / f"{key}.json"] = build_secondary(key, location)
    for path, payload in outputs.items():
        if write_if_changed(path, dumps(payload)):
            written.append(path)

    owned = {LOCATION_DIR / name for name in FIXED_LOCATION_FILES}
    owned.update(LOCATION_DIR / f"{key}.json" for key in keys)
    for path in LOCATION_DIR.glob("*.json"):
        if path in owned:
            continue
        path.unlink()
        written.append(path)

    task_text, task_newline = _normalize(AUTO_ESSENCE_TASK.read_text(encoding="utf-8"))
    if _replace_file(AUTO_ESSENCE_TASK, strip_choose_location(task_text), task_newline):
        written.append(AUTO_ESSENCE_TASK)

    interface_text, interface_newline = _normalize(INTERFACE_PATH.read_text(encoding="utf-8"))
    if _replace_file(INTERFACE_PATH, sync_interface_imports(interface_text, keys), interface_newline):
        written.append(INTERFACE_PATH)

    pipeline_text, pipeline_newline = _normalize(PIPELINE_PATH.read_text(encoding="utf-8"))
    if _replace_file(PIPELINE_PATH, sync_anchor_list(pipeline_text, keys), pipeline_newline):
        written.append(PIPELINE_PATH)

    for locale_name in sync_weapon_locales(weapons):
        written.append(LOCALE_DIR / f"{locale_name}.json")

    if written:
        for path in written:
            print(f"updated {path.relative_to(REPO_ROOT)}")
    else:
        print("AutoEssence task options are up to date")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, json.JSONDecodeError, KeyError, TypeError) as error:
        print(f"build_autoessence_tasks: {error}", file=sys.stderr)
        sys.exit(1)
