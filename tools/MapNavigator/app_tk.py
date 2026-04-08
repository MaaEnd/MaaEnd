from __future__ import annotations

import json
import tkinter as tk
from tkinter import filedialog, messagebox, ttk

from connection_models import AdbConnectionConfig, RecordingSessionConfig, Win32ConnectionConfig
from connectors import list_adb_devices, resolve_adb_path
from history_store import UndoRedoHistory
from json_import import (
    export_assert_location_node,
    export_path_nodes,
    infer_missing_zones,
    list_available_zone_ids,
    load_points_from_json_file,
    split_route_into_segments,
)
from model import (
    ACTION_COLORS,
    ACTION_MENU_NAMES,
    ACTION_NAMES,
    ActionType,
    PathPoint,
    get_point_actions,
    normalize_path_points,
    normalize_zone_id,
    resolve_zone_image,
    set_manual_point_actions,
    simplify_path,
)
from point_editing import PointEditingService
from recording_service import RecordingService
from renderer_tk import MapRenderer
from runtime import MAP_IMAGE_DIR, configure_runtime_env, load_maa_runtime
from settings_store import MapNavigatorSettings, MapNavigatorSettingsStore
from zone_index import ZoneState


class RouteEditorApp:
    """轨迹录制与编辑 GUI。"""

    BOX_SELECT_MODIFIER_MASK = 0x0004
    DRAG_ACTIVATION_DISTANCE = 4

    def __init__(self, root: tk.Tk) -> None:
        self.root = root
        self.root.title("MapNavigator 录制与编辑器")
        self.root.geometry("1100x850")
        self.root.protocol("WM_DELETE_WINDOW", self.on_close)

        configure_runtime_env()
        self.settings_store = MapNavigatorSettingsStore()
        self.settings = self.settings_store.load()

        runtime = load_maa_runtime()
        self.recording_service: RecordingService | None = None
        if runtime:
            self.recording_service = RecordingService(
                runtime=runtime,
                on_status=lambda text, color: self.root.after(0, lambda: self._set_status(text, color)),
                on_finished=lambda raw_path: self.root.after(0, lambda: self._on_recording_finished(raw_path)),
                on_error=lambda err: self.root.after(0, lambda: self._on_recording_error(err)),
                on_locator_detail=lambda text: self.root.after(0, lambda: self._set_locator_debug(text)),
            )

        # 轨迹数据状态
        self.raw_points: list[PathPoint] = []
        self.points: list[PathPoint] = []
        self.available_zone_ids = list_available_zone_ids()
        self.density_val = tk.IntVar(value=50)
        self.disable_optimization_var = tk.BooleanVar(value=False)
        self.assert_mode_var = tk.BooleanVar(value=False)
        self.assert_zone_var = tk.StringVar(value="")
        self.strict_var = tk.BooleanVar(value=False)
        self.action_chain_var = tk.StringVar(value="Run")
        self.locator_debug_var = tk.StringVar(value="Locator: --")
        self.connection_kind_var = tk.StringVar(value=self.settings.connection_kind)
        self.win32_window_title_var = tk.StringVar(value=self.settings.win32_window_title)
        self.adb_path_var = tk.StringVar(value=self.settings.adb_path or resolve_adb_path(""))
        self.adb_target_var = tk.StringVar(value=self.settings.adb_address)
        self.connection_summary_var = tk.StringVar(value="")
        self.discovered_adb_devices = []
        self.adb_device_labels: list[str] = []
        self.adb_label_to_address: dict[str, str] = {}

        # 领域服务
        self.zone_state = ZoneState()
        self.history = UndoRedoHistory[list[PathPoint]](max_depth=50)
        self.point_editor = PointEditingService()

        # 编辑态
        self.selected_idx: int | None = None
        self.selected_indices: set[int] = set()
        self.zone_point_global_indices: list[int] = []

        # 画布对象池
        self.path_line_id: int | None = None
        self.ui_nodes: list[int] = []
        self.ui_texts: list[int] = []
        self.selection_rect_id: int | None = None
        self.assert_rect_id: int | None = None
        self.assert_rect_text_id: int | None = None

        # 交互状态
        self.drag_start_x = 0
        self.drag_start_y = 0
        self.is_panning = False
        self.is_dragging = False
        self.is_box_selecting = False
        self.is_assert_selecting = False
        self.is_pan_candidate = False
        self.box_select_start_x = 0
        self.box_select_start_y = 0
        self.assert_start_world_x = 0.0
        self.assert_start_world_y = 0.0
        self.assert_rect_world: tuple[float, float, float, float] | None = None
        self.pointer_down_x = 0
        self.pointer_down_y = 0
        self._redraw_pending = False

        self._build_layout()
        self.renderer = MapRenderer(self.canvas, root, MAP_IMAGE_DIR)
        self._bind_events()
        self._sync_connection_controls()
        self._refresh_connection_summary()
        self._sync_assert_controls()
        self._refresh_zone_label()

    def _build_layout(self) -> None:
        toolbar_frame = tk.Frame(self.root)
        toolbar_frame.pack(fill=tk.X, pady=2, padx=8)

        primary_row = tk.Frame(toolbar_frame)
        primary_row.pack(fill=tk.X)

        left_frame = tk.Frame(primary_row)
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

        self.btn_copy_assert = tk.Button(left_frame, text="📍 复制 Assert", command=self.copy_assert_location, padx=10)
        self.btn_copy_assert.pack(side=tk.LEFT, padx=3)

        self.btn_import = tk.Button(left_frame, text="📂 导入 JSON", command=self.import_json, padx=10)
        self.btn_import.pack(side=tk.LEFT, padx=3)

        zone_frame = tk.Frame(primary_row)
        zone_frame.pack(side=tk.LEFT, fill=tk.X, expand=True, padx=12)

        self.btn_prev = tk.Button(zone_frame, text="◀", command=self.prev_zone, width=4)
        self.btn_prev.pack(side=tk.LEFT, padx=(0, 4))

        self.zone_label = tk.Label(
            zone_frame,
            text="— 无区域信息 —",
            font=("Consolas", 10, "bold"),
            fg="#1e293b",
            anchor="center",
        )
        self.zone_label.pack(side=tk.LEFT, expand=True, fill=tk.X, padx=4)

        self.btn_next = tk.Button(zone_frame, text="▶", command=self.next_zone, width=4)
        self.btn_next.pack(side=tk.LEFT, padx=(4, 0))

        density_frame = tk.Frame(primary_row)
        density_frame.pack(side=tk.RIGHT, fill=tk.Y)

        tk.Label(density_frame, text="密度:", font=("Microsoft YaHei", 9)).pack(side=tk.LEFT, padx=(2, 0))
        self.slider_density = tk.Scale(
            density_frame,
            from_=0,
            to=100,
            orient=tk.HORIZONTAL,
            variable=self.density_val,
            showvalue=False,
            width=10,
            length=88,
            command=lambda _value: self.reprocess_points(),
        )
        self.slider_density.pack(side=tk.LEFT)

        self.btn_zoom_out = tk.Button(density_frame, text="-", command=self.zoom_out, width=3)
        self.btn_zoom_out.pack(side=tk.LEFT, padx=(6, 2))

        self.btn_zoom_in = tk.Button(density_frame, text="+", command=self.zoom_in, width=3)
        self.btn_zoom_in.pack(side=tk.LEFT, padx=(0, 6))

        self.disable_optimization_check = tk.Checkbutton(
            density_frame,
            text="禁优化",
            variable=self.disable_optimization_var,
            onvalue=True,
            offvalue=False,
            font=("Microsoft YaHei", 9),
            command=self._on_optimization_mode_changed,
        )
        self.disable_optimization_check.pack(side=tk.LEFT, padx=(6, 0))

        secondary_row = tk.Frame(toolbar_frame)
        secondary_row.pack(fill=tk.X, pady=(4, 0))

        tk.Label(secondary_row, text="动作:", font=("Microsoft YaHei", 9)).pack(side=tk.LEFT, padx=(0, 4))
        self.action_menu = ttk.Combobox(secondary_row, values=ACTION_MENU_NAMES, width=10, state="readonly")
        self.action_menu.set(ACTION_NAMES[ActionType.RUN])
        self.action_menu.pack(side=tk.LEFT, padx=2)

        self.btn_apply_action = tk.Button(secondary_row, text="设单", command=self.apply_action_to_selected)
        self.btn_apply_action.pack(side=tk.LEFT, padx=2)

        self.btn_append_action = tk.Button(secondary_row, text="追加", command=self.append_action_to_selected)
        self.btn_append_action.pack(side=tk.LEFT, padx=2)

        self.btn_pop_action = tk.Button(secondary_row, text="退一", command=self.pop_action_from_selected, width=4)
        self.btn_pop_action.pack(side=tk.LEFT, padx=2)

        self.action_chain_label = tk.Label(
            secondary_row,
            textvariable=self.action_chain_var,
            font=("Consolas", 8),
            fg="#475569",
            anchor="w",
        )
        self.action_chain_label.pack(side=tk.LEFT, fill=tk.X, expand=True, padx=(8, 8))

        self.assert_mode_check = tk.Checkbutton(
            secondary_row,
            text="Assert 模式",
            variable=self.assert_mode_var,
            onvalue=True,
            offvalue=False,
            font=("Microsoft YaHei", 9),
            command=self._on_assert_mode_changed,
        )
        self.assert_mode_check.pack(side=tk.LEFT, padx=(4, 2))

        self.assert_zone_combo = ttk.Combobox(
            secondary_row,
            values=self.available_zone_ids,
            width=20,
            state="disabled",
            textvariable=self.assert_zone_var,
        )
        self.assert_zone_combo.pack(side=tk.LEFT, padx=(2, 8))
        self.assert_zone_combo.bind("<<ComboboxSelected>>", lambda _event: self._on_assert_zone_changed())

        self.strict_check = tk.Checkbutton(
            secondary_row,
            text="严格",
            variable=self.strict_var,
            onvalue=True,
            offvalue=False,
            font=("Microsoft YaHei", 9),
        )
        self.strict_check.pack(side=tk.LEFT, padx=(4, 2))

        self.btn_del_point = tk.Button(
            secondary_row,
            text="🗑",
            command=self.delete_selected_point,
            fg="#e74c3c",
            font=("", 10, "bold"),
        )
        self.btn_del_point.pack(side=tk.LEFT, padx=2)

        connection_row = tk.Frame(toolbar_frame)
        connection_row.pack(fill=tk.X, pady=(4, 0))

        tk.Label(connection_row, text="连接:", font=("Microsoft YaHei", 9)).pack(side=tk.LEFT, padx=(0, 4))
        self.connection_kind_combo = ttk.Combobox(
            connection_row,
            values=["Win32 窗口", "ADB 设备"],
            width=10,
            state="readonly",
        )
        self.connection_kind_combo.set("ADB 设备" if self.connection_kind_var.get() == "adb" else "Win32 窗口")
        self.connection_kind_combo.pack(side=tk.LEFT, padx=(0, 8))
        self.connection_kind_combo.bind("<<ComboboxSelected>>", lambda _event: self._on_connection_kind_changed())

        self.connection_controls_frame = tk.Frame(connection_row)
        self.connection_controls_frame.pack(side=tk.LEFT)

        self.win32_label = tk.Label(self.connection_controls_frame, text="窗口标题:", font=("Microsoft YaHei", 9))
        self.win32_label.pack(side=tk.LEFT, padx=(0, 4))
        self.win32_entry = tk.Entry(self.connection_controls_frame, textvariable=self.win32_window_title_var, width=18)
        self.win32_entry.pack(side=tk.LEFT, padx=(0, 10))

        self.adb_path_label = tk.Label(self.connection_controls_frame, text="ADB:", font=("Microsoft YaHei", 9))
        self.adb_path_entry = tk.Entry(self.connection_controls_frame, textvariable=self.adb_path_var, width=28)
        self.btn_browse_adb = tk.Button(self.connection_controls_frame, text="浏览", command=self._browse_adb_path, width=5)
        self.adb_target_label = tk.Label(self.connection_controls_frame, text="设备:", font=("Microsoft YaHei", 9))
        self.adb_target_combo = ttk.Combobox(self.connection_controls_frame, textvariable=self.adb_target_var, width=34)
        self.btn_refresh_adb = tk.Button(self.connection_controls_frame, text="刷新", command=self.refresh_adb_devices, width=5)

        self.connection_summary_label = tk.Label(
            connection_row,
            textvariable=self.connection_summary_var,
            font=("Consolas", 8),
            fg="#475569",
            anchor="w",
        )
        self.connection_summary_label.pack(side=tk.LEFT, fill=tk.X, expand=True, padx=(8, 0))

        self.adb_target_combo.bind("<<ComboboxSelected>>", lambda _event: self._on_adb_target_selected())

        self.status_label = tk.Label(
            self.root,
            text="准备就绪",
            fg="#64748b",
            anchor="w",
            font=("Microsoft YaHei", 9),
        )
        self.status_label.pack(fill=tk.X, padx=10, pady=2)

        self.locator_debug_label = tk.Label(
            self.root,
            textvariable=self.locator_debug_var,
            fg="#475569",
            anchor="w",
            justify=tk.LEFT,
            font=("Consolas", 8),
        )
        self.locator_debug_label.pack(fill=tk.X, padx=10, pady=(0, 4))

        self.canvas = tk.Canvas(self.root, bg="#0f172a", highlightthickness=0)
        self.canvas.pack(fill=tk.BOTH, expand=True)

    def _bind_events(self) -> None:
        self.canvas.bind("<Button-1>", self.on_click)
        self.canvas.bind("<B1-Motion>", self.on_drag)
        self.canvas.bind("<ButtonRelease-1>", self.on_release)
        self.canvas.bind("<Button-3>", self.on_pan_start)
        self.canvas.bind("<B3-Motion>", self.on_pan_move)
        self.canvas.bind("<ButtonRelease-3>", self.on_pan_end)
        self.canvas.bind("<Configure>", lambda _event: self.schedule_redraw(fast=True))
        self.canvas.bind("<MouseWheel>", self.on_scroll)
        self.canvas.bind("<Button-4>", self.on_scroll)
        self.canvas.bind("<Button-5>", self.on_scroll)

        self.root.bind_all("<Control-z>", lambda _event: self.undo())
        self.root.bind_all("<Control-y>", lambda _event: self.redo())
        self.root.bind_all("<Delete>", self._on_delete_key)
        self.root.bind_all("<BackSpace>", self._on_delete_key)
        self.root.bind_all("<plus>", lambda _event: self.zoom_in())
        self.root.bind_all("<equal>", lambda _event: self.zoom_in())
        self.root.bind_all("<KP_Add>", lambda _event: self.zoom_in())
        self.root.bind_all("<minus>", lambda _event: self.zoom_out())
        self.root.bind_all("<underscore>", lambda _event: self.zoom_out())
        self.root.bind_all("<KP_Subtract>", lambda _event: self.zoom_out())
        self.adb_path_var.trace_add("write", lambda *_args: self._refresh_connection_summary())
        self.adb_target_var.trace_add("write", lambda *_args: self._refresh_connection_summary())
        self.win32_window_title_var.trace_add("write", lambda *_args: self._refresh_connection_summary())

    def _on_delete_key(self, event) -> None:
        widget = event.widget.focus_get() if hasattr(event.widget, "focus_get") else None
        if widget is None and hasattr(self.root, "focus_get"):
            widget = self.root.focus_get()

        text_like_types = [tk.Entry, tk.Text]
        for ttk_widget_name in ("Entry", "Combobox", "Spinbox"):
            ttk_widget_type = getattr(ttk, ttk_widget_name, None)
            if ttk_widget_type is not None:
                text_like_types.append(ttk_widget_type)

        if isinstance(widget, tuple(text_like_types)):
            return

        self.delete_selected_point()

    def _set_status(self, text: str, color: str) -> None:
        self.status_label.config(text=text, fg=color)

    def _set_locator_debug(self, text: str) -> None:
        self.locator_debug_var.set(text)

    def _connection_kind(self) -> str:
        return "adb" if self.connection_kind_combo.get() == "ADB 设备" else "win32"

    def _on_connection_kind_changed(self) -> None:
        self.connection_kind_var.set(self._connection_kind())
        self._sync_connection_controls()
        self._refresh_connection_summary()
        self._persist_settings()

    def _sync_connection_controls(self) -> None:
        is_adb = self._connection_kind() == "adb"

        win32_widgets = [self.win32_label, self.win32_entry]
        adb_widgets = [
            self.adb_path_label,
            self.adb_path_entry,
            self.btn_browse_adb,
            self.adb_target_label,
            self.adb_target_combo,
            self.btn_refresh_adb,
        ]

        for widget in win32_widgets:
            if is_adb:
                widget.pack_forget()
            elif not widget.winfo_manager():
                widget.pack(side=tk.LEFT, padx=(0, 4) if widget is self.win32_label else (0, 10))

        if is_adb:
            adb_pack_specs = [
                (self.adb_path_label, {"side": tk.LEFT, "padx": (0, 4)}),
                (self.adb_path_entry, {"side": tk.LEFT, "padx": (0, 4)}),
                (self.btn_browse_adb, {"side": tk.LEFT, "padx": (0, 10)}),
                (self.adb_target_label, {"side": tk.LEFT, "padx": (0, 4)}),
                (self.adb_target_combo, {"side": tk.LEFT, "padx": (0, 4)}),
                (self.btn_refresh_adb, {"side": tk.LEFT, "padx": (0, 8)}),
            ]
            for widget, pack_kwargs in adb_pack_specs:
                if not widget.winfo_manager():
                    widget.pack(**pack_kwargs)
        else:
            for widget in adb_widgets:
                widget.pack_forget()

        if is_adb and not self.adb_device_labels:
            self.refresh_adb_devices()

    def _refresh_connection_summary(self) -> None:
        if self._connection_kind() == "adb":
            adb_path = self.adb_path_var.get().strip() or "<PATH>"
            target = self.adb_target_var.get().strip() or "未选择设备"
            self.connection_summary_var.set(f"ADB: {adb_path} -> {target}")
            return

        title = self.win32_window_title_var.get().strip() or "Endfield"
        self.connection_summary_var.set(f"Win32: title={title}")

    def _browse_adb_path(self) -> None:
        file_path = filedialog.askopenfilename(title="选择 adb 可执行文件")
        if not file_path:
            return
        self.adb_path_var.set(file_path)
        self._persist_settings()
        self.refresh_adb_devices()

    def _merge_recent_adb_targets(self, addresses: list[str]) -> list[str]:
        merged: list[str] = []
        for address in addresses + self.settings.recent_adb_targets:
            normalized = address.strip()
            if not normalized or normalized in merged:
                continue
            merged.append(normalized)
        return merged[:10]

    def _on_adb_target_selected(self) -> None:
        selected = self.adb_target_var.get().strip()
        mapped = self.adb_label_to_address.get(selected, selected)
        if mapped != selected:
            self.adb_target_var.set(mapped)
        self._persist_settings()

    def refresh_adb_devices(self) -> None:
        adb_path = self.adb_path_var.get().strip()
        resolved_path = resolve_adb_path(adb_path)

        devices = list_adb_devices(adb_path)
        self.discovered_adb_devices = devices
        self.adb_label_to_address = {}
        self.adb_device_labels = []
        for device in devices:
            label = device.display_name()
            self.adb_device_labels.append(label)
            self.adb_label_to_address[label] = device.address

        recent_addresses = self._merge_recent_adb_targets([device.address for device in devices])
        combo_values = self.adb_device_labels + [address for address in recent_addresses if address not in self.adb_device_labels]
        self.adb_target_combo["values"] = combo_values

        current_target = self.adb_target_var.get().strip()
        if not current_target:
            online_device = next((device.address for device in devices if device.state == "device"), "")
            if online_device:
                self.adb_target_var.set(online_device)

        self._persist_settings()
        if resolved_path:
            self._set_status(f"已刷新 ADB 设备，共 {len(devices)} 个。", "#10b981")
        else:
            self._set_status("未找到 adb，可手动指定 adb 路径。", "#f59e0b")

    def _build_recording_session(self) -> RecordingSessionConfig:
        kind = self._connection_kind()
        session = RecordingSessionConfig(
            kind=kind,
            win32=Win32ConnectionConfig(window_title=self.win32_window_title_var.get().strip() or "Endfield"),
            adb=AdbConnectionConfig(
                adb_path=self.adb_path_var.get().strip(),
                address=self.adb_target_var.get().strip(),
            ),
        )
        return session

    def _persist_settings(self) -> None:
        self.settings = MapNavigatorSettings(
            connection_kind=self._connection_kind(),
            adb_path=self.adb_path_var.get().strip(),
            adb_address=self.adb_target_var.get().strip(),
            win32_window_title=self.win32_window_title_var.get().strip() or "Endfield",
            recent_adb_targets=self._merge_recent_adb_targets(
                [self.adb_target_var.get().strip()] + self.settings.recent_adb_targets
            ),
        )
        try:
            self.settings_store.save(self.settings)
        except Exception:
            return

    @staticmethod
    def _is_box_select_modifier_pressed(event) -> bool:
        return bool(getattr(event, "state", 0) & RouteEditorApp.BOX_SELECT_MODIFIER_MASK)

    @staticmethod
    def _movement_exceeded_threshold(start_x: int, start_y: int, current_x: int, current_y: int) -> bool:
        return (
            abs(current_x - start_x) > RouteEditorApp.DRAG_ACTIVATION_DISTANCE
            or abs(current_y - start_y) > RouteEditorApp.DRAG_ACTIVATION_DISTANCE
        )

    def _zoom_view(self, factor: float, focus_x: int | None = None, focus_y: int | None = None) -> None:
        if focus_x is None or focus_y is None:
            focus_x = self.canvas.winfo_width() // 2
            focus_y = self.canvas.winfo_height() // 2

        world_x, world_y = self.renderer.canvas_to_world(focus_x, focus_y)
        new_scale = self.renderer.view_scale * factor
        new_scale = max(0.002, min(500.0, new_scale))

        new_off_x = focus_x / new_scale - world_x
        new_off_y = focus_y / new_scale - world_y

        self.renderer.set_viewport(new_scale, new_off_x, new_off_y)
        self.schedule_redraw(fast=True)

    def zoom_in(self) -> None:
        self._zoom_view(1.25)

    def zoom_out(self) -> None:
        self._zoom_view(0.8)

    def _on_optimization_mode_changed(self) -> None:
        slider_state = tk.DISABLED if self.disable_optimization_var.get() else tk.NORMAL
        self.slider_density.config(state=slider_state)
        if self.raw_points:
            self.reprocess_points()

    def _default_assert_zone(self) -> str:
        current_zone = normalize_zone_id(self.zone_state.current_zone())
        if current_zone:
            return current_zone
        return self.available_zone_ids[0] if self.available_zone_ids else ""

    def _display_zone_id(self) -> str:
        if self.assert_mode_var.get():
            return normalize_zone_id(self.assert_zone_var.get(), default=self._default_assert_zone())
        return self.zone_state.current_zone()

    def _current_assert_target(self) -> tuple[float, float, float, float] | None:
        if self.assert_rect_world is None:
            return None
        x0, y0, x1, y1 = self.assert_rect_world
        left, right = sorted((x0, x1))
        top, bottom = sorted((y0, y1))
        return round(left, 2), round(top, 2), round(right - left, 2), round(bottom - top, 2)

    def _clear_assert_rect(self, redraw: bool = True) -> None:
        self.assert_rect_world = None
        self.is_assert_selecting = False
        if self.assert_rect_id is not None:
            self.canvas.itemconfig(self.assert_rect_id, state="hidden")
        if self.assert_rect_text_id is not None:
            self.canvas.itemconfig(self.assert_rect_text_id, state="hidden")
        if redraw:
            self.schedule_redraw(fast=True)

    def _set_assert_rect_world(self, x0: float, y0: float, x1: float, y1: float) -> None:
        self.assert_rect_world = (x0, y0, x1, y1)

    def _sync_assert_controls(self) -> None:
        if self.assert_mode_var.get():
            self.btn_prev.config(state=tk.DISABLED)
            self.btn_next.config(state=tk.DISABLED)
            combo_state = "readonly" if self.available_zone_ids else "disabled"
            self.assert_zone_combo.config(state=combo_state)
        else:
            self.btn_prev.config(state=tk.NORMAL)
            self.btn_next.config(state=tk.NORMAL)
            self.assert_zone_combo.config(state="disabled")

    def _on_assert_mode_changed(self) -> None:
        if self.assert_mode_var.get():
            if not self.available_zone_ids and not normalize_zone_id(self.zone_state.current_zone()):
                messagebox.showerror("Assert 模式不可用", "未找到可用 zone 底图，无法进入 Assert 模式。")
                self.assert_mode_var.set(False)
                return
            if not normalize_zone_id(self.assert_zone_var.get()):
                self.assert_zone_var.set(self._default_assert_zone())
            self._set_status("Assert 模式：先选地图，再用左键拖拽框出判定区域；Delete 或垃圾桶可清除。", "#3b82f6")
        else:
            self.is_assert_selecting = False
            self._set_status("返回路径编辑模式。", "#10b981")
        self._sync_assert_controls()
        self._refresh_zone_label()
        self.fit_view()

    def _on_assert_zone_changed(self) -> None:
        zone_id = normalize_zone_id(self.assert_zone_var.get())
        if not zone_id:
            return
        self.assert_zone_var.set(zone_id)
        self._clear_assert_rect(redraw=False)
        self._refresh_zone_label()
        self.fit_view()

    def _refresh_zone_label(self) -> None:
        if self.assert_mode_var.get():
            zone_id = self._display_zone_id()
            text = f"Assert: {zone_id}" if zone_id else "Assert: 请选择地图"
            self.zone_label.config(text=self._compact_zone_label_text(text))
            return
        self.zone_label.config(text=self._compact_zone_label_text(self.zone_state.label_text()))

    def _on_points_structure_changed(self, redraw_fast: bool = False) -> None:
        self.points = normalize_path_points(self.points)
        self.zone_state.rebuild(self.points)
        current_zone_indices = self.zone_state.point_indices(self.points)
        self._normalize_selection_state(current_zone_indices)
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
        self._normalize_selection_state(zone_indices)
        if self.selected_idx is None or self.selected_idx >= len(zone_indices):
            return None
        return self.points[zone_indices[self.selected_idx]]

    def _normalize_selection_state(self, zone_indices: list[int] | None = None) -> None:
        if zone_indices is None:
            zone_indices = self.zone_point_global_indices

        valid_count = len(zone_indices)
        self.selected_indices = {idx for idx in self.selected_indices if 0 <= idx < valid_count}
        if not self.selected_indices:
            self.selected_idx = None
        elif self.selected_idx not in self.selected_indices:
            self.selected_idx = min(self.selected_indices)

    def _clear_selection(self) -> None:
        self.selected_idx = None
        self.selected_indices.clear()

    def _set_selection(self, indices_in_zone: list[int], primary_idx: int | None = None) -> None:
        self.selected_indices = set(indices_in_zone)
        if not self.selected_indices:
            self._clear_selection()
            return
        self.selected_idx = primary_idx if primary_idx in self.selected_indices else min(self.selected_indices)

    def _show_selection_rect(self, x0: int, y0: int, x1: int, y1: int) -> None:
        if self.selection_rect_id is None:
            self.selection_rect_id = self.canvas.create_rectangle(
                x0,
                y0,
                x1,
                y1,
                outline="#38bdf8",
                width=2,
                dash=(4, 2),
            )
        else:
            self.canvas.coords(self.selection_rect_id, x0, y0, x1, y1)
            self.canvas.itemconfig(self.selection_rect_id, state="normal")
        self.canvas.tag_raise(self.selection_rect_id)

    def _hide_selection_rect(self) -> None:
        if self.selection_rect_id is not None:
            self.canvas.itemconfig(self.selection_rect_id, state="hidden")

    def _collect_indices_in_rect(self, x0: float, y0: float, x1: float, y1: float) -> list[int]:
        left, right = sorted((x0, x1))
        top, bottom = sorted((y0, y1))
        selected: list[int] = []
        for idx_in_zone, global_idx in enumerate(self.zone_point_global_indices):
            point = self.points[global_idx]
            cx, cy = self.renderer.world_to_canvas(point["x"], point["y"])
            if left <= cx <= right and top <= cy <= bottom:
                selected.append(idx_in_zone)
        return selected

    def _sync_action_controls(self, zone_indices: list[int] | None = None) -> None:
        if zone_indices is None:
            zone_indices = self.zone_point_global_indices
        self._normalize_selection_state(zone_indices)

        selected_indices = sorted(self.selected_indices)
        if not selected_indices:
            self._reset_point_property_controls()
            return

        if len(selected_indices) > 1:
            selected_points = [self.points[zone_indices[idx]] for idx in selected_indices]
            action_chains = {tuple(get_point_actions(point)) for point in selected_points}
            strict_values = {bool(point.get("strict", False)) for point in selected_points}
            if len(action_chains) == 1:
                unified_actions = list(next(iter(action_chains)))
                self.action_menu.set(ACTION_NAMES.get(unified_actions[-1], "Run"))
            if len(strict_values) == 1:
                self.strict_var.set(next(iter(strict_values)))
            self.action_chain_var.set(f"多选 {len(selected_indices)} 点")
            return

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
        self._persist_settings()
        self.root.destroy()

    # ---- 视图交互 ----
    def on_scroll(self, event) -> None:
        delta = getattr(event, "delta", 0)
        if delta:
            factor = 1.25 if float(delta) > 0 else 0.8
        else:
            button_num = getattr(event, "num", 0)
            if button_num == 4:
                factor = 1.25
            elif button_num == 5:
                factor = 0.8
            else:
                return
        self._zoom_view(factor, focus_x=event.x, focus_y=event.y)

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
        zone_id = self._display_zone_id()
        points = [] if self.assert_mode_var.get() else self.zone_state.current_points(self.points)

        box_min_x, box_max_x, box_min_y, box_max_y = 0, 100, 0, 100
        map_image = self.renderer._get_map_pil(zone_id)
        if map_image:
            box_max_x, box_max_y = map_image.size

        assert_target = self._current_assert_target()
        if self.assert_mode_var.get() and assert_target is not None:
            target_x, target_y, target_w, target_h = assert_target
            box_min_x, box_max_x = target_x, target_x + target_w
            box_min_y, box_max_y = target_y, target_y + target_h
        elif points:
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
        zone_id = self._display_zone_id()
        if self.assert_mode_var.get():
            self.zone_point_global_indices = []
            points = []
        else:
            self.zone_point_global_indices = self.zone_state.point_indices(self.points)
            points = [self.points[index] for index in self.zone_point_global_indices]

        self.renderer.request_render(zone_id, fast=fast)
        self._render_path(points)
        self._render_nodes(points)
        self._render_assert_rect()

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

            is_selected = idx in self.selected_indices
            is_primary_selected = self.selected_idx == idx
            if is_primary_selected:
                outline_color = "#ef4444"
                outline_width = 3
            elif is_selected:
                outline_color = "#f59e0b"
                outline_width = 2
            else:
                outline_color = "#facc15" if is_strict else "white"
                outline_width = 1
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
        if self.selection_rect_id is not None:
            self.canvas.tag_raise(self.selection_rect_id)

    def _render_assert_rect(self) -> None:
        target = self._current_assert_target()
        if not self.assert_mode_var.get() or target is None:
            if self.assert_rect_id is not None:
                self.canvas.itemconfig(self.assert_rect_id, state="hidden")
            if self.assert_rect_text_id is not None:
                self.canvas.itemconfig(self.assert_rect_text_id, state="hidden")
            return

        target_x, target_y, target_w, target_h = target
        x0, y0 = self.renderer.world_to_canvas(target_x, target_y)
        x1, y1 = self.renderer.world_to_canvas(target_x + target_w, target_y + target_h)

        if self.assert_rect_id is None:
            self.assert_rect_id = self.canvas.create_rectangle(
                x0,
                y0,
                x1,
                y1,
                outline="#f43f5e",
                fill="#f43f5e",
                stipple="gray25",
                width=3,
            )
        else:
            self.canvas.coords(self.assert_rect_id, x0, y0, x1, y1)
            self.canvas.itemconfig(
                self.assert_rect_id,
                outline="#f43f5e",
                fill="#f43f5e",
                stipple="gray25",
                width=3,
                state="normal",
            )

        label_text = f"Assert [{target_x:.1f}, {target_y:.1f}, {target_w:.1f}, {target_h:.1f}]"
        label_x = min(x0, x1) + 8
        label_y = min(y0, y1) + 8
        if self.assert_rect_text_id is None:
            self.assert_rect_text_id = self.canvas.create_text(
                label_x,
                label_y,
                text=label_text,
                fill="#fff1f2",
                anchor="nw",
                font=("Consolas", 9, "bold"),
            )
        else:
            self.canvas.coords(self.assert_rect_text_id, label_x, label_y)
            self.canvas.itemconfig(self.assert_rect_text_id, text=label_text, fill="#fff1f2", state="normal")

        self.canvas.tag_raise(self.assert_rect_id)
        self.canvas.tag_raise(self.assert_rect_text_id)
        if self.selection_rect_id is not None:
            self.canvas.tag_raise(self.selection_rect_id)

    # ---- 区域导航 ----
    def prev_zone(self) -> None:
        self.zone_state.prev_zone()
        self._clear_selection()
        self._reset_point_property_controls()
        self._refresh_zone_label()
        self.fit_view()

    def next_zone(self) -> None:
        self.zone_state.next_zone()
        self._clear_selection()
        self._reset_point_property_controls()
        self._refresh_zone_label()
        self.fit_view()

    # ---- 录制控制 ----
    def start_recording(self) -> None:
        if not self.recording_service:
            messagebox.showerror("环境错误", "未找到 maafw 库，请先安装 requirements 并配置运行环境。")
            return
        if self.recording_service.is_running:
            return

        session = self._build_recording_session()
        if session.kind == "adb" and not session.adb.address:
            messagebox.showerror("连接错误", "请选择 ADB 设备或手动填写设备序列号/地址。")
            return

        self._persist_settings()
        self.btn_start.config(state=tk.DISABLED)
        self.btn_stop.config(state=tk.NORMAL)
        self._set_status(f"● 正在启动识别引擎... [{session.display_name()}]", "#3b82f6")
        self._set_locator_debug("Locator: waiting for first result...")
        try:
            self.recording_service.start(session)
        except Exception as exc:
            self._on_recording_error(str(exc))

    def stop_recording(self) -> None:
        if not self.recording_service:
            return
        self.recording_service.stop()
        if self.disable_optimization_var.get():
            self._set_status("正在停止录制并输出完整路径点...", "#f59e0b")
        else:
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
        if self.disable_optimization_var.get():
            self.points = normalize_path_points(self.raw_points)
        else:
            self.points = simplify_path(self.raw_points, self.density_val.get())
        self.history.clear()
        self._clear_selection()
        self._reset_point_property_controls()
        self._on_points_structure_changed(redraw_fast=False)

    def _reset_ui(self) -> None:
        self.btn_start.config(state=tk.NORMAL)
        self.btn_stop.config(state=tk.DISABLED)
        self._set_status(
            "录制结束。鼠标滚轮缩放，右键平移，左键空白拖拽也可平移，左键单击空白插点，左键拖拽点微调，Ctrl+点击增减选，Ctrl+左键框选批量操作。也可用 +/- 按钮或键盘 +/- 缩放。",
            "#10b981",
        )

    # ---- 撤销与重做 ----
    def push_undo(self) -> None:
        self.history.snapshot(self.points)

    def undo(self) -> None:
        restored = self.history.undo(self.points)
        if restored is None:
            return
        self.points = restored
        self._clear_selection()
        self._reset_point_property_controls()
        self._on_points_structure_changed(redraw_fast=False)

    def redo(self) -> None:
        restored = self.history.redo(self.points)
        if restored is None:
            return
        self.points = restored
        self._clear_selection()
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
        if self.assert_mode_var.get():
            zone_id = self._display_zone_id()
            if not zone_id:
                messagebox.showinfo("提示", "请先在 Assert 模式下选择地图。")
                return
            self.is_dragging = False
            self.is_pan_candidate = False
            self.is_panning = False
            self.is_box_selecting = False
            self.is_assert_selecting = True
            self.assert_start_world_x, self.assert_start_world_y = self.renderer.canvas_to_world(event.x, event.y)
            self._set_assert_rect_world(
                self.assert_start_world_x,
                self.assert_start_world_y,
                self.assert_start_world_x,
                self.assert_start_world_y,
            )
            self.schedule_redraw(fast=True)
            return

        if self._is_box_select_modifier_pressed(event):
            self.is_box_selecting = True
            self.is_dragging = False
            self.is_pan_candidate = False
            self.is_panning = False
            self.box_select_start_x = event.x
            self.box_select_start_y = event.y
            self._show_selection_rect(event.x, event.y, event.x, event.y)
            return

        idx_in_zone = self.get_node_at(event.x, event.y)
        if idx_in_zone is None:
            self.is_dragging = False
            self.is_panning = False
            self.is_pan_candidate = True
            self.pointer_down_x = event.x
            self.pointer_down_y = event.y
            return

        self.push_undo()
        self.is_pan_candidate = False
        self.is_panning = False
        self._set_selection([idx_in_zone], primary_idx=idx_in_zone)
        self.is_dragging = True

        self._sync_action_controls()
        self.schedule_redraw(fast=True)

    def apply_action_to_selected(self) -> None:
        self._normalize_selection_state()
        if not self.selected_indices:
            messagebox.showinfo("提示", "请先点击选中一个点")
            return

        self.push_undo()
        changed = False
        for selected_idx in sorted(self.selected_indices):
            changed = self.point_editor.apply_attributes(
                points=self.points,
                zone_indices=self.zone_point_global_indices,
                selected_idx=selected_idx,
                action_name=self.action_menu.get(),
                strict_arrival=self.strict_var.get(),
            ) or changed
        if changed:
            self._sync_action_controls()
            self._on_points_structure_changed(redraw_fast=False)

    def append_action_to_selected(self) -> None:
        self._normalize_selection_state()
        if not self.selected_indices:
            messagebox.showinfo("提示", "请先点击选中一个点")
            return

        self.push_undo()
        action_type = self.point_editor.action_name_to_type(self.action_menu.get())
        for selected_idx in sorted(self.selected_indices):
            point = self.points[self.zone_point_global_indices[selected_idx]]
            set_manual_point_actions(point, get_point_actions(point) + [action_type])
        self._sync_action_controls()
        self._on_points_structure_changed(redraw_fast=False)

    def pop_action_from_selected(self) -> None:
        self._normalize_selection_state()
        if not self.selected_indices:
            messagebox.showinfo("提示", "请先点击选中一个点")
            return

        self.push_undo()
        for selected_idx in sorted(self.selected_indices):
            point = self.points[self.zone_point_global_indices[selected_idx]]
            actions = get_point_actions(point)
            if len(actions) <= 1:
                set_manual_point_actions(point, [int(ActionType.RUN)])
            else:
                set_manual_point_actions(point, actions[:-1])
        self._sync_action_controls()
        self._on_points_structure_changed(redraw_fast=False)

    def delete_selected_point(self) -> None:
        if self.assert_mode_var.get():
            if self.assert_rect_world is None:
                messagebox.showinfo("提示", "当前没有可删除的 Assert 区域")
                return
            self._clear_assert_rect(redraw=False)
            self._set_status("已清除 Assert 区域。", "#10b981")
            self.schedule_redraw(fast=True)
            return

        self._normalize_selection_state()
        if not self.selected_indices:
            messagebox.showinfo("提示", "请先点击选中一个点")
            return

        self.push_undo()
        global_indices = sorted((self.zone_point_global_indices[idx] for idx in self.selected_indices), reverse=True)
        for global_idx in global_indices:
            self.points.pop(global_idx)
        if global_indices:
            self._clear_selection()
            self._reset_point_property_controls()
            self._on_points_structure_changed(redraw_fast=False)

    def on_drag(self, event) -> None:
        if self.assert_mode_var.get():
            if not self.is_assert_selecting:
                return
            world_x, world_y = self.renderer.canvas_to_world(event.x, event.y)
            self._set_assert_rect_world(self.assert_start_world_x, self.assert_start_world_y, world_x, world_y)
            self.schedule_redraw(fast=True)
            return

        if self.is_box_selecting:
            self._show_selection_rect(self.box_select_start_x, self.box_select_start_y, event.x, event.y)
            return

        if self.is_pan_candidate:
            if not self._movement_exceeded_threshold(self.pointer_down_x, self.pointer_down_y, event.x, event.y):
                return
            self.is_pan_candidate = False
            self.is_panning = True
            self.drag_start_x, self.drag_start_y = event.x, event.y
            self.canvas.config(cursor="fleur")
            return

        if self.is_panning:
            dx = (event.x - self.drag_start_x) / self.renderer.view_scale
            dy = (event.y - self.drag_start_y) / self.renderer.view_scale
            self.renderer.view_offset_x += dx
            self.renderer.view_offset_y += dy
            self.drag_start_x, self.drag_start_y = event.x, event.y
            self.schedule_redraw(fast=True)
            return

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

    def on_release(self, event) -> None:
        if self.assert_mode_var.get():
            if not self.is_assert_selecting:
                return
            world_x, world_y = self.renderer.canvas_to_world(event.x, event.y)
            self._set_assert_rect_world(self.assert_start_world_x, self.assert_start_world_y, world_x, world_y)
            self.is_assert_selecting = False
            target = self._current_assert_target()
            if target is not None:
                zone_id = self._display_zone_id()
                self._set_status(
                    f"Assert 区域已更新: zone={zone_id} target=[{target[0]:.1f}, {target[1]:.1f}, {target[2]:.1f}, {target[3]:.1f}]",
                    "#10b981",
                )
            self.schedule_redraw(fast=True)
            return

        if self.is_box_selecting:
            if abs(event.x - self.box_select_start_x) <= 4 and abs(event.y - self.box_select_start_y) <= 4:
                idx_in_zone = self.get_node_at(event.x, event.y)
                if idx_in_zone is not None:
                    selected = set(self.selected_indices)
                    if idx_in_zone in selected:
                        selected.remove(idx_in_zone)
                    else:
                        selected.add(idx_in_zone)
                    self._set_selection(list(selected), primary_idx=idx_in_zone)
            else:
                self._set_selection(
                    self._collect_indices_in_rect(
                        self.box_select_start_x,
                        self.box_select_start_y,
                        event.x,
                        event.y,
                    ),
                )
            self._sync_action_controls()
            self._hide_selection_rect()
            self.is_box_selecting = False
            self.schedule_redraw(fast=True)
            return

        if self.is_panning:
            self.is_panning = False
            self.canvas.config(cursor="cross")
            return

        if self.is_pan_candidate:
            self.is_pan_candidate = False
            self.push_undo()
            self._clear_selection()
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
        self._clear_selection()
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
        zone_ids = sorted({normalize_zone_id(point.get("zone", "")) for point in points if normalize_zone_id(point.get("zone", ""))})
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
            zone_name = normalize_zone_id(point.get("zone", ""))
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
    def copy_assert_location(self) -> None:
        zone_id = self._display_zone_id()
        if not zone_id:
            messagebox.showwarning("复制失败", "请先选择 Assert 地图")
            return

        target = self._current_assert_target()
        if target is None:
            messagebox.showwarning("复制失败", "请先开启 Assert 模式并在地图上拖拽框出判定区域")
            return

        try:
            node = export_assert_location_node(zone_id, target)
        except Exception as exc:
            messagebox.showerror("复制失败", str(exc))
            return

        assert_text = json.dumps(node, indent=4, ensure_ascii=False)
        self.root.clipboard_clear()
        self.root.clipboard_append(assert_text)
        self.root.update()
        self._set_status("MapLocateAssertLocation 节点已复制到剪贴板", "#10b981")

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
