import time
from typing import Callable, Generic, TypeVar

import numpy as np

from .core_utils import Drawer, cv2
from .sprite_utils import get_sprite_image


class Button:
    def __init__(
        self,
        rect: tuple[int, int, int, int],
        text: str,
        base_color: int,
        text_color: int = 0xFFFFFF,
        hotkey: int | tuple[int, ...] | None = None,
        on_click: Callable[[], None] | None = None,
        thickness: int = -1,
        font_scale: float = 0.5,
        icon_name: str | None = None,
        icon_offset_x: int = 0,
    ):
        self.rect = rect
        self.text = text
        self.base_color = base_color
        self.text_color = text_color
        self.hotkey = (
            hotkey if isinstance(hotkey, tuple) else ((hotkey,) if hotkey else ())
        )
        self.on_click = on_click
        self.thickness = thickness
        self.font_scale = font_scale
        self.icon_name = icon_name
        self.icon_offset_x = icon_offset_x

        self.hovered = False
        self.needs_render = True

    def _get_draw_color(self) -> int:
        if not self.hovered:
            return self.base_color
        r = (self.base_color >> 16) & 0xFF
        g = (self.base_color >> 8) & 0xFF
        b = self.base_color & 0xFF
        r = min(255, r + 40)
        g = min(255, g + 40)
        b = min(255, b + 40)
        return (r << 16) | (g << 8) | b

    def render(self, drawer: "Drawer", border_color: int = 0xB4B4B4):
        x1, y1, x2, y2 = self.rect
        color = self._get_draw_color()
        drawer.rect((x1, y1), (x2, y2), color=color, thickness=self.thickness)
        if border_color != -1:
            drawer.rect((x1, y1), (x2, y2), color=border_color, thickness=1)

        icon = None
        icon_size = None
        if self.icon_name:
            bh = y2 - y1
            icon_size = max(14, min(28, bh - 20))
            icon = get_sprite_image(self.icon_name, (icon_size, icon_size))
        if icon is not None and icon_size is not None:
            ix = x1 + 16 + self.icon_offset_x
            iy = y1 + (bh - icon_size) // 2
            drawer.paste(
                icon,
                (ix, iy),
                with_alpha=(icon.ndim == 3 and icon.shape[2] == 4),
            )

        cx, cy = x1 + (x2 - x1) // 2, y1 + (y2 - y1) // 2 + 5
        drawer.text_centered(
            self.text, (cx, cy), self.font_scale, color=self.text_color
        )
        self.needs_render = False

    def handle_mouse(self, event, x: int, y: int) -> bool:
        x1, y1, x2, y2 = self.rect
        in_rect = x1 <= x <= x2 and y1 <= y <= y2

        if self.hovered != in_rect:
            self.hovered = in_rect
            self.needs_render = True

        if event == cv2.EVENT_LBUTTONDOWN and in_rect:
            if self.on_click:
                self.on_click()
            self.needs_render = True
            return True
        return False

    def handle_key(self, key: int) -> bool:
        if key in self.hotkey:
            if self.on_click:
                self.on_click()
            self.needs_render = True
            return True
        return False


class TextInputWidget:
    """Single-line text input widget."""

    def __init__(self, placeholder: str = "", max_length: int = 200):
        self.text = ""
        self.placeholder = placeholder
        self.max_length = max_length
        self._cursor_blink_start = time.time()

    def clear(self) -> None:
        self.text = ""
        self._cursor_blink_start = time.time()

    def handle_key(self, key: int) -> bool:
        if key == 8 or key == 127:  # Backspace / Del
            if self.text:
                self.text = self.text[:-1]
                self._cursor_blink_start = time.time()
            return True
        if 32 <= key <= 126:  # Printable ASCII
            if len(self.text) < self.max_length:
                self.text += chr(key)
                self._cursor_blink_start = time.time()
            return True
        return False

    def render(
        self,
        drawer: "Drawer",
        rect: tuple[int, int, int, int],
        *,
        focused: bool = True,
        font_scale: float = 0.55,
    ) -> None:
        x1, y1, x2, y2 = rect
        h = y2 - y1
        drawer.rect((x1, y1), (x2, y2), color=0x0D0D1A, thickness=-1)
        border_color = 0x4488FF if focused else 0x555566
        drawer.rect((x1, y1), (x2, y2), color=border_color, thickness=2)
        pad_x = 10
        text_y = y1 + h // 2 + 6
        cursor_visible = (
            focused and int((time.time() - self._cursor_blink_start) * 1.6) % 2 == 0
        )
        if self.text:
            display = self.text + ("|" if cursor_visible else "")
            drawer.text(display, (x1 + pad_x, text_y), font_scale, color=0xFFFFFF)
        else:
            display = "|" if cursor_visible else self.placeholder
            color = 0xFFFFFF if cursor_visible else 0x666677
            drawer.text(display, (x1 + pad_x, text_y), font_scale, color=color)


