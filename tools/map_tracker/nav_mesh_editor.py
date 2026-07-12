# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "opencv-python>=4",
# ]
# ///

# NavMesh - Editor Tool
# GUI for creating and editing MapTracker NavMesh (.mtnm) files.

import math
import os
import queue
import re
import time

import numpy as np
from functools import lru_cache
from dataclasses import dataclass

from _internal.nav_mesh import (
    FLAG_EDGE_BIDIRECTIONAL,
    FLAG_VERTEX_COLLECTABLE,
    FLAG_VERTEX_DIG,
    FLAG_VERTEX_HIDDEN,
    FLAG_VERTEX_RARE,
    FLAG_VERTEX_SYSTEM,
    FLAG_VERTEX_TELEPORT,
    NavEdge,
    NavMeshData,
    NavMeshDataSnapshot,
    NavMeshFile,
    NavVertex,
)
from _internal.core_utils import (
    _0,
    _C,
    _G,
    _R,
    _Y,
    Color,
    Drawer,
    MapName,
    cv2,
)
from _internal.gui_pages import (
    MapViewportPage,
    PageStepper,
    StepPage,
    StepData,
    MapImageSelectStep,
)
from _internal.gui_widgets import (
    ScrollableListWidget,
    Button,
    SwitchWidget,
    UndoRedoHistory,
    UndoRedoWidget,
)
from _internal.location_service import LocationService
from _internal.zmdmap_schemas import EntitiesTable

MAP_DIR = "assets/resource/image/MapTracker/map"
NAVMESH_DIR = "assets/data/MapTrackerNavMesh"
_REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
MAP_ENTITIES_DATA_FILE = os.path.join(
    _REPO_ROOT,
    "assets",
    "data",
    "ZmdMap",
    "maaend_entities.json",
)

ENTITY_TEMPLATE_CAMPFIRE = "int_campfire_v2"
"""Teleport anchor entity template name."""

ENTITY_TEMPLATE_DOODAD = "int_doodad_common"
"""Generic doodad entity template name. Should use key name to identify specific types of doodads."""

ENTITY_TEMPLATE_SYSTEM_SET = {
    ENTITY_TEMPLATE_CAMPFIRE,
    "int_system_world_energy_point",  # World energy point
    "int_system_deliver_target",  # Delivery target point
}
"""System entity template names."""

ENTITY_KEY_COMMON_COLLECT_RES = (
    r"^int_doodad_insect_\d+$",  # Common insects
    r"^int_doodad_flower_spc_\d+$",  # Common special flower plants
    r"^int_doodad_(corp|crop)_\d+$",  # Common crop plants (corp is a typo in game)
)
"""Common doodads that can be collected by the player."""

ENTITY_KEY_RARE_COLLECT_RE_LIST = (
    r"^int_doodad_mushroom_\d+_\d+$",  # Rare mushroom plants
    r"^int_doodad_crylplant_\d+_\d+$",  # Rare crystal plants
)
"""Rare doodads that can be collected by the player."""

ENTITY_KEY_RARE_DIG_RE_LIST = (r"^int_doodad_spcstone_\d+_\d+$",)  # Rare special stones
"""Rare doodads that can be dug by the player, typically mines"""

_TIER_MAP_RE = re.compile(r"_tier_(?P<tier>\d+)$")


@lru_cache(maxsize=16)
def _extract_entity_flags(template_name: str, key_name: str) -> int | None:
    def _matches_any(patterns: tuple[str, ...], text: str) -> bool:
        return any(re.fullmatch(pattern, text) is not None for pattern in patterns)

    if template_name in ENTITY_TEMPLATE_SYSTEM_SET:
        flags = FLAG_VERTEX_SYSTEM
        if template_name == ENTITY_TEMPLATE_CAMPFIRE:
            flags |= FLAG_VERTEX_TELEPORT
        return flags

    if template_name != ENTITY_TEMPLATE_DOODAD:
        return None

    if _matches_any(ENTITY_KEY_COMMON_COLLECT_RES, key_name):
        return FLAG_VERTEX_COLLECTABLE
    if _matches_any(ENTITY_KEY_RARE_COLLECT_RE_LIST, key_name):
        return FLAG_VERTEX_RARE | FLAG_VERTEX_COLLECTABLE
    if _matches_any(ENTITY_KEY_RARE_DIG_RE_LIST, key_name):
        return FLAG_VERTEX_RARE | FLAG_VERTEX_DIG
    return None


@lru_cache(maxsize=1)
def _load_entities_index() -> dict[tuple[str, str], list[dict]]:
    if not os.path.exists(MAP_ENTITIES_DATA_FILE):
        return {}

    try:
        entities_table = EntitiesTable.load(MAP_ENTITIES_DATA_FILE)
    except Exception:
        return {}

    index: dict[tuple[str, str], list[dict]] = {}
    for map_id, region in entities_table.regions.items():
        for map_level_id, level in region.levels.items():
            entries: list[dict] = []
            for entities in level.categories.values():
                for entity in entities:
                    entity_flags = _extract_entity_flags(
                        entity.template_name, entity.key_name
                    )
                    if entity_flags is None:
                        continue

                    x, y = entity.map_location or entity.pixel_location
                    entries.append(
                        {
                            "template_name": entity.template_name,
                            "key_name": entity.key_name,
                            "entity_id": entity.id,
                            "flags": entity_flags,
                            "x": x,
                            "y": y,
                        }
                    )

            if entries:
                index[(map_id, map_level_id)] = entries
    return index


def _tier_id_from_map_name(map_name: str) -> int:
    match = _TIER_MAP_RE.search(os.path.splitext(os.path.basename(map_name))[0])
    return int(match.group("tier")) if match else 0


def _import_entities_to_navmesh(
    data: "NavMeshData", map_id: str, map_level_id: str
) -> int:
    index = _load_entities_index()
    rows = index.get((map_id, map_level_id), [])
    if not rows:
        return 0

    imported = 0
    seen_ids: set[int] = set()
    for row in rows:
        entity_id = int(row.get("entity_id", 0))
        if entity_id in seen_ids:
            continue
        seen_ids.add(entity_id)

        flags = int(row.get("flags", 0))

        data.new_vertex(
            float(row.get("x", 0.0)),
            float(row.get("y", 0.0)),
            flags=flags,
            entity_id=entity_id,
            tier_id=0,
        )
        imported += 1

    return imported


@dataclass(frozen=True)
class NavMeshRecorderConfig:
    vertex_merge_distance: float = 4.5
    vertex_max_distance: float = 10.0
    vertex_k: float = 1.2
    edge_broken_sec: float = 2.0
    edge_max_distance: float = 15.0


@dataclass(frozen=True)
class NavMeshRecordingResult:
    operation: str
    location: tuple[float, float] | None = None
    loc_conf: float | None = None
    vertex_id: int | None = None
    k_value: float | None = None
    created_vertex: bool = False
    created_edge_ids: tuple[int, ...] = ()
    skipped_edge_reason: str | None = None
    dirty: bool = False
    chain_broken: bool = False


@dataclass
class _RecordingPoint:
    """Mutable state for the current recording point."""

    vertex_id: int | None = None
    ts: float | None = None
    mutable: bool = False
    origin_x: float = 0.0
    origin_y: float = 0.0

    def reset(self) -> "_RecordingPoint":
        return _RecordingPoint()


