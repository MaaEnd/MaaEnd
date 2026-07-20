import json
import os
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Self

# ── Version ───────────────────────────────────────────────────────────────────


@dataclass
class ResourceData:
    """
    Properties:
    - name: resource identifier, like `main` or `initial`.
    - version: resource version string, like `7025848-16`.
    - path: download URL for the resource.
    """

    name: str
    version: str
    path: str

    @classmethod
    def from_raw(cls, raw: dict) -> Self:
        return cls(name=raw["name"], version=raw["version"], path=raw["path"])


@dataclass
class ResourceBundleData:
    """
    Properties:
    - res_version: composite version, like `initial_7025848-16_main_7025848-16`.
    - resources: list of resource entries.
    """

    res_version: str
    resources: list[ResourceData] = field(default_factory=list)

    @classmethod
    def from_raw(cls, raw: dict) -> Self:
        return cls(
            res_version=raw["res_version"],
            resources=[ResourceData.from_raw(r) for r in raw["resources"]],
        )


@dataclass
class GameVersionData:
    """
    Properties:
    - version: client version string, like `1.2.5`.
    - game_version: client short version string, like `1.2`.
    - file_path: index URL for hot-update files.
    - rand_str: random string used in URL construction.
    - resource_bundles: list of resource bundle entries.
    """

    version: str
    game_version: str
    file_path: str
    rand_str: str
    resource_bundles: list[ResourceBundleData] = field(default_factory=list)

    @classmethod
    def from_raw(cls, raw: dict) -> Self:
        return cls(
            version=raw["version"],
            game_version=raw["game_version"],
            file_path=raw["file_path"],
            rand_str=raw["rand_str"],
            resource_bundles=[
                ResourceBundleData.from_raw(b) for b in raw["resource_bundles"]
            ],
        )


# ── Layout ────────────────────────────────────────────────────────────────────


@dataclass
class LevelLayoutMetaData:
    """
    Properties:
    - x, y: top-left unscaled coordinate of the level map within the region map.
    - width, height: unscaled dimensions of the level map.
    - tile_w, tile_h: unscaled dimensions of each tile of the level map.
    - tile_max_x, tile_max_y: number of tiles along x and y axes.
    """

    x: int
    y: int
    width: int
    height: int
    tile_w: int
    tile_h: int

    @property
    def tile_max_x(self) -> int:
        assert self.width % self.tile_w == 0
        return self.width // self.tile_w

    @property
    def tile_max_y(self) -> int:
        assert self.height % self.tile_h == 0
        return self.height // self.tile_h


@dataclass
class RegionLayoutTable:
    """
    Properties:
    - base_map: region name of the region map, like `map01`.
    - canvas_width, canvas_height: unscaled dimensions of the region map.
    - tile_w, tile_h: unscaled dimensions of each tile of the region map.
    - tile_max_x, tile_max_y: number of tiles along x and y axes.
    - levels: mapping from *full* level name (like `map01_lv001`) to its data object.
    """

    base_map: str
    canvas_width: int
    canvas_height: int
    tile_w: int
    tile_h: int
    levels: dict[str, LevelLayoutMetaData] = field(default_factory=dict)

    @property
    def tile_max_x(self) -> int:
        assert self.canvas_width % self.tile_w == 0
        return self.canvas_width // self.tile_w

    @property
    def tile_max_y(self) -> int:
        assert self.canvas_height % self.tile_h == 0
        return self.canvas_height // self.tile_h

    # ── deserialization ───────────────────────────────────────────────────

    @classmethod
    def loads(cls, text: str) -> Self:
        raw = json.loads(text)
        tile_w, tile_h = raw["tile_w"], raw["tile_h"]
        levels = {
            k: LevelLayoutMetaData(tile_w=tile_w, tile_h=tile_h, **v)
            for k, v in raw["levels"].items()
        }
        return cls(
            base_map=raw["base_map"],
            canvas_width=raw["canvas_width"],
            canvas_height=raw["canvas_height"],
            tile_w=raw["tile_w"],
            tile_h=raw["tile_h"],
            levels=levels,
        )

    @classmethod
    def load(cls, path: str | Path) -> Self:
        return cls.loads(Path(path).read_text(encoding="utf-8"))

    RE_LAYOUT_FILE = re.compile(r"^([a-z]+\d*)_layout\.json$")
    RE_INCLUDED_REGION_NAME = re.compile(r"^(map\d+)|(base\d+)|(dung\d+)|(indie)$")

    @classmethod
    def load_from_dir(
        cls,
        layout_dir: str | Path,
        include_pattern: re.Pattern | None = None,
    ) -> dict[str, "RegionLayoutTable"]:
        """Load all `*_layout.json` files from a directory."""
        layouts: dict[str, RegionLayoutTable] = {}
        layout_path = Path(layout_dir)
        for fname in sorted(os.listdir(layout_dir)):
            m = cls.RE_LAYOUT_FILE.match(fname)
            if not m:
                continue
            region_name = m.group(1)
            if include_pattern and not include_pattern.match(region_name):
                continue
            try:
                layouts[region_name] = RegionLayoutTable.load(str(layout_path / fname))
            except Exception:
                continue
        return layouts


