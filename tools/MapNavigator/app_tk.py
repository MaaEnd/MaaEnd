from __future__ import annotations

import json
import tkinter as tk
from tkinter import filedialog, messagebox, ttk

from history_store import UndoRedoHistory
from json_import import (
    export_path_nodes,
    infer_missing_zones,
    list_available_zone_ids,
    load_points_from_json_file,
    split_route_into_segments,
)
from model import (
    ACTION_COLORS,
    ACTION_NAMES,
    ActionType,
    PathPoint,
    get_point_actions,
    normalize_path_points,
    resolve_zone_image,
    set_point_actions,
    simplify_path,
)
from point_editing import PointEditingService
from recording_service import RecordingService
from renderer_tk import MapRenderer
from runtime import MAP_IMAGE_DIR, configure_runtime_env, load_maa_runtime
from zone_index import ZoneState


class RouteEditorApp:
    """轨迹录制与编辑 GUI。"""

    def __init__(self, root: tk.Tk) -> None:
        self.root = root
        self.root.title("MapNavigator 录制与编辑器")
        self.root.geometry("1100x850")
        self.root.protocol("WM_DELETE_WINDOW", self.on_close)

        configure_runtime_env()
        runtime = load_maa_runtime()
        self.recording_service: RecordingService | None = None
        if runtime:
            self.recording_service = RecordingService(
                runtime=runtime,
                on_status=lambda text, color: self.root.after(0, lambda: self._set_status(text, color)),
                on_finished=lambda raw_path: self.root.after(0, lambda: self._on_recording_finished(raw_path)),
                on_error=lambda err: self.root.after(0, lambda: self._on_recording_error(err)),
            )

        # 轨迹数据状态
        self.raw_points: list[PathPoint] = []
        self.points: list[PathPoint] = []
        self.density_val = tk.IntVar(value=50)
        self.strict_var = tk.BooleanVar(value=False)
        self.action_chain_var = tk.StringVar(value="Run")

        # 领域服务
        self.zone_state = ZoneState()
        self.history = UndoRedoHistory[list[PathPoint]](max_depth=50)
        self.point_editor = PointEditingService()

        # 编辑态
        self.selected_idx: int | None = None
        self.zone_point_global_indices: list[int] = []

        # 画布对象池
        self.path_line_id: int | None = None
        self.ui_nodes: list[int] = []
        self.ui_texts: list[int] = []

        # 交互状态
        self.drag_start_x = 0
        self.drag_start_y = 0
        self.is_panning = False
        self.is_dragging = False
        self._redraw_pending = False

        self._build_layout()
        self.renderer = MapRenderer(self.canvas, root, MAP_IMAGE_DIR)
        self._bind_events()
        self._refresh_zone_label()

    def _build_layout(self) -> None:
        btn_frame = tk.Frame(self.root)
        btn_frame.pack(fill=tk.X, pady=2, padx=8)

        left_frame = tk.Frame(btn_frame)
        left_frame.pack(side=tk.LEFT, fill=tk.Y)

        self.btn_start = tk.Button(
            left_frame,
            text="▶ 开始录制",
            command=self.start_recording,
            bg="#2ecc71",
            fg="white",
            font=("Microsoft YaHei", 9, "bold"),
            padx=15,
            relief=tk.FLAT,
        )
        self.btn_start.pack(side=tk.LEFT, padx=3)

        self.btn_stop = tk.Button(
            left_frame,
            text="⏹ 停止录制",
            command=self.stop_recording,
            state=tk.DISABLED,
            bg="#e74c3c",
            fg="white",
            font=("Microsoft YaHei", 9, "bold"),
            padx=15,
            relief=tk.FLAT,
        )
        self.btn_stop.pack(side=tk.LEFT, padx=3)

        self.btn_copy_path = tk.Button(left_frame, text="📋 复制 Path", command=self.copy_path, padx=10)
        self.btn_copy_path.pack(side=tk.LEFT, padx=3)

        self.btn_import = tk.Button(left_frame, text="📂 导入 JSON", command=self.import_json, padx=10)
        self.btn_import.pack(side=tk.LEFT, padx=3)

        ttk.Separator(left_frame, orient=tk.VERTICAL).pack(side=tk.LEFT, fill=tk.Y, padx=10)

        self.btn_prev = tk.Button(left_frame, text="◀", command=self.prev_zone, width=4)
        self.btn_prev.pack(side=tk.LEFT, padx=2)

        self.zone_label = tk.Label(
            btn_frame,
            text="— 无区域信息 —",
            font=("Consolas", 10, "bold"),
            fg="#1e293b",
            anchor="center",
            width=28,
        )
        self.zone_label.pack(side=tk.LEFT, expand=True, fill=tk.X, padx=10)

        right_frame = tk.Frame(btn_frame)
        right_frame.pack(side=tk.RIGHT, fill=tk.Y)

        self.btn_next = tk.Button(right_frame, text="▶", command=self.next_zone, width=4)
        self.btn_next.pack(side=tk.LEFT, padx=2)

        ttk.Separator(right_frame, orient=tk.VERTICAL).pack(side=tk.LEFT, fill=tk.Y, padx=10)

        tk.Label(right_frame, text="密度:", font=("Microsoft YaHei", 9)).pack(side=tk.LEFT, padx=(2, 0))
        self.slider_density = tk.Scale(
            right_frame,
            from_=0,
            to=100,
            orient=tk.HORIZONTAL,
            variable=self.density_val,
            showvalue=False,
            width=10,
            length=70,
            command=lambda _value: self.reprocess_points(),
        )
        self.slider_density.pack(side=tk.LEFT)

        ttk.Separator(right_frame, orient=tk.VERTICAL).pack(side=tk.LEFT, fill=tk.Y, padx=10)

        self.action_menu = ttk.Combobox(right_frame, values=list(ACTION_NAMES.values()), width=10, state="readonly")
        self.action_menu.set(ACTION_NAMES[ActionType.RUN])
        self.action_menu.pack(side=tk.LEFT, padx=2)

        self.btn_apply_action = tk.Button(right_frame, text="设单", command=self.apply_action_to_selected)
        self.btn_apply_action.pack(side=tk.LEFT, padx=2)

        self.btn_append_action = tk.Button(right_frame, text="追加", command=self.append_action_to_selected)
        self.btn_append_action.pack(side=tk.LEFT, padx=2)

        self.btn_pop_action = tk.Button(right_frame, text="退一", command=self.pop_action_from_selected, width=4)
        self.btn_pop_action.pack(side=tk.LEFT, padx=2)

        self.action_chain_label = tk.Label(
            right_frame,
            textvariable=self.action_chain_var,
            font=("Consolas", 8),
            fg="#475569",
            anchor="w",
            width=18,
        )
        self.action_chain_label.pack(side=tk.LEFT, padx=(4, 6))

        self.strict_check = tk.Checkbutton(
            right_frame,
            text="严格",
            variable=self.strict_var,
            onvalue=True,
            offvalue=False,
            font=("Microsoft YaHei", 9),
        )
        self.strict_check.pack(side=tk.LEFT, padx=(4, 2))

        self.btn_del_point = tk.Button(
            right_frame,
            text="🗑",
            command=self.delete_selected_point,
            fg="#e74c3c",
            font=("", 10, "bold"),
        )
        self.btn_del_point.pack(side=tk.LEFT, padx=2)

        self.status_label = tk.Label(
            self.root,
            text="准备就绪",
            fg="#64748b",
            anchor="w",
            font=("Microsoft YaHei", 9),
        )
        self.status_label.pack(fill=tk.X, padx=10, pady=2)

        self.canvas = tk.Canvas(self.root, bg="#0f172a", highlightthickness=0)
        self.canvas.pack(fill=tk.BOTH, expand=True)

    def _bind_events(self) -> None:
        self.canvas.bind("<Button-1>", self.on_click)
        self.canvas.bind("<B1-Motion>", self.on_drag)
        self.canvas.bind("<ButtonRelease-1>", self.on_release)
        self.canvas.bind("<Button-3>", self.on_pan_start)
        self.canvas.bind("<B3-Motion>", self.on_pan_move)
        self.canvas.bind("<ButtonRelease-3>", self.on_pan_end)
        self.canvas.bind("<MouseWheel>", self.on_scroll)
        self.canvas.bind("<Configure>", lambda _event: self.schedule_redraw(fast=True))

        self.root.bind_all("<Control-z>", lambda _event: self.undo())
        self.root.bind_all("<Control-y>", lambda _event: self.redo())

    def _set_status(self, text: str, color: str) -> None:
        self.status_label.config(text=text, fg=color)

    def _refresh_zone_label(self) -> None:
        self.zone_label.config(text=self._compact_zone_label_text(self.zone_state.label_text()))

    def _on_points_structure_changed(self, redraw_fast: bool = False) -> None:
        self.points = normalize_path_points(self.points)
        self.zone_state.rebuild(self.points)
        current_zone_indices = self.zone_state.point_indices(self.points)
        if self.selected_idx is not None and self.selected_idx >= len(current_zone_indices):
            self.selected_idx = None
        self._sync_action_controls(current_zone_indices)
        self._refresh_zone_label()
        self.schedule_redraw(fast=redraw_fast)

    def _reset_point_property_controls(self) -> None:
        self.action_menu.set(ACTION_NAMES[ActionType.RUN])
        self.strict_var.set(False)
        self.action_chain_var.set("Run")

    def _format_action_chain(self, point: PathPoint | None) -> str:
        if point is None:
            return "Run"
        return " -> ".join(ACTION_NAMES.get(action, "Run") for action in get_point_actions(point))

    @staticmethod
    def _compact_zone_label_text(text: str, max_zone_chars: int = 22) -> str:
        if ":" not in text:
            return text

        prefix, zone_id = text.split(":", maxsplit=1)
        zone_id = zone_id.strip()
        if len(zone_id) <= max_zone_chars:
            return text

        head_chars = max_zone_chars // 2
        tail_chars = max_zone_chars - head_chars - 1
        compact_zone_id = f"{zone_id[:head_chars]}…{zone_id[-tail_chars:]}"
        return f"{prefix}: {compact_zone_id}"

    def _selected_point(self, zone_indices: list[int] | None = None) -> PathPoint | None:
        if zone_indices is None:
            zone_indices = self.zone_point_global_indices
        if self.selected_idx is None or self.selected_idx >= len(zone_indices):
            return None
        return self.points[zone_indices[self.selected_idx]]

    def _sync_action_controls(self, zone_indices: list[int] | None = None) -> None:
        point = self._selected_point(zone_indices)
        if point is None:
            self._reset_point_property_controls()
            return

        actions = get_point_actions(point)
        self.action_menu.set(ACTION_NAMES.get(actions[-1], "Run"))
        self.strict_var.set(bool(point.get("strict", False)))
        self.action_chain_var.set(self._format_action_chain(point))

    def on_close(self) -> None:
        if self.recording_service and self.recording_service.is_running:
            self.recording_service.stop()
        self.root.destroy()

    # ---- 视图交互 ----
    def on_scroll(self, event) -> None:
        factor = 1.25 if event.delta > 0 else 0.8
        mouse_x, mouse_y = event.x, event.y
        world_x, world_y = self.renderer.canvas_to_world(mouse_x, mouse_y)

        new_scale = self.renderer.view_scale * factor
        new_scale = max(0.002, min(500.0, new_scale))

        new_off_x = mouse_x / new_scale - world_x
        new_off_y = mouse_y / new_scale - world_y

        self.renderer.set_viewport(new_scale, new_off_x, new_off_y)
        self.schedule_redraw(fast=True)

    def on_pan_start(self, event) -> None:
        self.is_panning = True
        self.drag_start_x, self.drag_start_y = event.x, event.y
        self.canvas.config(cursor="fleur")

    def on_pan_move(self, event) -> None:
        if not self.is_panning:
            return

        dx = (event.x - self.drag_start_x) / self.renderer.view_scale
        dy = (event.y - self.drag_start_y) / self.renderer.view_scale
        self.renderer.view_offset_x += dx
        self.renderer.view_offset_y += dy
        self.drag_start_x, self.drag_start_y = event.x, event.y
        self.schedule_redraw(fast=True)

    def on_pan_end(self, _event) -> None:
        self.is_panning = False
        self.canvas.config(cursor="cross")

    def fit_view(self) -> None:
        points = self.zone_state.current_points(self.points)
        zone_id = self.zone_state.current_zone()

        box_min_x, box_max_x, box_min_y, box_max_y = 0, 100, 0, 100
        map_image = self.renderer._get_map_pil(zone_id)
        if map_image:
            box_max_x, box_max_y = map_image.size

        if points:
            xs = [point["x"] for point in points]
            ys = [point["y"] for point in points]
            box_min_x, box_max_x = min(xs), max(xs)
            box_min_y, box_max_y = min(ys), max(ys)

        route_width = (box_max_x - box_min_x) or 100
        route_height = (box_max_y - box_min_y) or 100
        canvas_width = self.canvas.winfo_width() or 800
        canvas_height = self.canvas.winfo_height() or 600

        scale = min((canvas_width - 120) / route_width, (canvas_height - 120) / route_height)
        off_x = -box_min_x + 60 / scale
        off_y = -box_min_y + 60 / scale

        self.renderer.set_viewport(scale, off_x, off_y)
        self.schedule_redraw(fast=False)

    # ---- 渲染调度 ----
    def schedule_redraw(self, fast: bool = True) -> None:
        if self._redraw_pending:
            return
        self._redraw_pending = True
        self.root.after(16, lambda: self._do_redraw(fast))

    def _do_redraw(self, fast: bool) -> None:
        self._redraw_pending = False
        zone_id = self.zone_state.current_zone()
        self.zone_point_global_indices = self.zone_state.point_indices(self.points)
        points = [self.points[index] for index in self.zone_point_global_indices]

        self.renderer.request_render(zone_id, fast=fast)
        self._render_path(points)
        self._render_nodes(points)

    def _render_path(self, points: list[PathPoint]) -> None:
        if len(points) <= 1:
            if self.path_line_id is not None:
                self.canvas.delete(self.path_line_id)
                self.path_line_id = None
            return

        line_coords = []
        for point in points:
            line_coords.extend(self.renderer.world_to_canvas(point["x"], point["y"]))

        if self.path_line_id is None:
            self.path_line_id = self.canvas.create_line(*line_coords, fill="#f8fafc", width=2, dash=(4, 2))
            return

        self.canvas.coords(self.path_line_id, *line_coords)

    def _render_nodes(self, points: list[PathPoint]) -> None:
        while len(self.ui_nodes) > len(points):
            self.canvas.delete(self.ui_nodes.pop())
            self.canvas.delete(self.ui_texts.pop())

        node_radius = max(2, min(10, 5 * self.renderer.view_scale))
        for idx, point in enumerate(points):
            cx, cy = self.renderer.world_to_canvas(point["x"], point["y"])
            color = ACTION_COLORS.get(point["action"], "#3498db")
            is_strict = bool(point.get("strict", False))
            action_count = len(get_point_actions(point))

            is_selected = self.selected_idx == idx
            outline_color = "#ef4444" if is_selected else ("#facc15" if is_strict else "white")
            outline_width = 3 if is_selected else 1
            label_core = f"{idx}x{action_count}" if action_count > 1 else str(idx)
            label_text = f"{label_core}!" if is_strict else label_core

            if idx >= len(self.ui_nodes):
                node_id = self.canvas.create_oval(
                    cx - node_radius,
                    cy - node_radius,
                    cx + node_radius,
                    cy + node_radius,
                    fill=color,
                    outline=outline_color,
                    width=outline_width,
                    tags="node",
                )
                text_id = self.canvas.create_text(
                    cx,
                    cy + node_radius + 4,
                    text=label_text,
                    fill="#94a3b8",
                    font=("Consolas", 8),
                )
                self.ui_nodes.append(node_id)
                self.ui_texts.append(text_id)
                continue

            self.canvas.itemconfig(self.ui_nodes[idx], fill=color, outline=outline_color, width=outline_width)
            self.canvas.coords(
                self.ui_nodes[idx],
                cx - node_radius,
                cy - node_radius,
                cx + node_radius,
                cy + node_radius,
            )
            self.canvas.coords(self.ui_texts[idx], cx, cy + node_radius + 4)
            self.canvas.itemconfig(self.ui_texts[idx], text=label_text)

        self.canvas.tag_raise("node")

    # ---- 区域导航 ----
    def prev_zone(self) -> None:
        self.zone_state.prev_zone()
        self._refresh_zone_label()
        self.fit_view()

    def next_zone(self) -> None:
        self.zone_state.next_zone()
        self._refresh_zone_label()
        self.fit_view()

    # ---- 录制控制 ----
    def start_recording(self) -> None:
        if not self.recording_service:
            messagebox.showerror("环境错误", "未找到 maafw 库，请先安装 requirements 并配置运行环境。")
            return
        if self.recording_service.is_running:
            return

        self.btn_start.config(state=tk.DISABLED)
        self.btn_stop.config(state=tk.NORMAL)
        self._set_status("● 正在启动识别引擎...", "#3b82f6")
        try:
            self.recording_service.start()
        except Exception as exc:
            self._on_recording_error(str(exc))

    def stop_recording(self) -> None:
        if not self.recording_service:
            return
        self.recording_service.stop()
        self._set_status("正在停止录制并优化路径点...", "#f59e0b")
        self.btn_stop.config(state=tk.DISABLED)

    def _on_recording_finished(self, raw_path: list[PathPoint]) -> None:
        self.raw_points = raw_path
        self.reprocess_points()
        self._reset_ui()
        self.fit_view()

    def _on_recording_error(self, error_message: str) -> None:
        messagebox.showerror("错误", error_message)
        self._reset_ui()

    def reprocess_points(self) -> None:
        if not self.raw_points:
            return
        self.points = simplify_path(self.raw_points, self.density_val.get())
        self.history.clear()
        self.selected_idx = None
        self._reset_point_property_controls()
        self._on_points_structure_changed(redraw_fast=False)

    def _reset_ui(self) -> None:
        self.btn_start.config(state=tk.NORMAL)
        self.btn_stop.config(state=tk.DISABLED)
        self._set_status("录制结束。鼠标滚轮缩放，右键平移，左键拖拽点微调。", "#10b981")

    # ---- 撤销与重做 ----
    def push_undo(self) -> None:
        self.history.snapshot(self.points)

    def undo(self) -> None:
        restored = self.history.undo(self.points)
        if restored is None:
            return
        self.points = restored
        self.selected_idx = None
        self._reset_point_property_controls()
        self._on_points_structure_changed(redraw_fast=False)

    def redo(self) -> None:
        restored = self.history.redo(self.points)
        if restored is None:
            return
        self.points = restored
        self.selected_idx = None
        self._reset_point_property_controls()
        self._on_points_structure_changed(redraw_fast=False)

    # ---- 点编辑 ----
    def get_node_at(self, event_x: float, event_y: float) -> int | None:
        return self.point_editor.hit_test(
            points=self.points,
            zone_indices=self.zone_point_global_indices,
            projector=self.renderer,
            event_x=event_x,
            event_y=event_y,
        )

    def on_click(self, event) -> None:
        idx_in_zone = self.get_node_at(event.x, event.y)
        if idx_in_zone is None:
            self.push_undo()
            self.is_dragging = False
            self.selected_idx = None
            world_x, world_y = self.renderer.canvas_to_world(event.x, event.y)
            self.point_editor.insert_point(
                points=self.points,
                zone_indices=self.zone_point_global_indices,
                current_zone=self.zone_state.current_zone(),
                action_name=self.action_menu.get(),
                strict_arrival=self.strict_var.get(),
                world_x=world_x,
                world_y=world_y,
            )
            self._on_points_structure_changed(redraw_fast=False)
            return

        self.push_undo()
        self.selected_idx = idx_in_zone
        self.is_dragging = True

        global_idx = self.zone_point_global_indices[self.selected_idx]
        point = self.points[global_idx]
        self._sync_action_controls()
        self.schedule_redraw(fast=True)

    def apply_action_to_selected(self) -> None:
        if self.selected_idx is None or self.selected_idx >= len(self.zone_point_global_indices):
            messagebox.showinfo("提示", "请先点击选中一个点")
            return

        self.push_undo()
        changed = self.point_editor.apply_attributes(
            points=self.points,
            zone_indices=self.zone_point_global_indices,
            selected_idx=self.selected_idx,
            action_name=self.action_menu.get(),
            strict_arrival=self.strict_var.get(),
        )
        if changed:
            self._sync_action_controls()
            self._on_points_structure_changed(redraw_fast=False)

    def append_action_to_selected(self) -> None:
        point = self._selected_point()
        if point is None:
            messagebox.showinfo("提示", "请先点击选中一个点")
            return

        self.push_undo()
        action_type = self.point_editor.action_name_to_type(self.action_menu.get())
        set_point_actions(point, get_point_actions(point) + [action_type])
        self._sync_action_controls()
        self._on_points_structure_changed(redraw_fast=False)

    def pop_action_from_selected(self) -> None:
        point = self._selected_point()
        if point is None:
            messagebox.showinfo("提示", "请先点击选中一个点")
            return

        self.push_undo()
        actions = get_point_actions(point)
        if len(actions) <= 1:
            set_point_actions(point, [int(ActionType.RUN)])
        else:
            set_point_actions(point, actions[:-1])
        self._sync_action_controls()
        self._on_points_structure_changed(redraw_fast=False)

    def delete_selected_point(self) -> None:
        if self.selected_idx is None or self.selected_idx >= len(self.zone_point_global_indices):
            messagebox.showinfo("提示", "请先点击选中一个点")
            return

        self.push_undo()
        deleted = self.point_editor.delete_selected(
            points=self.points,
            zone_indices=self.zone_point_global_indices,
            selected_idx=self.selected_idx,
        )
        if deleted:
            self.selected_idx = None
            self._reset_point_property_controls()
            self._on_points_structure_changed(redraw_fast=False)

    def on_drag(self, event) -> None:
        if not self.is_dragging:
            return

        world_x, world_y = self.renderer.canvas_to_world(event.x, event.y)
        moved = self.point_editor.move_selected(
            points=self.points,
            zone_indices=self.zone_point_global_indices,
            selected_idx=self.selected_idx,
            world_x=world_x,
            world_y=world_y,
        )
        if moved:
            self.schedule_redraw(fast=True)

    def on_release(self, _event) -> None:
        self.is_dragging = False

    # ---- 导入 ----
    def import_json(self) -> None:
        input_path = filedialog.askopenfilename(
            filetypes=[("JSON Files", "*.json *.jsonc"), ("All Files", "*.*")],
        )
        if not input_path:
            return

        try:
            imported = load_points_from_json_file(input_path, apply_zone_inference=False)
        except Exception as exc:
            messagebox.showerror("导入失败", str(exc))
            return

        imported_points = imported.points
        if not imported.source_has_zone_info:
            assigned_points = self._prompt_zone_assignment_for_import(imported_points)
            if assigned_points is None:
                return
            imported_points = assigned_points

        imported_points = infer_missing_zones(imported_points)
        if not self._validate_zone_assignments(imported_points, title="导入失败"):
            return

        self.raw_points = []
        self.points = imported_points
        self.history.clear()
        self.selected_idx = None
        self._reset_point_property_controls()
        self._on_points_structure_changed(redraw_fast=False)
        self.fit_view()

        status = f"已导入 {len(self.points)} 个路径点"
        if imported.route_count > 1:
            status += f"（共找到 {imported.route_count} 条候选路径，已加载点数最多的一条）"
        self._set_status(status, "#10b981")

    def _prompt_zone_assignment_for_import(self, points: list[PathPoint]) -> list[PathPoint] | None:
        segments = split_route_into_segments(points)
        zone_options = list_available_zone_ids()
        if not segments or not zone_options:
            return points

        suggested_points = infer_missing_zones(points)
        suggested_zone_by_segment = [
            self._dominant_zone(suggested_points[start:end])
            for start, end in segments
        ]

        dialog = tk.Toplevel(self.root)
        dialog.title("导入区域映射")
        dialog.transient(self.root)
        dialog.grab_set()
        dialog.resizable(True, False)

        container = tk.Frame(dialog, padx=12, pady=12)
        container.pack(fill=tk.BOTH, expand=True)

        tk.Label(
            container,
            text="导入数据没有 zone 信息。请为每个片段选择对应地图：",
            anchor="w",
            justify=tk.LEFT,
            font=("Microsoft YaHei", 9),
        ).pack(fill=tk.X, pady=(0, 10))

        combos: list[ttk.Combobox] = []
        for idx, (start, end) in enumerate(segments):
            row = tk.Frame(container)
            row.pack(fill=tk.X, pady=3)

            summary = self._format_import_segment_summary(points, start, end)
            tk.Label(
                row,
                text=f"片段 {idx + 1}: {summary}",
                width=42,
                anchor="w",
                justify=tk.LEFT,
                font=("Consolas", 9),
            ).pack(side=tk.LEFT, padx=(0, 8))

            suggested_zone = suggested_zone_by_segment[idx]
            if suggested_zone not in zone_options:
                suggested_zone = zone_options[0]
            combo = ttk.Combobox(
                row,
                values=zone_options,
                width=26,
                state="readonly",
            )
            combo.set(suggested_zone)
            combo.pack(side=tk.LEFT, fill=tk.X, expand=True)
            combos.append(combo)

        button_frame = tk.Frame(container)
        button_frame.pack(fill=tk.X, pady=(12, 0))

        result: dict[str, list[PathPoint] | None] = {"points": None}

        def confirm() -> None:
            assigned_points = [dict(point) for point in points]
            selected_zone_names: list[str] = []
            for (start, end), combo in zip(segments, combos):
                zone_name = combo.get().strip()
                if not zone_name:
                    messagebox.showwarning("区域未选择", "请先为每个片段选择对应地图。", parent=dialog)
                    return
                selected_zone_names.append(zone_name)
                for point_idx in range(start, end):
                    assigned_points[point_idx]["zone"] = zone_name

            if not selected_zone_names:
                messagebox.showwarning("区域未选择", "当前没有任何可用区域映射。", parent=dialog)
                return

            result["points"] = assigned_points
            dialog.destroy()

        def cancel() -> None:
            dialog.destroy()

        tk.Button(button_frame, text="确定", command=confirm, width=10).pack(side=tk.RIGHT, padx=(8, 0))
        tk.Button(button_frame, text="取消", command=cancel, width=10).pack(side=tk.RIGHT)

        dialog.wait_visibility()
        dialog.focus_set()
        self.root.wait_window(dialog)
        return result["points"]

    def _validate_zone_assignments(self, points: list[PathPoint], title: str) -> bool:
        zone_ids = sorted({str(point.get("zone", "") or "").strip() for point in points if str(point.get("zone", "") or "").strip()})
        if not zone_ids:
            return True

        unresolved_zone_ids = [zone_id for zone_id in zone_ids if resolve_zone_image(zone_id, MAP_IMAGE_DIR) is None]
        if unresolved_zone_ids:
            unresolved_text = "、".join(unresolved_zone_ids[:6])
            if len(unresolved_zone_ids) > 6:
                unresolved_text += "..."
            messagebox.showerror(title, f"以下 zone 无法映射到底图：{unresolved_text}")
            return False

        return True

    @staticmethod
    def _dominant_zone(points: list[PathPoint]) -> str:
        counts: dict[str, int] = {}
        for point in points:
            zone_name = str(point.get("zone", "") or "")
            if not zone_name:
                continue
            counts[zone_name] = counts.get(zone_name, 0) + 1
        if not counts:
            return ""
        return max(counts.items(), key=lambda item: item[1])[0]

    @staticmethod
    def _format_import_segment_summary(points: list[PathPoint], start: int, end: int) -> str:
        segment_points = points[start:end]
        xs = [point["x"] for point in segment_points]
        ys = [point["y"] for point in segment_points]
        return (
            f"{start:02d}-{end - 1:02d} / {end - start:02d}点 "
            f"[{min(xs):.0f},{min(ys):.0f}]~[{max(xs):.0f},{max(ys):.0f}]"
        )

    # ---- 导出 ----
    def copy_path(self) -> None:
        if not self.points:
            messagebox.showwarning("复制失败", "当前没有任何轨迹数据")
            return
        if not self._validate_zone_assignments(self.points, title="复制失败"):
            return

        path_text = json.dumps(export_path_nodes(self.points), indent=4, ensure_ascii=False)
        self.root.clipboard_clear()
        self.root.clipboard_append(path_text)
        self.root.update()
        self._set_status("MapNavigator path 已复制到剪贴板", "#10b981")
