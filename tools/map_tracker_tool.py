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
    @staticmethod
    def text(
        img,
        text,
        pos,
        font_scale=0.5,
        color=(255, 255, 255),
        thickness=1,
        bg_color=None,
        bg_padding=5,
    ):
        if bg_color is not None:
            text_size = cv2.getTextSize(
                text, cv2.FONT_HERSHEY_SIMPLEX, font_scale, thickness
            )[0]
            cv2.rectangle(
                img,
                (pos[0] - bg_padding, pos[1] - text_size[1] - bg_padding),
                (pos[0] + text_size[0] + bg_padding, pos[1] + bg_padding),
                bg_color,
                -1,
            )
        cv2.putText(
            img, text, pos, cv2.FONT_HERSHEY_SIMPLEX, font_scale, color, thickness
        )

    @staticmethod
    def text_centered(img, text, y, font_scale=0.6, color=(255, 255, 255), thickness=1):
        text_size = cv2.getTextSize(
            text, cv2.FONT_HERSHEY_SIMPLEX, font_scale, thickness
        )[0]
        x = (img.shape[1] - text_size[0]) // 2
        Drawer.text(img, text, (x, y), font_scale, color, thickness)

    @staticmethod
    def rect(img, pt1, pt2, color=(255, 255, 255), thickness=1):
        cv2.rectangle(img, pt1, pt2, color, thickness)

    @staticmethod
    def circle(img, center, radius, color=(255, 255, 255), thickness=-1):
        cv2.circle(img, center, radius, color, thickness)

    @staticmethod
    def line(img, pt1, pt2, color=(255, 255, 255), thickness=1):
        cv2.line(img, pt1, pt2, color, thickness)


