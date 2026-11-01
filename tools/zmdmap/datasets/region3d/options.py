"""生成器自身设置，与客户端固定规则分开维护。"""

import math
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class GenerationOptions:
    reference_screen: tuple[int, int] = (1280, 720)
    panel_style_state: str = "Standalone"
    min_click_clearance_px: float = 2.0

    def __post_init__(self) -> None:
        if any(not math.isfinite(v) or v <= 0 for v in self.reference_screen):
            raise ValueError("screen dimensions must be finite and positive")
        if abs(self.reference_screen[0] / self.reference_screen[1] - 16 / 9) > 1e-9:
            raise ValueError("only 16:9 output is supported")
        if not self.panel_style_state:
            raise ValueError("panel_style_state must be non-empty")
        if not math.isfinite(self.min_click_clearance_px) or self.min_click_clearance_px < 0:
            raise ValueError("click clearance must be finite and non-negative")
