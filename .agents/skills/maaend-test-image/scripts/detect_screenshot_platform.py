# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "numpy>=2.0",
#     "pillow>=11",
# ]
# ///

"""判断 MaaEnd 节点测试截图来自哪个控制器（ADB / Win32）。

两个平台把底部 HUD 的 UID 文字渲染成不同大小，`FIXED_ROIS` 里两套 ROI 正是按各自的文字范围标定的：

- `Win32`：UID 文字右边界约 x=157、高约 8，落在 `[70, 696, 90, 13]` 内；
- `ADB`：UID 文字右边界约 x=196、高约 10，落在 `[84, 690, 114, 18]` 内。

把 ADB 截图当成 Win32 导入时，Win32 的固定 ROI 只盖住文字前半段，尾部数字会留在图里；而 OCR 兜底只认 `UID` / `#` 标记词，前缀被盖掉后不会再报警。所以在脱敏前先判定平台。

判据取自 UID 行条带：条带里亮度超过"自适应阈值"（条带 75 分位 + 40，限制在 120..210）的像素视为文字。条带从 x=85 起，避开左侧的延迟文字（`42ms`）；明亮场景背景比文字暗，自适应阈值会随之上移，因此不会被条纹干扰。

用法：

```powershell
uv run .agents/skills/maaend-test-image/scripts/detect_screenshot_platform.py <截图.png> [更多截图]
```

退出码：每张图都能给出明确平台时为 0，存在无法判定（unknown / ambiguous）时为 2。
"""

from __future__ import annotations

import sys
from dataclasses import dataclass, field
from pathlib import Path

import numpy as np
from PIL import Image


SUPPORTED_CONTROLLERS = {"adb": "ADB", "win32": "Win32"}

# 按平台标定的 UID 固定打码 ROI，格式 x, y, w, h。
UID_FIXED_ROIS = {
    "ADB": [84, 690, 114, 18],
    "Win32": [70, 696, 90, 13],
}

# UID 文字所在条带（y0, y1, x0, x1）；x 从 85 起是为了避开左侧延迟文字。
UID_BAND = (690, 711, 85, 251)
# 自适应阈值：条带 75 分位 + 偏移，并限制在上下限之间。
UID_TEXT_PERCENTILE = 75
UID_TEXT_OFFSET = 40
UID_TEXT_MIN_THRESHOLD = 120
UID_TEXT_MAX_THRESHOLD = 210
# 一行/一列至少要有这么多文字像素，才算文字的一部分（滤掉噪点）。
MIN_PIXELS_PER_LINE = 2

EXPECTED_SIZE = (1280, 720)

# 用于在"两套 ROI 都覆盖"时倾向哪一边的经验值：右边界、字高、文字上边界。
WIN32_TEXT_MAX_RIGHT = 168
ADB_TEXT_MIN_RIGHT = 175
ADB_TEXT_MIN_HEIGHT = 10
WIN32_TEXT_MAX_HEIGHT = 9
WIN32_TEXT_MIN_TOP = 698
ADB_TEXT_MAX_TOP = 697


@dataclass
class PlatformDetection:
    """平台判定结果。"""

    platform: str | None
    confidence: str  # definite / ambiguous / unknown
    reason: str
    text_box: tuple[int, int, int, int] | None = None  # x0, y0, x1, y1
    suggested_extra_rois: list[list[int]] = field(default_factory=list)

    @property
    def decided(self) -> bool:
        return self.platform is not None and self.confidence == "definite"

    def describe(self) -> str:
        return f"{self.platform or '未判定'}({self.confidence}): {self.reason}"


def covers(roi: list[int], box: tuple[int, int, int, int]) -> bool:
    """判断 ROI 是否完整覆盖文字框。"""
    x, y, width, height = roi
    x0, y0, x1, y1 = box
    return x <= x0 and y <= y0 and x1 <= x + width - 1 and y1 <= y + height - 1


def uid_text_threshold(image: np.ndarray) -> int:
    """按 UID 行条带的亮度分布算出文字阈值。"""
    y0, y1, x0, x1 = UID_BAND
    band = image[y0:y1, x0:x1, :3].min(axis=2)
    percentile = float(np.percentile(band, UID_TEXT_PERCENTILE))
    return int(min(UID_TEXT_MAX_THRESHOLD, max(UID_TEXT_MIN_THRESHOLD, percentile + UID_TEXT_OFFSET)))


def find_uid_text_box(image: np.ndarray) -> tuple[int, int, int, int] | None:
    """在 UID 行条带里找文字范围，返回 (x0, y0, x1, y1)，找不到返回 None。"""
    y0, y1, x0, x1 = UID_BAND
    if image.shape[0] < y1 or image.shape[1] < x1:
        return None
    band = image[y0:y1, x0:x1, :3]
    mask = np.all(band >= uid_text_threshold(image), axis=2)
    columns = np.where(mask.sum(axis=0) >= MIN_PIXELS_PER_LINE)[0]
    if columns.size == 0:
        return None
    rows = np.where(mask.sum(axis=1) >= MIN_PIXELS_PER_LINE)[0]
    return (
        int(columns.min()) + x0,
        int(rows.min()) + y0,
        int(columns.max()) + x0,
        int(rows.max()) + y0,
    )


