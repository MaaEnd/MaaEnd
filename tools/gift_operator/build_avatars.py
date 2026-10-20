"""由 zmdmap CharIcon 原图生成缺失的赠礼头像；任务生成器通过 stdin 传入增量。"""

from __future__ import annotations

import json
import sys
from io import BytesIO
from pathlib import Path

from PIL import Image, ImageDraw

from tools.zmdmap.utils.http import get, new_client

REPO_ROOT = Path(__file__).resolve().parents[2]
CHARICON_URL = "https://voice.fz.wiki/assets/beyond/dynamicassets/gameplay/ui/sprites/charicon"
BACKGROUND = (230, 230, 230)
# PC 模板坐标下的半开区域：信息/礼物图标、右上角状态图标、上/左/右边框。
MASKS = ((0, 0, 19, 35), (53, 0, 80, 29), (0, 0, 80, 2), (0, 0, 2, 72), (78, 0, 80, 72))
RESOURCE_FACTORS = {"resource": 1.0, "resource_adb": 1.25}


def build_avatar(source: Image.Image, factor: float) -> Image.Image:
    background = Image.new("RGBA", source.size, (*BACKGROUND, 255))
    background.alpha_composite(source.convert("RGBA"))
    scale = 0.55 * factor
    offset_x = -factor
    template = background.convert("RGB").transform(
        (round(80 * factor), round(72 * factor)),
        Image.Transform.AFFINE,
        (1 / scale, 0, -offset_x / scale, 0, 1 / scale, 0),
        resample=Image.Resampling.BILINEAR,
        fillcolor=BACKGROUND,
    )
    draw = ImageDraw.Draw(template)
    for box in MASKS:
        left, top, right, bottom = (round(value * factor) for value in box)
        draw.rectangle((left, top, right - 1, bottom - 1), fill=(0, 255, 0))
    return template


def main() -> None:
    jobs = json.load(sys.stdin)
    outputs = []
    with new_client() as client:
        for job in jobs:
            response = get(client, f"{CHARICON_URL}/icon_{job['id']}.png@raw")
            response.raise_for_status()
            with Image.open(BytesIO(response.content)) as source:
                # 固定裁剪只适用于这一规格；源图改变时必须重新验证几何参数。
                if source.format != "PNG" or source.size != (152, 212):
                    raise ValueError(
                        f"{job['id']}: expected a 152x212 PNG, "
                        f"got {source.format} {source.size}"
                    )
                for resource in job["resources"]:
                    path = REPO_ROOT / "assets" / resource / "image" / job["template"]
                    outputs.append((path, build_avatar(source, RESOURCE_FACTORS[resource])))
    for path, template in outputs:
        path.parent.mkdir(parents=True, exist_ok=True)
        # 即使在下载期间有人补入了模板，也不覆盖已有文件。
        if not path.exists():
            with path.open("xb") as output:
                template.save(output, format="PNG")
            print(f"[GiftOperator] {path.relative_to(REPO_ROOT)}")


if __name__ == "__main__":
    main()
