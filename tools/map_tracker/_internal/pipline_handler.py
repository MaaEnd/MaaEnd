import json
import re

from .core_utils import _R

NODE_TYPE_MOVE = "MapTrackerMove"
NODE_TYPE_ASSERT_LOCATION = "MapTrackerAssertLocation"


class PipelineHandler:
    """Handle reading and writing of Pipeline JSON, using regex to preserve comments and formatting."""

    def __init__(self, file_path):
        self.file_path = file_path
        self._content = ""
        self.nodes: dict[str, dict] = {}

    def _load(self):
        try:
            with open(self.file_path, "r", encoding="utf-8") as f:
                self._content = f.read()
            return True
        except Exception as e:
            print(f"{_R}Error reading file:{e}")
            return False

    @staticmethod
    def _extract_json_array(text: str, field_name: str) -> tuple[int, int, str] | None:
        key_match = re.search(r'"' + re.escape(field_name) + r'"\s*:\s*\[', text)
        if not key_match:
            return None
        start = text.find("[", key_match.start())
        if start < 0:
            return None

        i = start
        depth = 0
        in_str = False
        escape = False
        while i < len(text):
            ch = text[i]
            if in_str:
                if escape:
                    escape = False
                elif ch == "\\":
                    escape = True
                elif ch == '"':
                    in_str = False
            else:
                if ch == '"':
                    in_str = True
                elif ch == "[":
                    depth += 1
                elif ch == "]":
                    depth -= 1
                    if depth == 0:
                        end = i + 1
                        return (start, end, text[start:end])
            i += 1
        return None

    @staticmethod
    def _find_matching_brace(text: str, start: int) -> int:
        i = start
        depth = 0
        in_str = False
        escape = False
        while i < len(text):
            ch = text[i]
            if in_str:
                if escape:
                    escape = False
                elif ch == "\\":
                    escape = True
                elif ch == '"':
                    in_str = False
            else:
                if ch == '"':
                    in_str = True
                elif ch == "{":
                    depth += 1
                elif ch == "}":
                    depth -= 1
                    if depth == 0:
                        return i
            i += 1
        return -1

    @staticmethod
    def _iter_top_level_nodes(content: str) -> list[tuple[str, str]]:
        nodes: list[tuple[str, str]] = []
        root_start = content.find("{")
        if root_start < 0:
            return nodes

        i = root_start + 1
        n = len(content)
        while i < n:
            while i < n and content[i] in " \t\r\n,":
                i += 1
            if i >= n or content[i] == "}":
                break
            if content[i] != '"':
                i += 1
                continue

            key_start = i + 1
            i += 1
            escape = False
            while i < n:
                ch = content[i]
                if escape:
                    escape = False
                elif ch == "\\":
                    escape = True
                elif ch == '"':
                    break
                i += 1
            if i >= n:
                break
            key = content[key_start:i]
            i += 1

            while i < n and content[i] in " \t\r\n":
                i += 1
            if i >= n or content[i] != ":":
                continue
            i += 1

            while i < n and content[i] in " \t\r\n":
                i += 1
            if i >= n or content[i] != "{":
                continue

            obj_start = i
            obj_end = PipelineHandler._find_matching_brace(content, obj_start)
            if obj_end < 0:
                break
            node_content = content[obj_start : obj_end + 1]
            nodes.append((key, node_content))
            i = obj_end + 1

        return nodes

    @staticmethod
    def _find_top_level_node_bounds(
        content: str, node_name: str
    ) -> tuple[int, int, str] | None:
        root_start = content.find("{")
        if root_start < 0:
            return None

        i = root_start + 1
        n = len(content)
        while i < n:
            while i < n and content[i] in " \t\r\n,":
                i += 1
            if i >= n or content[i] == "}":
                break
            if content[i] != '"':
                i += 1
                continue

            key_start = i + 1
            i += 1
            escape = False
            while i < n:
                ch = content[i]
                if escape:
                    escape = False
                elif ch == "\\":
                    escape = True
                elif ch == '"':
                    break
                i += 1
            if i >= n:
                break
            key = content[key_start:i]
            i += 1

            while i < n and content[i] in " \t\r\n":
                i += 1
            if i >= n or content[i] != ":":
                continue
            i += 1

            while i < n and content[i] in " \t\r\n":
                i += 1
            if i >= n or content[i] != "{":
                continue

            obj_start = i
            obj_end = PipelineHandler._find_matching_brace(content, obj_start)
            if obj_end < 0:
                break
            if key == node_name:
                return (obj_start, obj_end + 1, content[obj_start : obj_end + 1])
            i = obj_end + 1

        return None

    @staticmethod
    def _parse_tracker_move_fields(node_content: str) -> dict | None:
        if f'"custom_action": "{NODE_TYPE_MOVE}"' not in node_content:
            return None

        is_new_structure = re.search(r'"action"\s*:\s*\{', node_content) is not None

        m_match = re.search(r'"map_name"\s*:\s*"([^"]+)"', node_content)
        map_name = m_match.group(1) if m_match else "Unknown"

        t_match = re.search(r'"path"\s*:\s*(\[[\s\S]*?\]\s*\]|\[\s*\])', node_content)
        if not t_match:
            return None
        try:
            path = json.loads(t_match.group(1))
        except Exception:
            return None

        return {
            "node_type": NODE_TYPE_MOVE,
            "map_name": map_name,
            "path": path,
            "is_new_structure": is_new_structure,
        }

    @staticmethod
    def _parse_assert_location_fields(node_content: str) -> dict | None:
        if f'"custom_recognition": "{NODE_TYPE_ASSERT_LOCATION}"' not in node_content:
            return None

        expected_range = PipelineHandler._extract_json_array(node_content, "expected")
        if expected_range is None:
            return None
        try:
            expected = json.loads(expected_range[2])
        except Exception:
            return None
        if not isinstance(expected, list) or len(expected) == 0:
            return None
        first = expected[0]
        if not isinstance(first, dict):
            return None
        map_name = first.get("map_name")
        target = first.get("target")
        if (
            not isinstance(map_name, str)
            or not isinstance(target, list)
            or len(target) != 4
        ):
            return None

        return {
            "node_type": NODE_TYPE_ASSERT_LOCATION,
            "map_name": map_name,
            "target": [float(v) for v in target],
            "expected": expected,
        }

    def read_all_nodes(self) -> bool:
        if not self._load():
            return False

        self.nodes.clear()
        for node_name, node_content in self._iter_top_level_nodes(self._content):
            entry: dict = {"content": node_content}
            tracker = self._parse_tracker_move_fields(node_content)
            if tracker is None:
                tracker = self._parse_assert_location_fields(node_content)
            if tracker is not None:
                entry.update(tracker)
                entry["is_tracker"] = True
            else:
                entry["is_tracker"] = False
            self.nodes[node_name] = entry
        return True

    def read_nodes(self) -> list[dict]:
        self.read_all_nodes()
        results = []
        for node_name, entry in self.nodes.items():
            if entry.get("is_tracker"):
                results.append(
                    {
                        "node_name": node_name,
                        "node_type": entry.get("node_type", NODE_TYPE_MOVE),
                        "map_name": entry["map_name"],
                        "path": entry.get("path", []),
                        "target": entry.get("target"),
                        "is_new_structure": entry.get("is_new_structure", False),
                    }
                )
        return results

    def get_tracker_nodes(self) -> list[dict]:
        return [
            {
                "node_name": name,
                "node_type": entry.get("node_type", NODE_TYPE_MOVE),
                "map_name": entry["map_name"],
                "path": entry.get("path", []),
                "target": entry.get("target"),
                "is_new_structure": entry.get("is_new_structure", False),
            }
            for name, entry in self.nodes.items()
            if entry.get("is_tracker")
        ]

    def _replace_node_body(self, node_name: str, new_body: str) -> bool:
        bounds = self._find_top_level_node_bounds(self._content, node_name)
        if bounds is None:
            print(f"{_R}Error: Node {node_name} not found in file when saving.")
            return False
        node_start, node_end, _ = bounds
        self._content = self._content[:node_start] + new_body + self._content[node_end:]
        return True

    def replace_path(self, node_name: str, new_path: list) -> bool:
        if not self._load():
            return False

        bounds = self._find_top_level_node_bounds(self._content, node_name)
        if bounds is None:
            print(f"{_R}Error: Node {node_name} not found in file when saving.")
            return False
        _, _, body = bounds

        path_pattern = re.compile(
            r'("path"\s*:\s*)(\[[\s\S]*?\]\s*\]|\[\s*\])',
            re.MULTILINE,
        )
        path_match = path_pattern.search(body)
        if not path_match:
            print(f"{_R}Error: 'path' field not found in node {node_name} when saving.")
            return False

        if self.nodes.get(node_name, {}).get("is_new_structure", False):
            indent_sm = " " * 20
            indent_lg = " " * 24
        else:
            indent_sm = " " * 12
            indent_lg = " " * 16

        if not new_path:
            formatted_path = "[]"
        else:
            formatted_path = "[\n"
            for i, p in enumerate(new_path):
                comma = "," if i < len(new_path) - 1 else ""
                formatted_path += f"{indent_lg}[{p[0]:.1f}, {p[1]:.1f}]{comma}\n"
            formatted_path += f"{indent_sm}]"

        new_body = (
            body[: path_match.start(2)] + formatted_path + body[path_match.end(2) :]
        )
        if not self._replace_node_body(node_name, new_body):
            return False

        try:
            with open(self.file_path, "w", encoding="utf-8") as f:
                f.write(self._content)
        except Exception as e:
            print(f"{_R}Error writing file:{e}")
            return False

        if node_name in self.nodes:
            self.nodes[node_name]["path"] = [
                [round(p[0], 1), round(p[1], 1)] for p in new_path
            ]
        return True

    def replace_assert_location(
        self, node_name: str, map_name: str, target: list[float]
    ) -> bool:
        if not self._load():
            return False

        bounds = self._find_top_level_node_bounds(self._content, node_name)
        if bounds is None:
            print(f"{_R}Error: Node {node_name} not found in file when saving.")
            return False
        _, _, body = bounds

        expected_range = self._extract_json_array(body, "expected")
        if expected_range is None:
            print(
                f"{_R}Error: 'expected' field not found in node {node_name} when saving."
            )
            return False

        try:
            expected = json.loads(expected_range[2])
        except Exception:
            print(f"{_R}Error: failed to parse 'expected' field in node {node_name}.")
            return False
        if (
            not isinstance(expected, list)
            or len(expected) == 0
            or not isinstance(expected[0], dict)
        ):
            print(f"{_R}Error: invalid 'expected' structure in node {node_name}.")
            return False

        expected[0]["map_name"] = map_name
        expected[0]["target"] = [round(float(v), 1) for v in target]

        formatted_expected = json.dumps(expected, ensure_ascii=False, indent=4)
        line_start = body.rfind("\n", 0, expected_range[0]) + 1
        line_prefix = body[line_start : expected_range[0]]
        base_indent = re.match(r"[ \t]*", line_prefix).group(0)
        formatted_expected = formatted_expected.replace("\n", "\n" + base_indent)

        new_body = (
            body[: expected_range[0]] + formatted_expected + body[expected_range[1] :]
        )
        if not self._replace_node_body(node_name, new_body):
            return False

        try:
            with open(self.file_path, "w", encoding="utf-8") as f:
                f.write(self._content)
        except Exception as e:
            print(f"{_R}Error writing file:{e}")
            return False

        if node_name in self.nodes:
            self.nodes[node_name]["map_name"] = map_name
            self.nodes[node_name]["target"] = [round(float(v), 1) for v in target]
        return True
