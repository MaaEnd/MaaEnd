import os
import time
from dataclasses import dataclass, field
from typing import Any, Callable

import numpy as np

from .core_utils import Drawer, cv2, get_icon_image


class BasePage:
    def __init__(
        self, window_name: str = "App", window_w: int = 1280, window_h: int = 720
    ):
        self.window_name = window_name
        self.window_w = window_w
        self.window_h = window_h
        self.mouse_pos: tuple[int, int] = (-1, -1)
        self._frame_interval = 1.0 / 120.0
        self._last_render_ts = 0.0
        self._needs_render = True
        self.done = False
        self.stepper: Any = None
        self.buttons: list[Button] = []

    def render_page(self) -> None:
        # Any explicit render request should mark the page dirty.
        self._needs_render = True

    def _render(self, drawer: Drawer) -> None:
        pass

    def render(self) -> Any:
        now = time.monotonic()
        btn_needs_render = any(b.needs_render for b in self.buttons)
        if (
            self._needs_render
            or btn_needs_render
            or (now - self._last_render_ts >= self._frame_interval)
        ):
            self._last_render_ts = now
            self._needs_render = False
            drawer = Drawer.new(self.window_w, self.window_h)

            self._render(drawer)

            for btn in self.buttons:
                btn.render(drawer)

            return drawer.get_image()
        return None

    def on_enter(self, stepper: Any):
        """Attach to stepper and prepare the page for rendering."""
        self.stepper = stepper
        # PageStepper owns the real cv2 window; use its name to avoid resizing
        # a non-existent page-local window.
        if hasattr(stepper, "window_name"):
            self.window_name = stepper.window_name
        cv2.resizeWindow(self.window_name, self.window_w, self.window_h)
        self.render_page()

    def on_exit(self):
        """Lifecycle hook called when page leaves the stack."""
        pass

    def handle_mouse(self, event, x: int, y: int, flags, param):
        """Dispatch mouse input to buttons first, then page handler."""
        self.mouse_pos = (x, y)
        for btn in self.buttons:
            if btn.handle_mouse(event, x, y):
                self.render_page()
                return
        self._on_mouse(event, x, y, flags, param)

    def _on_mouse(self, event, x: int, y: int, flags, param) -> None:
        pass

    def handle_key(self, key: int):
        """Dispatch key input to buttons first, then page handler."""
        for btn in self.buttons:
            if btn.handle_key(key):
                self.render_page()
                return
        self._on_key(key)

    def _on_key(self, key: int) -> None:
        pass

    def handle_idle(self):
        """Execute idle hook for background updates."""
        self._on_idle()

    def _on_idle(self) -> None:
        pass


@dataclass
class StepData:
    """Data for a simplified wizard-style step."""

    step_id: str
    title: str
    data: dict[str, Any] = field(default_factory=dict)
    can_go_back: bool = True


