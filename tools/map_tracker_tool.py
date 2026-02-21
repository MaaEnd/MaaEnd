import os
import math
import re
import json
import sys

_R = "\033[31m"
_G = "\033[32m"
_Y = "\033[33m"
_C = "\033[36m"
_0 = "\033[0m"
_A = "\033[90m"

Point = tuple[int, int]
Color = tuple[int, int, int]

try:
    import numpy as np
except ImportError:
    print(f"{_R}Cannot import 'numpy'!{_0}")
    print(f"  Please run 'pip install numpy' first.")
    sys.exit(1)

try:
    import cv2
except ImportError:
    print(f"{_R}Cannot import 'opencv-python'!{_0}")
    print(f"  Please run 'pip install opencv-python' first.")
    sys.exit(1)


MAP_DIR = "assets/resource/image/MapTracker/map"


class Drawer:
    def __init__(self, img: cv2.Mat, font_face: int = cv2.FONT_HERSHEY_SIMPLEX):
        self._img = img
        self._font_face = font_face

    @property
    def w(self):
        return self._img.shape[1]

    @property
    def h(self):
        return self._img.shape[0]

    def get_image(self):
        return self._img

    def get_text_size(self, text: str, font_scale: float, *, thickness: int):
        return cv2.getTextSize(text, self._font_face, font_scale, thickness)[0]

    def text(
        self,
        text: str,
        pos: Point,
        font_scale: float,
        *,
        color: Color,
        thickness: int,
        bg_color: Color | None = None,
        bg_padding: int = 5,
    ):
        if bg_color is not None:
            text_size = self.get_text_size(text, font_scale, thickness=thickness)
            cv2.rectangle(
                self._img,
                (pos[0] - bg_padding, pos[1] - text_size[1] - bg_padding),
                (pos[0] + text_size[0] + bg_padding, pos[1] + bg_padding),
                bg_color,
                -1,
            )
        cv2.putText(self._img, text, pos, self._font_face, font_scale, color, thickness)

    def text_centered(
        self, text: str, pos: Point, font_scale: float, *, color: Color, thickness: int
    ):
        text_size = self.get_text_size(text, font_scale, thickness=thickness)
        x = pos[0] - text_size[0] // 2
        self.text(text, (x, pos[1]), font_scale, color=color, thickness=thickness)

    def rect(self, pt1: Point, pt2: Point, *, color: Color, thickness: int):
        cv2.rectangle(self._img, pt1, pt2, color, thickness)

    def circle(self, center: Point, radius: int, *, color: Color, thickness: int):
        cv2.circle(self._img, center, radius, color, thickness)

    def line(self, pt1: Point, pt2: Point, *, color: Color, thickness: int):
        cv2.line(self._img, pt1, pt2, color, thickness)

    @staticmethod
    def new(w: int, h: int, **kwargs) -> "Drawer":
        img = np.zeros((h, w, 3), dtype=np.uint8)
        return Drawer(img, **kwargs)


