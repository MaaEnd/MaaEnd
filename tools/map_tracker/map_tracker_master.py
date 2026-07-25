# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "fastapi>=0.129,<1.0",
#     "maafw>=5.12,<6.0",
#     "opencv-python>=4.10,<5.0",
#     "uvicorn>=0.41,<1.0",
# ]
# ///

from __future__ import annotations

import argparse
import asyncio
import hashlib
import mimetypes
import socket
import struct
import threading
import webbrowser
from contextlib import asynccontextmanager
from pathlib import Path, PurePosixPath
from zipfile import BadZipFile, ZipFile

from fastapi import Body, FastAPI, HTTPException, Request
from fastapi.responses import FileResponse, RedirectResponse, Response

from _internal.core_utils import (
    MAP_DIR,
    NAVMESH_DIR,
    PIPELINE_DIR,
    MapName,
    find_map_file,
)
from _internal.log_parser import parse_track_points, split_track_periods
from _internal.nav_mesh import NavEdge, NavMeshData, NavMeshFile, NavVertex
from _internal.nav_mesh_entities import import_entities
from _internal.pipeline_handler import (
    NODE_TYPE_ASSERT_LOCATION,
    NODE_TYPE_GOAL,
    NODE_TYPE_MOVE,
    PipelineHandler,
)

FRONTEND_DIST = Path(__file__).resolve().parent / "_frontend" / "dist"
FRONTEND_ARCHIVE = FRONTEND_DIST / "static_files.zip"
MAX_PIPELINE_BYTES = 1024 * 1024
_location_service = None
_sample_collector = None
_service_lock = threading.Lock()
_frontend_files: dict[str, bytes] = {}
_frontend_archive_error: str | None = "Frontend archive has not been loaded"


def _safe_child(root: Path, relative: str) -> Path:
    candidate = (root / relative).resolve()
    try:
        candidate.relative_to(root.resolve())
    except ValueError as exc:
        raise HTTPException(400, "Invalid relative path") from exc
    return candidate


