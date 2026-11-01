# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "numpy>=2.0",
#     "pillow>=11",
# ]
# ///

"""判断 MaaEnd 节点测试截图来自哪个控制器（ADB / CloudADB / Win32）。

判据是左下状态行（HUD）里的 UID 文字，三个平台把它画在不同位置，`UID_FIXED_ROIS` 里三套 ROI 正是按各自的文字范围标定的：

- `Win32`：字号最小，UID 文字右边界约 x=157、高约 8、上边界约 y=700，落在 `[70, 696, 90, 13]` 内；
- `ADB`：UID 文字右边界约 x=196、高约 10、上边界约 y=694，前面跟着"信号格 + xx ms"延迟指示，落在 `[84, 690, 114, 18]` 内；
- `CloudADB`：云客户端左下角**没有延迟指示**，整行 UID 因此左移到 x≈18 起、右边界约 x=130、高约 10，落在 `[18, 690, 114, 18]` 内。

所以"有没有延迟指示"是区分 CloudADB 的关键：条带里只找到一段文字时，这一段就是贴着左边缘的 UID（CloudADB）；找到两段以上时，左侧一段是延迟指示，最右侧一段才是 UID（ADB / Win32），再按字高和上边界分档。

文字判定用条带亮度的自适应阈值（条带 75 分位 + 40，限制在 120..210）；明亮场景的背景比文字暗，阈值会随之上移，不会被条纹干扰。判定前还会先看条带里有没有纯绿打码块，有就直接报 `unknown`。

把 ADB 截图当成 Win32 导入时，Win32 的固定 ROI 只盖住文字前半段，尾部数字会留在图里；CloudADB 截图按 ADB 的 ROI 打码则会漏掉左侧的 `UID:` 前缀。而 OCR 兜底只认 `UID` / `#` 标记词，前缀被盖掉后不会再报警。所以在脱敏前先判定平台。

用法：

```powershell
uv run .agents/skills/maaend-test-image/scripts/detect_screenshot_platform.py <截图.png> [更多截图]
```

退出码：每张图都能给出明确平台时为 0，存在无法判定（unknown / ambiguous）时为 2。只能拿原始截图来判：已经打过码的图里 UID 被色块盖住，脚本会报 `unknown`。
"""

import sys
from dataclasses import dataclass, field
from pathlib import Path

import numpy as np
from PIL import Image

SUPPORTED_CONTROLLERS = {"adb": "ADB", "cloudadb": "CloudADB", "win32": "Win32"}

# 按平台标定的 UID 固定打码 ROI，格式 x, y, w, h。
UID_FIXED_ROIS = {
    "ADB": [84, 690, 114, 18],
    "CloudADB": [18, 690, 114, 18],
    "Win32": [70, 696, 90, 13],
}

# UID 文字所在条带（y0, y1, x0, x1）。x 从 0 起：CloudADB 的 UID 贴着左边缘，
# 从中间起扫会把左侧的 "UID:" 切掉，判定和打码边界都会跟着错。
UID_BAND = (688, 712, 0, 280)
# 自适应阈值：条带 75 分位 + 偏移，并限制在上下限之间。
UID_TEXT_PERCENTILE = 75
UID_TEXT_OFFSET = 40
UID_TEXT_MIN_THRESHOLD = 120
UID_TEXT_MAX_THRESHOLD = 210
# 一行/一列至少要有这么多文字像素，才算文字的一部分（滤掉噪点）。
MIN_PIXELS_PER_LINE = 2
# 列方向空隙达到这个宽度就切成两段文字（延迟指示 / UID），实测间隙 15px 以上，字内空格 7px 左右。
RUN_GAP = 12
# 条带里出现这么多纯绿打码块（RGB(0, 255, 0)）个像素，就认定这张图已经脱敏过。
GREEN_MIN_PIXELS = 400

EXPECTED_SIZE = (1280, 720)

# 平台特征（取自已确认样本）：UID 文字左边界、上边界、字高。
# CloudADB 的 UID 紧贴左边缘，ADB / Win32 的 UID 跟在 "xx ms" 之后。
CLOUDADB_MAX_LEFT = 45
# UID 文字至少要这么宽（"UID:" + 六位以上数字）；更窄的单段文字其实是延迟指示本身，
# 即这张图已经打过码、只剩延迟指示，不能当成 CloudADB。
CLOUDADB_MIN_WIDTH = 70
ADB_MIN_LEFT = 80
ADB_TEXT_MIN_HEIGHT = 10
ADB_TEXT_MAX_TOP = 697
WIN32_TEXT_MAX_HEIGHT = 9
WIN32_TEXT_MIN_TOP = 698


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


def count_redaction_green(image: np.ndarray) -> int:
    """数状态行条带里纯绿打码块（RGB(0, 255, 0)）的像素，判断这张图是否已经脱敏过。"""
    y0, y1, x0, x1 = UID_BAND
    band = image[y0:y1, x0:x1, :3]
    red, green, blue = band[:, :, 0], band[:, :, 1], band[:, :, 2]
    return int(((red <= 10) & (green >= 245) & (blue <= 10)).sum())


