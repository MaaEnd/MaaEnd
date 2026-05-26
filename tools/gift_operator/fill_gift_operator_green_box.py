#!/usr/bin/env python3
"""
这个工具用于将赠送礼物的干员头像的礼物图标区域涂成绿色
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
TARGETS = (
    ("resource", RESOURCE_DIRECTORY, (0, 16, 18, 35)),
    ("adb", ADB_DIRECTORY, (0, 20, 20, 40)),
)


def build_parser() -> ArgumentParser:
    parser = ArgumentParser(
        description="Fill hardcoded GiftOperator PNG regions with green."
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


def paint_png(
    png_path: Path, box: tuple[int, int, int, int], color: tuple[int, int, int]
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

    for name, directory, box in targets:
        png_paths = collect_pngs(directory)
        if not png_paths:
            print(f"SKIP {name}: no PNG files found in {directory}")
            continue

        for png_path in png_paths:
            print(
                f"{'DRY-RUN' if args.dry_run else 'PROCESS'} "
                f"[{name}] box={box}: {png_path}"
            )
            if not args.dry_run:
                paint_png(png_path, box, color)

        total_png_count += len(png_paths)

    if total_png_count == 0:
        print("No PNG files found.")
        return 0

    print(f"{'DRY-RUN COMPLETE' if args.dry_run else 'DONE'}: {total_png_count} PNG files, color={color}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
