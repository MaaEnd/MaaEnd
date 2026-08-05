import math
import re
import threading
import time
import uuid
from dataclasses import dataclass
from pathlib import Path

import numpy as np

from .core_utils import cv2
from .maa_interface import MaaInterface, MaaRuntimeError, MapTrackerInferResult


_INVALID_FILENAME_CHARS = re.compile(r'[<>:"/\\|?*\x00-\x1f]')
_MIN_SAMPLE_DISTANCE = 10.0
_INTERVAL_SECONDS = 1.0
_MOSAIC_CELL_SIZE = 10
_UNMOSAIC_TOP_LEFT_SIZE = (260, 180)
_UNMOSAIC_TOP_RIGHT_SIZE = (410, 60)
_MAX_CANDIDATES = 30


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


@dataclass(frozen=True)
class SampleFile:
    id: str
    path: Path
    filename: str
    timestamp: float
    map_name: str
    x: float
    y: float
    rot: int


def read_sample_file(
    path: Path,
    parser: SampleFilenameParser,
    sample_id: str | None = None,
) -> SampleFile | None:
    if path.is_symlink() or not path.is_file():
        return None
    parsed = parser.parse_filename(path.name)
    if parsed is None:
        return None
    try:
        timestamp = path.stat().st_mtime
    except OSError:
        return None
    map_name, x, y, rot = parsed
    return SampleFile(
        id=sample_id or uuid.uuid4().hex,
        path=path,
        filename=path.name,
        timestamp=timestamp,
        map_name=map_name,
        x=x,
        y=y,
        rot=rot,
    )


class SampleCoordinateIndex:
    def __init__(
        self, parser: SampleFilenameParser, min_distance: float = _MIN_SAMPLE_DISTANCE
    ):
        self.parser = parser
        self.min_distance = min_distance
        self.coords_by_map: dict[str, list[tuple[float, float]]] = {}

    def load_dir(self, output_dir: str | Path) -> list[SampleFile]:
        files: list[SampleFile] = []
        for path in sorted(Path(output_dir).iterdir()):
            sample = read_sample_file(path, self.parser)
            if sample is None:
                continue
            self.add(sample.map_name, sample.x, sample.y)
            files.append(sample)
        return files

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
        return all(
            (x - known_x) ** 2 + (y - known_y) ** 2 > self.min_distance**2
            for known_x, known_y in self.coords_by_map.get(key, [])
        )


@dataclass(eq=False)
class SampleCandidate:
    id: str
    result: MapTrackerInferResult
    image: np.ndarray
    created_at: float