class SelectMapPage:
    """Map selection page"""

    def __init__(self, map_dir=MAP_DIR):
        self.map_dir = map_dir
        self.map_files = self._load_and_sort_maps()
        self.rows, self.cols = 2, 5
        self.nav_height = 90
        self.window_w, self.window_h = 1280, 720
        self.cell_size = min(
            self.window_w // self.cols, (self.window_h - self.nav_height) // self.rows
        )
        self.page_size = self.rows * self.cols
        self.window_name = "MapTracker Tool - Map Selector"

        self.current_page = 0
        self.cached_page = -1
        self.cached_img = None
        self.selected_index = -1
        self.total_pages = math.ceil(len(self.map_files) / self.page_size)

    def _load_and_sort_maps(self):
        map_files = [f for f in os.listdir(self.map_dir) if f.endswith(".png")]
        if not map_files:
            return []

        def natural_sort_key(s):
            return [
                int(text) if text.isdigit() else text.lower()
                for text in re.split("([0-9]+)", s)
            ]

        map_files.sort(key=lambda x: (len(x), natural_sort_key(x)))
        return map_files

    def _render_page(self):
        if self.cached_page == self.current_page:
            return self.cached_img
        drawer: Drawer = Drawer.new(self.window_w, self.window_h)
        start_idx = self.current_page * self.page_size
        end_idx = min(start_idx + self.page_size, len(self.map_files))

        # Content area height (excluding bottom navigation)
        content_h = self.window_h - self.nav_height
        content_w = self.window_w

        # Calculate horizontal and vertical spacing (space-between)
        if self.cols > 1:
            gap_x = int((content_w - self.cols * self.cell_size) / (self.cols - 1))
        else:
            gap_x = 0
        if self.rows > 1:
            gap_y = int((content_h - self.rows * self.cell_size) / (self.rows - 1))
        else:
            gap_y = 0

        # Draw map previews in space-between layout
        for i in range(start_idx, end_idx):
            idx_in_page = i - start_idx
            r = idx_in_page // self.cols
            c = idx_in_page % self.cols

            cell_x = int(c * (self.cell_size + gap_x))
            cell_y = int(r * (self.cell_size + gap_y))

            path = os.path.join(self.map_dir, self.map_files[i])
            img = cv2.imread(path)
            if img is not None:
                h, w = img.shape[:2]
                # Calculate scaling to maintain aspect ratio, fit image completely into cell
                scale = min(self.cell_size / w, self.cell_size / h)
                new_w = max(1, int(w * scale))
                new_h = max(1, int(h * scale))
                resized = cv2.resize(img, (new_w, new_h))
                # Center the image within the cell
                x1 = cell_x
                y1 = cell_y
                x2 = x1 + self.cell_size
                y2 = y1 + self.cell_size
                # Calculate placement offset
                dx = (self.cell_size - new_w) // 2
                dy = (self.cell_size - new_h) // 2
                dest_x1 = x1 + dx
                dest_y1 = y1 + dy
                dest_x2 = dest_x1 + new_w
                dest_y2 = dest_y1 + new_h
                # Boundary clipping (to prevent exceeding content area)
                dest_x2 = min(self.window_w, dest_x2)
                dest_y2 = min(content_h, dest_y2)
                src_x2 = dest_x2 - dest_x1
                src_y2 = dest_y2 - dest_y1
                if src_x2 > 0 and src_y2 > 0:
                    drawer._img[
                        dest_y1 : dest_y1 + src_y2, dest_x1 : dest_x1 + src_x2
                    ] = resized[0:src_y2, 0:src_x2]

                # Label (bottom)
                label = self.map_files[i]
                drawer.rect(
                    (x1, y1 + self.cell_size - 30),
                    (x1 + self.cell_size, y1 + self.cell_size),
                    color=(0, 0, 0),
                    thickness=-1,
                )
                drawer.text_centered(
                    label,
                    (x1 + self.cell_size // 2, y1 + self.cell_size - 10),
                    0.4,
                    color=(255, 255, 255),
                    thickness=1,
                )

        # Bottom navigation bar
        drawer.line(
            (0, content_h),
            (self.window_w, content_h),
            color=(128, 128, 128),
            thickness=2,
        )

        # Top navigation prompt text
        drawer.text_centered(
            "Please click a map to continue",
            (drawer.w // 2, content_h + 30),
            0.7,
            color=(255, 255, 255),
            thickness=1,
        )

        # Left arrow
        drawer.text(
            "< PREV",
            (150, self.window_h - 20),
            0.6,
            color=(0, 255, 0) if self.current_page > 0 else (128, 128, 128),
            thickness=2,
        )

        # Middle page info
        page_text = f"Page {self.current_page + 1} / {self.total_pages}"
        drawer.text_centered(
            page_text,
            (drawer.w // 2, self.window_h - 20),
            0.5,
            color=(255, 255, 255),
            thickness=1,
        )

        # Right arrow
        drawer.text(
            "NEXT >",
            (self.window_w - 200, self.window_h - 20),
            0.6,
            color=(
                (0, 255, 0)
                if self.current_page < self.total_pages - 1
                else (128, 128, 128)
            ),
            thickness=2,
        )

        self.cached_img = drawer.get_image()
        self.cached_page = self.current_page
        return self.cached_img

    def _handle_mouse(self, event, x, y, flags, param):
        if event == cv2.EVENT_LBUTTONDOWN:
            # Content area height (excluding bottom navigation)
            content_h = self.window_h - self.nav_height
            if y < content_h:
                # Use layout calculation to determine which cell the click falls into
                if self.cols > 1:
                    gap_x = int(
                        (self.window_w - self.cols * self.cell_size) / (self.cols - 1)
                    )
                else:
                    gap_x = 0
                if self.rows > 1:
                    gap_y = int(
                        (content_h - self.rows * self.cell_size) / (self.rows - 1)
                    )
                else:
                    gap_y = 0

                found = False
                for r in range(self.rows):
                    for c in range(self.cols):
                        cell_x = int(c * (self.cell_size + gap_x))
                        cell_y = int(r * (self.cell_size + gap_y))
                        if (
                            x >= cell_x
                            and x < cell_x + self.cell_size
                            and y >= cell_y
                            and y < cell_y + self.cell_size
                        ):
                            idx = self.current_page * self.page_size + r * self.cols + c
                            if idx < len(self.map_files):
                                self.selected_index = idx
                                found = True
                                break
                    if found:
                        break
            else:
                # Bottom navigation
                if x < self.window_w // 3:
                    if self.current_page > 0:
                        self.current_page -= 1
                elif x > 2 * self.window_w // 3:
                    if self.current_page < self.total_pages - 1:
                        self.current_page += 1

    def run(self):
        if not self.map_files:
            print(f"Error: No maps found in {self.map_dir}")
            return None

        cv2.namedWindow(self.window_name)
        cv2.setMouseCallback(self.window_name, self._handle_mouse)

        while True:
            cv2.imshow(self.window_name, self._render_page())

            if self.selected_index != -1:
                break
            key = cv2.waitKey(30) & 0xFF
            if key == 27:  # ESC
                break
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break

        cv2.destroyAllWindows()
        if self.selected_index != -1:
            return self.map_files[self.selected_index]
        return None


class PathEditPage:
    """Path editing page"""

    def __init__(self, map_name, initial_points=None, map_dir=MAP_DIR):
        self.map_name = map_name
        self.map_path = os.path.join(map_dir, map_name)
        if not os.path.exists(self.map_path):
            print(f"Error: Map file not found: {self.map_path}")

        self.img = cv2.imread(self.map_path)
        if self.img is None:
            raise ValueError(f"Cannot load map: {self.map_path}")

        self.points = [list(p) for p in initial_points] if initial_points else []
        self.scale = 1.0
        self.offset_x, self.offset_y = 0, 0
        self.window_w, self.window_h = 1280, 720
        self.window_name = "MapTracker Tool - Path Editor"

        self.drag_idx = -1
        self.selected_idx = -1
        self.panning = False
        self.pan_start = (0, 0)
        self.line_width = 1.75
        self.point_radius = 4.5
        self.selection_threshold = 10
        # Action state for point interactions (left button):
        self.action_down_idx = -1
        self.action_mouse_down = False
        self.action_down_pos = (0, 0)
        self.action_moved = False
        self.action_dragging = False
        self.done = False

    def _get_map_coords(self, screen_x, screen_y):
        """Convert screen (viewport) coordinates to original map coordinates"""
        mx = round(screen_x / self.scale + self.offset_x)
        my = round(screen_y / self.scale + self.offset_y)
        return mx, my

    def _get_screen_coords(self, map_x, map_y):
        """Convert original map coordinates to screen (viewport) coordinates"""
        sx = round((map_x - self.offset_x) * self.scale)
        sy = round((map_y - self.offset_y) * self.scale)
        return sx, sy

    def _is_on_line(self, mx, my, p1, p2, threshold=10):
        """Check if a point is on the line between two points"""
        x1, y1 = p1
        x2, y2 = p2
        px, py = mx, my
        dx = x2 - x1
        dy = y2 - y1
        if dx == 0 and dy == 0:
            return math.hypot(px - x1, py - y1) < threshold
        t = max(0, min(1, ((px - x1) * dx + (py - y1) * dy) / (dx * dx + dy * dy)))
        closest_x = x1 + t * dx
        closest_y = y1 + t * dy
        dist = math.hypot(px - closest_x, py - closest_y)
        return dist < threshold

    def _render(self):
        src_x1 = max(0, int(self.offset_x))
        src_y1 = max(0, int(self.offset_y))
        src_x2 = min(self.img.shape[1], int(self.offset_x + self.window_w / self.scale))
        src_y2 = min(self.img.shape[0], int(self.offset_y + self.window_h / self.scale))

        patch = self.img[src_y1:src_y2, src_x1:src_x2]
        display_img = np.zeros((self.window_h, self.window_w, 3), dtype=np.uint8)
        drawer = Drawer(display_img)

        if patch.size > 0:
            view_w = int((src_x2 - src_x1) * self.scale)
            view_h = int((src_y2 - src_y1) * self.scale)
            view_w = min(view_w, self.window_w)
            view_h = min(view_h, self.window_h)

            interp = cv2.INTER_NEAREST if self.scale > 1.0 else cv2.INTER_AREA
            resized_patch = cv2.resize(patch, (view_w, view_h), interpolation=interp)
            dst_x = int(max(0, -self.offset_x * self.scale))
            dst_y = int(max(0, -self.offset_y * self.scale))

            h, w = resized_patch.shape[:2]
            display_img[dst_y : dst_y + h, dst_x : dst_x + w] = resized_patch

        for i in range(len(self.points)):
            sx, sy = self._get_screen_coords(self.points[i][0], self.points[i][1])
            if i > 0:
                psx, psy = self._get_screen_coords(
                    self.points[i - 1][0], self.points[i - 1][1]
                )
                drawer.line(
                    (psx, psy),
                    (sx, sy),
                    color=(0, 0, 255),
                    thickness=max(1, int(self.line_width * self.scale**0.5)),
                )

        for i in range(len(self.points)):
            sx, sy = self._get_screen_coords(self.points[i][0], self.points[i][1])
            drawer.circle(
                (sx, sy),
                int(self.point_radius * max(0.5, self.scale**0.5)),
                color=(0, 165, 255) if i == self.drag_idx else (0, 0, 255),
                thickness=-1,
            )

        for i in range(len(self.points)):
            sx, sy = self._get_screen_coords(self.points[i][0], self.points[i][1])
            drawer.text(
                str(i), (sx + 5, sy - 5), 0.5, color=(255, 255, 255), thickness=1
            )

        legend_x, legend_y = 10, 10
        legend_lines = [
            "[ Tips ]",
            "Mouse Left Click: Add/Delete Point",
            "Mouse Left Drag: Move Point",
            "Mouse Right Drag: Drag Map",
            "Close Window: Finish",
        ]
        font_scale = 0.5
        thickness = 1
        padding = 10
        line_height = 25

        max_width = 0
        for line in legend_lines:
            text_size = drawer.get_text_size(line, font_scale, thickness=thickness)
            max_width = max(max_width, text_size[0])
        legend_w = max_width + 2 * padding
        legend_h = len(legend_lines) * line_height + 2 * padding

        cv2.rectangle(
            display_img,
            (legend_x, legend_y),
            (legend_x + legend_w, legend_y + legend_h),
            (0, 0, 0),
            -1,
        )
        cv2.rectangle(
            display_img,
            (legend_x, legend_y),
            (legend_x + legend_w, legend_y + legend_h),
            (255, 255, 255),
            1,
        )

        for i, line in enumerate(legend_lines):
            y_pos = legend_y + padding + (i + 1) * line_height - 5
            drawer.text(
                line,
                (legend_x + padding, y_pos),
                font_scale=font_scale,
                color=(255, 255, 255),
                thickness=thickness,
            )

        # Draw bottom-left status display
        drawer.text(
            f"Zoom: {self.scale:.2f}x",
            (20, self.window_h - 45),
            font_scale=0.5,
            color=(0, 255, 255),
            thickness=1,
            bg_color=(0, 0, 0),
            bg_padding=10,
        )

        if 0 <= self.selected_idx < len(self.points):
            p = self.points[self.selected_idx]
            info = (
                f"Point: Index={self.selected_idx}, Location=({int(p[0])}, {int(p[1])})"
            )
        else:
            info = f"Total Points: {len(self.points)}"

        drawer.text(
            info,
            (20, self.window_h - 20),
            font_scale=0.5,
            color=(255, 255, 255),
            thickness=1,
            bg_color=(0, 0, 0),
            bg_padding=10,
        )

        cv2.imshow(self.window_name, display_img)

    def _handle_mouse(self, event, x, y, flags, param):
        mx, my = self._get_map_coords(x, y)
        if event == cv2.EVENT_MOUSEWHEEL:
            if flags > 0:
                self.scale *= 1.14514
            else:
                self.scale /= 1.14514
            self.scale = max(0.5, min(self.scale, 10.0))

            self.offset_x = mx - x / self.scale
            self.offset_y = my - y / self.scale
            self._render()

        elif event == cv2.EVENT_MOUSEMOVE:
            # Pan
            if self.panning:
                dx = (x - self.pan_start[0]) / self.scale
                dy = (y - self.pan_start[1]) / self.scale
                self.offset_x -= dx
                self.offset_y -= dy
                self.pan_start = (x, y)
                self._render()
                return

            # Action (left button) dragging
            if self.action_mouse_down:
                # If dragging started on a point, move it
                if self.action_dragging and self.drag_idx != -1:
                    self.points[self.drag_idx] = [mx, my]
                    self.action_moved = True
                    self._render()
                    return

                # Otherwise record small movement to distinguish click vs drag
                dx = x - self.action_down_pos[0]
                dy = y - self.action_down_pos[1]
                if dx * dx + dy * dy > 25:
                    self.action_moved = True
                    # if press was on a point, begin dragging
                    if self.action_down_idx != -1:
                        self.action_dragging = True
                        self.drag_idx = self.action_down_idx
                        self.points[self.drag_idx] = [mx, my]
                        self._render()
                        return

            # If left button held and drag_idx set, start move point
            if (flags & cv2.EVENT_FLAG_LBUTTON) and self.drag_idx != -1:
                self.points[self.drag_idx] = [mx, my]
                self.action_dragging = True
                self._render()

        elif event == cv2.EVENT_RBUTTONDOWN:
            # Right button starts panning
            self.panning = True
            self.pan_start = (x, y)

        elif event == cv2.EVENT_RBUTTONUP:
            # Right button stop panning
            self.panning = False

        elif event == cv2.EVENT_LBUTTONDOWN:
            # Left button: prepare add/delete/move
            found_idx = -1
            for i, p in enumerate(self.points):
                sx, sy = self._get_screen_coords(p[0], p[1])
                dist = math.hypot(x - sx, y - sy)
                if dist < self.selection_threshold:
                    found_idx = i
                    break

            self.action_down_idx = found_idx
            self.action_mouse_down = True
            self.action_down_pos = (x, y)
            self.action_moved = False
            self.action_dragging = False
            if found_idx != -1:
                self.drag_idx = found_idx
                self.selected_idx = found_idx

        elif event == cv2.EVENT_LBUTTONUP:
            # If was dragging a point, finish
            if self.action_dragging and self.drag_idx != -1:
                self.drag_idx = -1
            else:
                # If moved in empty area, do nothing
                if self.action_moved and self.action_down_idx == -1:
                    pass
                else:
                    if self.action_down_idx != -1:
                        # delete point
                        del_idx = self.action_down_idx
                        if 0 <= del_idx < len(self.points):
                            self.points.pop(del_idx)
                            if self.drag_idx == del_idx:
                                self.drag_idx = -1
                            elif self.drag_idx > del_idx:
                                self.drag_idx -= 1
                            if self.selected_idx == del_idx:
                                self.selected_idx = -1
                            elif self.selected_idx > del_idx:
                                self.selected_idx -= 1
                    elif self.action_down_pos == (x, y):
                        # insert on line or append
                        inserted = False
                        for i in range(1, len(self.points)):
                            map_threshold = self.selection_threshold / max(
                                0.01, self.scale
                            )
                            if self._is_on_line(
                                mx,
                                my,
                                self.points[i - 1],
                                self.points[i],
                                threshold=map_threshold,
                            ):
                                self.points.insert(i, [mx, my])
                                self.selected_idx = i
                                inserted = True
                                break
                        if not inserted:
                            self.points.append([mx, my])
                            self.selected_idx = len(self.points) - 1

            # Reset action state and render
            self.action_down_idx = -1
            self.action_mouse_down = False
            self.action_down_pos = (0, 0)
            self.action_moved = False
            self.action_dragging = False
            self._render()

    def run(self):
        cv2.namedWindow(self.window_name)
        cv2.setMouseCallback(self.window_name, self._handle_mouse)

        self._render()
        while not self.done:
            # Check if the window is closed
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break
            if cv2.waitKey(1) & 0xFF == 27:
                break

        cv2.destroyAllWindows()
        return [list(p) for p in self.points]


def find_map_file(name, map_dir=MAP_DIR):
    """Find the filename corresponding to the given name on disk (keeping the suffix), return the filename or None."""
    if not os.path.isdir(map_dir):
        return None
    files = os.listdir(map_dir)
    if name in files:
        return name
    for suffix in [".png", "_merged.png"]:
        if name + suffix in files:
            return name + suffix
    return None


class PipelineHandler:
    """Handle reading and writing of Pipeline JSON, using regex to preserve comments and formatting"""

    def __init__(self, file_path):
        self.file_path = file_path
        self._content = ""

    def read_nodes(self):
        """Read all MapTrackerMove nodes"""
        try:
            with open(self.file_path, "r", encoding="utf-8") as f:
                self._content = f.read()
        except Exception as e:
            print(f"{_R}Error reading file:{_0} {e}")
            return []

        # First split into nodes
        # Match top-level nodes: "name": { ... }
        node_pattern = re.compile(
            r'^\s*"([^"]+)"\s*:\s*(\{[\s\S]*?\n\s*\})', re.MULTILINE
        )
        results = []
        for match in node_pattern.finditer(self._content):
            node_name = match.group(1)
            node_content = match.group(2)
            # Check if the node contains MapTrackerMove
            if '"custom_action": "MapTrackerMove"' in node_content:
                # Detect structure type
                # Old structure: "action": "Custom", "custom_action": "MapTrackerMove", "custom_action_param": { ... }
                # New structure: "action": { "custom_action": "MapTrackerMove", "custom_action_param": { ... } }
                is_new_structure = (
                    re.search(r'"action"\s*:\s*\{', node_content) is not None
                )

                # Extract map_name
                m_match = re.search(r'"map_name"\s*:\s*"([^"]+)"', node_content)
                map_name = m_match.group(1) if m_match else "Unknown"
                # Extract path
                t_match = re.search(
                    r'"path"\s*:\s*(\[[\s\S]*?\]\s*\]|\[\s*\])', node_content
                )
                if t_match:
                    path_str = t_match.group(1)
                    try:
                        path = json.loads(path_str)
                        results.append(
                            {
                                "node_name": node_name,
                                "map_name": map_name,
                                "path": path,
                                "is_new_structure": is_new_structure,
                            }
                        )
                    except:
                        continue
        return results

    def replace_path(self, node_name, new_path):
        """Regex replace the path list in the pipeline file, ensuring the target node's structure is maintained"""
        try:
            with open(self.file_path, "r", encoding="utf-8") as f:
                self._content = f.read()
        except:
            return False

        # Find the node block first to isolate the "path" update within it
        node_pattern = re.compile(
            r'^(\s*"' + re.escape(node_name) + r'"\s*:\s*\{)([\s\S]*?\n\s*\})',
            re.MULTILINE,
        )

        node_match = node_pattern.search(self._content)
        if not node_match:
            print(f"{_R}Error: Node {node_name} not found in file when saving.{_0}")
            return False

        header = node_match.group(1)
        body = node_match.group(2)

        # Look for the "path" field specifically within this node body
        path_pattern = re.compile(
            r'("path"\s*:\s*)(\[[\s\S]*?\]\s*\]|\[\s*\])',
            re.MULTILINE,
        )

        path_match = path_pattern.search(body)
        if not path_match:
            print(
                f"{_R}Error: 'path' field not found in node {node_name} when saving.{_0}"
            )
            return False

        # Format new path, following multi-line array convention
        if not new_path:
            formatted_path = "[]"
        else:
            formatted_path = "[\n"
            for i, p in enumerate(new_path):
                comma = "," if i < len(new_path) - 1 else ""
                formatted_path += f"                [{p[0]}, {p[1]}]{comma}\n"
            formatted_path += "            ]"

        new_body = (
            body[: path_match.start(2)] + formatted_path + body[path_match.end(2) :]
        )
        new_content = (
            self._content[: node_match.start(2)]
            + new_body
            + self._content[node_match.end(2) :]
        )

        try:
            with open(self.file_path, "w", encoding="utf-8") as f:
                f.write(new_content)
            return True
        except Exception as e:
            print(f"{_R}Error writing file:{_0} {e}")
            return False


def main():
    print(f"{_G}Welcome to MapTracker tool.{_0}")
    print(f"\n{_Y}Select a mode:{_0}")
    print(f"  {_C}[N]{_0} Create a new path")
    print(f"  {_C}[I]{_0} Import an existing path from pipeline file")

    mode = input("> ").strip().upper()

    map_name = None
    points = []

    # Store context for "Replace" functionality
    import_context = None

    if mode == "N":
        print("\n----------\n")
        print(f"{_Y}Please choose a map in the window.{_0}")
        # Step 1: Select Map
        map_selector = SelectMapPage()
        map_name = map_selector.run()
        if not map_name:
            print(f"\n{_Y}No map selected. Exiting.{_0}")
            return

        # Step 2: Edit Path (Empty initially)
        print(f"  Selected map: {map_name}")
        print(f"\n{_Y}Please edit the path in the window.{_0}")
        print("  Close the window when done.")
        try:
            editor = PathEditPage(map_name, [])
            points = editor.run()
        except ValueError as e:
            print(f"{_R}Error initializing editor:{_0} {e}")
            return

    elif mode == "I":
        print(f"\n{_Y}Where's your pipeline JSON file path?{_0}")
        file_path = input("> ").strip()
        file_path = file_path.strip('"').strip("'")

        handler = PipelineHandler(file_path)
        candidates = handler.read_nodes()

        if not candidates:
            print(f"{_R}No 'MapTrackerMove' nodes found in the file.{_0}")
            print(
                "Please make sure your JSON file contains nodes with 'custom_action' set to 'MapTrackerMove'."
            )
            return

        print(f"\n{_Y}Which node do you want to import?{_0}")
        for i, c in enumerate(candidates):
            print(
                f"  {_C}[{i+1}]{_0} {c['node_name']} {_A}(Map: {c['map_name']}, Points: {len(c['path'])}){_0}"
            )

        try:
            sel = int(input("> ")) - 1
            if not (0 <= sel < len(candidates)):
                print(f"{_R}Invalid selection.{_0}")
                return
            selected_node = candidates[sel]

            original_map_name = selected_node["map_name"]
            initial_points = selected_node["path"]

            # Try to resolve the actual map filename on disk (keeping suffix) for editing
            resolved = find_map_file(original_map_name)
            editor_map_name = resolved if resolved is not None else original_map_name

            print(
                f"  Editing node: {selected_node['node_name']} on map {original_map_name}"
            )
            print(f"\n{_Y}Please edit the path in the window.{_0}")
            print("  Close the window when done.")

            try:
                editor = PathEditPage(editor_map_name, initial_points)
                points = editor.run()

                # Setup context for Replace; keep original name from node for export normalization
                import_context = {
                    "file_path": file_path,
                    "handler": handler,
                    "node_name": selected_node["node_name"],
                    "original_map_name": original_map_name,
                    "is_new_structure": selected_node.get("is_new_structure", False),
                }

            except ValueError as e:
                print(f"{_R}Error initializing editor{_0}: {e}")
                return

        except ValueError:
            print(f"{_R}Invalid input.{_0}")
            return

    else:
        print(f"{_R}Invalid mode.{_0}")
        return

    # Export Logic
    print("\n----------\n")
    print(f"{_G}Finished editing.{_0}")
    print(f"  Total {len(points)} points")
    print(f"\n{_Y}Select an export mode:{_0}")
    if import_context:
        print(f"  {_C}[R]{_0} Replace original path in pipeline")
        print(f"      {_A}and write the changes back to your pipeline file.{_0}")
    print(f"  {_C}[J]{_0} Print the node JSON string")
    print(f"      {_A}which represents a new pipeline node.{_0}")
    print(f"  {_C}[D]{_0} Print the parameters dict")
    print(f"      {_A}which can be used as 'custom_action_param' field.{_0}")
    print(f"  {_C}[L]{_0} Print the point list")
    print(f"      {_A}which can be used as 'path'{_A} field.{_0}")

    export_mode = input("> ").strip().upper()

    param_data = {
        "map_name": map_name,
        "path": [[int(p[0]), int(p[1])] for p in points],
    }

    if export_mode == "R" and import_context:
        handler = import_context["handler"]
        node_name = import_context["node_name"]
        if handler.replace_path(node_name, points):
            print(
                f"\n{_G}Successfully updated node '{node_name}' in {import_context['file_path']}.{_0}"
            )
        else:
            print(f"\n{_R}Failed to update node.{_0}")

    elif export_mode == "J":
        raw_name = (
            import_context.get("original_map_name", map_name)
            if import_context
            else map_name
        )
        is_new = (
            import_context.get("is_new_structure", False) if import_context else False
        )
        norm = raw_name
        if isinstance(norm, str):
            if norm.endswith("_merged.png"):
                norm = norm[: -len("_merged.png")]
            elif norm.endswith(".png"):
                norm = norm[:-4]

        if is_new:
            node_data = {
                "action": {
                    "custom_action": "MapTrackerMove",
                    "custom_action_param": param_data,
                }
            }
        else:
            node_data = {
                "action": "Custom",
                "custom_action": "MapTrackerMove",
                "custom_action_param": param_data,
            }

        snippet = {"NodeName": node_data}
        print(f"\n{_C}--- JSON Snippet ---{_0}\n")
        print(json.dumps(snippet, indent=4, ensure_ascii=False))

    elif export_mode == "D":
        print(f"\n{_C}--- Parameters Dict ---{_0}\n")
        print(json.dumps(param_data, indent=None, ensure_ascii=False))

    else:
        SIMPact_str = "[" + ", ".join([str(p) for p in points]) + "]"
        if export_mode == "L":
            print(f"\n{_C}--- Point List ---{_0}\n")
            print(SIMPact_str)
        else:
            print(f"{_Y}Invalid export mode.{_0}")
            print(f"  To prevent data loss, the point list is printed below.{_0}")
            print(f"\n{_C}--- Point List ---{_0}\n")
            print(SIMPact_str)


if __name__ == "__main__":
    main()