class RadioSelectWidget:
    """Simple vertical radio selector widget."""

    def __init__(self, title: str = "", item_height: int = 24):
        self.title = title
        self.item_height = item_height
        self.items: list[dict] = []
        self.selected_idx: int = -1

    def set_items(self, items: list[dict], selected_data: object | None = None) -> None:
        self.items = items
        self.selected_idx = -1
        if not items:
            return
        if selected_data is not None:
            for i, item in enumerate(items):
                if item.get("data") == selected_data:
                    self.selected_idx = i
                    return
        self.selected_idx = 0

    def get_selected_data(self) -> object | None:
        if 0 <= self.selected_idx < len(self.items):
            return self.items[self.selected_idx].get("data")
        return None

    def select_by_data(self, data: object) -> bool:
        for i, item in enumerate(self.items):
            if item.get("data") == data:
                changed = self.selected_idx != i
                self.selected_idx = i
                return changed
        return False

    def get_height(self) -> int:
        title_h = 22 if self.title else 0
        return title_h + max(0, len(self.items)) * self.item_height + 8

    def handle_click(self, x: int, y: int, rect: tuple[int, int, int, int]) -> int:
        x1, y1, x2, y2 = rect
        if not (x1 <= x <= x2 and y1 <= y <= y2):
            return -1
        title_h = 22 if self.title else 0
        list_y1 = y1 + title_h
        if y < list_y1:
            return -1
        idx = (y - list_y1) // self.item_height
        if 0 <= idx < len(self.items):
            if self.selected_idx != idx:
                self.selected_idx = idx
                return idx
        return -1

    def render(
        self,
        drawer: "Drawer",
        rect: tuple[int, int, int, int],
        *,
        font_scale: float = 0.4,
    ) -> None:
        x1, y1, x2, y2 = rect
        drawer.rect((x1, y1), (x2, y2), color=0x0A0A14, thickness=-1)
        drawer.rect((x1, y1), (x2, y2), color=0x223044, thickness=1)

        cy = y1
        if self.title:
            drawer.text(f"[ {self.title} ]", (x1 + 8, cy + 16), 0.45, color=0x40FFFF)
            cy += 22

        for i, item in enumerate(self.items):
            iy1 = cy + i * self.item_height
            iy2 = iy1 + self.item_height
            selected = i == self.selected_idx
            if selected:
                drawer.rect((x1 + 2, iy1), (x2 - 2, iy2), color=0x132B4F, thickness=-1)
            label = str(item.get("label", ""))
            color = 0xFFFFFF if selected else 0xC8C8C8
            cy_mark = iy1 + self.item_height // 2
            mark_x = x1 + 14
            if selected:
                drawer.circle((mark_x, cy_mark), 6, color=0xFFFFFF, thickness=1)
                drawer.circle((mark_x, cy_mark), 3, color=0xFFFFFF, thickness=-1)
            else:
                drawer.circle((mark_x, cy_mark), 6, color=0x7A7A7A, thickness=1)
            drawer.text(label, (x1 + 26, iy2 - 7), font_scale, color=color)