def find_text_runs(image: np.ndarray) -> list[tuple[int, int, int, int]]:
    """把 UID 行条带里的文字按列空隙切段，返回每段的 (x0, y0, x1, y1)，从左到右。"""
    y0, y1, x0, x1 = UID_BAND
    if image.shape[0] < y1 or image.shape[1] < x1:
        return []
    band = image[y0:y1, x0:x1, :3]
    mask = np.all(band >= uid_text_threshold(image), axis=2)
    columns = np.where(mask.sum(axis=0) >= MIN_PIXELS_PER_LINE)[0]
    if columns.size == 0:
        return []

    ranges: list[tuple[int, int]] = []
    start = previous = int(columns[0])
    for column in columns[1:]:
        column = int(column)
        if column - previous > RUN_GAP:
            ranges.append((start, previous))
            start = column
        previous = column
    ranges.append((start, previous))

    runs: list[tuple[int, int, int, int]] = []
    for start, end in ranges:
        rows = np.where(mask[:, start : end + 1].sum(axis=1) >= MIN_PIXELS_PER_LINE)[0]
        if rows.size == 0:
            continue
        runs.append((start + x0, int(rows.min()) + y0, end + x0, int(rows.max()) + y0))
    return runs


def suggest_extra_rois(box: tuple[int, int, int, int], margin: int = 4) -> list[list[int]]:
    """按文字框给出建议的额外 ROI（四周各留 margin 像素）。"""
    x0, y0, x1, y1 = box
    width, height = EXPECTED_SIZE
    left = max(0, x0 - margin)
    top = max(0, y0 - margin)
    right = min(width - 1, x1 + margin)
    bottom = min(height - 1, y1 + margin)
    return [[left, top, right - left + 1, bottom - top + 1]]


def classify(text_box: tuple[int, int, int, int], run_count: int) -> tuple[str | None, str, str]:
    """按 UID 文字范围与文字段数给出平台候选，返回 (平台, 置信度, 说明)。"""
    x0, y0, x1, y1 = text_box
    text_width = x1 - x0 + 1
    text_height = y1 - y0 + 1
    adb_like = text_height >= ADB_TEXT_MIN_HEIGHT and y0 <= ADB_TEXT_MAX_TOP
    win32_like = text_height <= WIN32_TEXT_MAX_HEIGHT and y0 >= WIN32_TEXT_MIN_TOP

    # 只有一段文字：没有延迟指示，UID 贴着左边缘，只可能是 CloudADB。
    if run_count == 1:
        if x0 <= CLOUDADB_MAX_LEFT and text_width >= CLOUDADB_MIN_WIDTH:
            return "CloudADB", "definite", "左侧没有延迟指示，UID 贴着左边缘"
        if x0 <= CLOUDADB_MAX_LEFT:
            return (
                None,
                "unknown",
                "只找到一段很短的文字且贴着左边缘，这是延迟指示本身（该图可能已打过码），不是 UID",
            )
        return (
            ("ADB" if adb_like else "Win32" if win32_like else None),
            "ambiguous",
            "只有一段文字（没有延迟指示），但 UID 位置不符合既有 CloudADB 样本",
        )

    # 两段以上：左侧是延迟指示，最右侧是 UID，按字高与上边界分档。
    if adb_like and not win32_like:
        return "ADB", "definite", "存在延迟指示，UID 字高与上边界符合 ADB"
    if win32_like and not adb_like:
        return "Win32", "definite", "存在延迟指示，UID 字高与上边界符合 Win32"
    if x0 >= ADB_MIN_LEFT:
        return "ADB", "ambiguous", "存在延迟指示，UID 字高/位置两套特征都不完全符合，按左边界倾向 ADB"
    return "Win32", "ambiguous", "存在延迟指示，UID 字高/位置两套特征都不完全符合，按左边界倾向 Win32"


def detect_platform(image: np.ndarray) -> PlatformDetection:
    """按左下状态行的文字布局判定截图平台。"""
    size = (image.shape[1], image.shape[0])
    if size != EXPECTED_SIZE:
        return PlatformDetection(
            platform=None,
            confidence="unknown",
            reason=f"截图尺寸 {size[0]}x{size[1]} 不是 {EXPECTED_SIZE[0]}x{EXPECTED_SIZE[1]}",
        )

    if count_redaction_green(image) >= GREEN_MIN_PIXELS:
        return PlatformDetection(
            platform=None,
            confidence="unknown",
            reason="左下状态行已是纯绿色打码块，这张图已经脱敏过；平台只能在原始截图上判定",
        )

    runs = find_text_runs(image)
    if not runs:
        return PlatformDetection(
            platform=None,
            confidence="unknown",
            reason="UID 行条带里没有找到文字（可能已打过码，或该界面不显示 UID）",
        )

    box = runs[-1]
    x0, y0, x1, y1 = box
    text_height = y1 - y0 + 1
    metrics = (
        f"UID 文字 x {x0}..{x1}（宽 {x1 - x0 + 1}）、y {y0}..{y1}（高 {text_height}），"
        f"状态行共 {len(runs)} 段文字"
    )

    platform, confidence, reason = classify(box, len(runs))
    if platform is None:
        # 只有像 UID 那么宽的文字才给补 ROI 的建议；又短又贴边的是延迟指示（图已打过码）。
        suggested = suggest_extra_rois(box) if (x1 - x0 + 1) >= CLOUDADB_MIN_WIDTH else []
        return PlatformDetection(
            platform=None,
            confidence="unknown",
            reason=f"{metrics}，{reason}",
            text_box=box,
            suggested_extra_rois=suggested,
        )

    roi = UID_FIXED_ROIS[platform]
    if not covers(roi, box):
        return PlatformDetection(
            platform=platform,
            confidence="ambiguous",
            reason=(
                f"{metrics}，{reason}，但 {platform} 固定 ROI {roi} 盖不住这段文字"
            ),
            text_box=box,
            suggested_extra_rois=suggest_extra_rois(box),
        )

    return PlatformDetection(platform, confidence, f"{metrics}，{reason}", box)


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
