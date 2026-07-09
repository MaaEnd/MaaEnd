# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "maafw>=5",
#     "opencv-python>=4",
# ]
# ///

# MapTrackerTester - Collect live MapTracker inference samples.
#
# Usage:
#   python map_tracker_tester.py collect_data -o/--output-dir <dir>
#   python map_tracker_tester.py batch_test -i/--input-dir <dir> [-p/--precision <0.0-1.0>]

import argparse
import math
import os
import re
import threading
import time
from dataclasses import dataclass
from functools import lru_cache

import numpy as np

from _internal.core_utils import _R, _G, _Y, _C, _A, _B, _0, Drawer, cv2
from _internal.gui_pages import BasePage, PageStepper
from _internal.gui_widgets import ScrollableListWidget
from _internal.maa_interface import (
    MaaInitializationError,
    MaaInterface,
    MaaRuntimeError,
)

# collect_data mode constants
_INTERVAL_SECONDS = 1.0
_MOSAIC_CELL_SIZE = 10
_UNMOSAIC_TOP_LEFT_SIZE = (260, 180)
_UNMOSAIC_TOP_RIGHT_SIZE = (410, 60)

_MAP_DIR = "assets/resource/image/MapTracker/map"
_MAX_CANDIDATES = 30
_MIN_SAMPLE_DISTANCE = 10.0
_PREVIEW_CROP_SIZE = (400, 300)

# batch_test pass constants
_MAX_LOC_ERROR = 1.733
_MIN_LOC_PASSRATE = 0.95
_MAX_ROT_ERROR = 6.0
_MIN_ROT_PASSRATE = 0.95
_SUBPIXEL_ENTROPY_GEQ = 0.6
_BATCH_PRECISION = 0.7

_FULL_SEARCH_MODE = 0b01
_FAST_SEARCH_MODE = 0b11
_FAST_SEARCH_REPEATS = 4

_INVALID_FILENAME_CHARS = re.compile(r'[<>:"/\\|?*\x00-\x1f]')
_DISTRIBUTION_COLUMNS = (
    ("P1", 1),
    ("P25", 25),
    ("P50", 50),
    ("P75", 75),
    ("P99", 99),
)


class SampleFilenameParser:
    _PATTERN = re.compile(
        r"^(?P<map_name>.+)_x(?P<x>-?\d+(?:\.\d+)?)_y(?P<y>-?\d+(?:\.\d+)?)_r(?P<rot>-?\d+)\.png$"
    )

    @staticmethod
    def normalize_map_name(value: str) -> str:
        safe = _INVALID_FILENAME_CHARS.sub("_", value).strip(" .")
        return safe or "unknown"

    @staticmethod
    def _format_coord(value: float) -> str:
        return f"{value:.1f}"

    @staticmethod
    def _format_rotation(value: int) -> str:
        return str(int(value))

    def make_filename(self, map_name: str, x: float, y: float, rot: int) -> str:
        return (
            f"{self.normalize_map_name(map_name)}"
            f"_x{self._format_coord(x)}"
            f"_y{self._format_coord(y)}"
            f"_r{self._format_rotation(rot)}.png"
        )

    def parse_filename(self, filename: str) -> tuple[str, float, float, int] | None:
        match = self._PATTERN.match(filename)
        if match is None:
            return None
        return (
            match.group("map_name"),
            float(match.group("x")),
            float(match.group("y")),
            int(match.group("rot")),
        )


class SampleCoordinateIndex:
    def __init__(
        self, parser: SampleFilenameParser, min_distance: float = _MIN_SAMPLE_DISTANCE
    ):
        self.parser = parser
        self.min_distance = min_distance
        self.coords_by_map: dict[str, list[tuple[float, float]]] = {}

    def load_dir(self, output_dir: str) -> int:
        loaded = 0
        for filename in os.listdir(output_dir):
            parsed = self.parser.parse_filename(filename)
            if parsed is None:
                continue
            map_name, x, y, _ = parsed
            self.add(map_name, x, y)
            loaded += 1
        return loaded

    def add(self, map_name: str, x: float, y: float) -> None:
        key = self.parser.normalize_map_name(map_name)
        self.coords_by_map.setdefault(key, []).append((x, y))

    def remove(self, map_name: str, x: float, y: float) -> None:
        key = self.parser.normalize_map_name(map_name)
        coords = self.coords_by_map.get(key)
        if coords is None:
            return
        try:
            coords.remove((x, y))
        except ValueError:
            return
        if not coords:
            del self.coords_by_map[key]

    def should_keep(self, map_name: str, x: float, y: float) -> bool:
        key = self.parser.normalize_map_name(map_name)
        for known_x, known_y in self.coords_by_map.get(key, []):
            if (x - known_x) ** 2 + (y - known_y) ** 2 <= self.min_distance**2:
                return False
        return True