class SwitchWidget:
    """Simple two-state switch with both labels always visible."""

    def __init__(
        self,
        left_label: str,
        right_label: str,
        *,
        is_left_selected: bool = True,
        on_changed: Callable[[bool], None] | None = None,
    ):
        self.left_label = left_label
        self.right_label = right_label
        self.is_left_selected = is_left_selected
        self.on_changed = on_changed
        self.hovered = False

    def get_value(self) -> bool:
        return self.is_left_selected

    def set_value(self, is_left_selected: bool) -> bool:
        changed = self.is_left_selected != is_left_selected
        self.is_left_selected = is_left_selected
        if changed and self.on_changed is not None:
            self.on_changed(self.is_left_selected)
        return changed

    def toggle(self) -> bool:
        return self.set_value(not self.is_left_selected)

    def handle_click(self, x: int, y: int, rect: tuple[int, int, int, int]) -> bool:
        x1, y1, x2, y2 = rect
        in_rect = x1 <= x <= x2 and y1 <= y <= y2
        self.hovered = in_rect
        if not in_rect:
            return False
        self.toggle()
        return True

    def render(
        self,
        drawer: "Drawer",
        rect: tuple[int, int, int, int],
        *,
        font_scale: float = 0.42,
    ) -> None:
        x1, y1, x2, y2 = rect
        w = max(2, x2 - x1)
        mid_x = x1 + w // 2

        base_color = 0x0A0A14
        border_color = 0x223044 if not self.hovered else 0x3C5370
        active_color = 0x132B4F
        active_border = 0x6E95D2
        inactive_color = 0xC8C8C8

        drawer.rect((x1, y1), (x2, y2), color=base_color, thickness=-1)
        drawer.rect((x1, y1), (x2, y2), color=border_color, thickness=1)
        drawer.line((mid_x, y1 + 1), (mid_x, y2 - 1), color=border_color, thickness=1)

        active_rect = (
            (x1 + 2, y1 + 2, mid_x - 1, y2 - 2)
            if self.is_left_selected
            else (mid_x + 1, y1 + 2, x2 - 2, y2 - 2)
        )
        drawer.rect(
            (active_rect[0], active_rect[1]),
            (active_rect[2], active_rect[3]),
            color=active_color,
            thickness=-1,
        )
        drawer.rect(
            (active_rect[0], active_rect[1]),
            (active_rect[2], active_rect[3]),
            color=active_border,
            thickness=1,
        )

        cy = y1 + (y2 - y1) // 2 + 5
        left_cx = x1 + (mid_x - x1) // 2
        right_cx = mid_x + (x2 - mid_x) // 2
        drawer.text_centered(
            self.left_label,
            (left_cx, cy),
            font_scale,
            color=0xFFFFFF if self.is_left_selected else inactive_color,
        )
        drawer.text_centered(
            self.right_label,
            (right_cx, cy),
            font_scale,
            color=inactive_color if self.is_left_selected else 0xFFFFFF,
        )


