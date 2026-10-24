"""AutoCollect 采集路线生成器的本地后端：采集物目录、路线规划与写入。

地图前端与寻路查询复用 MapNavigator：它的静态模块挂在 /mapnav/，底图与运行时路线展开沿用同样的
/basemap/、/api/route-preview 接口，navmesh 查询走同一个 NavmeshBackend。这里只放采集业务。

接口:
  GET  /api/catalog           -> 采集物类型、点位、底图与传送点
  POST /api/preview           -> 选中的采集物 -> 采集顺序、路径与路线文件预览
  GET  /api/progress          -> 当前这次生成已发出的规划查询数
  POST /api/apply             -> 同上, 并写入路线文件与 AutoCollect 任务注册
  POST /api/route-preview     -> 按 MapNavigateAction 运行时语义展开生成的路线
  GET  /basemap/{path}        -> assets/resource/image/ 下的底图与图标
"""

import itertools
import math
import sys
from collections.abc import Callable
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Any

TOOL_DIR = Path(__file__).resolve().parent
MAPNAV_DIR = TOOL_DIR.parent / "MapNavigator"
# 本工具与 MapNavigator 的模块都以裸模块名互相 import
for _path in (MAPNAV_DIR, TOOL_DIR):
    if str(_path) not in sys.path:
        sys.path.insert(0, str(_path))

import route_builder  # noqa: E402
from fastapi import Body, FastAPI, HTTPException  # noqa: E402
from fastapi.concurrency import run_in_threadpool  # noqa: E402
from fastapi.responses import FileResponse, Response  # noqa: E402
from fastapi.staticfiles import StaticFiles  # noqa: E402
from local_server import NoStoreMiddleware, serve_local  # noqa: E402
from navmesh_backend import NAVMESH_GZ, NAVMESH_RAW, NOGO_JSON, NavmeshBackend, default_pack_path  # noqa: E402
from pydantic import BaseModel  # noqa: E402
from runtime import configure_runtime_env, map_image_file  # noqa: E402

WEB_DIR = TOOL_DIR / "web"
MAPNAV_STATIC_DIR = MAPNAV_DIR / "web" / "static"
DEFAULT_PORT = 8780

_LOG_PREFIX = "[AutoCollectRoute]"


def _log(message: str) -> None:
    print(f"{_LOG_PREFIX} {message}", file=sys.stderr, flush=True)


navmesh_backend = NavmeshBackend(default_pack_path())


@asynccontextmanager
async def lifespan(_app: FastAPI):
    configure_runtime_env()
    navmesh_backend.ensure_loading()  # 排序需逐段查询规划器, 启动时即加载 navmesh
    try:
        yield
    finally:
        navmesh_backend.close()


app = FastAPI(title="AutoCollect Route Builder", lifespan=lifespan)
# 页面引用的 MapNavigator 前端模块随时可能更新, 一律不缓存
app.add_middleware(NoStoreMiddleware)


# --- 步行长度 -------------------------------------------------------------------------
# 排序用的步行长度缓存；navmesh 包或禁区表变化后路径可能不同，整份作废
_walks: dict[str, Any] = {"version": None, "lengths": {}}
# 当前这次生成实际发出的规划查询数，供页面显示进度
_progress = {"queries": 0}


def walk_length() -> route_builder.WalkFn:
    """两点间的步行长度：交给运行时按 NAVMESH 展开一段（与路线展开同一套规划），量展开后的折线。"""

    def stamp(path: Path) -> int:
        try:
            return path.stat().st_mtime_ns
        except OSError:
            return 0

    version = (stamp(NAVMESH_GZ), stamp(NAVMESH_RAW), stamp(NOGO_JSON))
    if _walks["version"] != version:
        _walks["version"] = version
        _walks["lengths"] = {}
    lengths: dict[tuple[str, str, str], float | None] = _walks["lengths"]

    def walk(zone_id: str, start: dict[str, Any], goal: dict[str, Any]) -> float | None:
        key = (zone_id, start["id"], goal["id"])
        if key not in lengths:
            target: dict[str, Any] = {"action": "NAVMESH", "target": [goal["u"], goal["v"]]}
            if "y" in goal:
                target["target_deck_y"] = goal["y"]
            _progress["queries"] += 1
            try:
                result = navmesh_backend.route_preview(
                    [start["u"], start["v"]],
                    zone_id,
                    {"path": [{"action": "ZONE", "zone_id": zone_id}, target]},
                    floor_y=start.get("y"),
                )
            except RuntimeError as exc:
                raise route_builder.RouteError(f"navmesh 尚未就绪，无法按步行距离排序: {exc}") from exc
            points = result.get("points") or [] if result.get("ok") else None
            lengths[key] = None if points is None else sum(math.dist(a[:2], b[:2]) for a, b in itertools.pairwise(points))
        return lengths[key]

    return walk


