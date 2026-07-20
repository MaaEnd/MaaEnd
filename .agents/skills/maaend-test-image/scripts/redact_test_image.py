# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "maafw>=5.9.0",
#     "numpy>=2.0",
#     "opencv-python-headless>=4.10",
#     "pillow>=11",
# ]
# ///

"""Import one MaaEnd test screenshot after redacting private information."""

from __future__ import annotations

import argparse
from pathlib import Path
import sys
import uuid

import cv2
from maa.controller import CustomController
from maa.pipeline import JOCR
from maa.resource import Resource
from maa.tasker import Tasker
from maa.toolkit import Toolkit
import numpy as np
from PIL import Image as PILImage


SUPPORTED_CONTROLLERS = {"adb": "ADB", "win32": "Win32"}
FIXED_ROIS = {
    "ADB": [84, 690, 114, 18],
    "Win32": [70, 696, 90, 13],
}
EXPECTED_SIZE = (1280, 720)


class OfflineOcrController(CustomController):
    """Provide the no-op controller required by MaaFramework offline OCR."""

    def connect(self) -> bool:
        return True

    def request_uuid(self) -> str:
        return "maaend-test-image-offline-ocr"

    def start_app(self, intent: str) -> bool:
        return True

    def stop_app(self, intent: str) -> bool:
        return True

    def screencap(self) -> np.ndarray:
        return np.zeros((1, 1, 3), dtype=np.uint8)

    def click(self, x: int, y: int) -> bool:
        return True

    def swipe(self, x1: int, y1: int, x2: int, y2: int, duration: int) -> bool:
        return True

    def touch_down(self, contact: int, x: int, y: int, pressure: int) -> bool:
        return True

    def touch_move(self, contact: int, x: int, y: int, pressure: int) -> bool:
        return True

    def touch_up(self, contact: int) -> bool:
        return True

    def click_key(self, keycode: int) -> bool:
        return True

    def input_text(self, text: str) -> bool:
        return True

    def key_down(self, keycode: int) -> bool:
        return True

    def key_up(self, keycode: int) -> bool:
        return True

    def scroll(self, dx: int, dy: int) -> bool:
        return True

    def get_custom_info(self) -> dict[str, str]:
        return {"mode": "offline-ocr", "platform": sys.platform}


def find_repo_root(explicit: Path | None) -> Path:
    """Find a MaaEnd checkout containing the in-repository test set."""
    if explicit is not None:
        candidates = [explicit]
    else:
        script_repo = Path(__file__).resolve().parents[4]
        cwd = Path.cwd().resolve()
        candidates = [cwd, *cwd.parents, script_repo]

    for candidate in candidates:
        root = candidate.expanduser().resolve()
        markers = (
            root / "maatools.config.mts",
            root / "tests" / "MaaEndTestset",
            root / "assets" / "resource" / "model" / "ocr",
        )
        if all(marker.exists() for marker in markers):
            return root
    raise SystemExit("找不到 MaaEnd 根目录；请在仓库内运行，或使用 --repo 指定。")


def parse_roi(raw: str) -> list[int]:
    """Parse x,y,w,h into a validated ROI."""
    try:
        values = [int(part.strip()) for part in raw.split(",")]
    except ValueError as exc:
        raise argparse.ArgumentTypeError("ROI 必须是整数 x,y,w,h") from exc
    if len(values) != 4 or any(value < 0 for value in values):
        raise argparse.ArgumentTypeError("ROI 必须是四个非负整数 x,y,w,h")
    if values[2] == 0 or values[3] == 0:
        raise argparse.ArgumentTypeError("ROI 的宽和高必须大于 0")
    return values


def controller_from_destination(repo: Path, destination: Path) -> str:
    """Validate the MaaEnd test-set path and infer its controller."""
    testset = (repo / "tests" / "MaaEndTestset").resolve()
    try:
        relative = destination.resolve().relative_to(testset)
    except ValueError as exc:
        raise SystemExit("目标图片必须位于 tests/MaaEndTestset/ 内。") from exc

    if len(relative.parts) != 3:
        raise SystemExit(
            "目标图片必须使用 tests/MaaEndTestset/<controller>/<resource>/<文件名>.png。"
        )
    controller = SUPPORTED_CONTROLLERS.get(relative.parts[0].casefold())
    if controller is None:
        raise SystemExit("目标图片必须位于 ADB/ 或 Win32/ 目录中。")
    return controller


def read_image(path: Path) -> np.ndarray:
    """Read an image from a path that may contain non-ASCII characters."""
    image_bytes = np.fromfile(path, dtype=np.uint8)
    image = cv2.imdecode(image_bytes, cv2.IMREAD_COLOR)
    if image is None:
        raise SystemExit(f"无法读取图片：{path}")
    return image


