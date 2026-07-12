#!/usr/bin/env python3
"""
将赠送礼物干员头像模板格式化为 green_mask 可用形态：
- 上、左、右三边各 2px 绿色描边
- 右上角绿色遮罩，尺寸为左侧靠上礼物图标参考区域的 1.5 倍
"""
from __future__ import annotations

from argparse import ArgumentParser
from pathlib import Path

from PIL import Image, ImageDraw

PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent
DEFAULT_COLOR = (0, 255, 0)
RESOURCE_DIRECTORY = (
    PROJECT_ROOT / "assets" / "resource" / "image" / "GiftOperator" / "Operators"
)
ADB_DIRECTORY = (
    PROJECT_ROOT / "assets" / "resource_adb" / "image" / "GiftOperator" / "Operators"
)
# 左侧靠上礼物图标参考区域（半开区间 left, top, right, bottom）
TARGETS = (
    ("win32", RESOURCE_DIRECTORY, (0, 16, 18, 35)),
    ("adb", ADB_DIRECTORY, (0, 20, 20, 40)),
)
BORDER_WIDTH = 2
MASK_SCALE = 1.5


def build_parser() -> ArgumentParser:
    parser = ArgumentParser(
        description="Format GiftOperator operator PNGs for green_mask template matching."
    )
    parser.add_argument(
        "--color",
        nargs=3,
        type=int,
        metavar=("R", "G", "B"),
        default=DEFAULT_COLOR,
        help="Fill color as R G B. Default: 0 255 0",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Only print files that would be modified",
    )
    return parser


def validate_targets() -> tuple[tuple[str, Path, tuple[int, int, int, int]], ...]:
    missing_directories = [directory for _, directory, _ in TARGETS if not directory.is_dir()]
    if missing_directories:
        missing_text = "\n".join(f" - {directory}" for directory in missing_directories)
        raise FileNotFoundError(f"These directories do not exist:\n{missing_text}")

    validated_targets: list[tuple[str, Path, tuple[int, int, int, int]]] = []
    for name, directory, box in TARGETS:
        validated_targets.append((name, directory, validate_box(box)))
    return tuple(validated_targets)


def validate_box(box: tuple[int, int, int, int]) -> tuple[int, int, int, int]:
    left, top, right, bottom = box
    if left < 0 or top < 0 or right <= left or bottom <= top:
        raise ValueError(
            "Invalid box. Expected left >= 0, top >= 0, right > left, bottom > top."
        )
    return box


def validate_color(color: tuple[int, int, int]) -> tuple[int, int, int]:
    if any(channel < 0 or channel > 255 for channel in color):
        raise ValueError("Color channels must all be in the range 0..255.")
    return color


def box_size(box: tuple[int, int, int, int]) -> tuple[int, int]:
    left, top, right, bottom = box
    return right - left, bottom - top


def scaled_size(width: int, height: int, scale: float) -> tuple[int, int]:
    return round(width * scale), round(height * scale)


def compute_paint_regions(
    image_size: tuple[int, int],
    reference_box: tuple[int, int, int, int],
    *,
    border_width: int = BORDER_WIDTH,
    mask_scale: float = MASK_SCALE,
) -> tuple[tuple[int, int, int, int], ...]:
    image_width, image_height = image_size
    ref_width, ref_height = box_size(reference_box)
    mask_width, mask_height = scaled_size(ref_width, ref_height, mask_scale)

    top_border = (0, 0, image_width, border_width)
    left_border = (0, 0, border_width, image_height)
    right_border = (image_width - border_width, 0, image_width, image_height)
    top_right_mask = (
        image_width - mask_width,
        0,
        image_width,
        mask_height,
    )

    return (top_border, left_border, right_border, top_right_mask)


def paint_png(
    png_path: Path,
    reference_box: tuple[int, int, int, int],
    color: tuple[int, int, int],
) -> bool:
    with Image.open(png_path) as img:
        original_mode = img.mode

        if original_mode not in {"RGB", "RGBA"}:
            img = img.convert("RGBA")
            original_mode = "RGBA"
        else:
            img = img.copy()

        draw = ImageDraw.Draw(img)
        fill_color = color if original_mode == "RGB" else (*color, 255)
        for box in compute_paint_regions(img.size, reference_box):
            # Pillow includes the bottom-right pixel, so subtract 1 to match a half-open box.
            draw.rectangle((box[0], box[1], box[2] - 1, box[3] - 1), fill=fill_color)
        img.save(png_path)

    return True


def collect_pngs(directory: Path) -> list[Path]:
    return sorted(directory.rglob("*.png"))


def main() -> int:
    args = build_parser().parse_args()
    color = validate_color(tuple(args.color))
    targets = validate_targets()
    total_png_count = 0

    for name, directory, reference_box in targets:
        png_paths = collect_pngs(directory)
        if not png_paths:
            print(f"SKIP {name}: no PNG files found in {directory}")
            continue

        with Image.open(png_paths[0]) as sample_img:
            sample_regions = compute_paint_regions(sample_img.size, reference_box)
        print(f"[{name}] reference_box={reference_box} regions={sample_regions}")

        for png_path in png_paths:
            print(
                f"{'DRY-RUN' if args.dry_run else 'PROCESS'} "
                f"[{name}] {png_path}"
            )
            if not args.dry_run:
                paint_png(png_path, reference_box, color)

        total_png_count += len(png_paths)

    if total_png_count == 0:
        print("No PNG files found.")
        return 0

    print(
        f"{'DRY-RUN COMPLETE' if args.dry_run else 'DONE'}: "
        f"{total_png_count} PNG files, color={color}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
