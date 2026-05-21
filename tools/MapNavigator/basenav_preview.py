from __future__ import annotations

import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from runtime import PROJECT_ROOT

BASENAV_TOOLS_DIR = PROJECT_ROOT / "agent" / "cpp-algo" / "NavmeshWorkspace" / "tools"


def load_basenav_field(input_file: Path) -> Any:
    if str(BASENAV_TOOLS_DIR) not in sys.path:
        sys.path.insert(0, str(BASENAV_TOOLS_DIR))
    try:
        from basenav_lib import BaseNavField
    except ModuleNotFoundError as exc:
        if exc.name != "basenav_lib":
            raise
        raise RuntimeError(f"缺少 BaseNav 预览依赖：{BASENAV_TOOLS_DIR / 'basenav_lib.py'}") from exc
    return BaseNavField(input_file)


@dataclass(frozen=True)
class PreviewRoute:
    points: list[tuple[float, float]]
    world_points: list[tuple[float, float]]
    cells: list[object]
    segment_breaks: list[int] | None = None


def find_preview_route(
    field: Any,
    zone_id: int,
    display_zone_id: str,
    start: tuple[float, float],
    goal: tuple[float, float],
    snap_radius: float,
) -> PreviewRoute:
    del display_zone_id
    route = field.find_route(zone_id, start, goal, snap_radius)
    return PreviewRoute(points=route.points, world_points=route.points, cells=[], segment_breaks=route.segment_breaks)