class NavMeshRecorder:
    def __init__(
        self,
        navmesh_data: NavMeshData,
        config: NavMeshRecorderConfig | None = None,
    ) -> None:
        self._data = navmesh_data
        self._config = config or NavMeshRecorderConfig()
        self._prev_id: int | None = None
        self._rp = _RecordingPoint()
        self._merge_zone_vertex_id: int | None = None

    def start(self) -> None:
        self._prev_id = None
        self._rp = _RecordingPoint()
        self._merge_zone_vertex_id = None

    def stop(self) -> None:
        self.reset_chain()

    def reset_chain(self) -> None:
        self._prev_id = None
        self._rp = _RecordingPoint()
        self._merge_zone_vertex_id = None

    @staticmethod
    def _simplify_k(
        prev_p: tuple[float, float],
        mid_p: tuple[float, float],
        next_p: tuple[float, float],
    ) -> float:
        # Direction vectors: prev→mid and mid→next
        pm_dx, pm_dy = mid_p[0] - prev_p[0], mid_p[1] - prev_p[1]
        mn_dx, mn_dy = next_p[0] - mid_p[0], next_p[1] - mid_p[1]
        d_prev_mid = math.hypot(pm_dx, pm_dy)
        d_mid_next = math.hypot(mn_dx, mn_dy)
        # sin(Δθ) = |cross product| / (|prev→mid| × |mid→next|)
        sin_dtheta = abs(pm_dx * mn_dy - pm_dy * mn_dx) / (
            d_prev_mid * d_mid_next + 1e-6
        )
        # f(d, Δθ) = (d + 1) × |sin Δθ|, where d = |mid→next|
        return min(d_mid_next + 1, sin_dtheta * (d_mid_next + 1))

    def _density_decision_k(
        self, current: NavVertex, realtime_p: tuple[float, float]
    ) -> float | None:
        values = [
            self._simplify_k(
                (neighbor.x, neighbor.y), (current.x, current.y), realtime_p
            )
            for neighbor in self._data.neighbors(current.id)
        ]
        return max(values) if values else None

    def _nearest_merge_vertex(
        self, x: float, y: float, tier_id: int
    ) -> NavVertex | None:
        exclude_ids: set[int] = set()
        if self._prev_id is not None:
            exclude_ids.add(self._prev_id)
        if self._rp.vertex_id is not None:
            exclude_ids.add(self._rp.vertex_id)
        return self._data.get_nearest_vertex_for(
            x,
            y,
            max_distance=self._config.vertex_merge_distance,
            tier_id=tier_id,
            exclude_ids=exclude_ids,
        )

    def _create_or_extend_vertex(
        self, x: float, y: float, tier_id: int
    ) -> tuple[NavVertex, bool, bool, float | None]:
        current = (
            self._data.get_vertex(self._rp.vertex_id)
            if self._rp.vertex_id is not None and self._rp.mutable
            else None
        )
        if current is None:
            return self._data.new_vertex(x, y, tier_id=tier_id), True, False, None

        # Multi-neighbor vertices are junctions — never extend, always create
        if len(self._data.neighbors(current.id)) >= 2:
            return self._data.new_vertex(x, y, tier_id=tier_id), True, False, None

        # Already extended too far from origin — force create
        if (
            math.hypot(self._rp.origin_x - x, self._rp.origin_y - y)
            > self._config.vertex_max_distance
        ):
            return self._data.new_vertex(x, y, tier_id=tier_id), True, False, None

        decision_k = self._density_decision_k(current, (x, y))
        if decision_k is not None and decision_k < self._config.vertex_k:
            current.x = x
            current.y = y
            return current, False, True, decision_k

        # k > threshold but still within merge distance: don't create duplicate
        if (
            math.hypot(current.x - x, current.y - y)
            <= self._config.vertex_merge_distance
        ):
            return current, False, False, decision_k

        return self._data.new_vertex(x, y, tier_id=tier_id), True, False, decision_k

    def _remove_over_distance_edges(self, vertex: NavVertex) -> None:
        """Remove edges from *vertex* to non-previous neighbors that exceed max distance."""
        for edge in self._data.edges_for(vertex.id):
            neighbor_id = edge.to_id if edge.from_id == vertex.id else edge.from_id
            if neighbor_id == self._prev_id:
                continue
            neighbor = self._data.get_vertex(neighbor_id)
            if neighbor is None:
                continue
            if (
                math.hypot(vertex.x - neighbor.x, vertex.y - neighbor.y)
                > self._config.edge_max_distance
            ):
                self._data.delete_edge(edge.id)

    def _point_in_merge_zone(self, x: float, y: float) -> bool:
        """True when (x, y) is within the merge-corridor of the active merge zone."""
        vid = self._merge_zone_vertex_id
        if vid is None:
            return False
        v = self._data.get_vertex(vid)
        if v is None:
            return False
        merge_d = self._config.vertex_merge_distance
        # Check corridor around each edge of the merged vertex
        for edge in self._data.edges_for(vid):
            other_id = edge.to_id if edge.from_id == vid else edge.from_id
            other = self._data.get_vertex(other_id)
            if other is None:
                continue
            # Point-to-segment distance
            ax, ay = v.x, v.y
            bx, by = other.x, other.y
            abx, aby = bx - ax, by - ay
            apx, apy = x - ax, y - ay
            ab2 = abx * abx + aby * aby
            if ab2 < 1e-12:
                dist = math.hypot(apx, apy)
            else:
                t = max(0.0, min(1.0, (apx * abx + apy * aby) / ab2))
                dist = math.hypot(apx - t * abx, apy - t * aby)
            if dist <= merge_d:
                return True
        # Check circle around the merged vertex itself
        if math.hypot(x - v.x, y - v.y) <= merge_d:
            return True
        return False

    def _generate_vertex(
        self, x: float, y: float, tier_id: int
    ) -> tuple[NavVertex, bool, bool, str, float | None]:
        """Orchestrates vertex creation or extension based on location and density-decision.

        Design flow: merge → merge-zone fallback → multi-neighbor → k-strategy.
        """
        # Always try to find a merge target first
        merge_target = self._nearest_merge_vertex(x, y, tier_id)
        if merge_target is not None:
            return merge_target, False, False, "merged_vertex", None

        # No merge target found — fall back to merge zone corridor
        if self._point_in_merge_zone(x, y):
            zone_v = self._data.get_vertex(self._merge_zone_vertex_id)
            if zone_v is not None and zone_v.tier_id == tier_id:
                return zone_v, False, False, "merged_vertex", None

        old_x, old_y = self._rp.origin_x, self._rp.origin_y
        vertex, created_vertex, extended_vertex, k_value = (
            self._create_or_extend_vertex(x, y, tier_id)
        )
        if created_vertex:
            return vertex, True, False, "created_vertex", k_value
        if extended_vertex:
            # Clean up edges that became over-distance after extension
            if math.hypot(old_x - x, old_y - y) > 0:
                self._remove_over_distance_edges(vertex)
            return vertex, False, True, "extended_vertex", k_value
        return vertex, False, False, "current_vertex", k_value

    def record_error(self) -> NavMeshRecordingResult:
        self.reset_chain()
        return NavMeshRecordingResult(
            operation="error",
            chain_broken=True,
        )

    def accept_location(
        self, x: float, y: float, loc_conf: float, tier_id: int = 0
    ) -> NavMeshRecordingResult:
        now = time.time()
        x = round(x, 3)
        y = round(y, 3)
        location = (x, y)
        vertex, created_vertex, extended_vertex, operation, k_value = (
            self._generate_vertex(x, y, tier_id)
        )
        is_merged = operation == "merged_vertex"

        if created_vertex:
            self._rp.origin_x = x
            self._rp.origin_y = y

        connect_from_id = self._rp.vertex_id
        next_mutable = created_vertex or extended_vertex

        chain_broken = False
        skipped_edge_reason = None
        created_edges: tuple[int, ...] = ()
        if connect_from_id is not None:
            created_edges, chain_broken, skipped_edge_reason = self._connect_vertices(
                connect_from_id, vertex.id, now
            )
            if created_edges:
                operation = "connected"
        if created_edges:
            self._prev_id = connect_from_id
            self._rp.vertex_id = vertex.id
            self._rp.mutable = next_mutable
        elif chain_broken or self._rp.vertex_id is None:
            self._prev_id = None
            self._rp.vertex_id = vertex.id
            self._rp.mutable = next_mutable
        elif is_merged:
            self._rp.vertex_id = vertex.id
            self._rp.mutable = next_mutable
        else:
            # "current_vertex": k rejected but within merge distance, keep mutable
            self._rp.mutable = next_mutable or operation == "current_vertex"

        # Merge zone: activate on merge, clear on any other transition
        if is_merged:
            self._merge_zone_vertex_id = vertex.id
        else:
            self._merge_zone_vertex_id = None

        self._rp.ts = now

        dirty = created_vertex or extended_vertex or bool(created_edges)
        return NavMeshRecordingResult(
            operation=operation,
            location=location,
            loc_conf=loc_conf,
            vertex_id=vertex.id,
            k_value=k_value,
            created_vertex=created_vertex,
            created_edge_ids=created_edges,
            skipped_edge_reason=skipped_edge_reason,
            dirty=dirty,
            chain_broken=chain_broken,
        )

    def _connect_vertices(
        self, from_id: int, to_id: int, now: float
    ) -> tuple[tuple[int, ...], bool, str | None]:
        if from_id == to_id:
            return (), False, None
        if self._rp.ts is not None and now - self._rp.ts > self._config.edge_broken_sec:
            return (), True, "time_gap"
        src = self._data.get_vertex(from_id)
        dst = self._data.get_vertex(to_id)
        if src is None or dst is None:
            self.reset_chain()
            return (), True, "missing_vertex"
        dist = math.hypot(src.x - dst.x, src.y - dst.y)
        if dist > self._config.edge_max_distance:
            return (), True, "distance"
        if self._data.has_edge_between(from_id, to_id):
            return (), False, None
        edge = self._data.new_edge(from_id, to_id)
        return (edge.id,), False, None


