"""由 zmdmap CharIcon 原图生成培养总览干员卡片的头像模板（只补缺失，不覆盖已有文件）。

培养总览左侧卡片与赠礼联络台使用同一张 CharIcon 原图，只是缩放和裁剪不同。
所有干员共用一组固定参数：原图缩放 0.67 后取脸部区域（高 22%–62%、宽 20%–80%），
避开卡片左上职业图标、右上「当前/前瞻」角标和底部等级条。

参数在云终末地 1280×720 截图上验证：9 张可见卡片中 8 张命中正确干员（0.93–0.97，
与第二名差距 ≥ 0.29），未发布的「前瞻」干员没有原图，最高 0.50，不会误命中。

运行：uv run python -m tools.Cultivation.build_operator_templates
"""

from __future__ import annotations

import json
from io import BytesIO
from pathlib import Path

from PIL import Image

from tools.zmdmap.utils.http import get, new_client

REPO_ROOT = Path(__file__).resolve().parents[2]
CHARICON_URL = "https://voice.fz.wiki/assets/beyond/dynamicassets/gameplay/ui/sprites/charicon"
OPERATORS = REPO_ROOT / "tools" / "zmdmap" / "data" / "sell_product.json"
BACKGROUND = (190, 190, 190)
# 移动端（ADB / 云终末地）0.67 已用截图验证。移动端 UI 是 PC 的 1.25 倍，PC 按
# 0.67 / 1.25 ≈ 0.536 生成，尚无 PC 截图验证；任务目前只对 CloudADB 开放。
RESOURCE_SCALES = {"resource": 0.67 / 1.25, "resource_adb": 0.67}
FACE = (0.20, 0.22, 0.80, 0.62)


def template_path(resource: str, char_id: str) -> Path:
    return REPO_ROOT / "assets" / resource / "image" / "Cultivation" / "Operators" / f"{char_id}.png"


def build_template(source: Image.Image, scale: float) -> Image.Image:
    background = Image.new("RGBA", source.size, (*BACKGROUND, 255))
    background.alpha_composite(source.convert("RGBA"))
    width, height = round(source.width * scale), round(source.height * scale)
    scaled = background.convert("RGB").resize((width, height), Image.Resampling.BOX)
    left, top, right, bottom = FACE
    return scaled.crop((int(width * left), int(height * top), int(width * right), int(height * bottom)))


def main() -> None:
    operators = json.loads(OPERATORS.read_text(encoding="utf-8"))["operators"]
    jobs = [
        (char_id, resource)
        for char_id in operators
        for resource in RESOURCE_SCALES
        if not template_path(resource, char_id).exists()
    ]
    outputs = []
    with new_client() as client:
        for char_id in sorted({char_id for char_id, _ in jobs}):
            response = get(client, f"{CHARICON_URL}/icon_{char_id}.png@raw")
            response.raise_for_status()
            with Image.open(BytesIO(response.content)) as source:
                # 固定裁剪只适用于这一规格；源图改变时必须重新验证几何参数。
                if source.format != "PNG" or source.size != (152, 212):
                    raise ValueError(f"{char_id}: expected a 152x212 PNG, got {source.format} {source.size}")
                source.load()
                for job_id, resource in jobs:
                    if job_id == char_id:
                        outputs.append((template_path(resource, char_id), build_template(source, RESOURCE_SCALES[resource])))
    # 先全部下载、解码成功再写文件，避免中途失败留下半套模板。
    for path, template in outputs:
        path.parent.mkdir(parents=True, exist_ok=True)
        if not path.exists():
            with path.open("xb") as output:
                template.save(output, format="PNG")
            print(f"[Cultivation] {path.relative_to(REPO_ROOT)}")


if __name__ == "__main__":
    main()