class StepPage(BasePage):
    """A generic BasePage that provides standard Wizard UI (header/footer)."""

    WINDOW_W = 1280
    WINDOW_H = 720
    HEADER_H = 80
    FOOTER_H = 50

    @staticmethod
    def is_up_key(key: int) -> bool:
        return key in (82, 0x260000, 65362)

    @staticmethod
    def is_down_key(key: int) -> bool:
        return key in (84, 0x280000, 65364)

    def __init__(self, step_data: StepData):
        super().__init__("WizardStep", self.WINDOW_W, self.WINDOW_H)
        self.step_data = step_data

        if self.step_data.can_go_back:
            btn_w, btn_h = 120, 36
            btn_x1 = 20
            btn_y1 = self.WINDOW_H - self.FOOTER_H + (self.FOOTER_H - btn_h) // 2
            btn_x2, btn_y2 = btn_x1 + btn_w, btn_y1 + btn_h

            def on_back():
                if len(self.stepper.step_history) > 1:
                    self.stepper.pop_step()

            self.buttons.append(
                Button(
                    rect=(btn_x1, btn_y1, btn_x2, btn_y2),
                    text="< Back",
                    base_color=0x555566,
                    text_color=0xFFFFFF,
                    on_click=on_back,
                )
            )

    def on_enter(self, stepper: "PageStepper"):
        super().on_enter(stepper)

    def _render_header(self, drawer: Drawer) -> None:
        h = self.HEADER_H
        drawer.rect((0, 0), (self.WINDOW_W, h), color=0x0A0A14, thickness=-1)
        step_num = len(
            [p for p in self.stepper.step_history if isinstance(p, StepPage)]
        )
        drawer.text(f"Step {step_num}", (30, h - 35), 0.6, color=0x6688AA)
        drawer.text_centered(
            self.step_data.title, (self.WINDOW_W // 2, h - 20), 0.9, color=0xFFFFFF
        )
        drawer.line((0, h - 1), (self.WINDOW_W, h - 1), color=0x444455, thickness=2)

    def _render_footer(self, drawer: Drawer) -> None:
        y1 = self.WINDOW_H - self.FOOTER_H
        y2 = self.WINDOW_H
        drawer.rect((0, y1), (self.WINDOW_W, y2), color=0x0A0A14, thickness=-1)
        drawer.line((0, y1), (self.WINDOW_W, y1), color=0x444455, thickness=2)

    def _render(self, drawer: Drawer):
        drawer.rect(
            (0, 0),
            (self.WINDOW_W, self.WINDOW_H),
            color=0x14141E,
            thickness=-1,
        )
        self._render_header(drawer)
        self._render_content(drawer)
        self._render_footer(drawer)

    def _on_mouse(self, event, x, y, flags, param):
        self._handle_content_mouse(event, x, y, flags, param)

    def _on_key(self, key):
        self._handle_content_key(key)

    def _render_content(self, drawer: Drawer):
        pass

    def _handle_content_mouse(self, event, x, y, flags, param):
        pass

    def _handle_content_key(self, key):
        pass


class MapImageSelectStep(StepPage):
    """Reusable map image selection step with optional preview support."""

    def __init__(
        self,
        *,
        step_id: str,
        title: str,
        map_dir: str,
        sort_key: Callable[[str], tuple] | None = None,
        enable_preview: bool = True,
    ):
        super().__init__(StepData(step_id, title))
        self.map_dir = map_dir
        self.map_list = ScrollableListWidget(item_height=40)
        self._map_preview_cache: dict[str, object] = {}

        key_fn = sort_key or (lambda name: (len(name), name.lower()))
        items = []
        if os.path.isdir(self.map_dir):
            map_files = [
                f
                for f in os.listdir(self.map_dir)
                if f.lower().endswith((".png", ".jpg"))
            ]
            map_files.sort(key=key_fn)
            items = [{"label": m, "sub_label": "", "data": m} for m in map_files]
        self.map_list.set_items(items)

        if enable_preview:
            self.map_list.set_preview_generator(self._generate_map_preview)

    def _generate_map_preview(self, item: dict):
        map_name = str(item.get("data") or "")
        if map_name == "":
            return None
        if map_name in self._map_preview_cache:
            return self._map_preview_cache[map_name]

        map_path = os.path.join(self.map_dir, map_name)
        img = cv2.imread(map_path, cv2.IMREAD_UNCHANGED)
        self._map_preview_cache[map_name] = img
        return img

    def _render_content(self, drawer):
        self.map_list.render(
            drawer, (50, 100, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        )

    def _handle_content_mouse(self, event, x, y, flags, param):
        rect = (50, 100, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        if event == cv2.EVENT_LBUTTONDOWN:
            idx = self.map_list.handle_click(x, y, rect)
            if idx >= 0:
                self.on_map_selected(str(self.map_list.items[idx]["data"]))
        elif event == cv2.EVENT_MOUSEWHEEL:
            if self.map_list.handle_wheel(x, y, flags, rect):
                self.stepper.request_render()

    def _handle_content_key(self, key):
        is_up = self.is_up_key(key)
        is_down = self.is_down_key(key)
        if is_up or is_down:
            self.map_list.navigate(-1 if is_up else 1)
            self.stepper.request_render()
        elif key in (10, 13) and self.map_list.selected_idx >= 0:
            self.on_map_selected(
                str(self.map_list.items[self.map_list.selected_idx]["data"])
            )

    def on_map_selected(self, map_name: str) -> None:
        raise NotImplementedError()


class PageStepper:
    """Main application loop managing a stack of pages."""

    def __init__(self, window_name: str = "App"):
        self.window_name = window_name
        self.step_history: list[BasePage] = []
        self.done = False
        self.result: Any = None
        cv2.namedWindow(self.window_name)
        cv2.setMouseCallback(self.window_name, self._handle_mouse)

    @property
    def current_step(self) -> BasePage | None:
        """Return the active page on top of the stack."""
        return self.step_history[-1] if self.step_history else None

    def push_step(self, page: BasePage) -> None:
        """Push a new page and enter it."""
        if self.current_step:
            self.current_step.on_exit()
        self.step_history.append(page)
        page.on_enter(self)
        self.request_render()

    def pop_step(self) -> BasePage | None:
        """Pop current page when history allows and restore previous page."""
        if len(self.step_history) > 1:
            popped = self.step_history.pop()
            popped.on_exit()
            if self.current_step:
                self.current_step.on_enter(self)
            self.request_render()
            return popped
        return None

    def finish(self, result: Any = None) -> None:
        """Stop the loop and store final result."""
        self.result = result
        self.done = True

    def request_render(self):
        """Request current step to render on next loop tick."""
        if self.current_step:
            self.current_step.render_page()

    def _handle_mouse(self, event, x, y, flags, param):
        if self.current_step:
            self.current_step.handle_mouse(event, x, y, flags, param)

    def run(self) -> Any:
        """Run the main event loop until finished or window closed."""
        if not self.step_history:
            raise RuntimeError("No initial step provided.")

        self.request_render()

        while not self.done:
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break

            page = self.current_step
            if not page:
                break

            page.handle_idle()

            rendered_img = page.render()
            if rendered_img is not None:
                cv2.imshow(self.window_name, rendered_img)

            key = cv2.waitKeyEx(1)
            if key == 27:  # ESC
                if len(self.step_history) > 1:
                    self.pop_step()
                else:
                    break
            elif key != -1:
                page.handle_key(key)

        cv2.destroyAllWindows()
        return self.result


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

        self.hovered = False
        self.needs_render = True

    def _get_icon(self) -> np.ndarray | None:
        return get_icon_image(self.icon_name)

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

        icon = self._get_icon()
        if icon is not None:
            bh = y2 - y1
            icon_size = max(14, min(28, bh - 20))
            ix = x1 + 16
            iy = y1 + (bh - icon_size) // 2
            drawer.paste(
                icon,
                (ix, iy),
                scale_w=icon_size,
                scale_h=icon_size,
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

    def _enabled_indices(self) -> list[int]:
        return [i for i, item in enumerate(self.items) if not item.get("disabled")]

    def _get_icon(self, icon_name: str | None) -> np.ndarray | None:
        return get_icon_image(icon_name)

    def navigate(self, direction: int) -> None:
        enabled = self._enabled_indices()
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
            icon = self._get_icon(item.get("icon_name"))
            if icon is not None:
                icon_size = max(14, min(20, self.item_height - 10))
                icon_x = x1 + 8
                icon_y = iy1 + (self.item_height - icon_size) // 2
                drawer.paste(
                    icon,
                    (icon_x, icon_y),
                    scale_w=icon_size,
                    scale_h=icon_size,
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
