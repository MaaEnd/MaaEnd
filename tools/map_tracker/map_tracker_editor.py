# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "maafw>=5",
#     "opencv-python>=4",
# ]
# ///

# MapTracker - Editor Tool
# This tool provides a GUI to view and edit paths for MapTracker.

import os
import math
import json
import time
import queue
from typing import NamedTuple

from _internal.core_utils import (
    _G,
    _Y,
    _C,
    _0,
    Color,
    Drawer,
    cv2,
    MapName,
    ViewportManager,
    Layer,
    MapImageLayer,
    clipboard_copy_text,
)
from _internal.gui_widgets import (
    BasePage,
    StepData,
    StepPage,
    PageStepper,
    Button,
    SwitchWidget,
    ScrollableListWidget,
    TextInputWidget,
    MapImageSelectStep,
    RadioSelectWidget,
)
from _internal.location_service import LocationService, unique_map_key
from _internal.pipeline_handler import (
    PipelineHandler,
    NODE_TYPE_MOVE,
    NODE_TYPE_ASSERT_LOCATION,
)

MAP_DIR = "assets/resource/image/MapTracker/map"


def _resolve_editor_map_name(map_name: str, map_dir: str) -> str:
    raw_name = str(map_name)
    basename = os.path.basename(raw_name.replace("\\", "/"))
    has_ext = os.path.splitext(basename)[1] != ""
    if has_ext:
        if os.path.exists(os.path.join(map_dir, raw_name)):
            return raw_name
        return find_map_file(raw_name, map_dir) or raw_name
    return find_map_file(raw_name, map_dir) or raw_name


def _handle_view_mouse(
    page: "PathEditPage | AreaEditPage",
    event: int,
    x: int,
    y: int,
    flags: int,
    mx: float,
    my: float,
) -> bool:
    # Mouse wheel: zoom around cursor focus point.
    if event == cv2.EVENT_MOUSEWHEEL:
        if flags > 0:
            page.view.zoom_in()
        else:
            page.view.zoom_out()
        page.view.set_view_origin(mx - x / page.view.zoom, my - y / page.view.zoom)
        page.render_request()
        return True

    # Right-drag panning.
    if event == cv2.EVENT_RBUTTONDOWN:
        page.panning = True
        page.pan_start = (x, y)
        return True
    if event == cv2.EVENT_RBUTTONUP:
        page.panning = False
        return True
    if event == cv2.EVENT_MOUSEMOVE and page.panning:
        dx = (x - page.pan_start[0]) / page.view.zoom
        dy = (y - page.pan_start[1]) / page.view.zoom
        page.view.pan_by(-dx, -dy)
        page.pan_start = (x, y)
        page.render_request()
        return True
    return False


class _PathLayer(Layer):
    def __init__(self, view: ViewportManager, page: "PathEditPage"):
        super().__init__(view)
        self._page = page

    def render(self, drawer: Drawer) -> None:
        points = self._page.points
        active_idx = self._page._get_active_point_idx()
        # Draw path lines
        for i in range(len(points)):
            sx, sy = self.view.get_view_coords(points[i][0], points[i][1])
            if i > 0:
                psx, psy = self.view.get_view_coords(points[i - 1][0], points[i - 1][1])
                drawer.line(
                    (psx, psy),
                    (sx, sy),
                    color=0xFF0000,
                    thickness=max(1, int(self._page.LINE_WIDTH * self.view.zoom**0.5)),
                )

        # Draw point circles
        for i in range(len(points)):
            sx, sy = self.view.get_view_coords(points[i][0], points[i][1])
            radius = int(self._page.POINT_RADIUS * max(0.5, self.view.zoom**0.5))
            drawer.circle(
                (sx, sy),
                radius,
                color=0xFFA500 if i == active_idx else 0xFF0000,
                thickness=-1,
            )
            if i == self._page.selected_idx and self.view.zoom >= 1.0:
                drawer.circle(
                    (sx, sy),
                    max(1, radius - 1),
                    color=0xFF0000,
                    thickness=int(self._page.LINE_WIDTH * self.view.zoom**0.5),
                )

        # Draw point index labels
        for i in range(len(points)):
            if self.view.zoom < 1.0 and i not in (0, len(points) - 1):
                continue
            sx, sy = self.view.get_view_coords(points[i][0], points[i][1])
            drawer.text(str(i), (sx + 5, sy - 5), 0.5, color=0xFFFFFF)


class StatusRecord(NamedTuple):
    """Generic status bar record."""

    timestamp: float
    color: Color
    message: str