# ── Grid Tiers ────────────────────────────────────────────────────────────────


@dataclass
class GridTierData:
    """
    Properties:
    - center: (x, y) center *grid-wise coordinates* of the grid cell.
    - lb: (x, y) left-bottom *grid-wise coordinates* of the grid cell.
    - rt: (x, y) right-top *grid-wise coordinates* of the grid cell.
    - pixel_lb: (x, y) left-bottom *unscaled coordinates* of the grid cell.
    - pixel_rt: (x, y) right-top *unscaled coordinates* of the grid cell.
    - items: mapping from item hash to tier grid name like `map01_lv001_{gx}_{gy}_tier_{id}`, where gy counts from bottom to top.
    """

    UNSCALED_TO_GRID_SCALE = 9.375
    """Scale factor to convert *unscaled coordinates* to *grid-wise coordinates*."""

    center: tuple[float, float]
    lb: tuple[float, float]
    rt: tuple[float, float]
    items: dict[str, str] = field(default_factory=dict)

    @property
    def pixel_lb(self) -> tuple[float, float]:
        return (
            self.lb[0] * self.UNSCALED_TO_GRID_SCALE,
            self.lb[1] * self.UNSCALED_TO_GRID_SCALE,
        )

    @property
    def pixel_rt(self) -> tuple[float, float]:
        return (
            self.rt[0] * self.UNSCALED_TO_GRID_SCALE,
            self.rt[1] * self.UNSCALED_TO_GRID_SCALE,
        )

    @classmethod
    def from_raw(cls, raw: dict) -> Self:
        return cls(
            center=tuple(raw["center"]),
            lb=tuple(raw["lb"]),
            rt=tuple(raw["rt"]),
            items=raw.get("items", {}),
        )


@dataclass
class GridTiersTable:
    """
    Properties:
    - tiers: mapping from position key (like `map01_lv001_{gx}_{gy}`) to its tier data, where gy counts from bottom to top.
    - region_names: list of unique region names extracted from tier keys (like `map01`).
    """

    tiers: dict[str, GridTierData] = field(default_factory=dict)

    @property
    def region_names(self) -> list[str]:
        return list(set(k.split("_")[0] for k in self.tiers))

    # ── deserialization ───────────────────────────────────────────────────

    @classmethod
    def loads(cls, text: str) -> Self:
        raw = json.loads(text)
        tiers = {k: GridTierData.from_raw(v) for k, v in raw.items()}
        return cls(tiers=tiers)

    @classmethod
    def load(cls, path: str | Path) -> Self:
        return cls.loads(Path(path).read_text(encoding="utf-8"))


# ── Entities ──────────────────────────────────────────────────────────────────


@dataclass
class EntityData:
    """
    Properties:
    - id: unique identifier of the entity.
    - template_name: template name of the entity.
    - key_name: key name of the entity.
    - raw_location: (x, y, z) *internal coordinates* of the entity on the *region* map (rarely use).
    - pixel_location: (x, y) *unscaled coordinates* of the entity on the *region* map.
    - map_location: (x, y) *converted coordinates* of the entity on the *level* map.
    """

    id: int
    template_name: str
    key_name: str
    raw_location: tuple[float, float, float]
    pixel_location: tuple[float, float]
    map_location: tuple[float, float]

    @classmethod
    def from_raw(cls, raw: dict) -> Self:
        return cls(
            id=raw["id"],
            template_name=raw["template_name"],
            key_name=raw["key_name"],
            raw_location=tuple(raw["raw_location"]),
            pixel_location=tuple(raw["pixel_location"]),
            map_location=tuple(raw["map_location"]),
        )