# --- 采集路线 -------------------------------------------------------------------------
def _route_request(payload: dict[str, Any]) -> tuple[str, list[str], str, dict[str, str]]:
    ids = payload.get("ids", [])
    names = payload.get("names") or {}
    if not isinstance(ids, list) or not isinstance(names, dict):
        raise HTTPException(status_code=400, detail="ids 需为数组, names 需为对象")
    return (
        str(payload.get("map", "")),
        [str(item) for item in ids],
        str(payload.get("kind", "rare")),
        {str(key): str(value) for key, value in names.items()},
    )


async def _run(task: Callable[[], dict[str, Any]]) -> dict[str, Any]:
    try:
        return await run_in_threadpool(task)
    except route_builder.RouteError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc


@app.get("/api/catalog")
async def api_catalog() -> dict[str, Any]:
    return await _run(lambda: route_builder.Library().catalog())


@app.post("/api/preview")
async def api_preview(payload: dict[str, Any] = Body(default_factory=dict)) -> dict[str, Any]:
    map_id, ids, kind, names = _route_request(payload)
    _progress["queries"] = 0
    return await _run(lambda: route_builder.preview(route_builder.Library(), map_id, ids, kind, names, walk_length()))


@app.get("/api/progress")
async def api_progress() -> dict[str, int]:
    return dict(_progress)


@app.post("/api/apply")
async def api_apply(payload: dict[str, Any] = Body(default_factory=dict)) -> dict[str, Any]:
    map_id, ids, kind, names = _route_request(payload)
    return await _run(lambda: route_builder.apply(route_builder.Library(), map_id, ids, kind, names, walk_length()))


# --- 与 MapNavigator 前端同约定的底图与路线展开 -----------------------------------------
class RoutePreviewRequest(BaseModel):
    position: list[float]
    position_zone: str
    floor_y: float | None = None
    custom_action_param: dict[str, Any]
    zipline_account_id: str = ""


@app.post("/api/route-preview")
async def api_route_preview(req: RoutePreviewRequest) -> dict[str, Any]:
    def _compute() -> dict[str, Any]:
        try:
            return navmesh_backend.route_preview(
                req.position,
                req.position_zone,
                req.custom_action_param,
                floor_y=req.floor_y,
                zipline_account_id=req.zipline_account_id,
                latest_key="route-preview",
            )
        except RuntimeError as exc:
            return {"ok": False, "error": f"navmesh 尚未就绪: {exc}"}

    return await run_in_threadpool(_compute)


@app.get("/basemap/{path:path}")
async def api_basemap(path: str) -> FileResponse:
    try:
        return FileResponse(map_image_file(path))
    except PermissionError:
        raise HTTPException(status_code=403, detail="非法路径") from None
    except FileNotFoundError:
        raise HTTPException(status_code=404, detail="未找到底图") from None


@app.get("/favicon.ico", include_in_schema=False)
async def favicon() -> Response:
    return Response(status_code=204)


# 静态站点挂在最后, 使显式 API 路由优先匹配
app.mount("/mapnav", StaticFiles(directory=str(MAPNAV_STATIC_DIR)), name="mapnav")
app.mount("/", StaticFiles(directory=str(WEB_DIR), html=True), name="web")


def run(port: int | None = None, open_browser: bool = True) -> None:
    serve_local(
        app,
        port=port or DEFAULT_PORT,
        log=_log,
        open_browser=open_browser,
        silent_requests=("GET /api/progress", "GET /favicon.ico"),
    )