def _mosaic_private_regions(image: np.ndarray) -> np.ndarray:
    height, width = image.shape[:2]
    mosaic_width = max(1, (width + _MOSAIC_CELL_SIZE - 1) // _MOSAIC_CELL_SIZE)
    mosaic_height = max(1, (height + _MOSAIC_CELL_SIZE - 1) // _MOSAIC_CELL_SIZE)
    mosaic = cv2.resize(
        image, (mosaic_width, mosaic_height), interpolation=cv2.INTER_AREA
    )
    mosaic = cv2.resize(mosaic, (width, height), interpolation=cv2.INTER_NEAREST)

    mask = np.ones((height, width), dtype=bool)
    left_width, left_height = _UNMOSAIC_TOP_LEFT_SIZE
    right_width, right_height = _UNMOSAIC_TOP_RIGHT_SIZE
    mask[: min(left_height, height), : min(left_width, width)] = False
    mask[: min(right_height, height), max(0, width - right_width) :] = False

    output = image.copy()
    output[mask] = mosaic[mask]
    return output


class SampleCollector:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._lifecycle_lock = threading.Lock()
        self._stop_event: threading.Event | None = None
        self._thread: threading.Thread | None = None
        self._maa_interface: MaaInterface | None = None
        self._parser = SampleFilenameParser()
        self._file_index = SampleCoordinateIndex(self._parser)
        self._candidate_index = SampleCoordinateIndex(self._parser)
        self._candidates: list[SampleCandidate] = []
        self._existing: dict[str, SampleFile] = {}
        self._output_dir: Path | None = None
        self._running = False
        self._error: str | None = None

    @staticmethod
    def _normalize_metadata(
        map_name: str, x: float, y: float, rot: int
    ) -> tuple[str, float, float, int]:
        map_name = map_name.strip()
        if not map_name:
            raise ValueError("Map name is required")
        if not math.isfinite(x) or not math.isfinite(y):
            raise ValueError("Coordinates must be finite numbers")
        if isinstance(rot, bool) or not isinstance(rot, int) or not 0 <= rot < 360:
            raise ValueError("Rotation must be an integer from 0 to 359")
        return map_name, x, y, rot

    def _resolve_output_dir(self, output_dir: str) -> Path:
        path_text = output_dir.strip()
        if not path_text:
            raise ValueError("Output directory is required")
        path = Path(path_text).expanduser().resolve()
        if path.exists() and not path.is_dir():
            raise ValueError("Output path is not a directory")
        path.mkdir(parents=True, exist_ok=True)
        return path

    def _load_existing_unlocked(self, path: Path, *, clear_candidates: bool) -> None:
        previous_ids = {item.path: item.id for item in self._existing.values()}
        parser = SampleFilenameParser()
        file_index = SampleCoordinateIndex(parser)
        existing: dict[str, SampleFile] = {}
        for sample in file_index.load_dir(path):
            sample_id = previous_ids.get(sample.path, sample.id)
            if sample_id != sample.id:
                sample = SampleFile(
                    id=sample_id,
                    path=sample.path,
                    filename=sample.filename,
                    timestamp=sample.timestamp,
                    map_name=sample.map_name,
                    x=sample.x,
                    y=sample.y,
                    rot=sample.rot,
                )
            existing[sample_id] = sample
        self._parser = parser
        self._file_index = file_index
        if clear_candidates:
            self._candidate_index = SampleCoordinateIndex(parser)
            self._candidates.clear()
        self._existing = existing
        self._output_dir = path

    def load(self, output_dir: str) -> dict:
        path = self._resolve_output_dir(output_dir)
        with self._lifecycle_lock:
            with self._lock:
                if self._running:
                    if self._output_dir == path:
                        return self._status_unlocked()
                    raise RuntimeError(
                        "Cannot change output directory while collection is running"
                    )
                self._load_existing_unlocked(path, clear_candidates=True)
                self._error = None
                return self._status_unlocked()

    def start(self, output_dir: str) -> dict:
        path = self._resolve_output_dir(output_dir)

        with self._lifecycle_lock:
            if self._running or (self._thread is not None and self._thread.is_alive()):
                raise RuntimeError("Sample collection is already running")

            maa_interface = MaaInterface()
            try:
                maa_interface.init_controller()
                maa_interface.init_agent()
            except Exception:
                maa_interface.dispose_agent()
                raise

            with self._lock:
                self._load_existing_unlocked(path, clear_candidates=True)
                self._maa_interface = maa_interface
                self._error = None
                self._running = True
                stop_event = threading.Event()
                self._stop_event = stop_event

            self._thread = threading.Thread(
                target=self._collect_loop,
                args=(maa_interface, stop_event),
                name="MapTrackerSampleCollector",
                daemon=True,
            )
            self._thread.start()
        return self.status()

    def _collect_loop(
        self, maa_interface: MaaInterface, stop_event: threading.Event
    ) -> None:
        try:
            while not stop_event.is_set():
                started_at = time.monotonic()
                try:
                    raw_image = maa_interface.capture_screen()
                    result = maa_interface.do_infer_on_image(raw_image, precision=1.0)
                    image = _mosaic_private_regions(raw_image)
                    self._consider(result, image)
                    with self._lock:
                        self._error = None
                except MaaRuntimeError as exc:
                    with self._lock:
                        self._error = str(exc)
                except Exception as exc:
                    with self._lock:
                        self._error = str(exc)
                    break
                elapsed = time.monotonic() - started_at
                stop_event.wait(max(0.0, _INTERVAL_SECONDS - elapsed))
        finally:
            maa_interface.dispose_agent()
            with self._lock:
                self._running = False
                if self._maa_interface is maa_interface:
                    self._maa_interface = None
                if self._stop_event is stop_event:
                    self._stop_event = None

    def _consider(self, result: MapTrackerInferResult, image: np.ndarray) -> None:
        map_name = result["map_name"]
        x = result["x"]
        y = result["y"]
        with self._lock:
            if not self._file_index.should_keep(map_name, x, y):
                return
            if not self._candidate_index.should_keep(map_name, x, y):
                return
            if len(self._candidates) >= _MAX_CANDIDATES:
                self._drop_candidate(self._candidates[0])
            candidate = SampleCandidate(
                id=uuid.uuid4().hex,
                result=result,
                image=image,
                created_at=time.time(),
            )
            self._candidates.append(candidate)
            self._candidate_index.add(map_name, x, y)

    def _drop_candidate(self, candidate: SampleCandidate) -> None:
        try:
            self._candidates.remove(candidate)
        except ValueError:
            return
        self._candidate_index.remove(
            candidate.result["map_name"],
            candidate.result["x"],
            candidate.result["y"],
        )

    def stop(self) -> dict:
        with self._lifecycle_lock:
            stop_event = self._stop_event
            if stop_event is not None:
                stop_event.set()
            thread = self._thread
            if thread is not None and thread.is_alive():
                thread.join(timeout=5.0)
            if thread is not None and thread.is_alive():
                maa_interface = self._maa_interface
                if maa_interface is not None:
                    maa_interface.dispose_agent()
                thread.join(timeout=5.0)
            if thread is None or not thread.is_alive():
                self._thread = None
            else:
                with self._lock:
                    self._error = "Sample collection is still stopping"
        return self.status()

    def status(self) -> dict:
        with self._lock:
            return self._status_unlocked()

    def _status_unlocked(self) -> dict:
        candidates = [
            {
                "id": candidate.id,
                "kind": "candidate",
                "filename": self._parser.make_filename(
                    candidate.result["map_name"],
                    candidate.result["x"],
                    candidate.result["y"],
                    candidate.result["rot"],
                ),
                "timestamp": candidate.created_at,
                "map_name": candidate.result["map_name"],
                "x": candidate.result["x"],
                "y": candidate.result["y"],
                "rot": candidate.result["rot"],
                "inference": {
                    "infer_mode": candidate.result["infer_mode"],
                    "infer_time_ms": candidate.result["infer_time_ms"],
                    "loc_conf": candidate.result["loc_conf"],
                    "loc_time_ms": candidate.result["loc_time_ms"],
                    "rot_conf": candidate.result["rot_conf"],
                    "rot_time_ms": candidate.result["rot_time_ms"],
                },
            }
            for candidate in reversed(self._candidates)
        ]
        existing = [
            self._serialize_file(sample)
            for sample in sorted(
                self._existing.values(),
                key=lambda item: (item.timestamp, item.filename),
                reverse=True,
            )
        ]
        return {
            "running": self._running,
            "output_dir": str(self._output_dir) if self._output_dir else "",
            "candidate_limit": _MAX_CANDIDATES,
            "error": self._error,
            "candidates": candidates,
            "existing": existing,
        }

    @staticmethod
    def _serialize_file(sample: SampleFile) -> dict:
        return {
            "id": sample.id,
            "kind": "existing",
            "filename": sample.filename,
            "timestamp": sample.timestamp,
            "map_name": sample.map_name,
            "x": sample.x,
            "y": sample.y,
            "rot": sample.rot,
            "inference": None,
        }

    def image_png(self, sample_id: str) -> bytes | None:
        with self._lock:
            candidate = next(
                (item for item in self._candidates if item.id == sample_id), None
            )
            image = candidate.image.copy() if candidate is not None else None
            existing = self._existing.get(sample_id)
            path = existing.path if existing is not None else None
        if image is not None:
            encoded, buffer = cv2.imencode(".png", image)
            return buffer.tobytes() if encoded else None
        if path is None:
            return None
        try:
            return path.read_bytes()
        except OSError:
            return None

    def save(
        self,
        candidate_id: str,
        map_name: str,
        x: float,
        y: float,
        rot: int,
    ) -> dict | None:
        map_name, x, y, rot = self._normalize_metadata(map_name, x, y, rot)
        with self._lock:
            candidate = next(
                (item for item in self._candidates if item.id == candidate_id), None
            )
            if candidate is None or self._output_dir is None:
                return None
            filename = self._parser.make_filename(map_name, x, y, rot)
            output_path = self._output_dir / filename
            if output_path.exists():
                raise FileExistsError(f"Sample already exists: {filename}")
            if not cv2.imwrite(
                str(output_path),
                candidate.image,
                [cv2.IMWRITE_PNG_COMPRESSION, 9],
            ):
                raise OSError(f"Failed to write screenshot to {output_path}")
            sample = read_sample_file(output_path, self._parser, candidate.id)
            if sample is None:
                raise OSError(f"Failed to index screenshot at {output_path}")
            self._file_index.add(sample.map_name, sample.x, sample.y)
            self._existing[sample.id] = sample
            self._drop_candidate(candidate)
            return {
                "saved": self._serialize_file(sample),
                "status": self._status_unlocked(),
            }

    def update_existing(
        self,
        sample_id: str,
        map_name: str,
        x: float,
        y: float,
        rot: int,
    ) -> dict | None:
        map_name, x, y, rot = self._normalize_metadata(map_name, x, y, rot)
        with self._lock:
            sample = self._existing.get(sample_id)
            if sample is None or self._output_dir is None:
                return None
            filename = self._parser.make_filename(map_name, x, y, rot)
            output_path = self._output_dir / filename
            if output_path != sample.path and output_path.exists():
                raise FileExistsError(f"Sample already exists: {filename}")
            if output_path != sample.path:
                sample.path.rename(output_path)

            updated = read_sample_file(output_path, self._parser, sample.id)
            if updated is None:
                raise OSError(f"Failed to index screenshot at {output_path}")
            self._file_index.remove(sample.map_name, sample.x, sample.y)
            self._file_index.add(updated.map_name, updated.x, updated.y)
            self._existing[sample.id] = updated
            return {
                "updated": self._serialize_file(updated),
                "status": self._status_unlocked(),
            }

    def cleanup(self) -> None:
        self.stop()
