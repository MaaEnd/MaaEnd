#!/usr/bin/env python3
"""
给 IMS 物品模板图左上角涂绿，供 green_mask 模板匹配跳过协议空间奖励角标。

默认将 assets/resource/image/IMS/item/*_TEMPLATE.png
左上角 31×18 矩形涂为 RGB(0, 255, 0)。
"""
from __future__ import annotations

from argparse import ArgumentParser
from pathlib import Path

from PIL import Image, ImageDraw

PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent
DEFAULT_DIRECTORY = (
    PROJECT_ROOT / "assets" / "resource" / "image" / "IMS" / "item"
)
DEFAULT_COLOR = (0, 255, 0)
# 半开区间 [left, top, right, bottom)：宽 31、高 18
DEFAULT_BOX = (0, 0, 31, 18)
TEMPLATE_GLOB = "*_TEMPLATE.png"


def build_parser() -> ArgumentParser:
    parser = ArgumentParser(
        description=(
            "Paint a green rectangle on the top-left of IMS item template PNGs "
            "so TemplateMatch green_mask can ignore Protocol Space reward badges."
        )
    )
    parser.add_argument(
        "--directory",
        type=Path,
        default=DEFAULT_DIRECTORY,
        help=f"Directory of *_TEMPLATE.png. Default: {DEFAULT_DIRECTORY}",
    )
    parser.add_argument(
        "--width",
        type=int,
        default=DEFAULT_BOX[2] - DEFAULT_BOX[0],
        help="Green box width in pixels. Default: 31",
    )
    parser.add_argument(
        "--height",
        type=int,
        default=DEFAULT_BOX[3] - DEFAULT_BOX[1],
        help="Green box height in pixels. Default: 18",
    )
    parser.add_argument(
        "--color",
        nargs=3,
        type=int,
        metavar=("R", "G", "B"),
        default=list(DEFAULT_COLOR),
        help="Fill color as R G B. Default: 0 255 0",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Only print files that would be modified",
    )
    return parser


def validate_color(color: tuple[int, int, int]) -> tuple[int, int, int]:
    if any(channel < 0 or channel > 255 for channel in color):
        raise ValueError("Color channels must all be in the range 0..255.")
    return color


def validate_size(width: int, height: int) -> tuple[int, int]:
    if width <= 0 or height <= 0:
        raise ValueError("width and height must be positive.")
    return width, height


def paint_top_left(
    png_path: Path,
    width: int,
    height: int,
    color: tuple[int, int, int],
) -> None:
    with Image.open(png_path) as img:
        image_width, image_height = img.size
        if image_width < width or image_height < height:
            raise ValueError(
                f"{png_path.name}: image {image_width}x{image_height} "
                f"is smaller than green box {width}x{height}"
            )

        original_mode = img.mode
        if original_mode not in {"RGB", "RGBA"}:
            img = img.convert("RGBA")
            original_mode = "RGBA"
        else:
            img = img.copy()

        draw = ImageDraw.Draw(img)
        fill_color = color if original_mode == "RGB" else (*color, 255)
        # Pillow rectangle is inclusive on the bottom-right pixel.
        draw.rectangle((0, 0, width - 1, height - 1), fill=fill_color)
        img.save(png_path)


def collect_templates(directory: Path) -> list[Path]:
    return sorted(directory.glob(TEMPLATE_GLOB))


def main() -> int:
    args = build_parser().parse_args()
    color = validate_color(tuple(args.color))
    width, height = validate_size(args.width, args.height)
    directory = args.directory.resolve()

    if not directory.is_dir():
        raise FileNotFoundError(f"Directory does not exist: {directory}")

    png_paths = collect_templates(directory)
    if not png_paths:
        print(f"No {TEMPLATE_GLOB} files found in {directory}")
        return 0

    print(f"directory={directory}")
    print(f"green_box=[{0}, {0}, {width}, {height}] color={color}")

    for png_path in png_paths:
        print(f"{'DRY-RUN' if args.dry_run else 'PROCESS'} {png_path.name}")
        if not args.dry_run:
            paint_top_left(png_path, width, height, color)

    print(
        f"{'DRY-RUN COMPLETE' if args.dry_run else 'DONE'}: "
        f"{len(png_paths)} PNG files"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
