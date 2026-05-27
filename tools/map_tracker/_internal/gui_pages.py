import os
import time
from dataclasses import dataclass, field
from typing import Any, Callable

from .core_utils import Drawer, MapImageLayer, ViewportManager, cv2
from .gui_widgets import Button, ScrollableListWidget


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

    def hook_enter(self, stepper: Any):
        """Attaches to stepper and prepare the page for rendering."""
        self.stepper = stepper
        if hasattr(stepper, "window_name"):
            self.window_name = stepper.window_name
        cv2.resizeWindow(self.window_name, self.window_w, self.window_h)
        self.render_request()

    def hook_idle(self):
        """Execute idle hook for background updates."""
        pass

    def hook_exit(self):
        """Lifecycle hook called when page leaves the stack."""
        pass

    def render_request(self) -> None:
        """Requests the page to be re-rendered on next loop tick."""
        self._needs_render = True

    def _render_once(self, drawer: Drawer) -> None:
        """Subclasses should implement this method to render a single frame without handling buttons."""
        pass

    def render(self) -> Any:
        """Renders the page if needed and return the image to be displayed."""
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

            self._render_once(drawer)

            for btn in self.buttons:
                btn.render(drawer)

            return drawer.get_image()
        return None

    def handle_mouse(self, event, x: int, y: int, flags, param):
        """Dispatches mouse input to buttons first, then page handler."""
        self.mouse_pos = (x, y)
        for btn in self.buttons:
            if btn.handle_mouse(event, x, y):
                self.render_request()
                return
        self._on_mouse(event, x, y, flags, param)

    def _on_mouse(self, event, x: int, y: int, flags, param) -> None:
        """Subclasses can override this method to handle mouse events not consumed by buttons."""
        pass

    def handle_escape(self) -> bool:
        """Returns true when the page consumes ESC instead of leaving the step."""
        return False

    def handle_key(self, key: int):
        """Dispatches key input to buttons first, then page handler."""
        for btn in self.buttons:
            if btn.handle_key(key):
                self.render_request()
                return
        self._on_key(key)

    def _on_key(self, key: int) -> None:
        """Subclasses can override this method to handle key events not consumed by buttons."""
        pass


class MapViewportPage(BasePage):
    def __init__(
        self,
        window_name: str = "App",
        window_w: int = 1280,
        window_h: int = 720,
        *,
        image: cv2.typing.MatLike,
        zoom: float = 1.0,
        min_zoom: float = 0.5,
        max_zoom: float = 10.0,
    ):
        super().__init__(window_name, window_w, window_h)
        self.view = ViewportManager(
            self.window_w,
            self.window_h,
            zoom=zoom,
            min_zoom=min_zoom,
            max_zoom=max_zoom,
        )
        self._map_layer = MapImageLayer(self.view, image)
        self.panning = False
        self.pan_start = (0, 0)

    def set_map_image(self, image) -> None:
        self._map_layer = MapImageLayer(self.view, image)

    def _get_map_coords(self, screen_x: int, screen_y: int) -> tuple[float, float]:
        return self.view.get_real_coords(screen_x, screen_y)

    def _get_screen_coords(self, map_x: float, map_y: float) -> tuple[int, int]:
        return self.view.get_view_coords(map_x, map_y)

    def handle_view_mouse(
        self,
        event: int,
        x: int,
        y: int,
        flags: int,
        mx: float,
        my: float,
    ) -> bool:
        if event == cv2.EVENT_MOUSEWHEEL:
            if flags > 0:
                self.view.zoom_in()
            else:
                self.view.zoom_out()
            self.view.set_view_origin(mx - x / self.view.zoom, my - y / self.view.zoom)
            self.render_request()
            return True

        if event == cv2.EVENT_RBUTTONDOWN:
            self.panning = True
            self.pan_start = (x, y)
            return True
        if event == cv2.EVENT_RBUTTONUP:
            self.panning = False
            return True
        if event == cv2.EVENT_MOUSEMOVE and self.panning:
            dx = (x - self.pan_start[0]) / self.view.zoom
            dy = (y - self.pan_start[1]) / self.view.zoom
            self.view.pan_by(-dx, -dy)
            self.pan_start = (x, y)
            self.render_request()
            return True
        return False

    def _render_map_layer(self, drawer: Drawer) -> None:
        if self._map_layer is not None:
            self._map_layer.render(drawer)


@dataclass
class StepData:
    """Data for a simplified wizard-style step."""

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

    def _render_once(self, drawer: Drawer):
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
        title: str,
        map_dir: str,
        enable_preview: bool = True,
        on_select: Callable[[str], None] | None = None,
    ):
        super().__init__(StepData(title))
        self.map_dir = map_dir
        self.map_list = ScrollableListWidget(item_height=40)
        self._map_preview_cache: dict[str, object] = {}
        self._on_select = on_select

        items = []
        if os.path.isdir(self.map_dir):
            map_files = [
                f
                for f in os.listdir(self.map_dir)
                if f.lower().endswith((".png", ".jpg"))
            ]
            map_files.sort(key=lambda name: (len(name), name.lower()))
            items = [
                {
                    "label": m,
                    "sub_label": "",
                    "icon_name": "Layer" if "_tier_" in m.lower() else "Map",
                    "data": m,
                }
                for m in map_files
            ]
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
        if self._on_select is None:
            raise NotImplementedError()
        self._on_select(map_name)


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
            self.current_step.hook_exit()
        self.step_history.append(page)
        page.hook_enter(self)
        self.request_render()

    def pop_step(self) -> BasePage | None:
        """Pop current page when history allows and restore previous page."""
        if len(self.step_history) > 1:
            popped = self.step_history.pop()
            popped.hook_exit()
            if self.current_step:
                self.current_step.hook_enter(self)
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
            self.current_step.render_request()

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

            page.hook_idle()

            rendered_img = page.render()
            if rendered_img is not None:
                cv2.imshow(self.window_name, rendered_img)

            key = cv2.waitKeyEx(1)
            if key == 27:  # ESC
                if page.handle_escape():
                    self.request_render()
                elif len(self.step_history) > 1:
                    self.pop_step()
                else:
                    break
            elif key != -1:
                page.handle_key(key)

        cv2.destroyAllWindows()
        return self.result