def _revision(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _image_size(path: Path) -> tuple[int, int]:
    with path.open("rb") as stream:
        header = stream.read(24)
    if len(header) >= 24 and header[:8] == b"\x89PNG\r\n\x1a\n":
        width, height = struct.unpack(">II", header[16:24])
        return width, height
    import cv2

    image = cv2.imread(str(path))
    if image is None:
        raise ValueError(f"Cannot read image: {path.name}")
    return image.shape[1], image.shape[0]


def _map_groups() -> list[dict]:
    groups: dict[str, dict] = {}
    if not MAP_DIR.is_dir():
        return []
    for path in sorted(MAP_DIR.iterdir()):
        if path.suffix.lower() not in {".png", ".jpg", ".jpeg"}:
            continue
        try:
            parsed = MapName.parse(path.name)
            group_id = f"{parsed.map_id}_{parsed.map_level_id}"
            layer_id = parsed.tier_suffix if parsed.map_type == "tier" else "main"
        except ValueError:
            group_id, layer_id = path.stem, "main"
        group = groups.setdefault(
            group_id,
            {"id": group_id, "name": group_id, "width": 0, "height": 0, "layers": []},
        )
        try:
            width, height = _image_size(path)
        except ValueError:
            continue
        if layer_id == "main" or group["width"] == 0:
            group["width"], group["height"] = width, height
        group["layers"].append(
            {
                "id": layer_id,
                "label": layer_id if layer_id == "main" else f"Tier {layer_id}",
                "file_name": path.name,
                "map_name": path.stem,
            }
        )
    for group in groups.values():
        group["layers"].sort(key=lambda item: (item["id"] != "main", item["id"]))
    return sorted(groups.values(), key=lambda item: item["name"])


def _pipeline_files() -> list[dict]:
    files: list[dict] = []
    if not PIPELINE_DIR.is_dir():
        return files
    for path in sorted(PIPELINE_DIR.rglob("*.json")):
        try:
            if path.stat().st_size >= MAX_PIPELINE_BYTES:
                continue
            content = path.read_text(encoding="utf-8", errors="ignore")
        except OSError:
            continue
        if (
            NODE_TYPE_MOVE not in content
            and NODE_TYPE_ASSERT_LOCATION not in content
            and NODE_TYPE_GOAL not in content
        ):
            continue
        try:
            nodes = PipelineHandler(str(path)).read_nodes()
        except OSError:
            continue
        if not nodes:
            continue
        files.append(
            {
                "path": path.relative_to(PIPELINE_DIR).as_posix(),
                "name": path.name,
                "nodes": nodes,
                "revision": _revision(path),
            }
        )
    return files


def _navmesh_payload(name: str, nmf: NavMeshFile) -> dict:
    return {
        "name": name,
        "meta": {
            "name": nmf.name,
            "description": nmf.description,
            "map_region_name": nmf.map_region_name,
            "map_level_name": nmf.map_level_name,
            "geo_width": nmf.geo_width,
            "geo_height": nmf.geo_height,
        },
        "vertices": [vars(vertex) for vertex in nmf.vertices],
        "edges": [vars(edge) for edge in nmf.edges],
    }


def _get_location_service():
    global _location_service
    with _service_lock:
        if _location_service is None:
            from _internal.location_service import LocationService

            _location_service = LocationService()
    return _location_service


def _get_sample_collector():
    global _sample_collector
    with _service_lock:
        if _sample_collector is None:
            from _internal.sample_handler import SampleCollector

            _sample_collector = SampleCollector()
    return _sample_collector


def _sample_metadata_args(payload: dict) -> tuple[str, float, float, int]:
    rot = payload["rot"]
    if isinstance(rot, bool) or not isinstance(rot, int):
        raise ValueError("Rotation must be an integer from 0 to 359")
    return (
        str(payload.get("map_name", "")),
        float(payload["x"]),
        float(payload["y"]),
        rot,
    )


def _ensure_sample_collection_stopped() -> None:
    if _sample_collector is not None and _sample_collector.status()["running"]:
        raise HTTPException(409, "Stop sample collection before using location tools")


def _load_frontend_files() -> None:
    global _frontend_files, _frontend_archive_error
    try:
        with ZipFile(FRONTEND_ARCHIVE) as archive:
            files = {
                info.filename: archive.read(info)
                for info in archive.infolist()
                if not info.is_dir()
            }
    except FileNotFoundError:
        _frontend_files = {}
        _frontend_archive_error = (
            "Frontend is not built. Run pnpm build in tools/map_tracker/_frontend."
        )
        return
    except (BadZipFile, OSError):
        _frontend_files = {}
        _frontend_archive_error = "Frontend archive is invalid"
        return

    _frontend_files = files
    _frontend_archive_error = (
        None
        if "index.html" in files
        else "Frontend archive does not contain index.html"
    )


@asynccontextmanager
async def lifespan(_app: FastAPI):
    _load_frontend_files()
    yield
    if _location_service is not None:
        await asyncio.to_thread(_location_service.cleanup)
    if _sample_collector is not None:
        await asyncio.to_thread(_sample_collector.cleanup)


app = FastAPI(title="MapTracker Web Tool", lifespan=lifespan)


@app.get("/")
def root() -> RedirectResponse:
    return RedirectResponse("/web/")


@app.get("/api/maps")
def list_maps() -> dict:
    return {"maps": _map_groups()}


@app.get("/api/maps/image/{file_name}")
def get_map_image(file_name: str, composite: bool = False) -> Response:
    path = _safe_child(MAP_DIR, file_name)
    if path.parent != MAP_DIR.resolve() or not path.is_file():
        raise HTTPException(404, "Map image not found")
    if not composite or "_tier_" not in path.stem:
        return FileResponse(path)
    base_name = path.stem.split("_tier_", 1)[0] + path.suffix
    base_path = MAP_DIR / base_name
    if not base_path.is_file():
        return FileResponse(path)
    import cv2
    import numpy as np

    base = cv2.imread(str(base_path))
    tier = cv2.imread(str(path))
    if base is None or tier is None or base.shape != tier.shape:
        return FileResponse(path)
    result = (base.astype(np.float32) * 0.25).astype(np.uint8)
    mask = np.any(tier > 2, axis=2)
    result[mask] = tier[mask]
    ok, encoded = cv2.imencode(".png", result)
    if not ok:
        return FileResponse(path)
    return Response(content=encoded.tobytes(), media_type="image/png")


@app.get("/api/pipeline/maptracker-nodes")
def list_pipeline_nodes() -> dict:
    return {"files": _pipeline_files()}


@app.put("/api/pipeline/node")
def save_pipeline_node(payload: dict = Body(...)) -> dict:
    relative = str(payload.get("file_path", ""))
    node_name = str(payload.get("node_name", ""))
    path = _safe_child(PIPELINE_DIR, relative)
    if path.suffix.lower() != ".json" or not path.is_file():
        raise HTTPException(404, "Pipeline file not found")
    expected_revision = str(payload.get("revision", ""))
    if expected_revision and _revision(path) != expected_revision:
        raise HTTPException(409, "Pipeline file changed after it was opened")
    handler = PipelineHandler(str(path))
    try:
        handler.read_all_nodes()
    except OSError as exc:
        raise HTTPException(500, str(exc)) from exc
    if node_name not in handler.nodes:
        raise HTTPException(404, "Pipeline node not found")
    node_type = payload.get("node_type")
    try:
        if node_type == NODE_TYPE_MOVE:
            handler.replace_path(node_name, payload.get("path", []))
        elif node_type == NODE_TYPE_ASSERT_LOCATION:
            handler.replace_assert_location(
                node_name,
                str(payload.get("map_name", "")),
                payload.get("target", []),
            )
        elif node_type == NODE_TYPE_GOAL:
            goal_path = payload.get("path", [])
            if not isinstance(goal_path, list) or not goal_path:
                raise HTTPException(422, "Goal node requires a target point")
            point = goal_path[0]
            handler.replace_goal_target(
                node_name,
                str(payload.get("map_name", "")),
                point,
            )
        else:
            raise HTTPException(400, "Unsupported MapTracker node type")
    except ValueError as exc:
        raise HTTPException(422, str(exc)) from exc
    except OSError as exc:
        raise HTTPException(500, str(exc)) from exc
    return {"revision": _revision(path)}


@app.post("/api/log/analyse")
async def analyse_log(request: Request) -> dict:
    text = (await request.body()).decode("utf-8", errors="ignore")
    points, warnings = parse_track_points(text)
    periods = split_track_periods(points)
    return {
        "point_count": len(points),
        "warnings": warnings,
        "periods": [period.to_dict() for period in periods],
    }


@app.get("/api/navmeshes")
def list_navmeshes() -> dict:
    if not NAVMESH_DIR.is_dir():
        return {"files": []}
    return {
        "files": [
            path.relative_to(NAVMESH_DIR).as_posix()
            for path in sorted(NAVMESH_DIR.rglob("*.mtnm"))
        ]
    }


@app.get("/api/navmeshes/{name:path}")
def load_navmesh(name: str) -> dict:
    path = _safe_child(NAVMESH_DIR, name)
    if path.suffix.lower() != ".mtnm" or not path.is_file():
        raise HTTPException(404, "NavMesh file not found")
    try:
        nmf = NavMeshFile.read(str(path))
    except ValueError as exc:
        raise HTTPException(422, str(exc)) from exc
    payload = _navmesh_payload(path.relative_to(NAVMESH_DIR).as_posix(), nmf)
    payload["revision"] = _revision(path)
    payload["map_file"] = find_map_file(path.stem) or f"{path.stem}.png"
    return payload


@app.post("/api/navmeshes/new")
def new_navmesh(payload: dict = Body(...)) -> dict:
    map_file = find_map_file(str(payload.get("map_file", "")))
    if map_file is None:
        raise HTTPException(404, "Map image not found")
    map_path = MAP_DIR / map_file
    try:
        parsed = MapName.parse(map_file)
    except ValueError as exc:
        raise HTTPException(
            422, "NavMesh requires a standard MapTracker map name"
        ) from exc
    width, height = _image_size(map_path)
    data = NavMeshData()
    imported = import_entities(data, parsed.map_id, parsed.map_level_id)
    nmf = NavMeshFile(
        name=map_path.stem,
        map_region_name=parsed.map_id,
        map_level_name=parsed.map_level_id,
        geo_width=width,
        geo_height=height,
        vertices=data.vertices,
        edges=data.edges,
    )
    result = _navmesh_payload(f"{map_path.stem}.mtnm", nmf)
    result.update(
        {"revision": None, "map_file": map_file, "imported_entities": imported}
    )
    return result


@app.put("/api/navmeshes/{name:path}")
def save_navmesh(name: str, payload: dict = Body(...)) -> dict:
    path = _safe_child(NAVMESH_DIR, name)
    if path.suffix.lower() != ".mtnm":
        raise HTTPException(400, "NavMesh filename must end with .mtnm")
    if path.exists() and payload.get("revision") not in (None, _revision(path)):
        raise HTTPException(409, "NavMesh file changed after it was opened")
    meta = payload.get("meta") or {}
    try:
        nmf = NavMeshFile(
            name=str(meta["name"]),
            description=str(meta.get("description", "")),
            map_region_name=str(meta["map_region_name"]),
            map_level_name=str(meta["map_level_name"]),
            geo_width=float(meta["geo_width"]),
            geo_height=float(meta["geo_height"]),
            vertices=[NavVertex(**value) for value in payload.get("vertices", [])],
            edges=[NavEdge(**value) for value in payload.get("edges", [])],
        )
        path.parent.mkdir(parents=True, exist_ok=True)
        temporary = path.with_suffix(path.suffix + ".tmp")
        nmf.save(str(temporary))
        NavMeshFile.read(str(temporary))
        temporary.replace(path)
    except (KeyError, TypeError, ValueError) as exc:
        raise HTTPException(422, str(exc)) from exc
    return {"revision": _revision(path)}


@app.post("/api/location/infer")
async def infer_location(payload: dict = Body(...)) -> dict:
    _ensure_sample_collection_stopped()
    try:
        return await asyncio.to_thread(
            _get_location_service().infer_once, str(payload.get("map_name", ""))
        )
    except Exception as exc:
        raise HTTPException(503, str(exc)) from exc


@app.post("/api/location/goal")
async def run_goal(payload: dict = Body(...)) -> dict:
    _ensure_sample_collection_stopped()
    try:
        await asyncio.to_thread(
            _get_location_service().run_goal,
            str(payload.get("map_name", "")),
            float(payload["x"]),
            float(payload["y"]),
        )
    except Exception as exc:
        raise HTTPException(503, str(exc)) from exc
    return {"ok": True}


@app.get("/api/samples/status")
def sample_collection_status() -> dict:
    return _get_sample_collector().status()


@app.post("/api/samples/load")
async def load_sample_directory(payload: dict = Body(...)) -> dict:
    try:
        return await asyncio.to_thread(
            _get_sample_collector().load, str(payload.get("output_dir", ""))
        )
    except ValueError as exc:
        raise HTTPException(422, str(exc)) from exc
    except RuntimeError as exc:
        raise HTTPException(409, str(exc)) from exc
    except OSError as exc:
        raise HTTPException(500, str(exc)) from exc


@app.post("/api/samples/start")
async def start_sample_collection(payload: dict = Body(...)) -> dict:
    try:
        if _location_service is not None:
            await asyncio.to_thread(_location_service.cleanup)
        return await asyncio.to_thread(
            _get_sample_collector().start, str(payload.get("output_dir", ""))
        )
    except ValueError as exc:
        raise HTTPException(422, str(exc)) from exc
    except RuntimeError as exc:
        raise HTTPException(409, str(exc)) from exc
    except Exception as exc:
        raise HTTPException(503, str(exc)) from exc


@app.post("/api/samples/stop")
async def stop_sample_collection() -> dict:
    return await asyncio.to_thread(_get_sample_collector().stop)


@app.get("/api/samples/{sample_id}/image")
def sample_image(sample_id: str) -> Response:
    content = _get_sample_collector().image_png(sample_id)
    if content is None:
        raise HTTPException(404, "Sample not found")
    return Response(
        content=content,
        media_type="image/png",
        headers={"Cache-Control": "no-store"},
    )


@app.post("/api/samples/{sample_id}/save")
async def save_sample_candidate(sample_id: str, payload: dict = Body(...)) -> dict:
    try:
        result = await asyncio.to_thread(
            _get_sample_collector().save,
            sample_id,
            *_sample_metadata_args(payload),
        )
    except (KeyError, TypeError, ValueError) as exc:
        raise HTTPException(422, str(exc)) from exc
    except FileExistsError as exc:
        raise HTTPException(409, str(exc)) from exc
    except OSError as exc:
        raise HTTPException(500, str(exc)) from exc
    if result is None:
        raise HTTPException(404, "Sample candidate not found")
    return result


@app.put("/api/samples/{sample_id}")
async def update_existing_sample(sample_id: str, payload: dict = Body(...)) -> dict:
    try:
        result = await asyncio.to_thread(
            _get_sample_collector().update_existing,
            sample_id,
            *_sample_metadata_args(payload),
        )
    except (KeyError, TypeError, ValueError) as exc:
        raise HTTPException(422, str(exc)) from exc
    except FileExistsError as exc:
        raise HTTPException(409, str(exc)) from exc
    except OSError as exc:
        raise HTTPException(500, str(exc)) from exc
    if result is None:
        raise HTTPException(404, "Existing sample not found")
    return result


@app.get("/web/{path:path}")
def web_app(path: str) -> Response:
    requested_path = path or "index.html"
    archive_path = PurePosixPath(requested_path)
    if archive_path.is_absolute() or ".." in archive_path.parts:
        raise HTTPException(400, "Invalid frontend path")
    if _frontend_archive_error is not None:
        raise HTTPException(503, _frontend_archive_error)

    content = _frontend_files.get(archive_path.as_posix())
    if content is None:
        if requested_path.startswith("assets/"):
            raise HTTPException(404, "Frontend file not found")
        requested_path = "index.html"
        content = _frontend_files[requested_path]

    media_type = mimetypes.guess_type(requested_path)[0] or "application/octet-stream"
    cache_control = (
        "public, max-age=31536000, immutable"
        if requested_path.startswith("assets/")
        else "no-cache"
    )
    return Response(
        content=content,
        media_type=media_type,
        headers={"Cache-Control": cache_control},
    )


def _find_port(host: str, preferred: int) -> int:
    for port in range(preferred, preferred + 50):
        with socket.socket() as sock:
            try:
                sock.bind((host, port))
            except OSError:
                continue
            return port
    raise RuntimeError("No available local port found")


def main() -> None:
    parser = argparse.ArgumentParser(description="MapTracker Web development tool")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8060)
    parser.add_argument("--no-browser", action="store_true")
    args = parser.parse_args()
    port = _find_port(args.host, args.port)
    url = f"http://{args.host}:{port}/web/"
    if not args.no_browser:
        threading.Timer(0.8, lambda: webbrowser.open(url)).start()
    print(f"MapTracker Web: {url}")
    import uvicorn

    uvicorn.run(app, host=args.host, port=port)


if __name__ == "__main__":
    main()