@dataclass
class CommonGatherEntity(EntityData):
    """common_gather — e.g. insects, plants, ores."""


@dataclass
class DisposableEntity(EntityData):
    """disposable — one-time collectibles."""


@dataclass
class DnarrativeEntity(EntityData):
    """dnarrative — delayed narrative triggers."""


@dataclass
class EnemyEntity(EntityData):
    """enemy — hostile NPCs."""


@dataclass
class NarrativeEntity(EntityData):
    """narrative — story / interaction triggers."""


@dataclass
class RareGatherEntity(EntityData):
    """rare_gather — rare resource nodes."""


@dataclass
class SpecialEntity(EntityData):
    """special — campfires (teleport anchor), etc."""


@dataclass
class SystemEntity(EntityData):
    """system — dungeon entries, system objects."""


@dataclass
class TrchestEntity(EntityData):
    """trchest — treasure chests."""


_ENTITY_CLASS_REGISTRY: dict[str, type[EntityData]] = {
    "common_gather": CommonGatherEntity,
    "disposable": DisposableEntity,
    "dnarrative": DnarrativeEntity,
    "enemy": EnemyEntity,
    "narrative": NarrativeEntity,
    "rare_gather": RareGatherEntity,
    "special": SpecialEntity,
    "system": SystemEntity,
    "trchest": TrchestEntity,
}


def _entity_from_raw(category: str, raw: dict) -> EntityData:
    cls = _ENTITY_CLASS_REGISTRY.get(category, EntityData)
    return cls.from_raw(raw)


@dataclass
class LevelEntitiesTable:
    """
    Properties:
    - categories: mapping from category name (like `enemy`) to a list of entity objects.
    """

    categories: dict[str, list[EntityData]] = field(default_factory=dict)

    @classmethod
    def from_raw(cls, raw: dict) -> Self:
        categories: dict[str, list[EntityData]] = {}
        for cat in raw["categories"]:
            categories[cat["category"]] = [
                _entity_from_raw(cat["category"], e) for e in cat["data"]
            ]
        return cls(categories=categories)

    # ── entity search ─────────────────────────────────────────────────────

    def find_entity_by_id(self, entity_id: int) -> EntityData | None:
        """Find an entity by its unique ID across all categories."""
        for entities in self.categories.values():
            for entity in entities:
                if entity.id == entity_id:
                    return entity
        return None


@dataclass
class RegionEntitiesTable:
    """
    Properties:
    - levels: mapping from level name (like `lv001`) to its level entities table.
    """

    levels: dict[str, LevelEntitiesTable] = field(default_factory=dict)

    @classmethod
    def from_raw(cls, raw: dict) -> Self:
        levels = {
            lv["map_level_id"]: LevelEntitiesTable.from_raw(lv) for lv in raw["levels"]
        }
        return cls(levels=levels)

    # ── entity search ─────────────────────────────────────────────────────

    def find_entity_by_id(self, entity_id: int) -> EntityData | None:
        """Find an entity by its unique ID across all levels and categories."""
        for level in self.levels.values():
            entity = level.find_entity_by_id(entity_id)
            if entity is not None:
                return entity
        return None


@dataclass
class EntitiesTable:
    """
    Properties:
    - regions: mapping from region name (like `map01`) to its region entities table.
    """

    regions: dict[str, RegionEntitiesTable] = field(default_factory=dict)

    # ── deserialization ───────────────────────────────────────────────────

    @classmethod
    def loads(cls, text: str) -> Self:
        raw_list = json.loads(text)
        regions = {r["map_id"]: RegionEntitiesTable.from_raw(r) for r in raw_list}
        return cls(regions=regions)

    @classmethod
    def load(cls, path: str | Path) -> Self:
        return cls.loads(Path(path).read_text(encoding="utf-8"))

    # ── entity search ─────────────────────────────────────────────────────

    def find_entity_by_id(self, entity_id: int) -> EntityData | None:
        """Find an entity by its unique ID across all regions, levels, and categories."""
        for region in self.regions.values():
            entity = region.find_entity_by_id(entity_id)
            if entity is not None:
                return entity
        return None
