"""把 IconRecognition 的物品图标加工成任务前端的展示图标（16×16、带品质遮罩）。

任务前端的物品选项通过 option case 的 `icon` 字段，或 label 里的行内 Markdown
（`![](resource/image/icon/<iconId>.png) 名称 ★5`）引用展示图标。本脚本从识别素材
`assets/resource/image/IconRecognition/<稀有度>/<iconId>.png` 拷贝一份，叠上对应品质的
遮罩后缩放到 16×16 输出到 `assets/resource/image/icon/<iconId>.png`；识别素材本身不改动。

遮罩形变是预期的：遮罩先等比缩放到与源图同宽，底部对齐，再垂直向上拉伸到同高，
因此整张图都带上「越靠下越浓」的品质底色，最底部是实色条。

用法::

    python tools/icon_recognition/display_icons.py            # 生成 / 更新被引用的展示图标
    python tools/icon_recognition/display_icons.py --check     # 只校验，不写文件
    python tools/icon_recognition/display_icons.py --dry-run   # 只列出将要处理的内容

新增物品时的流程：先在任务 / interface 里引用图标（`icon` 字段或 label 富文本），
再运行一次本脚本；图标名固定为 `iconId`，与任务里的引用路径一致。
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Iterable, Mapping

from PIL import Image

REPO_ROOT = Path(__file__).resolve().parents[2]
CATALOG_PATH = REPO_ROOT / "assets/data/IconRecognition/recognition_items.json"
IMAGE_ROOT = REPO_ROOT / "assets/resource/image/IconRecognition"
TASKS_ROOT = REPO_ROOT / "assets/tasks"
INTERFACE_PATH = REPO_ROOT / "assets/interface.json"
OUTPUT_ROOT = REPO_ROOT / "assets/resource/image/icon"
MASKS_ROOT = Path(__file__).resolve().parent / "rarity_masks"

DISPLAY_SIZE = 16

# 品质遮罩：风格与游戏内品质色一致，文件名对应 rarity 1..6
RARITY_MASK_FILES: dict[int, str] = {
    1: "Gray.png",
    2: "Green.png",
    3: "Blue.png",
    4: "Purple.png",
    5: "Gold.png",
    6: "Orange.png",
}

# 展示图标的引用形式：icon 字段与 label 富文本都长这样
DISPLAY_ICON_REFERENCE = re.compile(r"resource/image/icon/([A-Za-z0-9_]+)\.png")


def display_path(path: Path) -> str:
    """尽量用仓库相对路径输出，便于日志与 CI 对照。"""
    try:
        return str(path.relative_to(REPO_ROOT))
    except ValueError:
        return str(path)


def load_catalog(path: Path = CATALOG_PATH) -> Mapping[str, dict]:
    """读取 IconRecognition 物品目录。"""
    return json.loads(path.read_text(encoding="utf-8"))


def build_icon_index(catalog: Mapping[str, dict]) -> dict[str, dict]:
    """构造 `iconId -> {rarity, item_id}` 索引。

    同一个 iconId 只能对应一个稀有度，否则遮罩无法确定：这类数据变化直接报错，
    由维护者确认后再调整。共享同一图标的别名物品（同 iconId 同稀有度）共用一张展示图标。
    """
    index: dict[str, dict] = {}
    for item_id, record in catalog.items():
        icon_id = record.get("iconId")
        rarity = record.get("rarity")
        if not icon_id:
            continue
        if not isinstance(rarity, int) or isinstance(rarity, bool) or rarity not in RARITY_MASK_FILES:
            raise ValueError(f"{item_id}.rarity 必须位于 {sorted(RARITY_MASK_FILES)}，实际为 {rarity!r}")
        previous = index.get(icon_id)
        if previous is None:
            index[icon_id] = {"rarity": rarity, "item_id": item_id}
        elif previous["rarity"] != rarity:
            raise ValueError(
                f"iconId {icon_id} 同时对应稀有度 {previous['rarity']}（{previous['item_id']}）"
                f"与 {rarity}（{item_id}），无法确定遮罩",
            )
    return index


def collect_referenced_icon_ids(roots: Iterable[Path] | None = None) -> dict[str, set[str]]:
    """扫描任务与 interface，收集被引用的 iconId 及其来源文件。"""
    scan_roots = [TASKS_ROOT, INTERFACE_PATH] if roots is None else list(roots)
    references: dict[str, set[str]] = {}
    for root in scan_roots:
        paths = sorted(root.rglob("*.json")) if root.is_dir() else [root]
        for path in paths:
            if not path.is_file():
                continue
            text = path.read_text(encoding="utf-8")
            for icon_id in DISPLAY_ICON_REFERENCE.findall(text):
                references.setdefault(icon_id, set()).add(display_path(path))
    return references


def source_icon_path(icon_id: str, rarity: int, image_root: Path = IMAGE_ROOT) -> Path:
    return image_root / str(rarity) / f"{icon_id}.png"


def mask_path(rarity: int, masks_root: Path = MASKS_ROOT) -> Path:
    return masks_root / RARITY_MASK_FILES[rarity]


def render_display_icon(source: Path, mask: Path, size: int = DISPLAY_SIZE) -> Image.Image:
    """合成展示图标：遮罩等比缩放到同宽 → 底部对齐 → 垂直向上拉伸 → 整体缩到 size×size。"""
    icon = Image.open(source).convert("RGBA")
    mask_image = Image.open(mask).convert("RGBA")
    width, height = icon.size

    scaled_height = max(1, round(mask_image.height * width / mask_image.width))
    scaled = mask_image.resize((width, scaled_height), Image.Resampling.LANCZOS)
    stretched = scaled.resize((width, height), Image.Resampling.LANCZOS)

    return Image.alpha_composite(icon, stretched).resize((size, size), Image.Resampling.LANCZOS)


def write_png(image: Image.Image, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    image.save(destination, format="PNG", optimize=True)


def images_equal(left: Image.Image, right: Image.Image) -> bool:
    if left.size != right.size or left.mode != right.mode:
        return False
    return left.tobytes() == right.tobytes()


def plan(
    catalog: Mapping[str, dict],
    references: Mapping[str, set[str]],
    image_root: Path = IMAGE_ROOT,
    masks_root: Path = MASKS_ROOT,
) -> tuple[dict[str, dict], list[str]]:
    """把引用整理成待生成列表，并返回无法处理的问题清单。"""
    index = build_icon_index(catalog)
    jobs: dict[str, dict] = {}
    problems: list[str] = []
    for icon_id, files in sorted(references.items()):
        record = index.get(icon_id)
        if record is None:
            problems.append(f"iconId {icon_id} 不在 IconRecognition 目录中（引用方：{', '.join(sorted(files))}）")
            continue
        source = source_icon_path(icon_id, record["rarity"], image_root)
        if not source.is_file():
            problems.append(f"iconId {icon_id} 缺少识别素材 {source}")
            continue
        mask = mask_path(record["rarity"], masks_root)
        if not mask.is_file():
            problems.append(f"稀有度 {record['rarity']} 缺少遮罩 {mask}")
            continue
        jobs[icon_id] = {"rarity": record["rarity"], "source": source, "mask": mask, "files": files}
    return jobs, problems


def generate(
    jobs: Mapping[str, dict],
    output_root: Path = OUTPUT_ROOT,
    size: int = DISPLAY_SIZE,
    dry_run: bool = False,
) -> list[str]:
    """生成缺失或已过期的展示图标，返回实际写入的文件列表。"""
    written: list[str] = []
    for icon_id, job in sorted(jobs.items()):
        destination = output_root / f"{icon_id}.png"
        image = render_display_icon(job["source"], job["mask"], size=size)
        if destination.is_file():
            with Image.open(destination) as existing:
                if images_equal(existing.convert("RGBA"), image):
                    continue
        if not dry_run:
            write_png(image, destination)
        written.append(display_path(destination))
    return written


def check(
    jobs: Mapping[str, dict],
    output_root: Path = OUTPUT_ROOT,
    size: int = DISPLAY_SIZE,
) -> list[str]:
    """校验展示图标存在且与当前素材、遮罩一致。"""
    problems: list[str] = []
    for icon_id, job in sorted(jobs.items()):
        destination = output_root / f"{icon_id}.png"
        if not destination.is_file():
            problems.append(f"缺少展示图标 {display_path(destination)}")
            continue
        expected = render_display_icon(job["source"], job["mask"], size=size)
        with Image.open(destination) as existing:
            if not images_equal(existing.convert("RGBA"), expected):
                problems.append(
                    f"{display_path(destination)} 与素材 / 遮罩不一致（重新运行本脚本即可更新）",
                )
    return problems


def parse_args(argv: Iterable[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="生成任务前端 16×16 展示图标（带品质遮罩）")
    parser.add_argument("--check", action="store_true", help="只校验展示图标是否存在且与素材一致")
    parser.add_argument("--dry-run", action="store_true", help="只列出将要更新的图标，不写文件")
    parser.add_argument("--catalog", type=Path, default=CATALOG_PATH, help="IconRecognition 目录 JSON")
    parser.add_argument(
        "--tasks-root",
        type=Path,
        default=None,
        help="只扫描该目录（默认扫描 assets/tasks 与 assets/interface.json）",
    )
    parser.add_argument("--output-root", type=Path, default=OUTPUT_ROOT, help="展示图标输出目录")
    parser.add_argument("--masks-root", type=Path, default=MASKS_ROOT, help="品质遮罩目录")
    parser.add_argument("--image-root", type=Path, default=IMAGE_ROOT, help="识别素材目录")
    parser.add_argument("--size", type=int, default=DISPLAY_SIZE, help="输出边长（前端为 16）")
    return parser.parse_args(argv)


def main(argv: Iterable[str] | None = None) -> int:
    args = parse_args(argv)

    catalog = load_catalog(args.catalog)
    references = collect_referenced_icon_ids(
        None if args.tasks_root is None else [args.tasks_root],
    )
    jobs, problems = plan(catalog, references, image_root=args.image_root, masks_root=args.masks_root)

    if problems:
        for problem in problems:
            print(f"[错误] {problem}", file=sys.stderr)
        return 1

    print(f"被引用的展示图标：{len(jobs)} 张（分布在 {sum(len(job['files']) for job in jobs.values())} 个引用文件上）")

    if args.check:
        stale = check(jobs, output_root=args.output_root, size=args.size)
        for problem in stale:
            print(f"[错误] {problem}", file=sys.stderr)
        if stale:
            return 1
        print("校验通过：展示图标与素材、遮罩一致")
        return 0

    written = generate(jobs, output_root=args.output_root, size=args.size, dry_run=args.dry_run)
    if args.dry_run:
        for path in written:
            print(f"[待更新] {path}")
        print(f"待更新 {len(written)} 张（--dry-run，未写文件）")
        return 0

    for path in written:
        print(f"[已更新] {path}")
    print(f"更新 {len(written)} 张，其余 {len(jobs) - len(written)} 张无需改动")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
