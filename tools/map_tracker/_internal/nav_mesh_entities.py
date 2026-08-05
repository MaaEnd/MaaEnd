from functools import lru_cache
from pathlib import Path
import re

from .core_utils import REPO_ROOT
from .nav_mesh import (
    FLAG_VERTEX_COLLECTABLE,
    FLAG_VERTEX_DIG,
    FLAG_VERTEX_RARE,
    FLAG_VERTEX_SYSTEM,
    FLAG_VERTEX_TELEPORT,
    NavMeshData,
)
from .zmdmap_schemas import EntitiesTable

ENTITIES_FILE = REPO_ROOT / "assets" / "data" / "ZmdMap" / "maaend_entities.json"
SYSTEM_TEMPLATES = {
    "int_campfire_v2",
    "int_system_world_energy_point",
    "int_system_deliver_target",
}
COMMON_PATTERNS = (
    r"^int_doodad_insect_\d+$",
    r"^int_doodad_flower_spc_\d+$",
    r"^int_doodad_(corp|crop)_\d+$",
)
RARE_COLLECT_PATTERNS = (
    r"^int_doodad_mushroom_\d+_\d+$",
    r"^int_doodad_crylplant_\d+_\d+$",
)
RARE_DIG_PATTERNS = (r"^int_doodad_spcstone_\d+_\d+$",)


def _matches(patterns: tuple[str, ...], value: str) -> bool:
    return any(re.fullmatch(pattern, value) for pattern in patterns)


def _flags(template_name: str, key_name: str) -> int | None:
    if template_name in SYSTEM_TEMPLATES:
        flags = FLAG_VERTEX_SYSTEM
        if template_name == "int_campfire_v2":
            flags |= FLAG_VERTEX_TELEPORT
        return flags
    if template_name != "int_doodad_common":
        return None
    if _matches(COMMON_PATTERNS, key_name):
        return FLAG_VERTEX_COLLECTABLE
    if _matches(RARE_COLLECT_PATTERNS, key_name):
        return FLAG_VERTEX_RARE | FLAG_VERTEX_COLLECTABLE
    if _matches(RARE_DIG_PATTERNS, key_name):
        return FLAG_VERTEX_RARE | FLAG_VERTEX_DIG
    return None


@lru_cache(maxsize=1)
def load_entities_index() -> dict[tuple[str, str], list[dict]]:
    if not Path(ENTITIES_FILE).is_file():
        return {}
    table = EntitiesTable.load(str(ENTITIES_FILE))
    index: dict[tuple[str, str], list[dict]] = {}
    for map_id, region in table.regions.items():
        for level_id, level in region.levels.items():
            rows: list[dict] = []
            for entities in level.categories.values():
                for entity in entities:
                    flags = _flags(entity.template_name, entity.key_name)
                    if flags is None:
                        continue
                    x, y = entity.map_location or entity.pixel_location
                    rows.append(
                        {
                            "entity_id": entity.id,
                            "flags": flags,
                            "x": x,
                            "y": y,
                            "template_name": entity.template_name,
                            "key_name": entity.key_name,
                        }
                    )
            if rows:
                index[(map_id, level_id)] = rows
    return index


def import_entities(data: NavMeshData, map_id: str, level_id: str) -> int:
    seen: set[int] = set()
    for row in load_entities_index().get((map_id, level_id), []):
        entity_id = int(row["entity_id"])
        if entity_id in seen:
            continue
        seen.add(entity_id)
        data.new_vertex(
            float(row["x"]),
            float(row["y"]),
            flags=int(row["flags"]),
            entity_id=entity_id,
        )
    return len(seen)