class PathEditPage(BasePage):
    """Path editing page"""

    SIDEBAR_W: int = 240
    STATUS_BAR_H: int = 32
    HISTORY_LIMIT = 100
    REALTIME_UNDO_GAP_SEC = 1.0
    LINE_WIDTH = 1.5
    POINT_RADIUS = 4.25
    POINT_SELECTION_THRESHOLD = 10

    @staticmethod
    def _coord1(value: float) -> float:
        return round(float(value), 1)

    def __init__(
        self,
        map_name,
        initial_points=None,
        map_dir=MAP_DIR,
        *,
        pipeline_context: dict | None = None,
        window_name: str = "MapTracker Tool - Path Editor",
    ):
        super().__init__(window_name, 1280, 720)
        self._map_dir = map_dir
        self.map_name = _resolve_editor_map_name(str(map_name), map_dir)
        self._main_map_name = self.map_name
        self._active_map_name = self.map_name
        self.map_path = os.path.join(map_dir, self.map_name)
        self.img = cv2.imread(self.map_path)

        if self.img is None:
            raise ValueError(f"Cannot load map: {self.map_name}")

        self._main_img = self.img.copy()
        self._main_dim_img = cv2.convertScaleAbs(self._main_img, alpha=0.25)
        self.view = ViewportManager(
            self.window_w, self.window_h, zoom=1.0, min_zoom=0.5, max_zoom=10.0
        )
        self._map_layer = MapImageLayer(self.view, self.img)
        self.panning = False
        self.pan_start = (0, 0)
        self._status = StatusRecord(
            time.time(), 0xFFFFFF, "Welcome to MapTracker Editor!"
        )

        self.points = [list(p) for p in initial_points] if initial_points else []
        self._point_snapshot: list[list] = [list(p) for p in self.points]
        self.pipeline_context = pipeline_context  # None → N mode
        self._path_layer = _PathLayer(self.view, self)
        self._fit_view_to_points_or_map()

        self.drag_idx = -1
        self.selected_idx = -1

        # Action state for point interactions (left button)
        self.action_down_idx = -1
        self.action_mouse_down = False
        self.action_down_pos = (0, 0)
        self.action_moved = False
        self.action_dragging = False
        self._drag_history_pushed = False

        self.location_service = LocationService()
        self._undo_stack: list[dict] = []
        self._redo_stack: list[dict] = []
        self._realtime_last_point_ts: float | None = None
        self._realtime_segment_has_checkpoint = False

        # Button hit-rects: (x1, y1, x2, y2) – populated by _render_sidebar
        self._btn_save_rect: tuple | None = None
        self._btn_record_rect: tuple | None = None
        self._btn_back_rect: tuple | None = None
        self._btn_finish_rect: tuple | None = None
        self._btn_delete_rect: tuple | None = None
        self._btn_copy_rect: tuple | None = None
        self._btn_undo_rect: tuple | None = None
        self._btn_redo_rect: tuple | None = None

        # Tier map selector in sidebar (shown only when tier maps exist)
        self._tier_selector = RadioSelectWidget(title="Tiers List", item_height=24)
        self._tier_selector_rect: tuple[int, int, int, int] | None = None
        self._tier_maps = self._collect_tier_maps(self._main_map_name)
        if len(self._tier_maps) > 1:
            self._tier_selector.set_items(self._tier_maps, selected_data=self.map_name)
        self._recorder_mode_switch = SwitchWidget(
            "Loop",
            "Once",
            is_left_selected=True,
            on_changed=self._on_recorder_mode_changed,
        )
        self._recorder_switch_rect: tuple[int, int, int, int] | None = None

        # Sidebar action buttons rendered by BasePage.
        self._save_button = Button(
            (-100, -100, -90, -90),
            "[S] Save",
            base_color=0x3C643C,
            hotkey=(ord("s"), ord("S")),
            on_click=self._on_click_save,
            font_scale=0.45,
        )
        self._record_button = Button(
            (-100, -100, -90, -90),
            "[Enter] Start Recording",
            base_color=0x1A40B8,
            hotkey=(10, 13),
            on_click=self._on_click_record,
            font_scale=0.42,
        )
        self._back_button = Button(
            (-100, -100, -90, -90),
            "Back",
            base_color=0x4C4C64,
            on_click=self._on_click_back,
            font_scale=0.45,
        )
        self._finish_button = Button(
            (-100, -100, -90, -90),
            "Finish",
            base_color=0x3C643C,
            on_click=self._on_click_finish,
            font_scale=0.45,
        )
        self._delete_button = Button(
            (-100, -100, -90, -90),
            "[Del] Delete",
            base_color=0x8C2A22,
            on_click=self._delete_selected_point,
            font_scale=0.42,
        )
        self._copy_button = Button(
            (-100, -100, -90, -90),
            "[C] Copy",
            base_color=0x2E6FD1,
            on_click=self._copy_selected_point,
            font_scale=0.42,
        )
        self.buttons.extend(
            [
                self._save_button,
                self._record_button,
                self._back_button,
                self._finish_button,
                self._delete_button,
                self._copy_button,
            ]
        )

    def hook_idle(self) -> None:
        self._update_recording()

    def hook_exit(self) -> None:
        self.location_service.cleanup()

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    @property
    def is_dirty(self) -> bool:
        """True when current points differ from the initial snapshot."""
        return self.points != self._point_snapshot

    def _get_selected_point(self) -> tuple[int, list[float]] | None:
        active_idx = self._get_active_point_idx()
        if active_idx >= 0:
            return active_idx, self.points[active_idx]
        return None

    def _get_active_point_idx(self) -> int:
        if 0 <= self.drag_idx < len(self.points):
            return self.drag_idx
        if 0 <= self.selected_idx < len(self.points):
            return self.selected_idx
        return -1

    def _reset_realtime_undo_collection(self) -> None:
        self._realtime_last_point_ts = None
        self._realtime_segment_has_checkpoint = False

    @property
    def is_loop_record_mode(self) -> bool:
        return self._recorder_mode_switch.get_value()

    def _on_recorder_mode_changed(self, is_left_selected: bool) -> None:
        if not is_left_selected and self.location_service.is_recording:
            self._stop_recording()
        self.render_request()

    def _capture_single_location(self) -> None:
        try:
            result = self.location_service.infer_once(self.map_name)
            map_name, x, y = result["map_name"], result["x"], result["y"]
            if map_name:
                self._sync_tier_by_log_map(map_name)
            updated = self._append_realtime_point(x, y)
            self._update_status(
                0x50DC50 if updated else 0xD2D200,
                "Captured current coordinate.",
            )
        except Exception as e:
            self._update_status(0xFC4040, f"Single coordinate capture failed: {e}")
        self.render_request()

    def _capture_point_state(self) -> dict:
        return {
            "points": [list(p) for p in self.points],
            "selected_idx": self.selected_idx,
        }

    def _restore_point_state(self, state: dict) -> None:
        self.points = [
            [self._coord1(p[0]), self._coord1(p[1])] for p in state.get("points", [])
        ]
        selected_idx = int(state.get("selected_idx", -1))
        if not self.points or selected_idx < 0:
            self.selected_idx = -1
        else:
            self.selected_idx = min(selected_idx, len(self.points) - 1)
        if self.drag_idx >= len(self.points):
            self.drag_idx = -1

    def _replace_points(
        self,
        new_points: list[list[float]],
        *,
        selected_idx: int | None = None,
        push_history: bool = True,
    ) -> bool:
        normalized_points = [
            [self._coord1(p[0]), self._coord1(p[1])] for p in new_points
        ]
        next_selected_idx = self.selected_idx if selected_idx is None else selected_idx
        next_state = {
            "points": normalized_points,
            "selected_idx": next_selected_idx,
        }
        current_state = self._capture_point_state()
        if next_state == current_state:
            return False
        if push_history:
            self._reset_realtime_undo_collection()
            self._push_current_state_to_undo()
        self._restore_point_state(next_state)
        return True

    def _push_current_state_to_undo(self) -> None:
        current_state = self._capture_point_state()
        if not self._undo_stack or self._undo_stack[-1] != current_state:
            self._undo_stack.append(current_state)
            if len(self._undo_stack) > self.HISTORY_LIMIT:
                self._undo_stack.pop(0)
        self._redo_stack.clear()

    def _undo_points_change(self) -> None:
        if not self._undo_stack:
            return
        self._reset_realtime_undo_collection()
        current_state = self._capture_point_state()
        previous_state = self._undo_stack.pop()
        if not self._redo_stack or self._redo_stack[-1] != current_state:
            self._redo_stack.append(current_state)
            if len(self._redo_stack) > self.HISTORY_LIMIT:
                self._redo_stack.pop(0)
        self._restore_point_state(previous_state)
        self._update_status(0xD2D200, "Reverted the previous point change.")
        self.render_request()

    def _redo_points_change(self) -> None:
        if not self._redo_stack:
            return
        self._reset_realtime_undo_collection()
        current_state = self._capture_point_state()
        next_state = self._redo_stack.pop()
        if not self._undo_stack or self._undo_stack[-1] != current_state:
            self._undo_stack.append(current_state)
            if len(self._undo_stack) > self.HISTORY_LIMIT:
                self._undo_stack.pop(0)
        self._restore_point_state(next_state)
        self._update_status(0x78DCFF, "Reapplied the reverted point change.")
        self.render_request()

    def _append_realtime_point(self, x: float, y: float) -> bool:
        ts = time.time()
        new_point = [self._coord1(x), self._coord1(y)]
        if self.points and new_point == self.points[-1]:
            target_idx = len(self.points) - 1
            selection_changed = self.selected_idx != target_idx
            self.selected_idx = target_idx
            self._realtime_last_point_ts = ts
            return selection_changed

        next_points = [list(p) for p in self.points]
        # Keep the old "generate from recorded history" pop-then-append simplifier.
        if len(next_points) >= 2 and self._can_simplify(
            tuple(next_points[-2]), tuple(next_points[-1]), tuple(new_point)
        ):
            next_points.pop()
        next_points.append(new_point)
        target_idx = len(next_points) - 1

        should_push_checkpoint = False
        if self._realtime_last_point_ts is None:
            self._realtime_segment_has_checkpoint = False
        else:
            delta = ts - self._realtime_last_point_ts
            if delta > self.REALTIME_UNDO_GAP_SEC:
                should_push_checkpoint = True
                self._realtime_segment_has_checkpoint = False
            elif not self._realtime_segment_has_checkpoint:
                should_push_checkpoint = True
                self._realtime_segment_has_checkpoint = True

        if should_push_checkpoint:
            self._push_current_state_to_undo()

        if not self._replace_points(
            next_points,
            selected_idx=target_idx,
            push_history=False,
        ):
            self._realtime_last_point_ts = ts
            return False
        self._realtime_last_point_ts = ts

        if not self.action_mouse_down and not self.panning:
            self.view.maybe_center_to(new_point[0], new_point[1])
        return True

    def _delete_selected_point(self) -> None:
        selected = self._get_selected_point()
        if selected is None:
            return
        del_idx, deleted_point = selected
        next_points = [list(p) for p in self.points]
        next_points.pop(del_idx)
        next_selected_idx = min(del_idx, len(next_points) - 1) if next_points else -1
        self._replace_points(
            next_points,
            selected_idx=next_selected_idx,
            push_history=True,
        )
        if self.drag_idx == del_idx:
            self.drag_idx = -1
        elif self.drag_idx > del_idx:
            self.drag_idx -= 1
        self._update_status(
            0x78DCFF,
            f"Deleted Point #{del_idx} ({deleted_point[0]:.1f}, {deleted_point[1]:.1f})",
        )
        self.render_request()

    def _copy_selected_point(self) -> None:
        selected = self._get_selected_point()
        if selected is None:
            return
        point_idx, point = selected
        text = json.dumps([self._coord1(point[0]), self._coord1(point[1])])
        if clipboard_copy_text(text):
            self._update_status(0x50DC50, f"Copied Point #{point_idx} coordinates.")
        else:
            self._update_status(0xFC4040, "Failed to copy point coordinates.")
        self.render_request()

    def _fit_view_to_points_or_map(self) -> None:
        if self.points:
            self.view.fit_to(self.points)
            return
        img_h, img_w = self.img.shape[:2]
        self.view.fit_to([(0, 0), (img_w, img_h)], padding=0.02)

    def _collect_tier_maps(self, main_map_name: str) -> list[dict]:
        main_base = os.path.basename(main_map_name)
        try:
            main_parsed = MapName.parse(main_base)
        except ValueError:
            return [{"label": "main", "data": main_base}]

        tiers: list[dict] = [{"label": "main", "data": main_base}]
        if not os.path.isdir(self._map_dir):
            return tiers

        for file_name in sorted(os.listdir(self._map_dir), key=lambda n: n.lower()):
            try:
                parsed = MapName.parse(file_name)
            except ValueError:
                continue
            if (
                parsed.map_type != "tier"
                or parsed.map_id != main_parsed.map_id
                or parsed.map_level_id != main_parsed.map_level_id
            ):
                continue
            tiers.append({"label": f"tier_{parsed.tier_suffix}", "data": file_name})
        return tiers

    def _switch_active_map(self, map_name: str) -> None:
        if map_name == self._active_map_name:
            return
        if map_name == self._main_map_name:
            target_path = os.path.join(self._map_dir, self._main_map_name)
            img = self._main_img
        else:
            target_path = os.path.join(self._map_dir, map_name)
            tier_img = cv2.imread(target_path)
            if tier_img is None:
                return
            # Compose once: dimmed main as base, tier non-black pixels as overlay.
            img = self._main_dim_img.copy()
            tier_mask = (
                (tier_img[:, :, 0] > 2)
                | (tier_img[:, :, 1] > 2)
                | (tier_img[:, :, 2] > 2)
            )
            img[tier_mask] = tier_img[tier_mask]
        self._active_map_name = map_name
        self.map_path = target_path
        self.img = img
        self._map_layer = MapImageLayer(self.view, self.img)
        self.render_request()

    def _sync_tier_by_log_map(self, log_map_name: str) -> None:
        if len(self._tier_maps) <= 1:
            return
        resolved = find_map_file(log_map_name, self._map_dir)
        if not resolved:
            return
        available = {str(item.get("data", "")) for item in self._tier_maps}
        if resolved not in available:
            return
        self._tier_selector.select_by_data(resolved)
        if resolved != self._active_map_name:
            self._switch_active_map(resolved)

    def _do_save(self):
        if self.pipeline_context is None:
            return
        handler: PipelineHandler = self.pipeline_context["handler"]
        node_name: str = self.pipeline_context["node_name"]
        if handler.replace_path(node_name, self.points):
            self._point_snapshot = [list(p) for p in self.points]
            self._update_status(0x50DC50, "Saved changes!")
            print(f"  {_G}Path saved to file.{_0}")
        else:
            self._update_status(0xFC4040, "Failed to save changes!")
            print(f"  {_Y}Failed to save path to file.{_0}")

    def _start_recording(self):
        if not self.location_service.start_recording(self.map_name):
            error_msg = "Cannot start recording."
            try:
                item = self.location_service.result_queue.get_nowait()
                if isinstance(item, Exception):
                    error_msg = str(item)
            except queue.Empty:
                pass
            self._update_status(0xFC4040, error_msg)
            self.render_request()
            return
        if self._replace_points([], selected_idx=-1, push_history=True):
            self._update_status(
                0x78DCFF, "Realtime path recording started from empty path."
            )
        else:
            self._update_status(0x78DCFF, "Realtime path recording started.")
        self.render_request()

    def _stop_recording(self):
        self.location_service.stop_recording()
        self._update_status(0xD2D200, "Realtime path recording stopped.")
        self.render_request()

    def _toggle_recording(self):
        if self.location_service.is_recording:
            self._stop_recording()
        else:
            self._start_recording()

    def _on_click_save(self):
        if self.pipeline_context and self.is_dirty:
            self._do_save()
            self.render_request()

    def _on_click_record(self):
        if self.is_loop_record_mode:
            self._toggle_recording()
        else:
            self._capture_single_location()
        self.render_request()

    def _on_click_back(self):
        if self.stepper and len(self.stepper.step_history) > 1:
            self.stepper.pop_step()
        else:
            self.done = True

    def _on_click_finish(self):
        self.done = True

    def _update_recording(self):
        if not self.location_service.is_recording:
            return False

        updated = False
        exception = None
        while True:
            try:
                result = self.location_service.result_queue.get_nowait()
                if isinstance(result, Exception):
                    exception = result
                    continue

                map_name, x, y = result["map_name"], result["x"], result["y"]

                if map_name:
                    self._sync_tier_by_log_map(map_name)

                updated = self._append_realtime_point(x, y) or updated
            except queue.Empty:
                break
            except Exception as e:
                print(f"  {_Y}Error processing location queue: {e}{_0}")
                break

        if updated:
            self._update_status(0x78DCFF, "Location recording is working normally.")
            self.render_request()
        elif exception:
            self._update_status(0xD2D200, "Location recording currently unavailable.")
            self.render_request()

        return updated

    @staticmethod
    def _can_simplify(
        prev_p: tuple[float, float],
        mid_p: tuple[float, float],
        next_p: tuple[float, float],
        k: float = 2.0,
    ) -> bool:
        if k < 1:
            raise ValueError("k must be >= 1")
        prev_next_dx, prev_next_dy = next_p[0] - prev_p[0], next_p[1] - prev_p[1]
        d_prev_next = math.hypot(prev_next_dx, prev_next_dy)
        if d_prev_next < (k - 1) + 1e-6:
            return True
        mid_next_dx, mid_next_dy = next_p[0] - mid_p[0], next_p[1] - mid_p[1]
        sin_prev_next_sub_mid_next = abs(
            prev_next_dx * mid_next_dy - prev_next_dy * mid_next_dx
        ) / (d_prev_next * math.hypot(mid_next_dx, mid_next_dy) + 1e-6)
        # y = arcsin(k / (x + 1)) -> sin(y) = k / (x + 1) -> sin(y) * (x + 1) = k
        return sin_prev_next_sub_mid_next * (d_prev_next + 1) < k

    def _get_map_coords(self, screen_x, screen_y):
        mx, my = self.view.get_real_coords(screen_x, screen_y)
        return self._coord1(mx), self._coord1(my)

    def _get_screen_coords(self, map_x, map_y):
        return self.view.get_view_coords(map_x, map_y)

    def _is_on_line(self, cmx, cmy, p1, p2, threshold=10):
        x1, y1 = p1
        x2, y2 = p2
        px, py = cmx, cmy
        dx = x2 - x1
        dy = y2 - y1
        if dx == 0 and dy == 0:
            return math.hypot(px - x1, py - y1) < threshold
        t = max(0, min(1, ((px - x1) * dx + (py - y1) * dy) / (dx * dx + dy * dy)))
        closest_x = x1 + t * dx
        closest_y = y1 + t * dy
        dist = math.hypot(px - closest_x, py - closest_y)
        return dist < threshold

    # ------------------------------------------------------------------
    # Rendering overrides
    # ------------------------------------------------------------------

    def _render_once(self, drawer: Drawer) -> None:
        self._map_layer.render(drawer)
        self._render_content(drawer)

        # Crosshair
        drawer.crosshair(self.mouse_pos, color=0xFFFF00, thickness=1)

        self._render_ui(drawer)

    def _render_content(self, drawer: Drawer) -> None:
        self._path_layer.render(drawer)

    def _update_status(self, color, message: str) -> None:
        self._status = StatusRecord(time.time(), color, message)

    def _render_status_bar(self, drawer: Drawer) -> None:
        x1 = self.SIDEBAR_W
        x2 = self.window_w
        y2 = self.window_h
        y1 = max(0, y2 - self.STATUS_BAR_H)
        drawer.rect((x1, y1), (x2, y2), color=0x000000, thickness=-1)
        if self._status:
            drawer.text(
                self._status.message, (x1 + 10, y2 - 10), 0.45, color=self._status.color
            )

    def _render_sidebar_bg(self, drawer: Drawer) -> None:
        sw = self.SIDEBAR_W
        h = self.window_h
        drawer.rect((0, 0), (sw, h), color=0x000000, thickness=-1)
        drawer.line((sw - 1, 0), (sw - 1, h), color=0xFFFFFF, thickness=1)

    def _render_ui(self, drawer: Drawer) -> None:
        self._render_status_bar(drawer)
        self._render_sidebar_bg(drawer)
        self._render_sidebar(drawer)

    @staticmethod
    def _hit_button(x: int, y: int, rect: tuple[int, int, int, int] | None) -> bool:
        if rect is None:
            return False
        x1, y1, x2, y2 = rect
        return x1 <= x <= x2 and y1 <= y <= y2

    def _render_attribute_panel(
        self,
        drawer: "Drawer",
        *,
        x0: int,
        y0: int,
        panel_w: int,
    ) -> int:
        selected = self._get_selected_point()
        hidden_rect = (-100, -100, -90, -90)
        self._delete_button.rect = hidden_rect
        self._copy_button.rect = hidden_rect
        self._btn_delete_rect = None
        self._btn_copy_rect = None

        if selected is None:
            return y0

        point_idx, point = selected
        panel_h = 108
        x1 = x0
        y1 = y0
        x2 = x0 + panel_w
        y2 = y1 + panel_h
        drawer.rect((x1, y1), (x2, y2), color=0x0A0A14, thickness=-1)
        drawer.rect((x1, y1), (x2, y2), color=0x223044, thickness=1)
        drawer.text("[ Attribute ]", (x1 + 8, y1 + 16), 0.45, color=0x40FFFF)

        item_y1 = y1 + 24
        item_y2 = item_y1 + 42
        drawer.rect((x1 + 2, item_y1), (x2 - 2, item_y2), color=0x132B4F, thickness=-1)
        cy_mark = item_y1 + (item_y2 - item_y1) // 2
        mark_x = x1 + 14
        drawer.circle((mark_x, cy_mark), 6, color=0xFFFFFF, thickness=1)
        drawer.circle((mark_x, cy_mark), 3, color=0xFFFFFF, thickness=-1)
        drawer.text(
            f"Point #{point_idx}", (x1 + 26, item_y1 + 16), 0.42, color=0xFFFFFF
        )
        detail_line = f"No. {point_idx} | ({point[0]:.1f}, {point[1]:.1f})"
        drawer.text(detail_line, (x1 + 26, item_y2 - 8), 0.36, color=0xC8C8C8)

        btn_h = 30
        btn_gap = 8
        btn_y0 = item_y2 + 8
        btn_y1 = btn_y0 + btn_h
        btn_w = (panel_w - btn_gap) // 2

        self._btn_delete_rect = (x0, btn_y0, x0 + btn_w, btn_y1)
        self._delete_button.rect = self._btn_delete_rect
        self._delete_button.text = "[Del] Delete"
        self._delete_button.text_color = 0xFFFFFF

        copy_x0 = x0 + btn_w + btn_gap
        self._btn_copy_rect = (copy_x0, btn_y0, copy_x0 + btn_w, btn_y1)
        self._copy_button.rect = self._btn_copy_rect
        self._copy_button.text = "[C] Copy"
        self._copy_button.text_color = 0xFFFFFF

        return y2 + 12

    def _render_sidebar(self, drawer: "Drawer"):
        self._render_sidebar_bg(drawer)
        sw = self.SIDEBAR_W
        h = self.window_h
        pad = 15
        divider_color = 0x18202C

        def _draw_section_divider(
            y: int,
            *,
            gap_before: int = 0,
            gap_after: int = 12,
        ) -> int:
            y += gap_before
            drawer.line(
                (pad, y),
                (sw - pad, y),
                color=divider_color,
                thickness=1,
            )
            return y + gap_after

        # ── Tips section ─────────────────────────────────────────────────
        cy = pad + 15
        drawer.text("[ Mouse Tips ]", (pad, cy), 0.5, color=0x40FFFF)
        cy += 10
        tips = [
            "Left Click: Select/Add Point",
            "Left Drag: Move Point",
            "Right Drag: Move Map",
            "Scroll: Zoom",
        ]
        for line in tips:
            cy += 20
            drawer.text(line, (pad, cy), 0.4, color=0xC8C8C8)
        cy = _draw_section_divider(cy, gap_before=12, gap_after=16)

        drawer.text("[ Recorder ]", (pad, cy), 0.5, color=0x40FFFF)
        cy += 12
        switch_h = 26
        self._recorder_switch_rect = (pad, cy, sw - pad, cy + switch_h)
        self._recorder_mode_switch.render(
            drawer,
            self._recorder_switch_rect,
            font_scale=0.4,
        )
        cy += switch_h + 12

        # ── Buttons ──────────────────────────────────────────────────────
        btn_h = 30
        btn_w = sw - pad * 2
        btn_x0 = pad
        has_pipeline = self.pipeline_context is not None
        dirty = self.is_dirty

        hidden_rect = (-100, -100, -90, -90)
        self._save_button.rect = hidden_rect
        self._record_button.rect = hidden_rect
        self._back_button.rect = hidden_rect
        self._finish_button.rect = hidden_rect
        self._delete_button.rect = hidden_rect
        self._copy_button.rect = hidden_rect
        self._btn_undo_rect = None
        self._btn_redo_rect = None

        self._btn_save_rect = None

        record_y0 = cy
        record_y1 = cy + btn_h
        self._btn_record_rect = (btn_x0, record_y0, btn_x0 + btn_w, record_y1)
        self._record_button.rect = self._btn_record_rect
        if self.is_loop_record_mode:
            is_recording = self.location_service.is_recording
            self._record_button.base_color = 0xB44022 if is_recording else 0x1A40B8
            self._record_button.text = (
                "[Enter] Stop Recording"
                if is_recording
                else "[Enter] Start Recording"
            )
        else:
            self._record_button.base_color = 0x1A40B8
            self._record_button.text = "[Enter] Get Location"
        self._record_button.text_color = 0xFFFFFF
        cy = record_y1 + 12
        cy = _draw_section_divider(cy, gap_after=14)

        self._tier_selector_rect = None
        rendered_info_panel = False
        if self._get_selected_point() is not None:
            cy = self._render_attribute_panel(
                drawer,
                x0=pad,
                y0=cy,
                panel_w=btn_w,
            )
            rendered_info_panel = True
        elif len(self._tier_maps) > 1:
            tier_h = self._tier_selector.get_height()
            self._tier_selector_rect = (pad, cy, sw - pad, cy + tier_h)
            self._tier_selector.render(
                drawer,
                self._tier_selector_rect,
                font_scale=0.4,
            )
            cy += tier_h + 12
            rendered_info_panel = True
        if rendered_info_panel:
            cy = _draw_section_divider(cy, gap_after=12)

        back_y0 = cy
        back_y1 = cy + btn_h
        self._btn_back_rect = (btn_x0, back_y0, btn_x0 + btn_w, back_y1)
        self._back_button.rect = self._btn_back_rect
        self._back_button.text = "Back"
        self._back_button.base_color = 0x4C4C64
        self._back_button.text_color = 0xFFFFFF
        cy = back_y1 + 8

        if has_pipeline:
            save_y0 = cy
            save_y1 = cy + btn_h
            self._btn_save_rect = (btn_x0, save_y0, btn_x0 + btn_w, save_y1)
            self._save_button.rect = self._btn_save_rect
            self._save_button.text = "[S] Save"
            self._save_button.base_color = 0x64C800 if dirty else 0x3C643C
            self._save_button.text_color = 0xFFFFFF if dirty else 0x648264
            cy = save_y1 + 8

        finish_y0 = cy
        finish_y1 = cy + btn_h
        self._btn_finish_rect = (btn_x0, finish_y0, btn_x0 + btn_w, finish_y1)
        self._finish_button.rect = self._btn_finish_rect
        self._finish_button.text = "Finish"
        self._finish_button.base_color = 0x4C4C64 if has_pipeline else 0x3C643C
        self._finish_button.text_color = 0xFFFFFF
        cy = finish_y1 + 12
        cy = _draw_section_divider(cy, gap_after=8)

        # Status messages moved to map area status bar

        # ── Status section (bottom) ──────────────────────────────────────
        status_zoom_y = h - 80
        status_point_y = h - 55
        history_btn_h = 22
        history_btn_y0 = h - 32
        history_btn_y1 = history_btn_y0 + history_btn_h
        history_btn_gap = 8
        history_btn_w = (btn_w - history_btn_gap) // 2

        drawer.text(
            f"Zoom: {self.view.zoom:.2f}x", (pad, status_zoom_y), 0.45, color=0xD2D200
        )

        active_point = self._get_selected_point()
        if active_point is not None:
            point_idx, p = active_point
            line = f"Point #{point_idx} ({p[0]:.1f}, {p[1]:.1f})"
        else:
            line = f"Points: {len(self.points)}"
        drawer.text(line, (pad, status_point_y), 0.45, color=0xFFFFFF)

        def _render_history_button(
            label: str,
            rect: tuple[int, int, int, int],
            *,
            enabled: bool,
            color: int,
        ) -> None:
            bx1, by1, bx2, by2 = rect
            drawer.rect(
                (bx1, by1),
                (bx2, by2),
                color=color if enabled else 0x303030,
                thickness=-1,
            )
            drawer.rect((bx1, by1), (bx2, by2), color=0xB4B4B4, thickness=1)
            drawer.text_centered(
                label,
                ((bx1 + bx2) // 2, by2 - 5),
                0.38,
                color=0xFFFFFF if enabled else 0x707070,
            )

        self._btn_undo_rect = (
            pad,
            history_btn_y0,
            pad + history_btn_w,
            history_btn_y1,
        )
        _render_history_button(
            "[Z] Undo",
            self._btn_undo_rect,
            enabled=bool(self._undo_stack),
            color=0xB44022,
        )

        redo_x0 = pad + history_btn_w + history_btn_gap
        self._btn_redo_rect = (
            redo_x0,
            history_btn_y0,
            redo_x0 + history_btn_w,
            history_btn_y1,
        )
        _render_history_button(
            "[Y] Redo",
            self._btn_redo_rect,
            enabled=bool(self._redo_stack),
            color=0x2E6FD1,
        )

    # ------------------------------------------------------------------
    # Mouse / keyboard / idle
    # ------------------------------------------------------------------

    def _get_point_at(self, x, y) -> int:
        for i, p in enumerate(self.points):
            sx, sy = self._get_screen_coords(p[0], p[1])
            dist = math.hypot(x - sx, y - sy)
            if dist < self.POINT_SELECTION_THRESHOLD:
                return i
        return -1

    def _on_mouse(self, event, x, y, flags, param) -> None:
        mx, my = self._get_map_coords(x, y)

        if _handle_view_mouse(self, event, x, y, flags, mx, my):
            return

        if event == cv2.EVENT_MOUSEMOVE:
            if self.action_mouse_down:
                if self.action_dragging and self.drag_idx != -1:
                    next_points = [list(p) for p in self.points]
                    next_points[self.drag_idx] = [self._coord1(mx), self._coord1(my)]
                    changed = self._replace_points(
                        next_points,
                        selected_idx=self.drag_idx,
                        push_history=not self._drag_history_pushed,
                    )
                    if changed and not self._drag_history_pushed:
                        self._drag_history_pushed = True
                    self.action_moved = True
                    self.render_request()
                    return

                dx = x - self.action_down_pos[0]
                dy = y - self.action_down_pos[1]
                if dx * dx + dy * dy > 25:
                    self.action_moved = True
                    if self.action_down_idx != -1:
                        self.action_dragging = True
                        self.drag_idx = self.action_down_idx
                        next_points = [list(p) for p in self.points]
                        next_points[self.drag_idx] = [
                            self._coord1(mx),
                            self._coord1(my),
                        ]
                        changed = self._replace_points(
                            next_points,
                            selected_idx=self.drag_idx,
                            push_history=not self._drag_history_pushed,
                        )
                        if changed and not self._drag_history_pushed:
                            self._drag_history_pushed = True
                        self.render_request()
                        return

            if (flags & cv2.EVENT_FLAG_LBUTTON) and self.drag_idx != -1:
                next_points = [list(p) for p in self.points]
                next_points[self.drag_idx] = [self._coord1(mx), self._coord1(my)]
                changed = self._replace_points(
                    next_points,
                    selected_idx=self.drag_idx,
                    push_history=not self._drag_history_pushed,
                )
                if changed and not self._drag_history_pushed:
                    self._drag_history_pushed = True
                self.action_dragging = True
                self.render_request()
                return

            # Keep crosshair and hover feedback responsive.
            self.render_request()

        elif event == cv2.EVENT_LBUTTONDOWN:
            if self._hit_button(x, y, self._btn_undo_rect):
                self._undo_points_change()
                self.render_request()
                return
            if self._hit_button(x, y, self._btn_redo_rect):
                self._redo_points_change()
                self.render_request()
                return

            # Sidebar action buttons are handled by BasePage/Button.
            if x < self.SIDEBAR_W:
                if (
                    self._recorder_switch_rect is not None
                    and self._recorder_mode_switch.handle_click(
                        x,
                        y,
                        self._recorder_switch_rect,
                    )
                ):
                    self.render_request()
                    return
                if self._get_selected_point() is not None:
                    self.selected_idx = -1
                    self._update_status(0xD2D200, "Cleared point selection.")
                    self.render_request()
                    return
                if self._tier_selector_rect is not None:
                    idx = self._tier_selector.handle_click(
                        x,
                        y,
                        self._tier_selector_rect,
                    )
                    if idx >= 0:
                        selected_map = self._tier_selector.get_selected_data()
                        if isinstance(selected_map, str) and selected_map:
                            self._switch_active_map(selected_map)
                return

            # ── Map area clicks ─────────────────────────────────
            self.action_down_idx = self._get_point_at(x, y)
            self.action_mouse_down = True
            self.action_down_pos = (x, y)
            self.action_moved = False
            self.action_dragging = False
            self._drag_history_pushed = False
            if self.action_down_idx != -1:
                self.drag_idx = self.action_down_idx
                self.selected_idx = self.action_down_idx
                self.render_request()

        elif event == cv2.EVENT_LBUTTONUP:
            if self.action_dragging and self.drag_idx != -1:
                self.drag_idx = -1
            else:
                if not (self.action_moved and self.action_down_idx == -1):
                    if self.action_down_idx != -1:
                        select_idx = self.action_down_idx
                        if 0 <= select_idx < len(self.points):
                            self.selected_idx = select_idx
                            selected_point = self.points[select_idx]
                            self._update_status(
                                0x78DCFF,
                                f"Selected Point #{select_idx} ({selected_point[0]:.1f}, {selected_point[1]:.1f})",
                            )
                            self.render_request()
                    elif self.action_down_pos == (x, y):
                        inserted = False
                        for i in range(1, len(self.points)):
                            map_threshold = self.POINT_SELECTION_THRESHOLD / max(
                                0.01, self.view.zoom
                            )
                            if self._is_on_line(
                                mx,
                                my,
                                self.points[i - 1],
                                self.points[i],
                                threshold=map_threshold,
                            ):
                                next_points = [list(p) for p in self.points]
                                next_points.insert(
                                    i, [self._coord1(mx), self._coord1(my)]
                                )
                                self._replace_points(
                                    next_points,
                                    selected_idx=i,
                                    push_history=True,
                                )
                                self._update_status(
                                    0x78DCFF,
                                    f"Added Point #{i} ({mx:.1f}, {my:.1f})",
                                )
                                inserted = True
                                self.render_request()
                                break
                        if not inserted:
                            next_points = [list(p) for p in self.points]
                            next_points.append([self._coord1(mx), self._coord1(my)])
                            next_selected_idx = len(next_points) - 1
                            self._replace_points(
                                next_points,
                                selected_idx=next_selected_idx,
                                push_history=True,
                            )
                            self._update_status(
                                0x78DCFF,
                                f"Added Point #{next_selected_idx} ({mx:.1f}, {my:.1f})",
                            )
                            self.render_request()

            self.action_down_idx = -1
            self.action_mouse_down = False
            self.action_down_pos = (0, 0)
            self.action_moved = False
            self.action_dragging = False
            self._drag_history_pushed = False

    def _on_key(self, key: int) -> None:
        if key in (ord("s"), ord("S")) and self.pipeline_context and self.is_dirty:
            self._do_save()
            self.render_request()
        elif key in (46, 0x2E0000):
            self._delete_selected_point()
        elif key in (ord("c"), ord("C")):
            self._copy_selected_point()
        elif key in (ord("z"), ord("Z")):
            self._undo_points_change()
        elif key in (ord("y"), ord("Y")):
            self._redo_points_change()

    # ------------------------------------------------------------------
    # Main loop
    # ------------------------------------------------------------------

    def run(self) -> list[list]:
        super().run()
        return [list(p) for p in self.points]


class AreaEditPage(BasePage):
    SIDEBAR_W: int = 240
    STATUS_BAR_H: int = 32

    @staticmethod
    def _coord1(value: float) -> float:
        return round(float(value), 1)

    def __init__(
        self,
        map_name,
        initial_target=None,
        map_dir=MAP_DIR,
        *,
        pipeline_context: dict | None = None,
        window_name: str = "MapTracker Tool - Area Editor",
    ):
        super().__init__(window_name, 1280, 720)
        self.map_name = _resolve_editor_map_name(str(map_name), map_dir)
        self.map_path = os.path.join(map_dir, self.map_name)
        self.img = cv2.imread(self.map_path)
        if self.img is None:
            raise ValueError(f"Cannot load map: {self.map_name}")

        self.view = ViewportManager(
            self.window_w, self.window_h, zoom=1.0, min_zoom=0.5, max_zoom=10.0
        )
        self._map_layer = MapImageLayer(self.view, self.img)
        self.panning = False
        self.pan_start = (0, 0)
        self._status = StatusRecord(time.time(), 0xFFFFFF, "Welcome to Area Editor!")

        self.pipeline_context = pipeline_context
        self.target: list[float] | None = None
        if initial_target and len(initial_target) == 4:
            self.target = [self._coord1(v) for v in initial_target]
        self._target_snapshot = list(self.target) if self.target is not None else None
        self._fit_view_to_target_or_map()

        self._drawing = False
        self._draw_start: tuple[float, float] | None = None

        self._save_button = Button(
            (-100, -100, -90, -90),
            "[S] Save",
            base_color=0x3C643C,
            hotkey=(ord("s"), ord("S")),
            on_click=self._on_click_save,
            font_scale=0.45,
        )
        self._back_button = Button(
            (-100, -100, -90, -90),
            "Back",
            base_color=0x4C4C64,
            on_click=self._on_click_back,
            font_scale=0.45,
        )
        self._finish_button = Button(
            (-100, -100, -90, -90),
            "Finish",
            base_color=0x3C643C,
            on_click=self._on_click_finish,
            font_scale=0.45,
        )
        self.buttons.extend([self._save_button, self._back_button, self._finish_button])

    @property
    def is_dirty(self) -> bool:
        return self.target != self._target_snapshot

    def _get_map_coords(self, screen_x, screen_y):
        mx, my = self.view.get_real_coords(screen_x, screen_y)
        return self._coord1(mx), self._coord1(my)

    def _get_screen_coords(self, map_x, map_y):
        return self.view.get_view_coords(map_x, map_y)

    def _normalized_target(
        self, p1: tuple[float, float], p2: tuple[float, float]
    ) -> list[float]:
        x1, y1 = p1
        x2, y2 = p2
        left = min(x1, x2)
        top = min(y1, y2)
        w = abs(x2 - x1)
        h = abs(y2 - y1)
        return [self._coord1(left), self._coord1(top), self._coord1(w), self._coord1(h)]

    def _fit_view_to_target_or_map(self) -> None:
        if self.target is not None:
            x, y, w, h = self.target
            self.view.fit_to([(x, y), (x + w, y + h)], padding=0.2)
            return
        img_h, img_w = self.img.shape[:2]
        self.view.fit_to([(0, 0), (img_w, img_h)], padding=0.02)

    def _update_status(self, color, message: str) -> None:
        self._status = StatusRecord(time.time(), color, message)

    def _do_save(self):
        if self.pipeline_context is None or self.target is None:
            return
        handler: PipelineHandler = self.pipeline_context["handler"]
        node_name: str = self.pipeline_context["node_name"]
        raw_map_name = self.pipeline_context.get("original_map_name", self.map_name)
        map_name_stem = os.path.splitext(os.path.basename(raw_map_name))[0]
        if handler.replace_assert_location(node_name, map_name_stem, self.target):
            self._target_snapshot = list(self.target)
            self._update_status(0x50DC50, "Saved changes!")
            print(f"  {_G}Area saved to file.{_0}")
        else:
            self._update_status(0xFC4040, "Failed to save changes!")
            print(f"  {_Y}Failed to save area to file.{_0}")

    def _on_click_save(self):
        if self.pipeline_context and self.is_dirty and self.target is not None:
            self._do_save()
            self.render_request()

    def _on_click_back(self):
        if self.stepper and len(self.stepper.step_history) > 1:
            self.stepper.pop_step()
        else:
            self.done = True

    def _on_click_finish(self):
        self.done = True

    def _render_status_bar(self, drawer: Drawer) -> None:
        x1 = self.SIDEBAR_W
        x2 = self.window_w
        y2 = self.window_h
        y1 = max(0, y2 - self.STATUS_BAR_H)
        drawer.rect((x1, y1), (x2, y2), color=0x000000, thickness=-1)
        if self._status:
            drawer.text(
                self._status.message, (x1 + 10, y2 - 10), 0.45, color=self._status.color
            )

    def _render_sidebar_bg(self, drawer: Drawer) -> None:
        sw = self.SIDEBAR_W
        h = self.window_h
        drawer.rect((0, 0), (sw, h), color=0x000000, thickness=-1)
        drawer.line((sw - 1, 0), (sw - 1, h), color=0xFFFFFF, thickness=1)

    def _render_ui(self, drawer: Drawer) -> None:
        self._render_status_bar(drawer)
        self._render_sidebar_bg(drawer)

        sw = self.SIDEBAR_W
        h = self.window_h
        pad = 15
        cy = pad + 15
        drawer.text("[ Mouse Tips ]", (pad, cy), 0.5, color=0x40FFFF)
        cy += 10
        for line in [
            "Left Drag: Draw Rectangle",
            "Right Drag: Move Map",
            "Scroll: Zoom",
        ]:
            cy += 20
            drawer.text(line, (pad, cy), 0.4, color=0xC8C8C8)
        cy += 20

        btn_h = 30
        btn_w = sw - pad * 2
        btn_x0 = pad
        hidden_rect = (-100, -100, -90, -90)
        self._save_button.rect = hidden_rect
        self._back_button.rect = hidden_rect
        self._finish_button.rect = hidden_rect

        self._back_button.rect = (btn_x0, cy, btn_x0 + btn_w, cy + btn_h)
        self._back_button.base_color = 0x4C4C64
        self._back_button.text_color = 0xFFFFFF
        cy += btn_h + 8

        has_pipeline = self.pipeline_context is not None
        if has_pipeline:
            self._save_button.rect = (btn_x0, cy, btn_x0 + btn_w, cy + btn_h)
            self._save_button.base_color = 0x64C800 if self.is_dirty else 0x3C643C
            self._save_button.text_color = 0xFFFFFF if self.is_dirty else 0x648264
            cy += btn_h + 8

        self._finish_button.rect = (btn_x0, cy, btn_x0 + btn_w, cy + btn_h)
        self._finish_button.base_color = 0x4C4C64 if has_pipeline else 0x3C643C
        self._finish_button.text_color = 0xFFFFFF

        drawer.text(f"Zoom: {self.view.zoom:.2f}x", (pad, h - 70), 0.45, color=0xD2D200)

    def _render_once(self, drawer: Drawer) -> None:
        self._map_layer.render(drawer)
        if self.target is not None:
            x, y, w, h = self.target
            p1 = self._get_screen_coords(x, y)
            p2 = self._get_screen_coords(x + w, y + h)
            x1, y1 = min(p1[0], p2[0]), min(p1[1], p2[1])
            x2, y2 = max(p1[0], p2[0]), max(p1[1], p2[1])
            drawer.mask(p1, p2, color=0x22BBFF, alpha=0.2)
            drawer.rect(p1, p2, color=0x22BBFF, thickness=2)

            origin_text = f"({x:.1f}, {y:.1f})"
            h_text = f"H={h:.1f}"
            w_text = f"W={w:.1f}"

            ox = max(self.SIDEBAR_W + 4, min(x1 + 4, self.window_w - 220))
            oy = max(20, y1 - 8)
            drawer.text(origin_text, (ox, oy), 0.45, color=0xFFFFFF)
            if self.view.zoom >= 1.0:
                hx = max(self.SIDEBAR_W + 4, min(x1 + 4, self.window_w - 90))
                h_size = drawer.get_text_size(h_text, 0.45)
                hy = max(
                    h_size[1] + 2,
                    min(y2 + h_size[1] + 2, self.window_h - self.STATUS_BAR_H - 6),
                )
                drawer.text(h_text, (hx, hy), 0.45, color=0xA8F0FF)

                wx = max(self.SIDEBAR_W + 4, min(x2 + 8, self.window_w - 90))
                wy = max(20, min(y2 - 6, self.window_h - self.STATUS_BAR_H - 6))
                drawer.text(w_text, (wx, wy), 0.45, color=0xA8F0FF)

        drawer.line(
            (self.mouse_pos[0], 0),
            (self.mouse_pos[0], self.window_h),
            color=0xFFFF00,
            thickness=1,
        )
        drawer.line(
            (0, self.mouse_pos[1]),
            (self.window_w, self.mouse_pos[1]),
            color=0xFFFF00,
            thickness=1,
        )
        self._render_ui(drawer)

    def _on_mouse(self, event, x, y, flags, param) -> None:
        mx, my = self._get_map_coords(x, y)

        if _handle_view_mouse(self, event, x, y, flags, mx, my):
            return

        if event == cv2.EVENT_LBUTTONDOWN:
            if x < self.SIDEBAR_W:
                return
            self._drawing = True
            self._draw_start = (mx, my)
            self.target = [mx, my, 0.0, 0.0]
            self.render_request()
            return

        if event == cv2.EVENT_MOUSEMOVE:
            if self._drawing and self._draw_start is not None:
                self.target = self._normalized_target(self._draw_start, (mx, my))
                self.render_request()
                return
            self.render_request()

        if event == cv2.EVENT_LBUTTONUP and self._drawing:
            self._drawing = False
            if self._draw_start is not None:
                self.target = self._normalized_target(self._draw_start, (mx, my))
                self._draw_start = None
                self._update_status(0x78DCFF, "Updated target area.")
                self.render_request()

    def _on_key(self, key: int) -> None:
        if (
            key in (ord("s"), ord("S"))
            and self.pipeline_context
            and self.is_dirty
            and self.target is not None
        ):
            self._do_save()
            self.render_request()

    def run(self) -> list[float] | None:
        super().run()
        return list(self.target) if self.target is not None else None


def find_map_file(name: str, map_dir: str = MAP_DIR) -> str | None:
    """Find the filename corresponding to the given name on disk (keeping the suffix), return the filename or None."""
    if not os.path.isdir(map_dir):
        return None
    files = os.listdir(map_dir)
    if name in files:
        return name

    target_key = unique_map_key(name)
    for file_name in files:
        if unique_map_key(file_name) == target_key:
            return file_name
    return None


class ModeSelectStep(StepPage):
    def __init__(self):
        super().__init__(StepData("mode", "Select Mode", can_go_back=False))

    def _render_content(self, drawer):
        drawer.text_centered(
            "Choose an operation mode:", (self.WINDOW_W // 2, 180), 0.8, color=0xDDDDDD
        )
        btn_w, btn_h = 420, 82
        spacing = 24
        col_x = (self.WINDOW_W - btn_w) // 2
        row1_y = 220
        row2_y = row1_y + btn_h + spacing
        row3_y = row2_y + btn_h + spacing

        if not self.buttons:
            self.buttons.append(
                Button(
                    (col_x, row1_y, col_x + btn_w, row1_y + btn_h),
                    "Create Move Node (M)",
                    base_color=0x334455,
                    hotkey=(ord("m"), ord("M")),
                    icon_name="Move",
                    on_click=lambda: self.stepper.push_step(
                        MapImageSelectStep(
                            step_id="map_select",
                            title="Select Map for Path",
                            map_dir=MAP_DIR,
                            on_select=lambda map_name: self.stepper.push_step(
                                EditorAdapterStep(map_name, mode="create")
                            ),
                        )
                    ),
                )
            )
            self.buttons.append(
                Button(
                    (
                        col_x,
                        row2_y,
                        col_x + btn_w,
                        row2_y + btn_h,
                    ),
                    "Create AssertLocation Node (A)",
                    base_color=0x355536,
                    hotkey=ord("a"),
                    icon_name="AssertLocation",
                    on_click=lambda: self.stepper.push_step(
                        MapImageSelectStep(
                            step_id="map_select",
                            title="Select Map for Assert Area",
                            map_dir=MAP_DIR,
                            on_select=lambda map_name: self.stepper.push_step(
                                RegionEditorAdapterStep(map_name, mode="create")
                            ),
                        )
                    ),
                )
            )
            self.buttons.append(
                Button(
                    (col_x, row3_y, col_x + btn_w, row3_y + btn_h),
                    "Import from Pipeline JSON (I)",
                    base_color=0x554433,
                    hotkey=(ord("i"), ord("I")),
                    icon_name="Import",
                    on_click=lambda: self.stepper.push_step(FileSelectStep()),
                )
            )


class FileSelectStep(StepPage):
    def __init__(self):
        super().__init__(StepData("file_select", "Select Pipeline JSON"))
        self.file_list = ScrollableListWidget(item_height=40)
        self.search_input = TextInputWidget("Search JSON files...")
        self._all_files = []
        pipeline_dir = "assets/resource/pipeline"
        if os.path.exists(pipeline_dir):
            for root, _, files in os.walk(pipeline_dir):
                for f in files:
                    if f.endswith(".json"):
                        path = os.path.join(root, f)
                        enabled = self._is_eligible_pipeline_file(path)
                        self._all_files.append(
                            {
                                "label": f,
                                "sub_label": (
                                    os.path.dirname(
                                        os.path.relpath(path, pipeline_dir)
                                    ).replace(os.path.sep, "/")
                                    or "."
                                ),
                                "data": path,
                                "disabled": not enabled,
                            }
                        )
        self._all_files.sort(
            key=lambda x: (
                bool(x.get("disabled", False)),
                str(x.get("sub_label", "")).lower(),
                str(x.get("label", "")).lower(),
            )
        )
        self.file_list.set_items(self._all_files)

    @staticmethod
    def _is_eligible_pipeline_file(file_path: str) -> bool:
        try:
            size = os.path.getsize(file_path)
            if size >= 1024 * 1024:
                return False
            with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
                content = f.read()
            return NODE_TYPE_MOVE in content or NODE_TYPE_ASSERT_LOCATION in content
        except Exception:
            return False

    def _render_content(self, drawer):
        self.search_input.render(drawer, (50, 100, self.WINDOW_W - 50, 140))
        self.file_list.render(
            drawer, (50, 160, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        )

    def _handle_content_mouse(self, event, x, y, flags, param):
        rect = (50, 160, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        if event == cv2.EVENT_LBUTTONDOWN:
            idx = self.file_list.handle_click(x, y, rect)
            if idx >= 0:
                self.stepper.push_step(
                    NodeSelectStep(self.file_list.items[idx]["data"])
                )
        elif event == cv2.EVENT_MOUSEWHEEL:
            if self.file_list.handle_wheel(x, y, flags, rect):
                self.stepper.request_render()

    def _handle_content_key(self, key):
        if self.search_input.handle_key(key):
            q = self.search_input.text.lower()
            filtered = [
                f
                for f in self._all_files
                if q in f["label"].lower() or q in f["sub_label"].lower()
            ]
            self.file_list.set_items(filtered)
            self.stepper.request_render()
            return
        is_up = self.is_up_key(key)
        is_down = self.is_down_key(key)
        if is_up or is_down:
            self.file_list.navigate(-1 if is_up else 1)
            self.stepper.request_render()
        elif key in (10, 13) and self.file_list.selected_idx >= 0:
            self.stepper.push_step(
                NodeSelectStep(
                    self.file_list.items[self.file_list.selected_idx]["data"]
                )
            )


class NodeSelectStep(StepPage):
    def __init__(self, file_path):
        super().__init__(
            StepData("node_select", f"Select Node from {os.path.basename(file_path)}")
        )
        self.file_path = file_path
        self.node_list = ScrollableListWidget(item_height=40)
        self.handler = PipelineHandler(file_path)
        nodes = self.handler.read_nodes()
        self.candidates = nodes
        self.node_list.set_items(
            [
                {
                    "label": n["node_name"],
                    "sub_label": self._build_node_sub_label(n),
                    "icon_name": (
                        "AssertLocation"
                        if n.get("node_type") == NODE_TYPE_ASSERT_LOCATION
                        else "Move"
                    ),
                    "data": n["node_name"],
                }
                for n in nodes
            ]
        )

    @staticmethod
    def _build_node_sub_label(node: dict) -> str:
        node_type = node.get("node_type", NODE_TYPE_MOVE)
        map_name = node.get("map_name", "Unknown")
        if node_type == NODE_TYPE_ASSERT_LOCATION:
            return f"Type: {NODE_TYPE_ASSERT_LOCATION} | Map: {map_name}"
        path = node.get("path", [])
        return f"Type: {NODE_TYPE_MOVE} | Map: {map_name} | Pts: {len(path)}"

    def _render_content(self, drawer):
        self.node_list.render(
            drawer, (50, 100, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        )

    def _handle_content_mouse(self, event, x, y, flags, param):
        rect = (50, 100, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        if event == cv2.EVENT_LBUTTONDOWN:
            idx = self.node_list.handle_click(x, y, rect)
            if idx >= 0:
                self._submit(idx)
        elif event == cv2.EVENT_MOUSEWHEEL:
            if self.node_list.handle_wheel(x, y, flags, rect):
                self.stepper.request_render()

    def _handle_content_key(self, key):
        is_up = self.is_up_key(key)
        is_down = self.is_down_key(key)
        if is_up or is_down:
            self.node_list.navigate(-1 if is_up else 1)
            self.stepper.request_render()
        elif key in (10, 13) and self.node_list.selected_idx >= 0:
            self._submit(self.node_list.selected_idx)

    def _submit(self, idx):
        selected = self.candidates[idx]
        import_context = {
            "file_path": self.file_path,
            "handler": self.handler,
            "node_name": selected["node_name"],
            "original_map_name": selected["map_name"],
            "is_new_structure": selected.get("is_new_structure", False),
            "node_type": selected.get("node_type", NODE_TYPE_MOVE),
        }
        if selected.get("node_type") == NODE_TYPE_ASSERT_LOCATION:
            self.stepper.push_step(
                RegionEditorAdapterStep(
                    selected["map_name"],
                    mode="import",
                    import_context=import_context,
                    initial_target=selected.get("target"),
                )
            )
            return

        self.stepper.push_step(
            EditorAdapterStep(
                selected["map_name"],
                mode="import",
                import_context=import_context,
                initial_points=selected.get("path", []),
            )
        )


class EditorAdapterStep(BasePage):
    """Adapts PathEditPage directly into Stepper loop!"""

    def __init__(
        self, map_name, mode="create", import_context=None, initial_points=None
    ):
        super().__init__("MapTracker App", 1280, 720)
        self.map_name = map_name
        self.mode = mode
        self.import_context = import_context
        self.initial_points = initial_points or []
        self.editor = None
        self._finished_once = False

    def hook_enter(self, stepper: PageStepper):
        if not self.editor:
            self.editor = PathEditPage(
                self.map_name,
                self.initial_points,
                window_name=stepper.window_name,
                pipeline_context=self.import_context if self.import_context else None,
            )
        # Returning from ExportStep should allow finishing again.
        self._finished_once = False
        self.editor.done = False
        self.editor.hook_enter(stepper)

    def hook_idle(self):
        if self.editor is None:
            return
        self.editor.hook_idle()

    def hook_exit(self):
        if self.editor:
            self.editor.hook_exit()

    def render(self):
        if self.editor is None:
            return None
        if self.editor.done and not self._finished_once:
            self._finished_once = True
            self.editor.stepper.push_step(
                ExportStep(
                    self.editor.points,
                    self.import_context,
                    self.map_name,
                    node_type=NODE_TYPE_MOVE,
                )
            )
            return None
        return self.editor.render()

    def _on_mouse(self, event, x, y, flags, param):
        if self.editor is None:
            return
        self.editor.handle_mouse(event, x, y, flags, param)

    def _on_key(self, key):
        if self.editor is None:
            return
        self.editor.handle_key(key)


class ExportStep(StepPage):
    def __init__(
        self, points, import_context, map_name, *, node_type: str = NODE_TYPE_MOVE
    ):
        super().__init__(StepData("export", "Export / Save Result"))
        self.points = points
        self.import_context = import_context
        self.map_name = map_name
        self.node_type = node_type

        self.options = [
            {
                "label": (
                    "Just Save to File (Replace path)"
                    if node_type == NODE_TYPE_MOVE
                    else "Just Save to File (Replace target)"
                ),
                "data": "S",
                "disabled": import_context is None,
            },
            {"label": "Print Context Dict", "data": "D"},
            {"label": "Print Node JSON", "data": "J"},
            {
                "label": (
                    "Print Point List"
                    if node_type == NODE_TYPE_MOVE
                    else "Print Target Rect"
                ),
                "data": "L",
            },
        ]
        self.list_widget = ScrollableListWidget(45)
        self.list_widget.set_items(self.options)
        self.saved_text = ""

    def _render_content(self, drawer):
        self.list_widget.render(drawer, (100, 150, self.WINDOW_W - 100, 350))
        if self.saved_text:
            drawer.text_centered(
                self.saved_text, (self.WINDOW_W // 2, 450), 0.8, color=0x50DC50
            )

    def _handle_content_mouse(self, event, x, y, flags, param):
        rect = (100, 150, self.WINDOW_W - 100, 350)
        if event == cv2.EVENT_LBUTTONDOWN:
            idx = self.list_widget.handle_click(x, y, rect)
            if idx >= 0:
                self._submit(self.list_widget.items[idx]["data"])

    def _handle_content_key(self, key):
        if key in (10, 13) and self.list_widget.selected_idx >= 0:
            self._submit(self.list_widget.items[self.list_widget.selected_idx]["data"])
        elif key in (82, 0x260000, 65362):
            self.list_widget.navigate(-1)
            self.stepper.request_render()
        elif key in (84, 0x280000, 65364):
            self.list_widget.navigate(1)
            self.stepper.request_render()

    def _submit(self, mode):
        if mode == "S":
            handler = self.import_context["handler"]
            node_name = self.import_context["node_name"]
            if self.node_type == NODE_TYPE_ASSERT_LOCATION:
                raw_map_name = self.import_context.get(
                    "original_map_name", self.map_name
                )
                map_name_stem = os.path.splitext(os.path.basename(raw_map_name))[0]
                ok = handler.replace_assert_location(
                    node_name, map_name_stem, self.points
                )
            else:
                ok = handler.replace_path(node_name, self.points)
            if ok:
                self.saved_text = f"Successfully updated node '{node_name}'!"
                print(f"\n{_G}Successfully updated node {_0}'{node_name}'")
            else:
                self.saved_text = "Failed to update node!"
            self.stepper.request_render()

        elif mode == "J":
            raw_map_name = (
                self.import_context.get("original_map_name", self.map_name)
                if self.import_context
                else self.map_name
            )
            map_stem = os.path.splitext(os.path.basename(raw_map_name))[0]
            if self.node_type == NODE_TYPE_ASSERT_LOCATION:
                param_data = {
                    "expected": [
                        {
                            "map_name": map_stem,
                            "target": [round(float(v), 1) for v in self.points],
                        }
                    ]
                }
                node_data = {
                    "recognition": "Custom",
                    "custom_recognition": NODE_TYPE_ASSERT_LOCATION,
                    "custom_recognition_param": param_data,
                    "action": "DoNothing",
                }
            else:
                param_data = {
                    "map_name": map_stem,
                    "path": [[round(p[0], 1), round(p[1], 1)] for p in self.points],
                }
                is_new = (
                    self.import_context.get("is_new_structure", False)
                    if self.import_context
                    else False
                )
                if is_new:
                    node_data = {
                        "action": {
                            "custom_action": NODE_TYPE_MOVE,
                            "custom_action_param": param_data,
                        }
                    }
                else:
                    node_data = {
                        "action": "Custom",
                        "custom_action": NODE_TYPE_MOVE,
                        "custom_action_param": param_data,
                    }
            print(f"\n{_C}--- JSON Snippet ---{_0}\n")
            print(json.dumps({"NodeName": node_data}, indent=4, ensure_ascii=False))
            self.saved_text = "JSON output printed to terminal!"
            self.stepper.request_render()

        elif mode == "D":
            raw_map_name = (
                self.import_context.get("original_map_name", self.map_name)
                if self.import_context
                else self.map_name
            )
            map_stem = os.path.splitext(os.path.basename(raw_map_name))[0]
            if self.node_type == NODE_TYPE_ASSERT_LOCATION:
                param_data = {
                    "expected": [
                        {
                            "map_name": map_stem,
                            "target": [round(float(v), 1) for v in self.points],
                        }
                    ]
                }
            else:
                param_data = {
                    "map_name": map_stem,
                    "path": [[round(p[0], 1), round(p[1], 1)] for p in self.points],
                }
            print(f"\n{_C}--- Parameters Dict ---{_0}\n")
            print(json.dumps(param_data, indent=4, ensure_ascii=False))
            self.saved_text = "Dict output printed to terminal!"
            self.stepper.request_render()

        elif mode == "L":
            if self.node_type == NODE_TYPE_ASSERT_LOCATION:
                target_rect = [round(float(v), 1) for v in self.points]
                print(f"\n{_C}--- Target Rect ---{_0}\n")
                print(target_rect)
                self.saved_text = "Target rect printed to terminal!"
            else:
                point_list = [[round(p[0], 1), round(p[1], 1)] for p in self.points]
                print(f"\n{_C}--- Point List ---{_0}\n")
                print(point_list)
                self.saved_text = "Point list printed to terminal!"
            self.stepper.request_render()


class RegionEditorAdapterStep(BasePage):
    def __init__(
        self, map_name, mode="create", import_context=None, initial_target=None
    ):
        super().__init__("MapTracker App", 1280, 720)
        self.map_name = map_name
        self.mode = mode
        self.import_context = import_context
        self.initial_target = initial_target
        self.editor = None
        self._finished_once = False

    def hook_enter(self, stepper: PageStepper):
        if not self.editor:
            self.editor = AreaEditPage(
                self.map_name,
                self.initial_target,
                window_name=stepper.window_name,
                pipeline_context=self.import_context if self.import_context else None,
            )
        self._finished_once = False
        self.editor.done = False
        self.editor.hook_enter(stepper)

    def hook_idle(self):
        if self.editor is None:
            return
        self.editor.hook_idle()

    def hook_exit(self):
        if self.editor:
            self.editor.hook_exit()

    def render(self):
        if self.editor is None:
            return None
        if self.editor.done and not self._finished_once:
            self._finished_once = True
            target = (
                self.editor.target
                if self.editor.target is not None
                else [0.0, 0.0, 0.0, 0.0]
            )
            self.editor.stepper.push_step(
                ExportStep(
                    target,
                    self.import_context,
                    self.map_name,
                    node_type=NODE_TYPE_ASSERT_LOCATION,
                )
            )
            return None
        return self.editor.render()

    def _on_mouse(self, event, x, y, flags, param):
        if self.editor is None:
            return
        self.editor.handle_mouse(event, x, y, flags, param)

    def _on_key(self, key):
        if self.editor is None:
            return
        self.editor.handle_key(key)


class App(PageStepper):
    def __init__(self):
        super().__init__("MapTracker App")
        self.points = []
        self.import_context = None


def main():
    app = App()
    app.push_step(ModeSelectStep())
    app.run()


if __name__ == "__main__":
    main()