def validate_image_and_rois(image: np.ndarray, rois: list[list[int]]) -> None:
    """Require MaaEnd's 720p baseline and in-bounds redaction rectangles."""
    height, width = image.shape[:2]
    if (width, height) != EXPECTED_SIZE:
        raise SystemExit(
            f"截图必须为 {EXPECTED_SIZE[0]}x{EXPECTED_SIZE[1]}，实际为 {width}x{height}。"
        )
    for x, y, roi_width, roi_height in rois:
        if x + roi_width > width or y + roi_height > height:
            raise SystemExit(
                "打码 ROI 超出图片范围："
                f"[{x}, {y}, {roi_width}, {roi_height}]，图片尺寸为 {width}x{height}。"
            )


def fill_roi(image: np.ndarray, roi: list[int]) -> None:
    """Fill an ROI with pure green in OpenCV BGR order."""
    x, y, width, height = roi
    image[y : y + height, x : x + width] = [0, 255, 0]


def build_tasker(repo: Path) -> tuple[Tasker, Resource, OfflineOcrController]:
    """Create an offline OCR tasker backed by MaaEnd's bundled OCR model."""
    runtime_path = repo / ".cache" / "maaend-test-image"
    runtime_path.mkdir(parents=True, exist_ok=True)
    Toolkit.init_option(runtime_path)

    controller = OfflineOcrController()
    controller.post_connection().wait()

    resource = Resource()
    resource.post_bundle(repo / "assets" / "resource").wait()
    tasker = Tasker()
    if not tasker.bind(resource, controller) or not tasker.inited:
        raise SystemExit("MaaFramework OCR 初始化失败。")
    return tasker, resource, controller


def find_private_rois(tasker: Tasker, image: np.ndarray) -> list[list[int]]:
    """Return OCR boxes containing UID markers."""
    detail = tasker.post_recognition("OCR", JOCR(expected=["UID", "#"]), image).wait().get()
    nodes = getattr(detail, "nodes", [])
    if not nodes:
        return []
    recognition = getattr(nodes[0], "recognition", None)
    results = getattr(recognition, "filtered_results", []) if recognition else []
    return [[int(value) for value in result.box] for result in results]


def save_png(image: np.ndarray, destination: Path) -> None:
    """Save a BGR OpenCV image as PNG through Pillow."""
    image_rgb = cv2.cvtColor(image, cv2.COLOR_BGR2RGB)
    PILImage.fromarray(image_rgb).save(destination, "PNG")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="使用 MaaEnd 固定 ROI + UID/# OCR 脱敏并导入单张节点测试截图。"
    )
    parser.add_argument("source", type=Path, help="仓库外原始截图路径")
    parser.add_argument(
        "destination",
        type=Path,
        help="tests/MaaEndTestset/<controller>/<resource>/ 下的目标 PNG",
    )
    parser.add_argument(
        "--controller",
        choices=("ADB", "Win32"),
        help="可选；默认从目标路径推断，并校验两者一致",
    )
    parser.add_argument(
        "--extra-roi",
        action="append",
        default=[],
        type=parse_roi,
        metavar="X,Y,W,H",
        help="额外纯绿色遮盖区域，可重复传入",
    )
    parser.add_argument("--force", action="store_true", help="允许替换已有目标图片")
    parser.add_argument("--repo", type=Path, help="MaaEnd 仓库根目录")
    return parser


def main() -> None:
    args = build_parser().parse_args()
    repo = find_repo_root(args.repo)
    source = args.source.expanduser().resolve()
    destination = args.destination.expanduser()
    if not destination.is_absolute():
        destination = repo / destination
    destination = destination.resolve()

    if not source.is_file():
        raise SystemExit(f"原始截图不存在：{source}")
    if destination.suffix.casefold() != ".png":
        raise SystemExit("目标图片必须使用 .png 扩展名。")

    inferred_controller = controller_from_destination(repo, destination)
    if args.controller is not None and args.controller != inferred_controller:
        raise SystemExit(
            f"--controller={args.controller} 与目标路径平台 {inferred_controller} 不一致。"
        )
    controller = args.controller or inferred_controller

    same_file = source == destination
    if destination.exists() and not args.force:
        raise SystemExit(f"目标已存在：{destination}；确认替换时使用 --force。")

    image = read_image(source)
    fixed_roi = FIXED_ROIS[controller]
    validate_image_and_rois(image, [fixed_roi, *args.extra_roi])
    fill_roi(image, fixed_roi)

    tasker, resource, ocr_controller = build_tasker(repo)
    ocr_rois = find_private_rois(tasker, image)
    del tasker, resource, ocr_controller
    validate_image_and_rois(image, ocr_rois)
    for roi in [*ocr_rois, *args.extra_roi]:
        fill_roi(image, roi)

    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_name(
        f".{destination.stem}.redacting-{uuid.uuid4().hex}.png"
    )
    try:
        save_png(image, temporary)
        temporary.replace(destination)
    finally:
        if temporary.exists():
            temporary.unlink()

    mode = "原地替换" if same_file else "导入"
    print(f"已{mode}并打码：{destination}")
    print(f"平台：{controller}；固定 ROI：{fixed_roi}")
    print(f"OCR ROI：{ocr_rois}")
    if args.extra_roi:
        print(f"额外 ROI：{args.extra_roi}")
    print("请目视检查最终图片，确认没有 OCR 漏掉的私人信息。")


if __name__ == "__main__":
    main()