def _mosaic_private_regions(image: np.ndarray) -> np.ndarray:
    h, w = image.shape[:2]
    mosaic_w = max(1, (w + _MOSAIC_CELL_SIZE - 1) // _MOSAIC_CELL_SIZE)
    mosaic_h = max(1, (h + _MOSAIC_CELL_SIZE - 1) // _MOSAIC_CELL_SIZE)
    mosaic = cv2.resize(image, (mosaic_w, mosaic_h), interpolation=cv2.INTER_AREA)
    mosaic = cv2.resize(mosaic, (w, h), interpolation=cv2.INTER_NEAREST)

    mask = np.ones((h, w), dtype=bool)
    left_w, left_h = _UNMOSAIC_TOP_LEFT_SIZE
    right_w, right_h = _UNMOSAIC_TOP_RIGHT_SIZE
    mask[: min(left_h, h), : min(left_w, w)] = False
    mask[: min(right_h, h), max(0, w - right_w) :] = False

    output = image.copy()
    output[mask] = mosaic[mask]
    return output


def _force_stop_agent(maa_interface: MaaInterface) -> None:
    process = maa_interface.agent_process
    maa_interface.agent_process = None
    maa_interface.agent_client = None
    if process is None or process.poll() is not None:
        return

    try:
        process.kill()
    except Exception:
        pass


def _format_result_line(result: dict) -> str:
    return (
        f"  {_G}{result['map_name']}{_0} "
        f"x={_C}{result['x']:.1f}{_0} "
        f"y={_C}{result['y']:.1f}{_0} "
        f"r={_C}{result['rot']}{_0} "
        f"loc_conf={_A}{result['loc_conf']:.3f}{_0} "
        f"rot_conf={_A}{result['rot_conf']:.3f}{_0}"
    )


def _save_sample(
    output_dir: str,
    parser: SampleFilenameParser,
    result: dict,
    image: np.ndarray,
) -> str:
    filename = parser.make_filename(
        result["map_name"], result["x"], result["y"], result["rot"]
    )
    output_path = os.path.join(output_dir, filename)

    if not cv2.imwrite(output_path, image, [cv2.IMWRITE_PNG_COMPRESSION, 9]):
        raise MaaRuntimeError(f"Failed to write screenshot to {output_path}")
    return output_path


@lru_cache(maxsize=4)
def _load_big_map(map_path: str) -> np.ndarray | None:
    """Loads a big map image by path, cached to avoid per-frame disk reads."""
    return cv2.imread(map_path)


def _resolve_map_path(map_name: str) -> str:
    base = os.path.basename(str(map_name).replace("\\", "/"))
    name = base if base.lower().endswith(".png") else f"{base}.png"
    return os.path.join(_MAP_DIR, name)


def _render_map_crop(result: dict, width: int, height: int) -> np.ndarray:
    """Renders a crop of the big map around (x, y) with the location point."""
    canvas = np.full((height, width, 3), 30, dtype=np.uint8)
    big_map = _load_big_map(_resolve_map_path(result["map_name"]))
    if big_map is None:
        Drawer(canvas).text_centered(
            f"map not found: {result['map_name']}",
            (width // 2, height // 2),
            0.45,
            color=0x8899AA,
        )
        return canvas

    crop_w, crop_h = _PREVIEW_CROP_SIZE
    mh, mw = big_map.shape[:2]
    cx, cy = float(result["x"]), float(result["y"])

    # Top-left of the crop window, clamped to map bounds.
    x0 = int(round(cx - crop_w / 2))
    y0 = int(round(cy - crop_h / 2))
    x0 = max(0, min(x0, max(0, mw - crop_w)))
    y0 = max(0, min(y0, max(0, mh - crop_h)))
    x1 = min(mw, x0 + crop_w)
    y1 = min(mh, y0 + crop_h)
    crop = big_map[y0:y1, x0:x1]
    if crop.size == 0:
        return canvas

    scale = min(width / crop.shape[1], height / crop.shape[0])
    new_w = max(1, int(crop.shape[1] * scale))
    new_h = max(1, int(crop.shape[0] * scale))
    resized = cv2.resize(crop, (new_w, new_h), interpolation=cv2.INTER_NEAREST)

    off_x = (width - new_w) // 2
    off_y = (height - new_h) // 2
    canvas[off_y : off_y + new_h, off_x : off_x + new_w] = resized

    px = off_x + int(round((cx - x0) * scale))
    py = off_y + int(round((cy - y0) * scale))
    drawer = Drawer(canvas)
    drawer.circle((px, py), 5, color=0xFF4040, thickness=-1)
    drawer.circle((px, py), 5, color=0xFFFFFF, thickness=1)
    return canvas


def _render_direction_crop(
    image: np.ndarray, result: dict, width: int, height: int
) -> np.ndarray:
    """Renders the preserved top-left screenshot region with a centered heading arrow."""
    canvas = np.full((height, width, 3), 30, dtype=np.uint8)
    if image.ndim == 3 and image.shape[2] == 4:
        image = cv2.cvtColor(image, cv2.COLOR_BGRA2BGR)

    h, w = image.shape[:2]
    keep_w, keep_h = _UNMOSAIC_TOP_LEFT_SIZE
    crop = image[: min(keep_h, h), : min(keep_w, w)]
    if crop.size == 0:
        return canvas

    scale = min(width / crop.shape[1], height / crop.shape[0])
    new_w = max(1, int(crop.shape[1] * scale))
    new_h = max(1, int(crop.shape[0] * scale))
    resized = cv2.resize(crop, (new_w, new_h), interpolation=cv2.INTER_AREA)

    off_x = (width - new_w) // 2
    off_y = (height - new_h) // 2
    canvas[off_y : off_y + new_h, off_x : off_x + new_w] = resized

    center = (width // 2, height // 2)
    arrow_len = max(24, min(width, height) // 4)
    rad = math.radians(float(result["rot"]))
    end = (
        center[0] + int(round(math.sin(rad) * arrow_len)),
        center[1] - int(round(math.cos(rad) * arrow_len)),
    )
    drawer = Drawer(canvas)
    drawer.arrow(center, end, color=0x40FFFF, thickness=2, arrow_size=10)
    drawer.circle(center, 4, color=0xFFFFFF, thickness=-1)
    return canvas


def _compose_preview(image: np.ndarray, result: dict, width: int = 480) -> np.ndarray:
    """Stacks the screenshot over side-by-side location and direction previews."""
    sh, sw = image.shape[:2]
    shot_h = max(1, int(width * sh / sw))
    shot = cv2.resize(image, (width, shot_h), interpolation=cv2.INTER_AREA)
    if shot.ndim == 3 and shot.shape[2] == 4:
        shot = cv2.cvtColor(shot, cv2.COLOR_BGRA2BGR)

    gap = 8
    left_w = max(1, (width - gap) // 2)
    right_w = max(1, width - gap - left_w)
    row_h = max(1, int(left_w * _PREVIEW_CROP_SIZE[1] / _PREVIEW_CROP_SIZE[0]))
    location_crop = _render_map_crop(result, left_w, row_h)
    direction_crop = _render_direction_crop(image, result, right_w, row_h)
    spacer = np.full((row_h, gap, 3), 20, dtype=np.uint8)
    bottom = np.hstack([location_crop, spacer, direction_crop])
    return np.vstack([shot, bottom])


@dataclass(eq=False)
class Candidate:
    """An in-memory inference sample awaiting user confirmation."""

    result: dict
    image: np.ndarray


class CollectDataPage(BasePage):
    """List of deduplicated inference candidates; double-click confirms a save."""

    def __init__(
        self,
        output_dir: str,
        parser: SampleFilenameParser,
        file_index: SampleCoordinateIndex,
    ):
        super().__init__("MapTrackerTester - Collect Data", 1280, 720)
        self._output_dir = output_dir
        self._parser = parser
        # Two distinct stores: samples loaded from disk (stable reference set,
        # grows on save) and the in-memory candidate queue mirror (kept in sync
        # with self._candidates, so FIFO eviction can drop coordinates cleanly).
        self._file_index = file_index
        self._candidate_index = SampleCoordinateIndex(parser)

        self._lock = threading.Lock()
        self._candidates: list[Candidate] = []
        self._candidates_dirty = True
        # Candidate to select on the next list rebuild (UX: jump to the newest
        # arrival; after a save, jump to the next candidate). None -> keep first.
        self._pending_select: Candidate | None = None

        self._list = ScrollableListWidget(item_height=40)
        self._list.set_preview_generator(self._generate_preview)

    # --- background-thread API ---------------------------------------------

    def consider(self, result: dict, image: np.ndarray) -> bool:
        """Adds a candidate if it clears distance dedup. Thread-safe."""
        map_name = result["map_name"]
        x = result["x"]
        y = result["y"]
        with self._lock:
            if not self._file_index.should_keep(map_name, x, y):
                return False
            if not self._candidate_index.should_keep(map_name, x, y):
                return False
            if len(self._candidates) >= _MAX_CANDIDATES:
                self._drop_candidate(self._candidates[0])
            candidate = Candidate(result=result, image=image)
            self._candidates.append(candidate)
            self._candidate_index.add(map_name, x, y)
            self._pending_select = candidate
            self._candidates_dirty = True
        self.render_request()
        return True

    def _drop_candidate(self, cand: Candidate) -> None:
        """Removes a candidate from both the queue and the candidate index."""
        try:
            self._candidates.remove(cand)
        except ValueError:
            return
        self._candidate_index.remove(
            cand.result["map_name"], cand.result["x"], cand.result["y"]
        )

    # --- rendering ----------------------------------------------------------

    def _generate_preview(self, item: dict) -> np.ndarray | None:
        cand = item.get("data")
        if not isinstance(cand, Candidate):
            return None
        return _compose_preview(cand.image, cand.result)

    def _rebuild_items(self) -> None:
        with self._lock:
            candidates = list(self._candidates)
            pending = self._pending_select
            self._pending_select = None
            self._candidates_dirty = False
        items = [
            {
                "label": self._parser.make_filename(
                    c.result["map_name"],
                    c.result["x"],
                    c.result["y"],
                    c.result["rot"],
                )[:-4],
                "sub_label": f"loc {c.result['loc_conf']:.2f} / rot {c.result['rot_conf']:.2f}",
                "icon_name": "Map",
                "data": c,
            }
            for c in candidates
        ]
        # Preserve selection by identity unless a specific candidate is pending.
        self._list.set_items(items, auto_select_first=pending is None)
        if pending is not None and pending in candidates:
            self._list.selected_idx = candidates.index(pending)

    def _render_once(self, drawer: Drawer) -> None:
        drawer.rect(
            (0, 0), (self.window_w, self.window_h), color=0x14141E, thickness=-1
        )
        if self._candidates_dirty:
            self._rebuild_items()

        drawer.text(
            f"Candidates: {len(self._list.items)}/{_MAX_CANDIDATES}  "
            f"(double-click to save, ESC to quit)",
            (20, 32),
            0.5,
            color=0x88AACC,
        )
        self._list.render(drawer, (20, 50, self.window_w - 20, self.window_h - 20))

    # --- input --------------------------------------------------------------

    def _on_mouse(self, event, x: int, y: int, flags, param) -> None:
        if self._list.consume_mouse(event, x, y, flags):
            if self._list.submitted_idx >= 0:
                self._confirm_save(self._list.submitted_idx)
            self.render_request()

    def _on_key(self, key: int) -> None:
        if self._list.consume_key(key):
            if self._list.submitted_idx >= 0:
                self._confirm_save(self._list.submitted_idx)
            self.render_request()

    def _confirm_save(self, idx: int) -> None:
        if not (0 <= idx < len(self._list.items)):
            return
        cand = self._list.items[idx].get("data")
        if not isinstance(cand, Candidate):
            return

        result = cand.result
        try:
            output_path = _save_sample(
                self._output_dir, self._parser, result, cand.image
            )
        except MaaRuntimeError as e:
            print(f"  {_R}{e}{_0}", flush=True)
            return

        with self._lock:
            # Promote to the file store, then drop from the candidate queue/index.
            self._file_index.add(result["map_name"], result["x"], result["y"])
            # UX: after saving, select the next candidate (or the new last one).
            pos = self._candidates.index(cand) if cand in self._candidates else -1
            self._drop_candidate(cand)
            if self._candidates:
                nxt = min(pos, len(self._candidates) - 1) if pos >= 0 else -1
                self._pending_select = self._candidates[nxt]
            else:
                self._pending_select = None
            self._candidates_dirty = True
        print(f"{_format_result_line(result)} -> {_C}{output_path}{_0}", flush=True)


def _inference_worker(
    maa_interface: MaaInterface,
    page: CollectDataPage,
    stop_event: threading.Event,
) -> None:
    """Infers once per interval and feeds deduplicated candidates to the page."""
    while not stop_event.is_set():
        loop_started_at = time.monotonic()
        try:
            raw_image = maa_interface.capture_screen()
            result = maa_interface.do_infer_on_image(raw_image, precision=1.0)
            image = _mosaic_private_regions(raw_image)
        except MaaRuntimeError as e:
            print(f"  {_Y}Warning: {e}{_0}", flush=True)
        except Exception as e:
            print(f"  {_R}Inference stopped: {e!r}{_0}", flush=True)
            break
        else:
            if page.consider(result, image):
                print(f"{_format_result_line(result)} {_A}(candidate){_0}", flush=True)

        elapsed = time.monotonic() - loop_started_at
        stop_event.wait(max(0.0, _INTERVAL_SECONDS - elapsed))


def cmd_collect_data(output_dir: str) -> None:
    """Launch the candidate review GUI; background thread infers continuously."""
    os.makedirs(output_dir, exist_ok=True)

    parser = SampleFilenameParser()
    sample_index = SampleCoordinateIndex(parser)
    loaded_count = sample_index.load_dir(output_dir)

    maa_interface = MaaInterface()
    stop_event = threading.Event()
    worker: threading.Thread | None = None
    interrupted = False
    force_exit = False
    try:
        print(f"  Loaded {_C}{loaded_count}{_0} existing sample(s).")
        print(f"  Initializing Maa...")
        maa_interface.init_controller()
        maa_interface.init_agent()
        print(f"  {_G}Ready{_0}. Double-click a candidate to save it.")

        page = CollectDataPage(output_dir, parser, sample_index)
        worker = threading.Thread(
            target=_inference_worker,
            args=(maa_interface, page, stop_event),
            daemon=True,
        )
        worker.start()

        stepper = PageStepper(page.window_name)
        stepper.push_step(page)
        stepper.run()
    except KeyboardInterrupt:
        interrupted = True
        force_exit = True
        print(f"\n  {_Y}Stopping...{_0}", flush=True)
        _force_stop_agent(maa_interface)
    except MaaInitializationError as e:
        print(f"  {_R}Initialization failed: {e}{_0}")
        raise SystemExit(1) from e
    finally:
        stop_event.set()
        if not force_exit:
            if worker is not None:
                worker.join(timeout=2.0)
            maa_interface.dispose_agent()

    if interrupted:
        print(f"  {_Y}Stopped{_0}", flush=True)
        os._exit(130)


@dataclass
class BatchCase:
    """A single batch_test case: expected answer parsed from the filename."""

    filename: str
    map_name: str
    x: float
    y: float
    rot: int


def _rotation_error(expected: int, actual: int) -> float:
    """Returns the smallest absolute angular difference in degrees (0..180)."""
    diff = abs(float(actual) - float(expected)) % 360.0
    return min(diff, 360.0 - diff)


def _signed_rotation_error(expected: int, actual: int) -> float:
    """Returns the signed shortest angular difference in degrees (-180..180)."""
    return (float(actual) - float(expected) + 180.0) % 360.0 - 180.0


def _batch_error_line(case: BatchCase) -> str:
    """Formats a batch case line for internal inference/read failures."""
    return f"  {_R}FAIL{_0} {case.filename} {_R}Internal error{_0}"


def _batch_result_line(
    case: BatchCase,
    result: dict,
    *,
    passed: bool,
    map_ok: bool,
    loc_passed: bool,
    rot_passed: bool,
    coord_err: float,
) -> str:
    """Formats one successful batch inference result line."""
    tag = f"{_G}PASS{_0}" if passed else f"{_R}FAIL{_0}"
    map_color = _A if map_ok else _Y
    loc_color = _B if loc_passed else _Y
    rot_color = _B if rot_passed else _Y
    dx = result["x"] - case.x
    dy = result["y"] - case.y
    signed_rot_err = round(_signed_rotation_error(case.rot, result["rot"]))
    return (
        f"  {tag} {case.filename} "
        f"{map_color}{result['map_name']}{_0} "
        f"loc_conf={loc_color}{result['loc_conf']:.2f}{_0} "
        f"loc_err={loc_color}{coord_err:.2f}{_0} "
        f"{_A}({dx:+.1f}, {dy:+.1f}){_0} "
        f"rot_conf={rot_color}{result['rot_conf']:.2f}{_0} "
        f"rot_err={rot_color}{signed_rot_err:+d}{_0}"
    )


def _batch_fast_error_line(error: MaaRuntimeError | None) -> str:
    """Formats a fast-search warning aligned to the image-name column."""
    suffix = f" ({error})" if error is not None else ""
    return f"       {_Y}Fast search failed{suffix}{_0}"


def _evaluate_case(case: BatchCase, result: dict) -> tuple[bool, float, float, bool]:
    """Compares a result against a case. Returns (passed, coord_err, rot_err, map_ok)."""
    map_ok = result["map_name"] == case.map_name
    coord_err = math.hypot(result["x"] - case.x, result["y"] - case.y)
    rot_err = _rotation_error(case.rot, result["rot"])
    passed = map_ok and coord_err <= _MAX_LOC_ERROR and rot_err <= _MAX_ROT_ERROR
    return passed, coord_err, rot_err, map_ok


def _load_batch_cases(input_dir: str, parser: SampleFilenameParser) -> list[BatchCase]:
    """Parses every sample filename in the directory into a BatchCase."""
    cases: list[BatchCase] = []
    for filename in sorted(os.listdir(input_dir)):
        parsed = parser.parse_filename(filename)
        if parsed is None:
            continue
        map_name, x, y, rot = parsed
        cases.append(BatchCase(filename, map_name, x, y, rot))
    return cases


def _print_distribution(title: str, values: list[float], value_format: str) -> None:
    """Prints a percentile distribution table."""

    def _format_distribution_header() -> str:
        cells = [f"{label:>6}" for label, _ in _DISTRIBUTION_COLUMNS]
        return "| " + " | ".join(cells) + " |"

    def _format_distribution_row(values: list[float], value_format: str) -> str:
        if not values:
            cells = [f"{'N/A':>6}" for _ in _DISTRIBUTION_COLUMNS]
        else:
            percentiles = np.percentile(values, [p for _, p in _DISTRIBUTION_COLUMNS])
            cells = [f"{value:{value_format}}" for value in percentiles]
        return "| " + " | ".join(cells) + " |"

    print(f"{_Y}{title}{_0}")
    print(f"  {_A}{_format_distribution_header()}{_0}")
    print(f"  {_C}{_format_distribution_row(values, value_format)}{_0}")


def _normalized_entropy(values: list[float]) -> float:
    """Computes normalized Shannon entropy of fractional-part distribution (0=clustered, 1=uniform)."""
    bins = [0] * 10
    for v in values:
        bins[min(9, max(0, int(v * 10)))] += 1
    total = sum(bins)
    if total == 0:
        return 0.0
    entropy = 0.0
    for b in bins:
        if b > 0:
            p = b / total
            entropy -= p * math.log(p)
    return entropy / math.log(10.0)


def _print_inference_matrix(
    title: str, passed: int, failed: int, confidences: list[float]
) -> None:
    """Prints a pass/fail summary and confidence distribution matrix."""
    total = passed + failed
    print(f"\n{_C}[{title}]{_0}")
    print(f"Passed={_G}{passed}{_0}, Failed={_R}{failed}{_0}, Total={_C}{total}{_0}")
    _print_distribution("Confidence Distribution:", confidences, f">6.3f")


def _maybe_output_ci_error(msg: str) -> None:
    """Prints a message to stderr if running in CI."""
    if os.environ.get("GITHUB_ACTIONS") == "true":
        print(f"::error::{msg}")


def _infer_on_image(
    maa_interface: MaaInterface,
    image: np.ndarray,
    *,
    precision: float,
    allowed_modes: int,
) -> tuple[dict | None, MaaRuntimeError | None]:
    """Runs fixed-image inference and returns (result, error)."""
    try:
        result = maa_interface.do_infer_on_image(
            image,
            precision=precision,
            allowed_modes=allowed_modes,
        )
    except MaaRuntimeError as e:
        return None, e
    return result, None


def cmd_batch_test(input_dir: str, precision: float = _BATCH_PRECISION) -> None:
    """Load labelled sample images and report inference accuracy against them."""
    if not os.path.isdir(input_dir):
        print(f"  {_R}Input directory not found: {input_dir}{_0}")
        print(f"    Did you forget to setup test set repository via git submodule?")
        _maybe_output_ci_error(f"Input directory not found: {input_dir}")
        raise SystemExit(1)

    parser = SampleFilenameParser()
    cases = _load_batch_cases(input_dir, parser)
    if not cases:
        print(f"  {_R}No valid sample images found in {input_dir}{_0}")
        _maybe_output_ci_error(f"No valid sample images found in {input_dir}")
        raise SystemExit(1)

    maa_interface = MaaInterface()
    passed_count = 0
    error_count = 0
    loc_passed_count = 0
    loc_failed_count = 0
    rot_passed_count = 0
    rot_failed_count = 0
    loc_confidences: list[float] = []
    rot_confidences: list[float] = []
    full_search_times: list[float] = []
    fast_search_times: list[float] = []
    subpixel_frac_x: list[float] = []
    subpixel_frac_y: list[float] = []
    try:
        print("[Preparing]")
        print(f"  CPU core(s): {_C}{os.cpu_count() or 'unknown'}{_0}")
        print(f"  Loaded {_C}{len(cases)}{_0} test case(s).")
        print(f"  Initializing MapTracker agent...")
        maa_interface.init_agent()
        print(f"  {_G}Ready{_0}. Warming up...")
        warmup_image = cv2.imread(os.path.join(input_dir, cases[0].filename))
        if warmup_image is not None:
            try:
                maa_interface.do_infer_on_image(
                    warmup_image,
                    precision=precision,
                    allowed_modes=_FULL_SEARCH_MODE,
                )
            except MaaRuntimeError as e:
                print(f"  {_Y}Warmup failed: {e}{_0}")
        else:
            print(f"  {_Y}Warmup skipped: cannot read {cases[0].filename}{_0}")
        print(f"  {_G}Ready{_0}. Running batch test...\n")
        print("[Testing]")

        for case in cases:
            image = cv2.imread(os.path.join(input_dir, case.filename))
            if image is None:
                error_count += 1
                loc_failed_count += 1
                rot_failed_count += 1
                print(_batch_error_line(case))
                continue

            result, error = _infer_on_image(
                maa_interface,
                image,
                precision=precision,
                allowed_modes=_FULL_SEARCH_MODE,
            )
            if error is not None or result is None:
                error_count += 1
                loc_failed_count += 1
                rot_failed_count += 1
                print(_batch_error_line(case))
                continue
            full_search_times.append(float(result["infer_time_ms"]))
            subpixel_frac_x.append(result["x"] - math.floor(result["x"]))
            subpixel_frac_y.append(result["y"] - math.floor(result["y"]))

            fast_result = None
            fast_error = None
            for _ in range(_FAST_SEARCH_REPEATS):
                fast_result, fast_error = _infer_on_image(
                    maa_interface,
                    image,
                    precision=precision,
                    allowed_modes=_FAST_SEARCH_MODE,
                )
            if fast_error is not None or fast_result is None:
                print(_batch_fast_error_line(fast_error))
            else:
                fast_search_times.append(float(fast_result["infer_time_ms"]))

            passed, coord_err, rot_err, map_ok = _evaluate_case(case, result)
            loc_passed = map_ok and coord_err <= _MAX_LOC_ERROR
            rot_passed = rot_err <= _MAX_ROT_ERROR
            loc_confidences.append(result["loc_conf"])
            rot_confidences.append(result["rot_conf"])
            if loc_passed:
                loc_passed_count += 1
            else:
                loc_failed_count += 1
            if rot_passed:
                rot_passed_count += 1
            else:
                rot_failed_count += 1
            if passed:
                passed_count += 1
            print(
                _batch_result_line(
                    case,
                    result,
                    passed=passed,
                    map_ok=map_ok,
                    loc_passed=loc_passed,
                    rot_passed=rot_passed,
                    coord_err=coord_err,
                )
            )
    except MaaInitializationError as e:
        print(f"  {_R}Initialization failed: {e}{_0}")
        _maybe_output_ci_error(f"Initialization failed: {e}")
        raise SystemExit(1) from e
    finally:
        maa_interface.dispose_agent()

    # Result display
    print("\n[Summary]")
    _print_inference_matrix(
        "Location Inference", loc_passed_count, loc_failed_count, loc_confidences
    )
    _print_distribution(
        "Full Search Time Distribution (ms):", full_search_times, f">6.1f"
    )
    _print_distribution(
        "Fast Search Time Distribution (ms):", fast_search_times, f">6.1f"
    )
    _print_inference_matrix(
        "Rotation Inference", rot_passed_count, rot_failed_count, rot_confidences
    )

    print(f"\n{_C}[Subpixel Analysis]{_0}")
    entropy_x = _normalized_entropy(subpixel_frac_x)
    entropy_x_color = _G if entropy_x >= _SUBPIXEL_ENTROPY_GEQ else _R
    entropy_y = _normalized_entropy(subpixel_frac_y)
    entropy_y_color = _G if entropy_y >= _SUBPIXEL_ENTROPY_GEQ else _R
    print(f"  X normalized entropy = {entropy_x_color}{entropy_x:.1%}{_A} / 100.0%{_0}")
    print(f"  Y normalized entropy = {entropy_y_color}{entropy_y:.1%}{_A} / 100.0%{_0}")

    # Final pass/fail decision
    print(f"\n{_C}[Standard Check]{_0}")
    should_fail = False
    matrix = [
        ("Location pass rate", loc_passed_count / len(cases), _MIN_LOC_PASSRATE),
        ("Rotation pass rate", rot_passed_count / len(cases), _MIN_ROT_PASSRATE),
        ("X subpixel entropy", entropy_x, _SUBPIXEL_ENTROPY_GEQ),
        ("Y subpixel entropy", entropy_y, _SUBPIXEL_ENTROPY_GEQ),
    ]
    for name, value, threshold in matrix:
        if value < threshold:
            should_fail = True
            print(f"  {_R}FAIL{_0} {name}: {value:>6.1%}{_A}  < {threshold:.1%}{_0}")
            _maybe_output_ci_error(
                f"Standard not satisfied: {name} is {value:.1%}, expected at least {threshold:.1%}"
            )
        else:
            print(f"  {_G}PASS{_0} {name}: {value:>6.1%}{_A} >= {threshold:.1%}{_0}")

    if should_fail:
        print(f"\n{_R}Batch test failed because some standards were not satisfied.{_0}")
        raise SystemExit(1)
    else:
        print(f"\n{_G}Batch test completed and passed all standards.{_0}")


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Collect and test live MapTracker inference samples."
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    collect_parser = subparsers.add_parser(
        "collect_data", help="Continuously infer and save screenshots."
    )
    collect_parser.add_argument(
        "-o", "--output-dir", required=True, help="Directory to save screenshots."
    )
    collect_parser.set_defaults(func=lambda args: cmd_collect_data(args.output_dir))

    batch_parser = subparsers.add_parser(
        "batch_test", help="Infer labelled sample images and report accuracy."
    )
    batch_parser.add_argument(
        "-i", "--input-dir", required=True, help="Directory of labelled sample images."
    )
    batch_parser.add_argument(
        "-p",
        "--precision",
        type=float,
        default=_BATCH_PRECISION,
        help=f"Inference precision for batch testing. Defaults to {_BATCH_PRECISION}.",
    )
    batch_parser.set_defaults(
        func=lambda args: cmd_batch_test(args.input_dir, args.precision)
    )

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