def suggest_extra_rois(box: tuple[int, int, int, int], margin: int = 4) -> list[list[int]]:
    """按文字框给出建议的额外 ROI（四周各留 margin 像素）。"""
    x0, y0, x1, y1 = box
    width, height = EXPECTED_SIZE
    left = max(0, x0 - margin)
    top = max(0, y0 - margin)
    right = min(width - 1, x1 + margin)
    bottom = min(height - 1, y1 + margin)
    return [[left, top, right - left + 1, bottom - top + 1]]


def detect_platform(image: np.ndarray) -> PlatformDetection:
    """按 UID 文字范围判定截图平台。"""
    size = (image.shape[1], image.shape[0])
    if size != EXPECTED_SIZE:
        return PlatformDetection(
            platform=None,
            confidence="unknown",
            reason=f"截图尺寸 {size[0]}x{size[1]} 不是 {EXPECTED_SIZE[0]}x{EXPECTED_SIZE[1]}",
        )

    box = find_uid_text_box(image)
    if box is None:
        return PlatformDetection(
            platform=None,
            confidence="unknown",
            reason="UID 行条带里没有找到文字（可能已打过码，或该界面不显示 UID）",
        )

    x0, y0, x1, y1 = box
    text_height = y1 - y0 + 1
    metrics = f"UID 文字 x {x0}..{x1}（宽 {x1 - x0 + 1}）、y {y0}..{y1}（高 {text_height}）"

    fits_win32 = covers(UID_FIXED_ROIS["Win32"], box)
    fits_adb = covers(UID_FIXED_ROIS["ADB"], box)

    # ADB 的 ROI 更宽，Win32 的 UID 文字往往两套都能盖住，所以还要看字高和文字上边界。
    win32_signature = (
        x1 <= WIN32_TEXT_MAX_RIGHT and text_height <= WIN32_TEXT_MAX_HEIGHT and y0 >= WIN32_TEXT_MIN_TOP
    )
    adb_signature = x1 >= ADB_TEXT_MIN_RIGHT or text_height >= ADB_TEXT_MIN_HEIGHT or y0 <= ADB_TEXT_MAX_TOP

    if fits_win32 and not fits_adb:
        if win32_signature:
            return PlatformDetection("Win32", "definite", f"{metrics}，只落在 Win32 固定 ROI 内", box)
        return PlatformDetection(
            "Win32",
            "ambiguous",
            f"{metrics}，只落在 Win32 固定 ROI 内，但字高/位置不符合既有 Win32 样本",
            box,
        )
    if fits_adb and not fits_win32:
        return PlatformDetection("ADB", "definite", f"{metrics}，只落在 ADB 固定 ROI 内", box)

    if fits_win32 and fits_adb:
        # UID 较短时两套 ROI 都能盖住，此时不影响脱敏安全，但字高和位置仍能区分平台。
        if win32_signature and not adb_signature:
            return PlatformDetection(
                "Win32", "definite", f"{metrics}，两套固定 ROI 都能盖住，字高与位置符合 Win32", box
            )
        lean = "ADB" if adb_signature else "Win32"
        return PlatformDetection(
            lean,
            "ambiguous",
            f"{metrics}，两套固定 ROI 都能盖住，按字高/位置倾向 {lean}",
            box,
        )

    return PlatformDetection(
        platform=None,
        confidence="unknown",
        reason=f"{metrics}，两套固定 ROI 都盖不住",
        text_box=box,
        suggested_extra_rois=suggest_extra_rois(box),
    )


def detect_from_file(path: Path) -> PlatformDetection:
    """读取图片并判定平台。"""
    if not path.is_file():
        raise SystemExit(f"截图不存在：{path}")
    with Image.open(path) as raw:
        image = np.asarray(raw.convert("RGB"), dtype=np.int16)
    return detect_platform(image)


def format_extra_rois(rois: list[list[int]]) -> str:
    """把建议的额外 ROI 拼成命令行参数。"""
    return " ".join(f"--extra-roi {x},{y},{w},{h}" for x, y, w, h in rois)


def main() -> int:
    if len(sys.argv) < 2:
        print(__doc__)
        return 2

    unresolved = False
    for raw_path in sys.argv[1:]:
        path = Path(raw_path)
        detection = detect_from_file(path)
        suffix = ""
        if detection.suggested_extra_rois:
            suffix = f"；建议 {format_extra_rois(detection.suggested_extra_rois)}"
        print(f"{path.name}: {detection.describe()}{suffix}")
        if not detection.decided:
            unresolved = True
    return 2 if unresolved else 0


if __name__ == "__main__":
    raise SystemExit(main())