class ScrollableListWidget:
    """Scrollable list widget."""

    def __init__(self, item_height: int = 38):
        self.items: list[dict] = []
        self.selected_idx: int = -1
        self.scroll_offset: int = 0
        self.item_height = item_height
        self._preview_generator: Callable[[dict], np.ndarray | None] | None = None
        self._last_list_x2: int | None = None

    def set_preview_generator(
        self, generator: Callable[[dict], np.ndarray | None] | None
    ) -> None:
        self._preview_generator = generator

    def set_items(self, items: list[dict], *, auto_select_first: bool = True) -> None:
        prev_selected_data = None
        if 0 <= self.selected_idx < len(self.items):
            prev_selected_data = self.items[self.selected_idx].get("data")

        self.items = items
        self.scroll_offset = 0
        self.selected_idx = -1

        if prev_selected_data is not None:
            for i, item in enumerate(items):
                if item.get("data") == prev_selected_data and not item.get("disabled"):
                    self.selected_idx = i
                    return

        if auto_select_first:
            for i, item in enumerate(items):
                if not item.get("disabled"):
                    self.selected_idx = i
                    break

    def navigate(self, direction: int) -> None:
        enabled = [i for i, item in enumerate(self.items) if not item.get("disabled")]
        if not enabled:
            return
        if self.selected_idx not in enabled:
            self.selected_idx = enabled[0]
            return
        curr_pos = enabled.index(self.selected_idx)
        new_pos = curr_pos + direction
        if 0 <= new_pos < len(enabled):
            self.selected_idx = enabled[new_pos]

    def handle_click(self, x: int, y: int, rect: tuple[int, int, int, int]) -> int:
        x1, y1, x2, y2 = rect
        list_x2 = self._last_list_x2 if self._last_list_x2 is not None else x2
        content_x2 = max(x1, list_x2 - 6)
        if not (x1 <= x <= content_x2 and y1 <= y <= y2):
            return -1
        rel_y = y - y1
        idx = self.scroll_offset + rel_y // self.item_height
        if 0 <= idx < len(self.items) and not self.items[idx].get("disabled"):
            if self.selected_idx == idx:
                return idx
            self.selected_idx = idx
            return -1
        return -1

    def handle_wheel(
        self, x: int, y: int, flags: int, rect: tuple[int, int, int, int]
    ) -> bool:
        x1, y1, x2, y2 = rect
        if not (x1 <= x <= x2 and y1 <= y <= y2):
            return False

        h = y2 - y1
        visible = max(1, h // self.item_height)
        max_offset = max(0, len(self.items) - visible)

        if flags > 0:
            self.scroll_offset = max(0, self.scroll_offset - 1)
        else:
            self.scroll_offset = min(max_offset, self.scroll_offset + 1)

        if self.selected_idx >= 0:
            if self.selected_idx < self.scroll_offset:
                self.selected_idx = self.scroll_offset
            elif self.selected_idx >= self.scroll_offset + visible:
                self.selected_idx = min(
                    len(self.items) - 1,
                    self.scroll_offset + visible - 1,
                )

        return True

    def _ensure_visible(self, visible_count: int) -> None:
        if self.selected_idx < 0:
            return
        if self.selected_idx < self.scroll_offset:
            self.scroll_offset = self.selected_idx
        elif self.selected_idx >= self.scroll_offset + visible_count:
            self.scroll_offset = self.selected_idx - visible_count + 1

    def render(
        self,
        drawer: "Drawer",
        rect: tuple[int, int, int, int],
        *,
        font_scale: float = 0.45,
    ) -> None:
        x1, y1, x2, y2 = rect
        preview_img: np.ndarray | None = None
        if self._preview_generator is not None and 0 <= self.selected_idx < len(
            self.items
        ):
            try:
                preview_img = self._preview_generator(self.items[self.selected_idx])
            except Exception:
                preview_img = None

        list_x2 = x2
        if preview_img is not None:
            list_x2 = x1 + max(1, (x2 - x1) // 2)
        self._last_list_x2 = list_x2

        h = y2 - y1
        visible = max(1, h // self.item_height)
        self._ensure_visible(visible)
        for i in range(visible):
            item_idx = self.scroll_offset + i
            if item_idx >= len(self.items):
                break
            item = self.items[item_idx]
            iy1 = y1 + i * self.item_height
            iy2 = iy1 + self.item_height
            disabled = item.get("disabled", False)
            priority = item.get("priority", False)
            selected = item_idx == self.selected_idx

            if selected:
                bg_color = 0x1E4A90
            elif priority:
                bg_color = 0x111828
            else:
                bg_color = 0x0A0A14
            drawer.rect((x1, iy1), (list_x2, iy2), color=bg_color, thickness=-1)

            label = item.get("label", "")
            text_color = (
                0x666677 if disabled else (0xFFFFFF if not priority else 0xAADDFF)
            )
            label_x = x1 + 12

            icon = None
            icon_size = None
            if item.get("icon_name"):
                icon_size = max(14, min(20, self.item_height - 10))
                icon = get_sprite_image(item.get("icon_name"), (icon_size, icon_size))
            if icon is not None and icon_size is not None:
                icon_x = x1 + 8
                icon_y = iy1 + (self.item_height - icon_size) // 2
                drawer.paste(
                    icon,
                    (icon_x, icon_y),
                    with_alpha=(icon.ndim == 3 and icon.shape[2] == 4),
                )
                label_x = icon_x + icon_size + 8
            drawer.text(label, (label_x, iy2 - 10), font_scale, color=text_color)

            sub = item.get("sub_label", "")
            if sub:
                sub_color = 0x445566 if disabled else 0x6688AA
                sub_size = drawer.get_text_size(sub, font_scale)
                drawer.text(
                    sub,
                    (list_x2 - sub_size[0] - 12, iy2 - 10),
                    font_scale,
                    color=sub_color,
                )

            drawer.line((x1, iy2 - 1), (list_x2, iy2 - 1), color=0x222233, thickness=1)

        total = len(self.items)
        if total > visible and total > 0:
            bar_x = list_x2 - 6
            bar_y1 = y1
            bar_y2 = y2
            bar_h = bar_y2 - bar_y1
            thumb_h = max(20, bar_h * visible // total)
            thumb_y = bar_y1 + (bar_h - thumb_h) * self.scroll_offset // max(
                1, total - visible
            )
            drawer.rect(
                (bar_x, bar_y1), (list_x2, bar_y2), color=0x111122, thickness=-1
            )
            drawer.rect(
                (bar_x, thumb_y),
                (list_x2, thumb_y + thumb_h),
                color=0x445566,
                thickness=-1,
            )

        if preview_img is not None:
            px1 = list_x2 + 1
            px2 = x2
            py1 = y1
            py2 = y2

            drawer.rect((px1, py1), (px2, py2), color=0x07070D, thickness=-1)
            drawer.rect((px1, py1), (px2, py2), color=0x223044, thickness=1)

            img = preview_img
            if img.ndim == 2:
                img = cv2.cvtColor(img, cv2.COLOR_GRAY2BGR)

            ph = max(1, py2 - py1 - 16)
            pw = max(1, px2 - px1 - 16)
            ih, iw = img.shape[:2]
            scale = min(pw / max(1, iw), ph / max(1, ih))
            nw = max(1, int(iw * scale))
            nh = max(1, int(ih * scale))
            ox = px1 + (px2 - px1 - nw) // 2
            oy = py1 + (py2 - py1 - nh) // 2
            drawer.paste(
                img,
                (ox, oy),
                scale_w=nw,
                scale_h=nh,
                with_alpha=(img.ndim == 3 and img.shape[2] == 4),
            )


T = TypeVar("T")


class UndoRedoHistory(Generic[T]):
    def __init__(
        self,
        capture: Callable[[], T],
        restore: Callable[[T], None],
        *,
        limit: int = 100,
        on_changed: Callable[[], None] | None = None,
    ) -> None:
        self._capture = capture
        self._restore = restore
        self._limit = limit
        self._on_changed = on_changed
        self._undo_stack: list[T] = []
        self._redo_stack: list[T] = []

    @property
    def can_undo(self) -> bool:
        return bool(self._undo_stack)

    @property
    def can_redo(self) -> bool:
        return bool(self._redo_stack)

    def push_current(self) -> None:
        self.push_state(self._capture())

    def push_state(self, state: T) -> None:
        if self._undo_stack and self._undo_stack[-1] == state:
            return
        self._undo_stack.append(state)
        if len(self._undo_stack) > self._limit:
            self._undo_stack.pop(0)
        self._redo_stack.clear()
        self._notify_changed()

    def undo(self) -> bool:
        if not self._undo_stack:
            return False
        current = self._capture()
        previous = self._undo_stack.pop()
        if not self._redo_stack or self._redo_stack[-1] != current:
            self._redo_stack.append(current)
            if len(self._redo_stack) > self._limit:
                self._redo_stack.pop(0)
        self._restore(previous)
        self._notify_changed()
        return True

    def redo(self) -> bool:
        if not self._redo_stack:
            return False
        current = self._capture()
        next_state = self._redo_stack.pop()
        if not self._undo_stack or self._undo_stack[-1] != current:
            self._undo_stack.append(current)
            if len(self._undo_stack) > self._limit:
                self._undo_stack.pop(0)
        self._restore(next_state)
        self._notify_changed()
        return True

    def clear(self) -> None:
        self._undo_stack.clear()
        self._redo_stack.clear()
        self._notify_changed()

    def _notify_changed(self) -> None:
        if self._on_changed is not None:
            self._on_changed()


class UndoRedoWidget:
    def __init__(
        self,
        *,
        on_undo: Callable[[], None],
        on_redo: Callable[[], None],
        can_undo: Callable[[], bool],
        can_redo: Callable[[], bool],
    ) -> None:
        self._on_undo = on_undo
        self._on_redo = on_redo
        self._can_undo = can_undo
        self._can_redo = can_redo
        self._undo_button = Button(
            (-100, -100, -90, -90),
            "[Z] Undo",
            base_color=0xB44022,
            hotkey=(ord("z"), ord("Z")),
            on_click=self._handle_undo,
            font_scale=0.38,
            icon_name="Undo",
            icon_offset_x=-10,
        )
        self._redo_button = Button(
            (-100, -100, -90, -90),
            "[Y] Redo",
            base_color=0x2E6FD1,
            hotkey=(ord("y"), ord("Y")),
            on_click=self._handle_redo,
            font_scale=0.38,
            icon_name="Redo",
            icon_offset_x=-10,
        )

    @property
    def buttons(self) -> tuple[Button, Button]:
        return (self._undo_button, self._redo_button)

    def place(
        self,
        rect: tuple[int, int, int, int],
        *,
        gap: int = 8,
    ) -> None:
        x1, y1, x2, y2 = rect
        btn_w = max(1, (x2 - x1 - gap) // 2)
        self._undo_button.rect = (x1, y1, x1 + btn_w, y2)
        self._redo_button.rect = (x1 + btn_w + gap, y1, x2, y2)
        self.sync_enabled()

    def hide(self) -> None:
        hidden_rect = (-100, -100, -90, -90)
        self._undo_button.rect = hidden_rect
        self._redo_button.rect = hidden_rect

    def sync_enabled(self) -> None:
        self._sync_button(self._undo_button, self._can_undo(), 0xB44022)
        self._sync_button(self._redo_button, self._can_redo(), 0x2E6FD1)

    @staticmethod
    def _sync_button(button: Button, enabled: bool, color: int) -> None:
        button.base_color = color if enabled else 0x303030
        button.text_color = 0xFFFFFF if enabled else 0x707070

    def _handle_undo(self) -> None:
        if self._can_undo():
            self._on_undo()

    def _handle_redo(self) -> None:
        if self._can_redo():
            self._on_redo()