# ---------------------------------------------------------------------------
# InfoBar overlay
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class InfoBarButton:
    kind: str
    label: str
    enabled: bool
    rect: tuple[int, int, int, int] | None = None


class InfoBar:
    def __init__(
        self, window_w: int, window_h: int, sidebar_w: int, bar_h: int = 76
    ) -> None:
        self.window_w = window_w
        self.sidebar_w = sidebar_w
        self.bar_h = bar_h
        self._visible = False
        self._selection: tuple[str, int] | None = None
        self._info_lines: list[tuple[str, str]] = []
        self._buttons: list[InfoBarButton] = []
        self._bar_rect = (sidebar_w, window_h - bar_h, window_w, window_h)
        self._button_rects: dict[str, tuple[int, int, int, int]] = {}

    @property
    def selection(self) -> tuple[str, int] | None:
        return self._selection

    def clear(self) -> None:
        self._visible = False
        self._selection = None
        self._info_lines = []
        self._buttons = []
        self._button_rects = {}

    def set_selection(
        self,
        selection: tuple[str, int] | None,
        *,
        info_lines: list[tuple[str, str]],
        buttons: list[InfoBarButton],
    ) -> None:
        self._visible = selection is not None
        self._selection = selection
        self._info_lines = info_lines
        self._buttons = buttons

    def contains(self, x: int, y: int) -> bool:
        x1, y1, x2, y2 = self._bar_rect
        return self._visible and x1 <= x <= x2 and y1 <= y <= y2

    def hit_button(self, x: int, y: int) -> str | None:
        for kind, rect in self._button_rects.items():
            x1, y1, x2, y2 = rect
            if x1 <= x <= x2 and y1 <= y <= y2:
                return kind
        return None

    def render(self, drawer: Drawer) -> None:
        if not self._visible:
            self._button_rects = {}
            return

        x1, y1, x2, y2 = self._bar_rect
        drawer.mask((x1, y1), (x2, y2), color=0x101418, alpha=0.78)
        drawer.line((x1, y1), (x2, y1), color=0x4C4C4C, thickness=1)

        title = "Selection"
        if self._selection is not None:
            sel_type, sel_id = self._selection
            title = f"{sel_type.title()} {sel_id}"
        drawer.text(title, (x1 + 14, y1 + 29), 0.62, color=0x40FFFF)

        row1_x = x1 + 165
        row2_x = x1 + 165
        row1_y = y1 + 26
        row2_y = y1 + 56
        first_row_count = 3
        for index, (label, value) in enumerate(self._info_lines):
            is_first_row = index < first_row_count
            font_scale = 0.46 if is_first_row else 0.36
            info_x = row1_x if is_first_row else row2_x
            info_y = row1_y if is_first_row else row2_y
            drawer.text(f"{label}:", (info_x, info_y), font_scale, color=0xA8A8A8)
            label_w = drawer.get_text_size(f"{label}: ", font_scale)[0]
            drawer.text(value, (info_x + label_w, info_y), font_scale, color=0xFFFFFF)
            value_w = drawer.get_text_size(value, font_scale)[0]
            if is_first_row:
                row1_x += label_w + value_w + 34
            else:
                row2_x += label_w + value_w + 28

        self._render_buttons(drawer, x2 - 14, y1 + 18)

    def _render_buttons(self, drawer: Drawer, right_edge: int, top: int) -> None:
        self._button_rects = {}
        cursor_x = right_edge
        for button in self._buttons:
            label_w = max(44, drawer.get_text_size(button.label, 0.58)[0] + 24)
            rect = (cursor_x - label_w, top, cursor_x, top + 40)
            self._button_rects[button.kind] = rect
            self._draw_button(drawer, rect, button.label, button.enabled)
            cursor_x -= label_w + 8

    @staticmethod
    def _draw_button(
        drawer: Drawer, rect: tuple[int, int, int, int], label: str, enabled: bool
    ) -> None:
        x1, y1, x2, y2 = rect
        color = 0x1A50A8 if enabled else 0x4C4C4C
        border = 0x78B8FF if enabled else 0x8A8A8A
        drawer.rect((x1, y1), (x2, y2), color=color, thickness=-1)
        drawer.rect((x1, y1), (x2, y2), color=border, thickness=1)
        drawer.text_centered(label, ((x1 + x2) // 2, y2 - 10), 0.58, color=0xFFFFFF)


# ---------------------------------------------------------------------------
# NavMesh editing page
# ---------------------------------------------------------------------------


class NavMeshEditPage(MapViewportPage):
    """cv2 GUI page for editing a NavMesh."""

    VERTEX_RADIUS = 3
    VERTEX_SELECT_PX = 8  # screen-pixel hit radius for vertices
    EDGE_SELECT_PX = 8  # screen-pixel hit distance for edges
    EDGE_WIDTH = 1.25
    ARROW_SIZE = 10
    SIDEBAR_W = 240
    COVERAGE_RADIUS = 20
    COVERAGE_ALPHA = 0.35
    COVERAGE_COLOR = (100, 224, 255)

    def __init__(
        self,
        map_path: str,
        navmesh_data: NavMeshData,
        navmesh_file: NavMeshFile,
        output_path: str | None = None,
    ) -> None:
        self.map_path = map_path
        self.img = cv2.imread(self.map_path)
        if self.img is None:
            raise ValueError(f"Cannot load map: {self.map_path}")
        super().__init__(
            "MapTracker NavMesh Editor",
            1280,
            720,
            image=self.img,
            min_zoom=0.5,
            max_zoom=20.0,
        )
        self._status_color: Color = 0xFFFFFF
        self._status_message = ""
        self._data = navmesh_data
        self._nmf = navmesh_file
        self._output_path = output_path
        self._dirty = False
        self._confirm_discard = False

        # Selection: ("vertex", id) | ("edge", id) | None
        self._selection: tuple[str, int] | None = None

        # Edge-creation drag state
        self._drag_src: int | None = None  # source vertex ID in link mode
        self._move_vertex_id: int | None = None
        self._move_history_pushed = False
        self._edge_create_armed: bool = False
        self._edit_history = UndoRedoHistory(
            self._capture_edit_state,
            self._restore_edit_state,
            limit=100,
            on_changed=self.render_request,
        )

        self._infobar = InfoBar(self.window_w, self.window_h, self.SIDEBAR_W)
        self._coverage_mode = False
        self._coverage_cache: cv2.typing.MatLike | None = None
        self._coverage_cache_key: (
            tuple[int, int, tuple[tuple[int, int, int, int, float, float], ...]] | None
        ) = None
        self._coverage_switch_widget = SwitchWidget(
            "Graph",
            "Coverage",
            is_left_selected=not self._coverage_mode,
            on_changed=self._on_coverage_switch_changed,
        )

        hidden_rect = (-100, -100, -90, -90)
        self._save_button = Button(
            hidden_rect,
            "[S] Save",
            base_color=0x3C643C,
            hotkey=(ord("s"), ord("S")),
            on_click=self._on_click_save,
            font_scale=0.42,
        )
        self._location_button = Button(
            hidden_rect,
            "[Enter] Start Record",
            base_color=0x3C3C64,
            hotkey=(13,),
            on_click=self._on_click_location,
            font_scale=0.37,
        )
        self._edge_mode_button = Button(
            hidden_rect,
            "[C] Create Edge",
            base_color=0x1A50A8,
            hotkey=(ord("c"), ord("C")),
            on_click=self._on_click_edge_mode,
            font_scale=0.42,
        )
        self._test_goal_button = Button(
            hidden_rect,
            "[T] Test Goal Action",
            base_color=0x5C2D91,
            hotkey=(ord("t"), ord("T")),
            on_click=self._on_click_test_goal,
            font_scale=0.42,
        )
        self._delete_button = Button(
            hidden_rect,
            "[D] Delete",
            base_color=0xB44022,
            hotkey=(ord("d"), ord("D")),
            on_click=self._on_click_delete,
            font_scale=0.42,
        )
        self._history_widget = UndoRedoWidget(
            on_undo=self._undo_edit_change,
            on_redo=self._redo_edit_change,
            can_undo=lambda: self._edit_history.can_undo,
            can_redo=lambda: self._edit_history.can_redo,
        )
        self.buttons.extend(
            [
                self._save_button,
                self._location_button,
                self._test_goal_button,
                self._edge_mode_button,
                self._delete_button,
                *self._history_widget.buttons,
            ]
        )

        self.location_service = LocationService()
        self._recorder = NavMeshRecorder(self._data)
        self._location_tracking_active = False
        self._latest_location: tuple[float, float] | None = None
        self._latest_location_conf: float | None = None
        self._location_map_name = os.path.basename(self.map_path)

        self.configure_map_layer_switching(
            logical_map_name=self._location_map_name,
            map_dir=os.path.dirname(self.map_path),
            base_image=self.img,
        )

        # Fit view to existing vertices
        if self._data.vertices:
            pts = [(v.x, v.y) for v in self._data.vertices]
            self.view.fit_to(pts)  # type: ignore[arg-type]
        else:
            h, w = self.img.shape[:2]
            self.view.fit_to([(0, 0), (w, h)], padding=0.02)

    def _update_status(self, color: Color, message: str) -> None:
        if self._confirm_discard and not message.startswith("Unsaved"):
            self._confirm_discard = False
        self._status_color = color
        self._status_message = message
        print(message)

    def _update_infobar(self) -> None:
        if self._selection is None:
            self._infobar.clear()
            return

        sel_type, sel_id = self._selection
        if sel_type == "vertex":
            vertex = self._vertex_by_id(sel_id)
            if vertex is None:
                self._infobar.clear()
                return
            template_name, key_name = self._entity_text_for_vertex(vertex)
            info_lines = [
                ("X", f"{vertex.x:.3f}"),
                ("Y", f"{vertex.y:.3f}"),
                ("Flags", self._vertex_flags_text(vertex.flags)),
                ("Tier", str(vertex.tier_id) if vertex.tier_id else "-"),
                ("Entity", str(vertex.entity_id) if vertex.entity_id else "-"),
                ("Template", template_name),
                ("Key", key_name),
            ]
            buttons: list[InfoBarButton] = []
            self._infobar.set_selection(
                self._selection, info_lines=info_lines, buttons=buttons
            )
            return

        edge = self._edge_by_id(sel_id)
        if edge is None:
            self._infobar.clear()
            return
        info_lines = [
            ("From", f"V{edge.from_id}"),
            ("To", f"V{edge.to_id}"),
            ("Direction", self._edge_flags_text(edge.flags)),
        ]
        buttons = [
            InfoBarButton(
                "edge-bidir", "<-->", bool(edge.flags & FLAG_EDGE_BIDIRECTIONAL)
            ),
        ]
        self._infobar.set_selection(
            self._selection, info_lines=info_lines, buttons=buttons
        )

    @staticmethod
    def _short_path(path: str | None, max_len: int = 28) -> str:
        if not path:
            return "-"
        normalized = path.replace("\\", "/")
        if len(normalized) <= max_len:
            return normalized
        return "..." + normalized[-(max_len - 3) :]

    def _entity_text_for_vertex(self, vertex: NavVertex) -> tuple[str, str]:
        if vertex.entity_id == 0:
            return "-", "-"
        index = _load_entities_index()
        rows = index.get((self._nmf.map_region_name, self._nmf.map_level_name), [])
        for row in rows:
            if int(row.get("entity_id", 0)) == vertex.entity_id:
                return str(row.get("template_name") or "-"), str(
                    row.get("key_name") or "-"
                )
        return "-", "-"

    @staticmethod
    def _vertex_flags_text(flags: int) -> str:
        parts: list[str] = []
        if flags & FLAG_VERTEX_TELEPORT:
            parts.append("Teleport")
        if flags & FLAG_VERTEX_HIDDEN:
            parts.append("Hidden")
        if flags & FLAG_VERTEX_SYSTEM:
            parts.append("System")
        if flags & FLAG_VERTEX_RARE:
            parts.append("Rare")
        if flags & FLAG_VERTEX_COLLECTABLE:
            parts.append("Collectable")
        if flags & FLAG_VERTEX_DIG:
            parts.append("Dig")
        return ", ".join(parts) if parts else "Normal"

    @staticmethod
    def _edge_flags_text(flags: int) -> str:
        return "Bidirectional" if (flags & FLAG_EDGE_BIDIRECTIONAL) else "One-way"

    @staticmethod
    def _vertex_marker_shape(vertex: NavVertex) -> str:
        if vertex.flags & FLAG_VERTEX_TELEPORT:
            return "square"
        if vertex.flags & FLAG_VERTEX_SYSTEM:
            return "triangle"
        return "circle"

    @staticmethod
    def _triangle_points(cx: int, cy: int, radius: int) -> list[tuple[int, int]]:
        return [
            (cx, cy - radius),
            (cx - int(round(radius * 0.866)), cy + radius // 2),
            (cx + int(round(radius * 0.866)), cy + radius // 2),
        ]

    def _draw_vertex_marker(
        self,
        drawer: Drawer,
        vertex: NavVertex,
        x: int,
        y: int,
        radius: int,
        color: Color,
    ) -> None:
        shape = self._vertex_marker_shape(vertex)
        outline_r = radius + 1
        if shape == "triangle":
            drawer.polygon(
                self._triangle_points(x, y, outline_r), color=0x202020, thickness=-1
            )
            drawer.polygon(
                self._triangle_points(x, y, radius), color=color, thickness=-1
            )
            return
        if shape == "square":
            drawer.rect(
                (x - outline_r, y - outline_r),
                (x + outline_r, y + outline_r),
                color=0x202020,
                thickness=-1,
            )
            drawer.rect(
                (x - radius, y - radius),
                (x + radius, y + radius),
                color=color,
                thickness=-1,
            )
            return
        drawer.circle((x, y), outline_r, color=0x202020, thickness=-1)
        drawer.circle((x, y), radius, color=color, thickness=-1)

    def _line(
        self, drawer: Drawer, text: str, x: int, y: int, color: Color = 0xC8C8C8
    ) -> int:
        drawer.text(text, (x, y), 0.38, color=color)
        return y + 17

    def _render_sidebar_bg(self, drawer: Drawer) -> None:
        sw = self.SIDEBAR_W
        h = self.window_h
        drawer.rect((0, 0), (sw, h), color=0x000000, thickness=-1)
        drawer.line((sw - 1, 0), (sw - 1, h), color=0xFFFFFF, thickness=1)

    def _render_once(self, drawer: Drawer) -> None:
        self._update_infobar()
        self._render_map_layer(drawer)
        self._render_coverage_layer(drawer)
        self._render_content(drawer)
        drawer.crosshair(self.mouse_pos, color=0xFFFF00, thickness=1)
        self._infobar.render(drawer)
        self._render_sidebar(drawer)
        self.render_map_layer_selector(drawer, sidebar_width=self.SIDEBAR_W)

    def _coverage_key(
        self,
    ) -> tuple[
        int, int, int, int, int, tuple[tuple[int, int, int, int, float, float], ...]
    ]:
        edges: list[tuple[int, int, int, int, float, float]] = []
        for e in self._data.edges:
            src = self._vertex_by_id(e.from_id)
            dst = self._vertex_by_id(e.to_id)
            if src is None or dst is None:
                continue
            edges.append(
                (
                    int(round(src.x)),
                    int(round(src.y)),
                    int(round(dst.x)),
                    int(round(dst.y)),
                    e.cost,
                    e.flags,
                )
            )
        origin = self.view.get_real_coords(0, 0)
        zoom_key = int(round(self.view.zoom * 10000))
        return (
            self.window_w,
            self.window_h,
            int(round(origin[0])),
            int(round(origin[1])),
            zoom_key,
            tuple(edges),
        )

    def _ensure_coverage_overlay(self) -> cv2.typing.MatLike:
        key = self._coverage_key()
        if self._coverage_cache is not None and self._coverage_cache_key == key:
            return self._coverage_cache

        overlay = np.zeros((self.window_h, self.window_w, 3), dtype=np.uint8)
        zoom = self.view.zoom
        radius = max(1, int(self.COVERAGE_RADIUS * zoom))
        thickness = max(1, radius * 2)
        color = self.COVERAGE_COLOR

        for e in self._data.edges:
            src = self._vertex_by_id(e.from_id)
            dst = self._vertex_by_id(e.to_id)
            if src is None or dst is None:
                continue
            sx1, sy1 = self.view.get_view_coords(src.x, src.y)
            sx2, sy2 = self.view.get_view_coords(dst.x, dst.y)
            cv2.line(
                overlay, (sx1, sy1), (sx2, sy2), color, thickness, lineType=cv2.LINE_AA
            )
            cv2.circle(overlay, (sx1, sy1), radius, color, -1, lineType=cv2.LINE_AA)
            cv2.circle(overlay, (sx2, sy2), radius, color, -1, lineType=cv2.LINE_AA)

        self._coverage_cache = overlay
        self._coverage_cache_key = key
        return self._coverage_cache

    def _render_coverage_layer(self, drawer: Drawer) -> None:
        if not self._coverage_mode:
            return
        overlay = self._ensure_coverage_overlay()
        bg = drawer.get_image()
        cv2.addWeighted(
            bg, 1.0 - self.COVERAGE_ALPHA, overlay, self.COVERAGE_ALPHA, 0, dst=bg
        )

    # ------------------------------------------------------------------
    # Data helpers
    # ------------------------------------------------------------------

    def _capture_edit_state(self) -> NavMeshDataSnapshot:
        return self._data.snapshot()

    def _restore_edit_state(self, snapshot: NavMeshDataSnapshot) -> None:
        self._data.restore(snapshot)
        self._selection = self._valid_selection(self._selection)
        self._dirty = True
        self._cancel_edge_link_mode()
        self._move_vertex_id = None
        self._move_history_pushed = False
        if self._location_tracking_active:
            self._recorder.reset_chain()

    def _valid_selection(
        self, selection: tuple[str, int] | None
    ) -> tuple[str, int] | None:
        if selection is None:
            return None
        sel_type, sel_id = selection
        if sel_type == "vertex" and self._data.get_vertex(sel_id) is not None:
            return selection
        if sel_type == "edge" and self._data.get_edge(sel_id) is not None:
            return selection
        return None

    def _push_current_edit_state(self) -> None:
        self._edit_history.push_current()

    def _undo_edit_change(self) -> None:
        if self._edit_history.undo():
            self._update_status(0xD2D200, "Reverted the previous navmesh change.")
            self.render_request()

    def _redo_edit_change(self) -> None:
        if self._edit_history.redo():
            self._update_status(0x78DCFF, "Reapplied the reverted navmesh change.")
            self.render_request()

    def _vertex_by_id(self, vid: int) -> NavVertex | None:
        return self._data.get_vertex(vid)

    def _edge_by_id(self, eid: int) -> NavEdge | None:
        return self._data.get_edge(eid)

    def _has_edge_between(self, a_id: int, b_id: int) -> bool:
        return self._data.has_edge_between(a_id, b_id)

    def _vertex_at_screen(self, sx: int, sy: int) -> int | None:
        """Return the ID of the vertex whose screen position is within hit radius."""
        for v in self._data.vertices:
            vsx, vsy = self.view.get_view_coords(v.x, v.y)
            if math.hypot(sx - vsx, sy - vsy) <= self.VERTEX_SELECT_PX:
                return v.id
        return None

    def _edge_at_screen(self, sx: int, sy: int) -> int | None:
        """Return the ID of the edge whose screen-space segment is within hit distance."""
        for e in self._data.edges:
            src = self._vertex_by_id(e.from_id)
            dst = self._vertex_by_id(e.to_id)
            if src is None or dst is None:
                continue
            sx1, sy1 = self.view.get_view_coords(src.x, src.y)
            sx2, sy2 = self.view.get_view_coords(dst.x, dst.y)
            if self._dist_to_segment(sx, sy, sx1, sy1, sx2, sy2) < self.EDGE_SELECT_PX:
                return e.id
        return None

    @staticmethod
    def _dist_to_segment(px: int, py: int, x1: int, y1: int, x2: int, y2: int) -> float:
        dx, dy = x2 - x1, y2 - y1
        if dx == 0 and dy == 0:
            return math.hypot(px - x1, py - y1)
        t = max(0.0, min(1.0, ((px - x1) * dx + (py - y1) * dy) / (dx * dx + dy * dy)))
        return math.hypot(px - (x1 + t * dx), py - (y1 + t * dy))

    # ------------------------------------------------------------------
    # Actions
    # ------------------------------------------------------------------

    def _do_save(self) -> None:
        if not self._output_path:
            self._update_status(0xFC4040, "No output path configured!")
            return
        self._nmf.vertices = list(self._data.vertices)
        self._nmf.edges = list(self._data.edges)
        out_dir = os.path.dirname(os.path.abspath(self._output_path))
        os.makedirs(out_dir, exist_ok=True)
        try:
            self._nmf.save(self._output_path)
            self._dirty = False
            self._confirm_discard = False
            self._update_status(
                0x50DC50, f"Saved → {self._short_path(self._output_path, 22)}"
            )
        except Exception as exc:
            self._update_status(0xFC4040, f"Save failed: {exc}")

    def _request_exit(self) -> None:
        if self._dirty and not self._confirm_discard:
            self._confirm_discard = True
            self._update_status(0xD2D200, "Unsaved. ESC again discards, S saves.")
            self.render_request()
            return
        if self.stepper:
            self.stepper.finish()
        self.done = True

    def _toggle_selected_edge_direction(self) -> None:
        if self._selection is None or self._selection[0] != "edge":
            return
        e = self._edge_by_id(self._selection[1])
        if e is None:
            return
        self._push_current_edit_state()
        e.flags ^= FLAG_EDGE_BIDIRECTIONAL
        self._dirty = True
        self._update_status(
            0x78DCFF, f"Selected edge is now {self._edge_flags_text(e.flags)}"
        )

    def _do_delete(self) -> None:
        if self._selection is None:
            return
        sel_type, sel_id = self._selection
        if sel_type == "vertex":
            vertex = self._vertex_by_id(sel_id)
            if vertex is not None and vertex.entity_id:
                self._update_status(
                    0xD2D200, "Entity vertex is read-only and cannot be deleted"
                )
                return
        self._push_current_edit_state()
        if sel_type == "vertex":
            removed_edges = [
                e for e in self._data.edges if e.from_id == sel_id or e.to_id == sel_id
            ]
            self._data.edges = [
                e for e in self._data.edges if e.from_id != sel_id and e.to_id != sel_id
            ]
            self._data.vertices = [v for v in self._data.vertices if v.id != sel_id]
            self._selection = None
            self._dirty = True
            self._update_status(
                0x78DCFF,
                f"Deleted selected vertex and {len(removed_edges)} edge(s)",
            )
        elif sel_type == "edge":
            self._data.edges = [e for e in self._data.edges if e.id != sel_id]
            self._selection = None
            self._dirty = True
            self._update_status(0x78DCFF, "Deleted selected edge")

    # ------------------------------------------------------------------
    # Rendering overrides
    # ------------------------------------------------------------------

    def _render_content(self, drawer: Drawer) -> None:
        zoom = self.view.zoom
        zoom_scale = max(0.5, zoom**0.5)
        edge_thickness = max(1, int(self.EDGE_WIDTH * zoom_scale))
        vertex_r = max(2, int(self.VERTEX_RADIUS * zoom_scale))

        # ── Edges ──────────────────────────────────────────────────────
        for e in self._data.edges:
            src = self._vertex_by_id(e.from_id)
            dst = self._vertex_by_id(e.to_id)
            if src is None or dst is None:
                continue
            sx1, sy1 = self.view.get_view_coords(src.x, src.y)
            sx2, sy2 = self.view.get_view_coords(dst.x, dst.y)
            color: Color = 0xFF4040 if self._selection == ("edge", e.id) else 0xFFD166
            if e.flags & FLAG_EDGE_BIDIRECTIONAL:
                drawer.line(
                    (sx1, sy1), (sx2, sy2), color=color, thickness=edge_thickness
                )
            else:
                drawer.line(
                    (sx1, sy1), (sx2, sy2), color=color, thickness=edge_thickness
                )
                dx, dy = sx2 - sx1, sy2 - sy1
                dist = math.hypot(dx, dy)
                if dist >= 1:
                    ux, uy = dx / dist, dy / dist
                    mid_x = (sx1 + sx2) // 2
                    mid_y = (sy1 + sy2) // 2
                    arrow_len = max(14, int(self.ARROW_SIZE * 1.8 * zoom_scale))
                    start = (
                        int(round(mid_x - ux * arrow_len * 0.5)),
                        int(round(mid_y - uy * arrow_len * 0.5)),
                    )
                    end = (
                        int(round(mid_x + ux * arrow_len * 0.5)),
                        int(round(mid_y + uy * arrow_len * 0.5)),
                    )
                    drawer.arrow(
                        start,
                        end,
                        color=color,
                        thickness=max(1, edge_thickness + 1),
                        arrow_size=max(8, int(self.ARROW_SIZE * zoom_scale)),
                    )

        # ── Preview edge in link mode ──────────────────────────────────
        if self._drag_src is not None and self._edge_create_armed:
            src = self._vertex_by_id(self._drag_src)
            if src is not None:
                sx, sy = self.view.get_view_coords(src.x, src.y)
                drawer.dashed_line(
                    (sx, sy), self.mouse_pos, color=0xF0F000, thickness=2
                )

        # ── Vertices ───────────────────────────────────────────────────
        for v in self._data.vertices:
            vsx, vsy = self.view.get_view_coords(v.x, v.y)
            is_sel = self._selection == ("vertex", v.id)
            color = 0xFF4040 if is_sel else (0xFFD166 if v.entity_id else 0xE6E6E6)
            self._draw_vertex_marker(drawer, v, vsx, vsy, vertex_r, color)

        # ── Realtime location marker ──────────────────────────────────
        if self._latest_location is not None:
            lx, ly = self._latest_location
            lsx, lsy = self.view.get_view_coords(lx, ly)
            drawer.crosshair(
                (lsx, lsy),
                color=0x50DC50,
                thickness=1,
                full_screen=True,
            )
            drawer.circle((lsx, lsy), 3, color=0x50DC50, thickness=-1)

    def _render_sidebar(self, drawer: Drawer) -> None:
        self._render_sidebar_bg(drawer)
        sw = self.SIDEBAR_W
        h = self.window_h
        pad = 12
        btn_h = 28
        btn_w = sw - pad * 2
        btn_x0 = pad
        cy = pad + 14

        drawer.text("[ Document ]", (pad, cy), 0.45, color=0x40FFFF)
        cy += 18
        map_name = os.path.basename(self.map_path)
        output_name = self._short_path(self._output_path)
        state_label = "Unsaved" if self._dirty else "Saved"
        state_color: Color = 0xD2D200 if self._dirty else 0x50DC50
        cy = self._line(drawer, f"Name: {self._nmf.name or '-'}", pad, cy)
        cy = self._line(drawer, f"Map: {map_name}", pad, cy)
        cy = self._line(drawer, f"Out: {output_name}", pad, cy)
        cy = self._line(drawer, f"State: {state_label}", pad, cy, state_color)
        cy += 7

        drawer.line((pad, cy), (sw - pad, cy), color=0x404040, thickness=1)
        cy += 18
        drawer.text("[ Mode ]", (pad, cy), 0.45, color=0x40FFFF)
        cy += 18
        if self._edge_create_armed and self._drag_src is not None:
            cy = self._line(drawer, "Mode: Link Edge", pad, cy, 0xF0F000)
            cy = self._line(drawer, f"Source: V{self._drag_src}", pad, cy)
            cy = self._line(drawer, "Click target vertex", pad, cy)
            cy = self._line(drawer, "[C] / [Esc]: Cancel", pad, cy, 0xD2D200)
        else:
            cy = self._line(drawer, "Mode: Select / Move", pad, cy)
            cy = self._line(drawer, "Click empty: Create vertex", pad, cy)
        cy += 7

        sw_y0 = cy
        sw_y1 = cy + btn_h
        self._coverage_switch_widget.render(
            drawer,
            (btn_x0, sw_y0, btn_x0 + btn_w, sw_y1),
        )
        cy = sw_y1 + 7

        drawer.line((pad, cy), (sw - pad, cy), color=0x404040, thickness=1)
        cy += 18
        drawer.text("[ Actions ]", (pad, cy), 0.45, color=0x40FFFF)
        cy += 11

        hidden_rect = (-100, -100, -90, -90)

        save_y0 = cy
        save_y1 = cy + btn_h
        self._save_button.rect = (btn_x0, save_y0, btn_x0 + btn_w, save_y1)
        self._save_button.base_color = 0x64C800 if self._dirty else 0x3C643C
        self._save_button.text_color = 0xFFFFFF if self._dirty else 0x648264
        cy = save_y1 + 7

        ls_y0 = cy
        ls_y1 = cy + btn_h
        self._location_button.rect = (btn_x0, ls_y0, btn_x0 + btn_w, ls_y1)
        ls_running = self._location_tracking_active
        self._location_button.base_color = 0x1C8A1C if ls_running else 0x3C3C64
        self._location_button.text = (
            "[Enter] Stop Record" if ls_running else "[Enter] Start Record"
        )
        cy = ls_y1 + 7

        history_y0 = cy
        history_y1 = cy + btn_h
        self._history_widget.place((btn_x0, history_y0, btn_x0 + btn_w, history_y1))
        cy = history_y1 + 7

        self._test_goal_button.rect = hidden_rect
        if self._selection is not None and self._selection[0] == "vertex":
            tg_y0 = cy
            tg_y1 = cy + btn_h
            self._test_goal_button.rect = (btn_x0, tg_y0, btn_x0 + btn_w, tg_y1)
            self._test_goal_button.text_color = 0xFFFFFF
            cy = tg_y1 + 7

        self._edge_mode_button.rect = hidden_rect
        if (
            self._selection is not None and self._selection[0] == "vertex"
        ) or self._edge_create_armed:
            em_y0 = cy
            em_y1 = cy + btn_h
            self._edge_mode_button.rect = (btn_x0, em_y0, btn_x0 + btn_w, em_y1)
            self._edge_mode_button.text = (
                "[C] Cancel Edge" if self._edge_create_armed else "[C] Create Edge"
            )
            self._edge_mode_button.base_color = (
                0x8A781C if self._edge_create_armed else 0x1A50A8
            )
            self._edge_mode_button.text_color = 0xFFFFFF
            cy = em_y1 + 7

        self._delete_button.rect = hidden_rect
        if self._selection is not None:
            del_y0 = cy
            del_y1 = cy + btn_h
            self._delete_button.rect = (btn_x0, del_y0, btn_x0 + btn_w, del_y1)
            self._delete_button.text_color = 0xFFFFFF
            cy = del_y1 + 7

        info_y = h - 58
        drawer.line((pad, info_y), (sw - pad, info_y), color=0x404040, thickness=1)
        info_y += 18
        mx, my = self._get_map_coords(*self.mouse_pos)
        drawer.text(f"Mouse: {mx:.1f}, {my:.1f}", (pad, info_y), 0.36, color=0xC8C8C8)
        info_y += 18
        vcount = len(self._data.vertices)
        ecount = len(self._data.edges)
        drawer.text(
            f"Zoom: {self.view.zoom:.2f}x  V: {vcount} E: {ecount}",
            (pad, info_y),
            0.36,
            color=0xFFFFFF,
        )
        if self._latest_location is not None:
            lx, ly = self._latest_location
            conf_text = (
                ""
                if self._latest_location_conf is None
                else f" c={self._latest_location_conf:.2f}"
            )
            drawer.text(
                f"Loc: {lx:.1f}, {ly:.1f}{conf_text}",
                (pad, h - 7),
                0.36,
                color=0x50DC50,
            )

    def _apply_recording_result(self, result: NavMeshRecordingResult) -> bool:
        if result.location is not None:
            self._latest_location = result.location
            self._latest_location_conf = result.loc_conf
            if self._move_vertex_id is None and not self.panning:
                self.view.maybe_center_to(*result.location)
        if result.dirty:
            self._dirty = True

        parts = [f"operation={result.operation}"]
        if result.location is not None:
            parts.append(
                f"location=({result.location[0]:.3f}, {result.location[1]:.3f})"
            )
        if result.loc_conf is not None:
            parts.append(f"loc_conf={result.loc_conf:.3f}")
        if result.vertex_id is not None:
            parts.append(f"vertex={result.vertex_id}")
        if result.k_value is not None:
            parts.append(f"k={result.k_value:.3f}")
        if result.created_vertex:
            parts.append("created=1")
        if result.skipped_edge_reason:
            parts.append(f"edge_skip={result.skipped_edge_reason}")
        if result.chain_broken:
            parts.append("chain_broken=1")
        prefix = _Y if result.operation in ("error", "skipped") else _G
        print(f"{prefix}Recording:{_0} " + " ".join(parts))
        return result.dirty or result.location is not None

    def _start_location_tracking(self) -> None:
        if not self.location_service.start_recording(self._location_map_name):
            error_msg = "Cannot start network recording."
            try:
                item = self.location_service.result_queue.get_nowait()
                if isinstance(item, Exception):
                    error_msg = str(item)
            except queue.Empty:
                pass
            self._update_status(0xFC4040, error_msg)
            self.render_request()
            return

        self._location_tracking_active = True
        self._latest_location = None
        self._latest_location_conf = None
        self._recorder.start()
        self._update_status(0x50DC50, "Network recording started.")
        self.render_request()

    def _stop_location_tracking(self) -> None:
        self.location_service.stop_recording()
        self._location_tracking_active = False
        self._recorder.stop()
        self._update_status(
            0xD2D200,
            "Recording stopped.",
        )
        self.render_request()

    def _toggle_location_tracking(self) -> None:
        if self._location_tracking_active:
            self._stop_location_tracking()
        else:
            self._start_location_tracking()

    def _on_click_save(self) -> None:
        if self._dirty:
            self._do_save()
            self.render_request()

    def _on_click_location(self) -> None:
        self._cancel_edge_link_mode()
        self._toggle_location_tracking()
        self.render_request()

    def _on_click_edge_mode(self) -> None:
        self._toggle_edge_link_mode()
        self.render_request()

    def _on_click_test_goal(self) -> None:
        if self._selection is None or self._selection[0] != "vertex":
            self._update_status(0xD2D200, "Select a vertex before pressing T")
            return
        vertex = self._vertex_by_id(self._selection[1])
        if vertex is None:
            return
        map_name = os.path.splitext(self._location_map_name)[0]
        self._update_status(
            0x50DC50, f"Running goal to ({vertex.x:.1f}, {vertex.y:.1f}) ..."
        )
        self.render_request()
        try:
            self.location_service.run_goal(map_name, vertex.x, vertex.y)
            self._update_status(0x50DC50, "Goal action completed")
        except Exception as e:
            self._update_status(0x2222DC, f"Goal failed: {e}")

    def _on_click_delete(self) -> None:
        if self._selection is None:
            return
        self._cancel_edge_link_mode()
        self._do_delete()
        self.render_request()

    def _cancel_edge_link_mode(self) -> None:
        self._edge_create_armed = False
        self._drag_src = None

    def _toggle_edge_link_mode(self) -> None:
        if self._edge_create_armed:
            self._cancel_edge_link_mode()
            self.render_request()
            return
        if self._selection is None or self._selection[0] != "vertex":
            self._update_status(0xD2D200, "Select a source vertex before pressing C")
            self.render_request()
            return
        self._edge_create_armed = True
        self._drag_src = self._selection[1]
        self.render_request()

    def _update_location_tracking(self) -> bool:
        if not self._location_tracking_active:
            return False

        updated = False
        latest_exception: Exception | None = None
        while True:
            try:
                result = self.location_service.result_queue.get_nowait()
            except queue.Empty:
                break

            if isinstance(result, Exception):
                latest_exception = result
                record_result = self._recorder.record_error()
                updated = self._apply_recording_result(record_result) or updated
                continue

            try:
                x = float(result["x"])
                y = float(result["y"])
                loc_conf = float(result.get("loc_conf", 0.0))
                map_name = str(result.get("map_name") or "")
                tier_id = _tier_id_from_map_name(map_name)
            except Exception as exc:
                latest_exception = exc
                record_result = self._recorder.record_error()
                updated = self._apply_recording_result(record_result) or updated
                continue

            self.sync_displayed_layer_from_map_name(map_name)

            before_state = self._capture_edit_state()
            record_result = self._recorder.accept_location(x, y, loc_conf, tier_id)
            if record_result.dirty:
                self._edit_history.push_state(before_state)
            updated = self._apply_recording_result(record_result) or updated

        if updated or latest_exception is not None:
            self.render_request()
        return updated

    # ------------------------------------------------------------------
    # Mouse / keyboard overrides
    # ------------------------------------------------------------------

    def _on_mouse(self, event, x: int, y: int, flags, param) -> None:
        mx, my = self._get_map_coords(x, y)
        if self.consume_view_mouse(event, x, y, flags, mx, my):
            return

        if event == cv2.EVENT_MOUSEMOVE:
            if (flags & cv2.EVENT_FLAG_LBUTTON) and self._move_vertex_id is not None:
                moving_v = self._vertex_by_id(self._move_vertex_id)
                if moving_v is not None:
                    next_x = round(mx, 3)
                    next_y = round(my, 3)
                    if (moving_v.x, moving_v.y) != (next_x, next_y):
                        if not self._move_history_pushed:
                            self._push_current_edit_state()
                            self._move_history_pushed = True
                        moving_v.x = next_x
                        moving_v.y = next_y
                        self._dirty = True
                    self.render_request()
                return

        elif event == cv2.EVENT_LBUTTONDOWN:
            if x < self.SIDEBAR_W:
                if self._coverage_switch_widget.consume_mouse(event, x, y, flags):
                    return
                return
            if self._handle_infobar_click(x, y):
                return

            hit_vid = self._vertex_at_screen(x, y)
            if hit_vid is not None:
                if self._edge_create_armed and self._drag_src is not None:
                    src_id = self._drag_src
                    if hit_vid != src_id:
                        if self._has_edge_between(src_id, hit_vid):
                            self._update_status(
                                0xD2D200,
                                "Edge already exists between selected vertices",
                            )
                        else:
                            self._push_current_edit_state()
                            self._data.new_edge(src_id, hit_vid)
                            self._dirty = True
                            self._selection = ("vertex", hit_vid)
                            self._update_status(0x78DCFF, "Created bidirectional edge")
                    self._cancel_edge_link_mode()
                    self.render_request()
                    return

                self._selection = ("vertex", hit_vid)
                hit_vertex = self._vertex_by_id(hit_vid)
                if hit_vertex is not None and hit_vertex.entity_id == 0:
                    self._move_vertex_id = hit_vid
                    self._move_history_pushed = False
                else:
                    self._move_vertex_id = None
                    self._move_history_pushed = False
                    self._update_status(
                        0xD2D200, "Entity vertex is read-only and cannot be moved"
                    )
            else:
                hit_eid = self._edge_at_screen(x, y)
                if hit_eid is not None:
                    self._selection = ("edge", hit_eid)
                else:
                    # Create a new vertex
                    self._push_current_edit_state()
                    new_v = self._data.new_vertex(round(mx, 3), round(my, 3))
                    self._dirty = True
                    self._selection = ("vertex", new_v.id)
                    self._update_status(
                        0x78DCFF,
                        f"Created vertex at ({int(mx)}, {int(my)})",
                    )
                self.render_request()

        elif event == cv2.EVENT_LBUTTONUP:
            if self._move_vertex_id is not None:
                self._move_vertex_id = None
                self._move_history_pushed = False
                self.render_request()
                return

    def _handle_infobar_click(self, x: int, y: int) -> bool:
        if not self._infobar.contains(x, y):
            return False
        kind = self._infobar.hit_button(x, y)
        if kind is None:
            return True
        self._cancel_edge_link_mode()
        if kind == "edge-bidir":
            self._toggle_selected_edge_direction()
        self.render_request()
        return True

    def _on_coverage_switch_changed(self, is_left_selected: bool) -> None:
        self._coverage_mode = not is_left_selected
        self._coverage_cache = None
        self.render_request()

    def handle_escape(self) -> bool:
        if self._edge_create_armed:
            self._cancel_edge_link_mode()
            self._update_status(0xD2D200, "Canceled edge creation")
            self.render_request()
            return True
        self._request_exit()
        return True

    def hook_idle(self) -> None:
        self._update_location_tracking()

    def hook_exit(self) -> None:
        self.location_service.cleanup()


class NavMeshModeStep(StepPage):
    def __init__(self):
        super().__init__(StepData("NavMesh Mode", can_go_back=False))

    def _render_content(self, drawer: Drawer):
        drawer.text_centered(
            "Choose an operation mode:", (self.WINDOW_W // 2, 180), 0.8, color=0xDDDDDD
        )
        if not self.buttons:
            btn_w, btn_h = 420, 82
            spacing = 24
            col_x = (self.WINDOW_W - btn_w) // 2
            y1 = 230
            y2 = y1 + btn_h + spacing
            self.buttons.append(
                Button(
                    (col_x, y1, col_x + btn_w, y1 + btn_h),
                    "Create New NavMesh (N)",
                    base_color=0x334455,
                    hotkey=(ord("n"), ord("N")),
                    on_click=lambda: self.stepper.push_step(
                        MapImageSelectStep(
                            title="Select Map For New NavMesh",
                            map_dir=MAP_DIR,
                            on_select=self._on_new_map_selected,
                        )
                    ),
                )
            )
            self.buttons.append(
                Button(
                    (col_x, y2, col_x + btn_w, y2 + btn_h),
                    "Load Existing NavMesh (L)",
                    base_color=0x554433,
                    hotkey=(ord("l"), ord("L")),
                    on_click=lambda: self.stepper.push_step(NavMeshFileSelectStep()),
                )
            )

    def _handle_content_key(self, key):
        return

    def _on_new_map_selected(self, map_file_name: str) -> None:
        navmesh_data = NavMeshData()
        nmf = NavMeshFile()
        imported_entities = 0

        map_path = os.path.join(MAP_DIR, map_file_name)
        img = cv2.imread(map_path)
        if img is not None:
            nmf.geo_height, nmf.geo_width = img.shape[:2]
        nmf.name = os.path.splitext(map_file_name)[0]
        nmf.description = ""

        try:
            parsed = MapName.parse(map_file_name)
            nmf.map_region_name = parsed.map_id
            nmf.map_level_name = parsed.map_level_id
            imported_entities = _import_entities_to_navmesh(
                navmesh_data,
                parsed.map_id,
                parsed.map_level_id,
            )
        except ValueError:
            nmf.map_region_name = ""
            nmf.map_level_name = ""

        stem = os.path.splitext(map_file_name)[0]
        output_path = os.path.join(NAVMESH_DIR, f"{stem}.mtnm")
        if imported_entities > 0:
            print(
                f"{_G}Imported {imported_entities} entity points from map_entities_data.json.{_0}"
            )
        print(
            f"{_G}New NavMesh Meta: Region={nmf.map_region_name}, Level={nmf.map_level_name}, Geo={nmf.geo_width:.0f} x {nmf.geo_height:.0f}{_0}"
        )
        self.stepper.push_step(
            NavMeshEditPage(
                map_path=map_path,
                navmesh_data=navmesh_data,
                navmesh_file=nmf,
                output_path=output_path,
            )
        )


class NavMeshFileSelectStep(StepPage):
    def __init__(self):
        super().__init__(StepData("Select Existing NavMesh File"))
        self.file_list = ScrollableListWidget(item_height=40)
        items = []
        if os.path.isdir(NAVMESH_DIR):
            for root, _, files in os.walk(NAVMESH_DIR):
                for f in files:
                    if f.lower().endswith(".mtnm"):
                        path = os.path.join(root, f)
                        items.append(
                            {
                                "label": f,
                                "sub_label": os.path.dirname(
                                    os.path.relpath(path, NAVMESH_DIR)
                                ).replace(os.path.sep, "/")
                                or ".",
                                "data": path,
                            }
                        )
        items.sort(key=lambda x: (x["sub_label"], x["label"]))
        self.file_list.set_items(items)

    def _render_content(self, drawer: Drawer):
        self.file_list.render(
            drawer, (50, 100, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        )

    def _handle_content_mouse(self, event, x, y, flags, param):
        if self.file_list.consume_mouse(event, x, y, flags):
            if self.file_list.submitted_idx >= 0:
                self._open_loaded_editor(
                    self.file_list.items[self.file_list.submitted_idx]["data"]
                )
            else:
                self.stepper.request_render()
            return

    def _handle_content_key(self, key):
        if self.file_list.consume_key(key):
            if self.file_list.submitted_idx >= 0:
                self._open_loaded_editor(
                    self.file_list.items[self.file_list.submitted_idx]["data"]
                )
            else:
                self.stepper.request_render()
            return

    def _open_loaded_editor(self, file_path: str) -> None:
        try:
            nmf = NavMeshFile.read(file_path)
        except Exception as exc:
            print(f"{_R}Error loading file: {exc}{_0}")
            return

        stem = os.path.splitext(os.path.basename(file_path))[0]
        map_file_name = f"{stem}.png"
        map_path = os.path.join(MAP_DIR, map_file_name)
        if not os.path.exists(map_path):
            print(f"{_R}Map file not found: {map_path}{_0}")
            return

        print(
            f"{_G}Loaded Meta:{_0} Region={nmf.map_region_name}, Level={nmf.map_level_name}, Geo={nmf.geo_width:.0f}x{nmf.geo_height:.0f}{_0}"
        )
        navmesh_data = NavMeshData.from_navmesh_file(nmf)
        self.stepper.push_step(
            NavMeshEditPage(
                map_path=map_path,
                navmesh_data=navmesh_data,
                navmesh_file=nmf,
                output_path=file_path,
            )
        )


def main() -> None:
    print(f"{_G}Welcome to NavMesh Editor (GUI Mode).{_0}")
    try:
        app = PageStepper("MapTracker NavMesh Editor")
        app.push_step(NavMeshModeStep())
        app.run()
    except Exception as exc:
        print(f"{_R}Unexpected error: {exc}{_0}")

    print(f"\n{_G}Done.{_0}")


if __name__ == "__main__":
    main()
