"""校准培养总览干员头像的缩放参数（给没验证过的端用，例如 PC）。

用法（在仓库根目录）：
    uv run --group images python -m tools.Cultivation.calibrate_operator_scale <截图.png> [<截图2.png> ...]

截图要求：1280×720 原图 PNG，打开「干员 → 右上角培养总览」，左侧列表露出至少 6 张完整卡片，
鼠标不要悬停在卡片上；左下角 UID 可以打码，不影响结果。

脚本按 build_operator_templates 的同一套裁剪（脸部区域），在 0.40–0.75 之间扫缩放，
用与 MaaFramework TemplateMatch 默认相同的归一化相关系数（TM_CCOEFF_NORMED）打分：
1. 选出让可见卡片得分最高的缩放；
2. 在该缩放下列出每张命中的卡片、对应干员、分数和与第二名的差距；
3. 打印要写进 build_operator_templates.RESOURCE_SCALES 的参数。

判定通过的参考：正确干员 ≥ 0.85，且同一张卡片上第二名比第一名低 0.2 以上。
"""

from __future__ import annotations

import argparse
import json
import sys
from io import BytesIO
from pathlib import Path

import numpy as np
from PIL import Image

from tools.Cultivation.build_operator_templates import CHARICON_URL, FACE, OPERATORS, build_template
from tools.zmdmap.utils.http import get, new_client

# 左侧干员列表在 1280×720 下所在区域，留足余量以兼容不同端的布局。
PANEL = (0, 60, 560, 690)
THRESHOLD = 0.8


def gray(image: Image.Image) -> np.ndarray:
    return np.asarray(image.convert("L"), dtype=np.float64)


def ncc_map(image: np.ndarray, template: np.ndarray) -> np.ndarray:
    """TM_CCOEFF_NORMED over all valid positions, computed with FFT."""
    th, tw = template.shape
    ih, iw = image.shape
    t = template - template.mean()
    t_norm = np.sqrt((t * t).sum())
    shape = (ih + th, iw + tw)
    corr = np.fft.irfft2(np.fft.rfft2(image, shape) * np.conj(np.fft.rfft2(t, shape)), shape)[: ih - th + 1, : iw - tw + 1]
    # Local sums of the image window for the normalisation term.
    integral = np.pad(image, ((1, 0), (1, 0))).cumsum(0).cumsum(1)
    integral2 = np.pad(image * image, ((1, 0), (1, 0))).cumsum(0).cumsum(1)

    def window(table: np.ndarray) -> np.ndarray:
        return table[th:, tw:] - table[:-th, tw:] - table[th:, :-tw] + table[:-th, :-tw]

    s, s2 = window(integral), window(integral2)
    variance = np.maximum(s2 - s * s / (th * tw), 1e-9)
    return corr / (np.sqrt(variance) * t_norm + 1e-9)


def download_sources(char_ids: list[str]) -> dict[str, Image.Image]:
    sources = {}
    with new_client() as client:
        for char_id in char_ids:
            response = get(client, f"{CHARICON_URL}/icon_{char_id}.png@raw")
            response.raise_for_status()
            with Image.open(BytesIO(response.content)) as source:
                source.load()
                sources[char_id] = source.copy()
    return sources


def evaluate(panels: list[np.ndarray], sources: dict[str, Image.Image], scale: float):
    """Best score and position of every operator in every screenshot at one scale."""
    hits = []
    for index, panel in enumerate(panels):
        for char_id, source in sources.items():
            scores = ncc_map(panel, gray(build_template(source, scale)))
            y, x = np.unravel_index(int(scores.argmax()), scores.shape)
            hits.append((float(scores[y, x]), index, char_id, int(x) + PANEL[0], int(y) + PANEL[1]))
    return hits


def main() -> None:
    # Windows 控制台默认不是 UTF-8，中文干员名会乱码。
    sys.stdout.reconfigure(encoding="utf-8")
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("screenshots", nargs="+", type=Path)
    parser.add_argument("--min-scale", type=float, default=0.40)
    parser.add_argument("--max-scale", type=float, default=0.75)
    args = parser.parse_args()

    operators = json.loads(OPERATORS.read_text(encoding="utf-8"))["operators"]
    panels = []
    for path in args.screenshots:
        with Image.open(path) as image:
            if image.size != (1280, 720):
                raise SystemExit(f"{path}: expected 1280x720, got {image.size}")
            panels.append(gray(image.crop(PANEL)))
    print(f"下载 {len(operators)} 张 CharIcon 原图……")
    sources = download_sources(list(operators))

    best_scale, best_value = None, -1.0
    for scale in np.arange(args.min_scale, args.max_scale + 1e-9, 0.01):
        hits = sorted(evaluate(panels, sources, float(scale)), reverse=True)
        top = [score for score, *_ in hits[:6]]
        value = sum(top) / len(top)
        print(f"缩放 {scale:.2f}：前 6 名平均 {value:.3f}")
        if value > best_value:
            best_scale, best_value = float(scale), value

    print(f"\n最佳缩放 {best_scale:.2f}（裁剪 {FACE}）")
    hits = evaluate(panels, sources, best_scale)
    for index, path in enumerate(args.screenshots):
        print(f"\n{path.name}:")
        found = sorted((h for h in hits if h[1] == index and h[0] >= THRESHOLD), reverse=True)
        for score, _, char_id, x, y in found:
            # Second best operator claiming the same card position.
            rivals = [h[0] for h in hits if h[1] == index and h[2] != char_id and abs(h[3] - x) < 30 and abs(h[4] - y) < 30]
            runner = max(rivals, default=0.0)
            verdict = "通过" if score >= 0.85 and score - runner >= 0.2 else "需人工确认"
            name = operators[char_id]["names"]["zh_cn"]
            print(f"  {name:6s} {char_id:22s} ({x},{y}) 分数 {score:.3f}  同位第二名 {runner:.3f}  {verdict}")
        if not found:
            print("  没有卡片达到阈值，检查截图是否为培养总览、分辨率是否 1280×720")
    print(f'\n把 tools/Cultivation/build_operator_templates.py 里 RESOURCE_SCALES 的对应端改成 {best_scale:.2f}，')
    print("再运行 uv run python -m tools.Cultivation.build_operator_templates --rebuild resource 重新生成该端模板。")


if __name__ == "__main__":
    main()