class SelectMapPage:
    """地图选择页面"""

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
        self.window_name = "Select Map"

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
        display_img = np.zeros((self.window_h, self.window_w, 3), dtype=np.uint8)
        start_idx = self.current_page * self.page_size
        end_idx = min(start_idx + self.page_size, len(self.map_files))

        # 内容区域高度（去掉底部导航）
        content_h = self.window_h - self.nav_height
        content_w = self.window_w

        # 计算横向与纵向间隔（space-between），当 cols/rows==1 时居中
        if self.cols > 1:
            gap_x = int((content_w - self.cols * self.cell_size) / (self.cols - 1))
        else:
            gap_x = 0
        if self.rows > 1:
            gap_y = int((content_h - self.rows * self.cell_size) / (self.rows - 1))
        else:
            gap_y = 0

        # 绘制地图预览，按 space-between 布局
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
                # 计算保持长宽比的缩放，使图片完整放入 cell
                scale = min(self.cell_size / w, self.cell_size / h)
                new_w = max(1, int(w * scale))
                new_h = max(1, int(h * scale))
                resized = cv2.resize(img, (new_w, new_h))
                # 在 cell 内居中放置
                x1 = cell_x
                y1 = cell_y
                x2 = x1 + self.cell_size
                y2 = y1 + self.cell_size
                # 计算放置偏移
                dx = (self.cell_size - new_w) // 2
                dy = (self.cell_size - new_h) // 2
                dest_x1 = x1 + dx
                dest_y1 = y1 + dy
                dest_x2 = dest_x1 + new_w
                dest_y2 = dest_y1 + new_h
                # 边界裁剪（以防超出 content 区域）
                dest_x2 = min(self.window_w, dest_x2)
                dest_y2 = min(content_h, dest_y2)
                src_x2 = dest_x2 - dest_x1
                src_y2 = dest_y2 - dest_y1
                if src_x2 > 0 and src_y2 > 0:
                    display_img[
                        dest_y1 : dest_y1 + src_y2, dest_x1 : dest_x1 + src_x2
                    ] = resized[0:src_y2, 0:src_x2]

                # 标签（底部）
                label = self.map_files[i]
                Drawer.rect(
                    display_img,
                    (x1, y1 + self.cell_size - 30),
                    (x1 + self.cell_size, y1 + self.cell_size),
                    (0, 0, 0),
                    -1,
                )
                Drawer.text(
                    display_img,
                    label,
                    (x1 + 5, y1 + self.cell_size - 10),
                    font_scale=0.4,
                )

        # 底部导航栏
        Drawer.line(
            display_img,
            (0, content_h),
            (self.window_w, content_h),
            (128, 128, 128),
            2,
        )

        # 顶部导航提示文本
        Drawer.text_centered(
            display_img,
            "Please click a map to continue",
            content_h + 30,
            font_scale=0.7,
        )

        # 左箭头
        Drawer.text(
            display_img,
            "< PREV",
            (150, self.window_h - 20),
            font_scale=0.6,
            color=(0, 255, 0) if self.current_page > 0 else (128, 128, 128),
            thickness=2,
        )

        # 中间分页信息
        page_text = f"Page {self.current_page + 1} / {self.total_pages}"
        Drawer.text_centered(display_img, page_text, self.window_h - 20, font_scale=0.5)

        # 右箭头
        Drawer.text(
            display_img,
            "NEXT >",
            (self.window_w - 200, self.window_h - 20),
            font_scale=0.6,
            color=(
                (0, 255, 0)
                if self.current_page < self.total_pages - 1
                else (128, 128, 128)
            ),
            thickness=2,
        )

        self.cached_img = display_img
        self.cached_page = self.current_page
        return display_img

    def _handle_mouse(self, event, x, y, flags, param):
        if event == cv2.EVENT_LBUTTONDOWN:
            # 内容区域高度（去掉底部导航）
            content_h = self.window_h - self.nav_height
            if y < content_h:
                # 使用布局计算，判断点击落在哪个格子内
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
                # 底部导航
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
            display_img = self._render_page()
            cv2.imshow(self.window_name, display_img)

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
    """路径编辑页面"""

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
        self.window_name = "Location Tool (Edit Mode)"

        self.drag_idx = -1
        self.panning = False
        self.pan_start = (0, 0)
        self.point_radius = 5
        self.selection_threshold = 10
        # Action state for point interactions (left button):
        self.action_down_idx = -1
        self.action_mouse_down = False
        self.action_down_pos = (0, 0)
        self.action_moved = False
        self.action_dragging = False
        self.done = False

    def _get_map_coords(self, screen_x, screen_y):
        """将屏幕(视口)坐标转换为地图原始坐标"""
        mx = int(screen_x / self.scale + self.offset_x)
        my = int(screen_y / self.scale + self.offset_y)
        return mx, my

    def _get_screen_coords(self, map_x, map_y):
        """将地图原始坐标转换为屏幕(视口)坐标"""
        sx = int((map_x - self.offset_x) * self.scale)
        sy = int((map_y - self.offset_y) * self.scale)
        return sx, sy

    def _is_on_line(self, mx, my, p1, p2, threshold=10):
        """检查点是否在两点连线上"""
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
            color = (0, 165, 255) if i == self.drag_idx else (0, 0, 255)

            if i > 0:
                psx, psy = self._get_screen_coords(
                    self.points[i - 1][0], self.points[i - 1][1]
                )
                Drawer.line(
                    display_img,
                    (psx, psy),
                    (sx, sy),
                    (0, 0, 255),
                    max(1, int(2 * self.scale**0.5)),
                )

            Drawer.circle(
                display_img,
                (sx, sy),
                int(self.point_radius * max(0.5, self.scale**0.5)),
                color,
            )

            Drawer.text(display_img, str(i), (sx + 5, sy - 5), font_scale=0.45)

        legend_x, legend_y = 10, 10
        legend_lines = [
            "[ Tips ]",
            "Mouse Left Click: Add/Delete Point",
            "Mouse Left Drag: Move Point",
            "Mouse Right Click: Drag Map",
            "Close Window: Finish",
        ]
        font_scale = 0.5
        thickness = 1
        padding = 10
        line_height = 25

        max_width = 0
        for line in legend_lines:
            text_size = cv2.getTextSize(
                line, cv2.FONT_HERSHEY_SIMPLEX, font_scale, thickness
            )[0]
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
            Drawer.text(
                display_img,
                line,
                (legend_x + padding, y_pos),
                font_scale=font_scale,
                color=(255, 255, 255),
                thickness=thickness,
            )

        # 绘制左下角状态显示
        Drawer.text(
            display_img,
            f"Zoom: {self.scale:.2f}x | Points: {len(self.points)}",
            (20, self.window_h - 20),
            color=(0, 255, 255),
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
            self.scale = max(0.2, min(self.scale, 5.0))

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

            # SIMPatibility: if left button held and drag_idx set, move point
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
                    else:
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
                                inserted = True
                                break
                        if not inserted:
                            self.points.append([mx, my])

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
            # 检查窗口是否被关闭
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break
            if cv2.waitKey(1) & 0xFF == 27:
                cv2.destroyAllWindows()
                return None

        cv2.destroyAllWindows()
        return [list(p) for p in self.points]


def find_map_file(name, map_dir=MAP_DIR):
    """在磁盘上寻找与给定 name 对应的文件名（保留后缀），返回文件名或 None。"""
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
    """处理 Pipeline JSON 的读写，使用正则以保留注释和格式"""

    def __init__(self, file_path):
        self.file_path = file_path
        self._content = ""

    def read_nodes(self):
        """读取所有 MapTrackerMove 节点"""
        try:
            with open(self.file_path, "r", encoding="utf-8") as f:
                self._content = f.read()
        except Exception as e:
            print(f"{_R}Error reading file:{_0} {e}")
            return []

        # 先分割成节点
        # 匹配顶级节点: "name": { ... }
        node_pattern = re.compile(
            r'^\s*"([^"]+)"\s*:\s*(\{[\s\S]*?\n\s*\})', re.MULTILINE
        )
        results = []
        for match in node_pattern.finditer(self._content):
            node_name = match.group(1)
            node_content = match.group(2)
            # 检查节点是否包含 MapTrackerMove
            if '"custom_action": "MapTrackerMove"' in node_content:
                # 提取 map_name
                m_match = re.search(r'"map_name"\s*:\s*"([^"]+)"', node_content)
                map_name = m_match.group(1) if m_match else "Unknown"
                # 提取 targets
                t_match = re.search(
                    r'"targets"\s*:\s*(\[[\s\S]*?\]\s*\]|\[\s*\])', node_content
                )
                if t_match:
                    targets_str = t_match.group(1)
                    try:
                        targets = json.loads(targets_str)
                        results.append(
                            {
                                "node_name": node_name,
                                "map_name": map_name,
                                "targets": targets,
                            }
                        )
                    except:
                        continue
        return results

    def replace_targets(self, node_name, new_targets):
        """正则替换 pipeline 文件中的 targets 列表"""
        try:
            with open(self.file_path, "r", encoding="utf-8") as f:
                self._content = f.read()
        except:
            return False

        # 构造正则：匹配该节点下的 targets 字段
        pattern = re.compile(
            r'(\s*"'
            + re.escape(node_name)
            + r'"\s*:\s*\{[\s\S]*?"targets"\s*:\s*)(\[[\s\S]*?\]\s*\]|\[\s*\])',
            re.MULTILINE,
        )

        match = pattern.search(self._content)
        if not match:
            print(f"{_R}Error: Node {node_name} not found in file when saving.{_0}")
            return False

        # 格式化新 targets，遵循多行数组规范
        if not new_targets:
            formatted_targets = "[]"
        else:
            formatted_targets = "[\n"
            for i, p in enumerate(new_targets):
                comma = "," if i < len(new_targets) - 1 else ""
                formatted_targets += f"                [{p[0]}, {p[1]}]{comma}\n"
            formatted_targets += "            ]"

        new_content = (
            self._content[: match.start(2)]
            + formatted_targets
            + self._content[match.end(2) :]
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
                f"  {_C}[{i+1}]{_0} {c['node_name']} {_A}(Map: {c['map_name']}, Points: {len(c['targets'])}){_0}"
            )

        try:
            sel = int(input("> ")) - 1
            if not (0 <= sel < len(candidates)):
                print(f"{_R}Invalid selection.{_0}")
                return
            selected_node = candidates[sel]

            original_map_name = selected_node["map_name"]
            initial_points = selected_node["targets"]

            # 尝试在磁盘上解析出实际的地图文件名（保留后缀）用于编辑
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

    if points is None:
        print(f"{_Y}Editing cancelled.{_0}")
        return

    # Export Logic
    print("\n----------\n")
    print(f"{_G}Finished editing.{_0}")
    print(f"  Total {len(points)} points")
    print(f"\n{_Y}Select an export mode:{_0}")
    if import_context:
        print(f"  {_C}[R]{_0} Replace original targets in pipeline")
        print(f"      {_A}Write the changes back to {import_context['file_path']}{_0}")
    print(f"  {_C}[J]{_0} Print the node JSON string")
    print(f"      {_A}You can then copy the string as a new node.{_0}")
    print(f"  {_C}[L]{_0} Print the point list")
    print(
        f"      {_A}You can copy the list and replace the {_0}'targets'{_A} field in your existing node.{_0}"
    )

    export_mode = input("> ").strip().upper()

    if export_mode == "R" and import_context:
        handler = import_context["handler"]
        node_name = import_context["node_name"]
        if handler.replace_targets(node_name, points):
            print(
                f"\n{_G}Successfully updated node '{node_name}' in {import_context['file_path']}.{_0}"
            )
        else:
            print(f"\n{_R}Failed to update node.{_0}")

    elif export_mode == "J":
        # Construct a snippet
        raw_name = (
            import_context.get("original_map_name", map_name)
            if import_context
            else map_name
        )
        norm = raw_name
        if isinstance(norm, str):
            if norm.endswith("_merged.png"):
                norm = norm[: -len("_merged.png")]
            elif norm.endswith(".png"):
                norm = norm[:-4]

        snippet = {
            "NodeName": {
                "action": "Custom",
                "custom_action": "MapTrackerMove",
                "custom_action_param": {
                    "map_name": norm,
                    "targets": [[int(p[0]), int(p[1])] for p in points],
                },
            }
        }
        print(f"\n{_C}--- JSON Snippet ---{_0}\n")
        print(json.dumps(snippet, indent=4, ensure_ascii=False))

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
