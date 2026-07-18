# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "fastapi",
#   "uvicorn",
#   "websockets",
#   "maafw",
#   "pynput",
#   "pyperclip",
#   "numpy",
# ]
# ///
"""MapNavigator Web 后端 (FastAPI, 仅监听 127.0.0.1)。

架构 PIVOT (见 web/DESIGN.md 顶部横幅):
  - 路线计算 = 纯 Python, 进程内 `import basenav_preview`; 无 cpp-algo 子进程, 无 navmesh_client.py。
  - 网格渲染 = serve.py 直接把已加载的 BaseNavField 几何序列化成 NMSH 二进制缓冲 (见 DESIGN §2.4)。
  - 录制 = 复用未改动的 recording_service.py / connectors.py, 通过 WebSocket 桥接。

本文件是 web 迁移唯一的 Python 后端交付物。前端 (web/static/*) 通过下列 HTTP/WS 端点访问:
  GET  /api/zones             -> 区表 (base 几何区 + tier 叠加区)
  GET  /api/load-status       -> navmesh 后台加载进度 (非阻塞)
  GET  /basemap/{path}        -> assets/resource/image/ 下的底图 PNG (防 .. 穿越)
  GET  /basemap-by-zone       -> 任意 zone 字符串 -> 解析后的底图 PNG (resolve_zone_image)
  GET  /api/zone-ids          -> assert 模式 zone 下拉可选值 (list_available_zone_ids)
  GET  /mesh/{zone_id}        -> 某几何区的 NMSH 二进制网格缓冲 (application/octet-stream)
  POST /api/route             -> A* 预览路线 (basenav_preview.find_route); 起点/终点离网时镜像运行时的盲走补全
  GET  /api/settings          -> 读取 ~/.maaend/mapnavigator.json
  PUT  /api/settings          -> 写入 ~/.maaend/mapnavigator.json
  GET  /api/adb/devices       -> adb devices -l 枚举 (容错)
  POST /api/connection/check  -> 主动探测当前连接配置是否可达 (win32 窗口 / adb 设备 / playcover 端口)
  POST /api/locate-once       -> 单次游戏内定位 (临时连接, 取第 3 个有效帧的 x/y/zone)
  POST /api/import/analyze    -> 解析上传 JSON (路线/Assert); 缺 zone 时回片段供前端指定
  POST /api/import/finalize   -> 按片段 zone 指定定稿导入 (convert_maptracker+infer+normalize)
  POST /api/export/path       -> 点位 -> path 节点 + JSON 文本 (与 tk 逐字节一致)
  POST /api/export/assert     -> zone_id + target -> AssertLocation 节点 + JSON 文本
  WS   /ws/record             -> 录制桥接 (start/stop; G 复制坐标, X 强制打点)
  /                           -> 静态站点 web/static/ (StaticFiles, html=True)
"""

from __future__ import annotations

import asyncio
import json
import math
import os
import socket
import struct
import subprocess
import sys
import threading
import time
import webbrowser
from contextlib import asynccontextmanager
from dataclasses import asdict
from pathlib import Path
from typing import Any

import numpy as np

# --- 让被复用的父目录模块 (bare-name import) 可被解析 --------------------------------
# 这些模块 (basenav_preview / model / runtime / recording_service / ...) 位于
# tools/MapNavigator/, 且彼此以裸模块名互相 import (e.g. `from runtime import ...`)。
# serve.py 在 tools/MapNavigator/web/ 下, 故须把父目录插到 sys.path 最前。
_PARENT_DIR = Path(__file__).resolve().parent.parent  # tools/MapNavigator
if str(_PARENT_DIR) not in sys.path:
    sys.path.insert(0, str(_PARENT_DIR))

import key_listener  # noqa: E402  (ensure_privileges, 录制开始时才调用)
from basenav_preview import load_basenav_field  # noqa: E402
from connection_models import (  # noqa: E402
    AdbConnectionConfig,
    RecordingSessionConfig,
    Win32ConnectionConfig,
    PlayCoverConnectionConfig,
)
from connectors import build_recording_connector, find_game_window, list_adb_devices, resolve_adb_path  # noqa: E402
from model import normalize_zone_id, resolve_zone_image  # noqa: E402
from recording_service import RecordingService  # noqa: E402
from runtime import (  # noqa: E402
    AGENT_DIR,
    CPP_AGENT_EXE,
    MAAFW_BIN_DIR,
    MAP_IMAGE_DIR,
    RESOURCE_DIR,
    configure_runtime_env,
    get_agent_env,
    load_maa_runtime,
)
from settings_store import (  # noqa: E402
    CONNECTION_KINDS,
    MapNavigatorSettings,
    MapNavigatorSettingsStore,
    default_connection_kind,
    supported_connection_kinds,
)

from fastapi import Body, FastAPI, HTTPException, WebSocket, WebSocketDisconnect  # noqa: E402
from fastapi.concurrency import run_in_threadpool  # noqa: E402
from fastapi.responses import FileResponse, JSONResponse, Response  # noqa: E402
from fastapi.staticfiles import StaticFiles  # noqa: E402
from pydantic import BaseModel  # noqa: E402


# --- 常量 -----------------------------------------------------------------------------
NAVMESH_DIR = RESOURCE_DIR / "model" / "map" / "navmesh"
NAVMESH_GZ = NAVMESH_DIR / "base.nav.gz"
NAVMESH_RAW = NAVMESH_DIR / "base.nav"
STATIC_DIR = Path(__file__).resolve().parent / "static"

# 起点/终点不在可走网格上时,运行时会盲走一段直线补上 (cpp-algo navmesh_path_expander.cpp 的同名常量)。
START_RECOVERY_MAX_BLIND_WALK = 32.0
WAYPOINT_ARRIVAL_SLACK = 0.5
BLIND_TARGET_FALLBACK_SNAP = 12.0
BLIND_TARGET_MAX_EXTENSION = 30.0
BLIND_TARGET_PROBE_STEP = 2.0

# NMSH 二进制网格缓冲 (小端), 见 DESIGN §2.4
NMSH_MAGIC = b"NMSH"
NMSH_VERSION = 1
_NMSH_HEADER = struct.Struct("<4sIII")  # magic, version, vertex_count, triangle_count

# 只绑 127.0.0.1 —— 后端会 spawn 进程 / 连 ADB / 载 maafw, 绝不暴露到局域网。
LISTEN_HOST = "127.0.0.1"
DEFAULT_PORT = 8770
PORT_SCAN_LIMIT = 20  # 首选端口被占用时, 顺延试探的端口个数

_LOG_PREFIX = "[Backend]"


def _log(message: str) -> None:
    print(f"{_LOG_PREFIX} {message}", file=sys.stderr, flush=True)


# --- Navmesh 场 (一次加载, 后台线程) ---------------------------------------------------
class FieldManager:
    """惰性、线程安全地加载 BaseNavField 一次, 并缓存各几何区的 NMSH 网格字节。

    加载在后台线程进行 (62MB pack, 冷加载约数秒); 就绪前 get() 阻塞在事件上。
    加载失败也会 set 事件 (get() 随即抛出记录的错误), 绝不永久挂起调用方。
    """

    def __init__(self, gz_path: Path, raw_path: Path) -> None:
        self._gz_path = gz_path
        self._raw_path = raw_path
        self._field: Any = None
        self._error: str | None = None
        self._progress: float = 0.0
        self._ready = threading.Event()
        self._start_lock = threading.Lock()
        self._started = False
        self._mesh_cache: dict[int, bytes] = {}
        self._mesh_lock = threading.Lock()

    def _resolve_path(self) -> Path:
        if self._gz_path.exists():
            return self._gz_path
        return self._raw_path

    def ensure_loading(self) -> None:
        with self._start_lock:
            if self._started:
                return
            self._started = True
        threading.Thread(target=self._load, name="basenav-load", daemon=True).start()

    def _load(self) -> None:
        path = self._resolve_path()
        try:
            if not path.exists():
                raise FileNotFoundError(f"未找到 NavMesh 文件: {self._gz_path} (或 {self._raw_path})")

            def _progress(value: float) -> None:
                # basenav 的粗粒度进度回调 (0..~0.59); 归一到 0..1 供前端展示。
                try:
                    self._progress = min(1.0, max(0.0, float(value)))
                except Exception:
                    pass

            _log(f"开始加载 navmesh: {path}")
            field = load_basenav_field(path, progress_callback=_progress)
            self._field = field
            self._progress = 1.0
            _log("navmesh 加载完成")
            try:
                field.start_background_verify()
            except Exception as exc:  # noqa: BLE001
                _log(f"start_background_verify 失败(忽略): {exc}")
        except Exception as exc:  # noqa: BLE001
            self._error = str(exc)
            _log(f"navmesh 加载失败: {exc}")
        finally:
            self._ready.set()

    def status(self) -> dict[str, Any]:
        return {
            "ready": self._field is not None,
            "loading": self._started and self._field is None and self._error is None,
            "progress": self._progress,
            "error": self._error,
            "path": str(self._resolve_path()),
        }

    def get(self, timeout: float | None = 600.0):
        """阻塞直到场就绪并返回它; 失败时抛 RuntimeError。仅在工作线程 (threadpool) 中调用。"""
        self.ensure_loading()
        if not self._ready.wait(timeout):
            raise RuntimeError("navmesh 加载超时")
        if self._field is None:
            raise RuntimeError(self._error or "navmesh 加载失败")
        return self._field

    def mesh_bytes(self, zone_id: int) -> bytes | None:
        """返回某区所对应几何区的 NMSH 缓冲 (tier 会被解析到其父几何区)。

        无三角面 (tier / 未知区) 时返回 None -> 端点回 404。结果按几何区 id 缓存。
        """
        field = self.get()
        geom_id = int(field.geometry_zone_id(zone_id))
        with self._mesh_lock:
            cached = self._mesh_cache.get(geom_id)
        if cached is not None:
            return cached

        zone = field.zone_by_id.get(geom_id)
        if zone is None or zone.triangle_count <= 0:
            return None

        data = _serialize_zone_mesh(field, zone)
        with self._mesh_lock:
            self._mesh_cache[geom_id] = data
        return data


def _serialize_zone_mesh(field: Any, zone: Any) -> bytes:
    """把一个几何区的三角面序列化成 NMSH 二进制缓冲 (顶点去重, 索引紧凑)。

    区的三角面在文件中是连续分区 (triangle_zone[i] 由 [first, first+count) 切片赋值),
    故直接切片等价于 `[i for i in ... if field.triangle_zone[i]==geom_id]`, 但只需 O(count)。
    """
    start = zone.first_triangle
    end = start + zone.triangle_count
    triangles = field.triangles

    # 展平该区所有三角形的全局顶点下标 (v0,v1,v2 顺序), 供 numpy 去重。
    flat: list[int] = []
    extend = flat.extend
    for index in range(start, end):
        extend(triangles[index].vertices)
    flat_arr = np.asarray(flat, dtype=np.int64)

    # 全局顶点下标 -> 局部下标 (顺序无关, 前端只按下标取顶点)。
    unique_global, inverse = np.unique(flat_arr, return_inverse=True)
    inverse = np.asarray(inverse).reshape(-1)  # 跨 numpy 版本保证 1-D

    vertices = field.vertices
    vertex_count = int(unique_global.shape[0])
    vertex_buffer = np.empty((vertex_count, 3), dtype="<f4")
    for local, global_index in enumerate(unique_global.tolist()):
        vertex = vertices[global_index]
        vertex_buffer[local, 0] = vertex.u
        vertex_buffer[local, 1] = vertex.v
        vertex_buffer[local, 2] = vertex.height  # world-Y, 供楼层带

    index_buffer = inverse.astype("<u4")

    header = _NMSH_HEADER.pack(NMSH_MAGIC, NMSH_VERSION, vertex_count, int(zone.triangle_count))
    return header + vertex_buffer.tobytes() + index_buffer.tobytes()


field_manager = FieldManager(NAVMESH_GZ, NAVMESH_RAW)


# --- maafw 运行时 (惰性加载, 仅录制需要) ----------------------------------------------
_runtime_cache: Any = None
_runtime_loaded = False
_runtime_lock = threading.Lock()


def get_runtime() -> Any:
    """惰性加载并缓存 maafw 运行时; 缺失 maafw 时返回 None (录制端点给友好错误)。"""
    global _runtime_cache, _runtime_loaded
    with _runtime_lock:
        if not _runtime_loaded:
            try:
                _runtime_cache = load_maa_runtime()
            except Exception as exc:  # noqa: BLE001
                _log(f"load_maa_runtime 失败: {exc}")
                _runtime_cache = None
            _runtime_loaded = True
        return _runtime_cache


# --- 剪贴板 (G 热键: 录制中游戏持有焦点, 必须由后端直接写系统剪贴板) ---------------------
def _copy_to_clipboard(text: str) -> bool:
    try:
        import pyperclip

        pyperclip.copy(text)
        return True
    except Exception:  # noqa: BLE001
        pass
    try:
        if sys.platform == "darwin":
            subprocess.run(["pbcopy"], input=text.encode("utf-8"), check=True)
            return True
        if sys.platform == "win32":
            # Windows 'clip' 读 UTF-16LE
            subprocess.run(["clip"], input=text.encode("utf-16-le"), check=True)
            return True
        subprocess.run(["xclip", "-selection", "clipboard"], input=text.encode("utf-8"), check=True)
        return True
    except Exception as exc:  # noqa: BLE001
        _log(f"剪贴板写入失败: {exc}")
        return False


# --- 设置存储 -------------------------------------------------------------------------
settings_store = MapNavigatorSettingsStore()


# --- 录制会话构造 + 单例守卫 ----------------------------------------------------------
_recording_lock = threading.Lock()


def _build_session_config(payload: dict[str, Any]) -> RecordingSessionConfig:
    kind = payload.get("kind", "win32")
    if kind == "adb":
        adb = payload.get("adb") or {}
        cfg = adb.get("config")
        return RecordingSessionConfig(
            kind="adb",
            adb=AdbConnectionConfig(
                adb_path=str(adb.get("adb_path", "") or ""),
                address=str(adb.get("address", "") or ""),
                config=cfg if isinstance(cfg, dict) else {},
            ),
        )
    elif kind == "playcover":
        playcover = payload.get("playcover") or {}
        return RecordingSessionConfig(
            kind="playcover",
            playcover=PlayCoverConnectionConfig(
                address=str(playcover.get("address", "127.0.0.1:1717") or "127.0.0.1:1717"),
                uuid=str(playcover.get("uuid", "maa.playcover") or "maa.playcover"),
            ),
        )
    win = payload.get("win32") or {}
    return RecordingSessionConfig(
        kind="win32",
        win32=Win32ConnectionConfig(window_title=str(win.get("window_title", "Endfield") or "Endfield")),
    )


# --- FastAPI app ----------------------------------------------------------------------
@asynccontextmanager
async def lifespan(_app: FastAPI):
    configure_runtime_env()
    STATIC_DIR.mkdir(parents=True, exist_ok=True)
    field_manager.ensure_loading()  # 立即在后台开始加载 navmesh
    yield


app = FastAPI(title="MapNavigator Web Backend", lifespan=lifespan)


class RouteRequest(BaseModel):
    zone_id: int
    start: list[float]
    goal: list[float]
    snap_radius: float = 5.0
    floor_y: float | None = None


@app.get("/api/load-status")
async def api_load_status() -> dict[str, Any]:
    return field_manager.status()


@app.get("/api/zones")
async def api_zones() -> Any:
    def _build() -> dict[str, Any]:
        field = field_manager.get()
        zones_out: list[dict[str, Any]] = []
        for zone in field.zones:
            image = resolve_zone_image(zone.name, MAP_IMAGE_DIR)
            image_path: str | None = None
            if image is not None:
                try:
                    image_path = image.resolve().relative_to(MAP_IMAGE_DIR.resolve()).as_posix()
                except Exception:  # noqa: BLE001
                    image_path = None
            zones_out.append(
                {
                    "zone_id": int(zone.zone_id),
                    "name": zone.name,
                    "is_tier": bool(field.is_tier(zone.zone_id)),
                    "geometry_zone_id": int(field.geometry_zone_id(zone.zone_id)),
                    "width": float(zone.width),
                    "height": float(zone.height),
                    "transform": [float(v) for v in zone.transform],
                    "floor_y": field.floor_y_for(zone.zone_id),  # None 当 <= FLOOR_Y_VALID_MIN
                    "triangle_count": int(zone.triangle_count),
                    "has_image": image is not None,
                    "image_path": image_path,  # 相对 MAP_IMAGE_DIR, 可直接拼到 /basemap/<image_path>
                }
            )
        return {"zones": zones_out}

    try:
        return await run_in_threadpool(_build)
    except RuntimeError as exc:
        return JSONResponse(status_code=503, content={"error": str(exc)})


@app.get("/mesh/{zone_id}")
async def api_mesh(zone_id: int) -> Response:
    try:
        data = await run_in_threadpool(field_manager.mesh_bytes, zone_id)
    except RuntimeError as exc:
        raise HTTPException(status_code=503, detail=str(exc))
    if data is None:
        raise HTTPException(status_code=404, detail="该区无三角面 (tier 叠加区或未知区)")
    return Response(content=data, media_type="application/octet-stream")


def _blind_target_fallback(
    field: Any,
    geom_zone_id: int,
    start: tuple[float, float],
    goal: tuple[float, float],
    floor_y: float | None,
) -> tuple[Any, dict[str, Any]] | None:
    """终点不在可走网格上时,运行时怎么走 (cpp-algo AppendBlindTargetFallback,即 #3505)。

    从终点沿直线往起点方向逐点回探,第一个能规划到的点就是最近的连通网格点:规划到那里,剩下
    的一小段直线盲走过去。残差超上限就放弃。
    """
    total = math.hypot(goal[0] - start[0], goal[1] - start[1])
    if total < 1e-6:
        return None

    probe_limit = min(total, BLIND_TARGET_MAX_EXTENSION + BLIND_TARGET_FALLBACK_SNAP)
    offset = 0.0
    while offset <= probe_limit + 1e-6:
        t = min(offset / total, 1.0)
        probe = (goal[0] + (start[0] - goal[0]) * t, goal[1] + (start[1] - goal[1]) * t)
        offset += BLIND_TARGET_PROBE_STEP
        try:
            route = field.find_route(geom_zone_id, start, probe, BLIND_TARGET_FALLBACK_SNAP, floor_y)
        except ValueError:
            continue
        if not len(route.points):
            continue
        reached = route.points[-1]
        gap = math.hypot(goal[0] - reached[0], goal[1] - reached[1])
        if gap > BLIND_TARGET_MAX_EXTENSION:
            return None
        return route, {"gap": float(gap), "reached": [float(reached[0]), float(reached[1])]}
    return None


def _offmesh_probe(
    field: Any,
    geom_zone_id: int,
    point: tuple[float, float],
    snap_radius: float,
    floor_y: float | None,
    budget: float | None = None,
) -> dict[str, Any] | None:
    """点不在可走网格上吗? 不在的话,最近的网格点在哪、有多远。在网格上则返回 None。

    判据直接用运行时的第一步:以 navmesh_snap_radius 吸附。吸得上,运行时会悄悄吸过去,什么
    都不会发生(实测 404 个 case:半径内的点无一触发盲走),所以不该报;吸不上,梯子才出手。

    budget 是这个位置上运行时肯盲走的上限(起点 32 / 终点 30),由调用方按角色给——探针本身不
    知道点是起点还是终点,不填就不下"超不超上限"的判断。
    """
    if field.snap(geom_zone_id, point, snap_radius, floor_y) is not None:
        return None
    nearest = field.snap(geom_zone_id, point, START_RECOVERY_MAX_BLIND_WALK, floor_y)
    if nearest is None:
        return {"distance": None, "nearest": None, "budget": budget}
    return {
        "distance": float(nearest.distance),
        "nearest": [float(nearest.point[0]), float(nearest.point[1])],
        "budget": budget,
    }


def _blind_walk_reason(field: Any, geom_zone_id: int, point: tuple[float, float]) -> str:
    """运行时为什么非盲走不可:点压根不在网格上,还是点脚下有网格、但那一块跟目的地连不过去?

    两种毛病差得很远(一个是点标错地方,一个是网格本身碎成了互不连通的块),提示里不能混为一谈。
    这里判"脚下有没有三角面"用纯几何的 _point_on_mesh:不带半径、不做小岛降权,问的就是这一件事。
    """
    return "off_mesh" if not field._point_on_mesh(geom_zone_id, point) else "disconnected"


class OffMeshProbeRequest(BaseModel):
    zone_id: int
    points: list[list[float]]
    snap_radius: float = 5.0
    floor_y: float | None = None


@app.post("/api/offmesh-probe")
async def api_offmesh_probe(req: OffMeshProbeRequest) -> dict[str, Any]:
    """批量问:这些点在可走网格上吗? 给没有起终点上下文的点用(编辑模式的路径点、刚点下的孤点)。

    只答几何事实(在/不在、最近网格多远)。盲走究竟走多远是跟起点有关的(终点盲走要朝起点回探),
    那个数只有 /api/route 给得出,别拿这里的距离冒充。
    """

    def _compute() -> dict[str, Any]:
        try:
            field = field_manager.get()
        except RuntimeError as exc:
            return {"ok": False, "error": f"navmesh 尚未就绪: {exc}"}

        geom_zone_id = int(field.geometry_zone_id(req.zone_id))
        results: list[dict[str, Any] | None] = []
        for raw in req.points:
            if len(raw) < 2:
                results.append(None)
                continue
            point = (float(raw[0]), float(raw[1]))
            results.append(_offmesh_probe(field, geom_zone_id, point, req.snap_radius, req.floor_y))
        return {"ok": True, "results": results}

    return await run_in_threadpool(_compute)


@app.post("/api/route")
async def api_route(req: RouteRequest) -> dict[str, Any]:
    def _compute() -> dict[str, Any]:
        try:
            field = field_manager.get()
        except RuntimeError as exc:
            return {"ok": False, "error": f"navmesh 尚未就绪: {exc}"}

        if len(req.start) < 2 or len(req.goal) < 2:
            return {"ok": False, "error": "start/goal 需为 [x, y]"}

        geom_zone_id = int(field.geometry_zone_id(req.zone_id))
        start = (float(req.start[0]), float(req.start[1]))
        goal = (float(req.goal[0]), float(req.goal[1]))

        blind_start: dict[str, Any] | None = None
        blind_target: dict[str, Any] | None = None
        route: Any = None
        plan_start = start
        try:
            route = field.find_route(geom_zone_id, start, goal, req.snap_radius, req.floor_y)
        except ValueError as exc:
            error = str(exc)
            entry = field.snap(geom_zone_id, start, START_RECOVERY_MAX_BLIND_WALK, req.floor_y)
            if entry is not None and entry.distance > WAYPOINT_ARRIVAL_SLACK:
                plan_start = (float(entry.point[0]), float(entry.point[1]))
                blind_start = {"entry": [plan_start[0], plan_start[1]], "distance": float(entry.distance)}
                try:
                    route = field.find_route(geom_zone_id, plan_start, goal, req.snap_radius, req.floor_y)
                except ValueError as exc2:
                    error = str(exc2)
            if route is None:
                fallback = _blind_target_fallback(field, geom_zone_id, plan_start, goal, req.floor_y)
                if fallback is None:
                    # 规划失败时把起终点的离网情况一并带回去 —— 不然前端只有一句"寻路失败",
                    # 看不出到底是点掉在网格外, 还是两点根本不连通。
                    return {
                        "ok": False,
                        "error": error,
                        "off_mesh": {
                            "start": _offmesh_probe(
                                field, geom_zone_id, start, req.snap_radius, req.floor_y,
                                budget=START_RECOVERY_MAX_BLIND_WALK,
                            ),
                            "goal": _offmesh_probe(
                                field, geom_zone_id, goal, req.snap_radius, req.floor_y,
                                budget=BLIND_TARGET_MAX_EXTENSION,
                            ),
                        },
                    }
                route, blind_target = fallback

        # 盲走确实会发生, 但成因有两种(点离网 / 网格不连通)。前端要照实说, 所以这里分清楚。
        if blind_start is not None:
            blind_start["reason"] = _blind_walk_reason(field, geom_zone_id, start)
        if blind_target is not None:
            blind_target["reason"] = _blind_walk_reason(field, geom_zone_id, goal)

        points = [[float(p[0]), float(p[1])] for p in route.points]
        breaks = [int(b) for b in (route.segment_breaks or [])]
        if blind_start is not None:
            # 盲走段插在最前面,故所有 break 下标 +1。points[0] 仍是本段请求的起点,前端的
            # 多段合并(丢掉后续段的首点)因此照常成立。
            points.insert(0, [float(req.start[0]), float(req.start[1])])
            breaks = [b + 1 for b in breaks]
        if blind_target is not None and blind_target["gap"] > WAYPOINT_ARRIVAL_SLACK:
            points.append([goal[0], goal[1]])

        return {
            "ok": True,
            "points": points,
            "segment_breaks": breaks,
            "cost": float(route.cost),
            "blind_start": blind_start,
            "blind_target": blind_target,
        }

    return await run_in_threadpool(_compute)


@app.get("/basemap/{path:path}")
async def api_basemap(path: str) -> FileResponse:
    root = MAP_IMAGE_DIR.resolve()
    target = (root / path).resolve()
    # 防 .. 穿越 / 符号链接逃逸: 目标必须在 image 根目录之内。
    if target != root and not target.is_relative_to(root):
        raise HTTPException(status_code=403, detail="非法路径")
    if not target.is_file():
        raise HTTPException(status_code=404, detail="未找到底图")
    return FileResponse(target)


@app.get("/basemap-by-zone")
async def api_basemap_by_zone(zone_id: str) -> FileResponse:
    """把任意 zone 字符串解析成底图 PNG —— tk `renderer._get_map_pil(zone_id)` 的等价物。

    编辑模式底图 = 路点 zone 字符串 (MapLocator zone id), assert 模式 = assert zone,
    astar 模式 = tier 名或 base 显示名; 三者都经 resolve_zone_image (含 fs 存在性检查 +
    目录扫描) 解析, 故统一走此端点。解析不到回 404; 前端从加载后的 <img> 读尺寸供 fit_view。
    """

    def _resolve() -> Path | None:
        image = resolve_zone_image(zone_id, MAP_IMAGE_DIR)
        if image is None:
            return None
        resolved = image.resolve()
        root = MAP_IMAGE_DIR.resolve()
        # resolve_zone_image 只会给出 image 根内的路径; 仍做一次逃逸校验以防万一。
        if resolved != root and not resolved.is_relative_to(root):
            return None
        if not resolved.is_file():
            return None
        return resolved

    target = await run_in_threadpool(_resolve)
    if target is None:
        raise HTTPException(status_code=404, detail=f"未找到 zone 底图: {zone_id}")
    return FileResponse(target)


@app.get("/api/zone-ids")
async def api_zone_ids() -> dict[str, Any]:
    """assert 模式 zone 下拉的可选值 (json_import.list_available_zone_ids, fs 扫描各图源目录)。

    惰性 import json_import —— 与导入端点一致, 使纯导航/编辑用户即使缺 maptracker 变换文件也能启动。
    """

    def _list() -> list[str]:
        from json_import import list_available_zone_ids

        return list(list_available_zone_ids())

    zone_ids = await run_in_threadpool(_list)
    return {"zone_ids": zone_ids}


@app.get("/api/platform")
async def api_platform() -> dict[str, Any]:
    """后端跑在用户本机, 所以它的 sys.platform 就是用户的系统。前端据此禁用连不上的方式。"""
    return {
        "platform": sys.platform,
        "supported_kinds": list(supported_connection_kinds()),
        "default_kind": default_connection_kind(),
    }


@app.get("/api/settings")
async def api_get_settings() -> dict[str, Any]:
    return asdict(settings_store.load())


@app.put("/api/settings")
async def api_put_settings(payload: dict[str, Any] = Body(default_factory=dict)) -> dict[str, Any]:
    current = settings_store.load()
    kind = payload.get("connection_kind", current.connection_kind)
    if kind not in CONNECTION_KINDS:
        kind = current.connection_kind
    recent = payload.get("recent_adb_targets", current.recent_adb_targets)
    if not isinstance(recent, list):
        recent = current.recent_adb_targets
    recent = [str(item) for item in recent if str(item).strip()]
    updated = MapNavigatorSettings(
        connection_kind=kind,
        adb_path=str(payload.get("adb_path", current.adb_path)),
        adb_address=str(payload.get("adb_address", current.adb_address)),
        win32_window_title=str(payload.get("win32_window_title", current.win32_window_title)),
        playcover_uuid=str(payload.get("playcover_uuid", current.playcover_uuid)),
        playcover_address=str(payload.get("playcover_address", current.playcover_address)),
        recent_adb_targets=recent,
    )
    try:
        settings_store.save(updated)
    except Exception as exc:  # noqa: BLE001
        return JSONResponse(status_code=500, content={"error": f"保存设置失败: {exc}"})
    return asdict(updated)


@app.post("/api/connection/check")
async def api_connection_check(payload: dict[str, Any] = Body(default_factory=dict)) -> dict[str, Any]:
    """主动探测当前连接配置是否可达 (不建立录制会话)。

    win32 = 窗口句柄查找; adb = 设备枚举 (网络地址先 adb connect); playcover = PlayTools
    端口 TCP 探活 + PlayCover.app 安装检查。探测均为阻塞调用 (adb 子进程 / socket 超时),
    必须在 threadpool 中执行, 否则会卡住事件循环上的其他请求 (前端输入防抖会频繁触发本端点)。
    """

    def _check() -> dict[str, Any]:
        current = settings_store.load()
        kind = payload.get("connection_kind", current.connection_kind)

        if kind == "win32":
            if sys.platform != "win32":
                return {"connected": False, "message": "非 Windows 环境不支持句柄连接"}
            win32_title = payload.get("win32_window_title", current.win32_window_title)
            try:
                hwnd = find_game_window(win32_title)
            except Exception as exc:  # noqa: BLE001
                return {"connected": False, "message": f"窗口查找异常: {exc}"}
            if hwnd != 0:
                return {"connected": True, "message": f"Win32 窗口已找到 (hWnd: {hwnd})"}
            return {"connected": False, "message": f"未找到标题为 '{win32_title}' 的可见窗口"}

        if kind == "adb":
            adb_path = payload.get("adb_path", current.adb_path)
            address = str(payload.get("adb_address", current.adb_address)).strip()
            try:
                resolved_adb = resolve_adb_path(adb_path)
                if not resolved_adb:
                    return {"connected": False, "message": "未找到 adb 执行程序"}
                if not address:
                    return {"connected": False, "message": "未指定 ADB 设备序列号/IP"}
                if ":" in address:
                    # 网络设备可能未连接; 先尝试 adb connect (失败不致命, 枚举兜底)。
                    try:
                        subprocess.run([resolved_adb, "connect", address], capture_output=True, timeout=2.0)
                    except Exception:  # noqa: BLE001
                        pass
                matched = next(
                    (d for d in list_adb_devices(adb_path) if d.serial == address or d.address == address), None
                )
            except Exception as exc:  # noqa: BLE001
                return {"connected": False, "message": f"ADB 检测异常: {exc}"}
            if matched is None:
                return {"connected": False, "message": f"未找到设备: {address}"}
            if matched.state != "device":
                return {"connected": False, "message": f"ADB 设备状态异常: {matched.state}"}
            return {"connected": True, "message": f"ADB 在线: {matched.display_name() or matched.serial}"}

        if kind == "playcover":
            if sys.platform != "darwin":
                return {"connected": False, "message": "非 macOS 环境不支持 PlayCover"}
            address = str(payload.get("playcover_address", current.playcover_address)).strip()
            if not address:
                return {"connected": False, "message": "未指定服务地址 (PlayTools 端口)"}
            try:
                host, _, port_str = address.partition(":")
                port = int(port_str) if port_str else 1717
                with socket.create_connection((host, port), timeout=1.0):
                    pass
            except Exception:  # noqa: BLE001
                return {"connected": False, "message": f"连接服务失败 (端口未监听): {address}"}
            runtime = get_runtime()
            if runtime is None or getattr(runtime, "PlayCoverController", None) is None:
                return {"connected": False, "message": "当前运行环境未提供 PlayCover 库支持"}
            installed = os.path.exists("/Applications/PlayCover.app") or os.path.exists(
                os.path.expanduser("~/Applications/PlayCover.app")
            )
            if not installed:
                return {"connected": False, "message": "未在默认位置找到 PlayCover.app 安装"}
            return {"connected": True, "message": f"PlayCover 在线, 端口: {address}"}

        return {"connected": False, "message": f"未知连接类型: {kind}"}

    return await run_in_threadpool(_check)


@app.get("/api/adb/devices")
async def api_adb_devices(adb_path: str = "") -> dict[str, Any]:
    def _list() -> dict[str, Any]:
        resolved = adb_path or settings_store.load().adb_path
        devices = list_adb_devices(resolved)
        out: list[dict[str, Any]] = []
        for device in devices:
            out.append(
                {
                    "serial": device.serial,
                    "state": device.state,
                    "model": device.model,
                    "device": device.device,
                    "transport_id": device.transport_id,
                    "address": device.address,
                    "display_name": device.display_name(),
                }
            )
        return {"devices": out}

    try:
        return await run_in_threadpool(_list)
    except Exception as exc:  # noqa: BLE001
        return {"devices": [], "error": str(exc)}


# --- 导入 / 导出 (Option 1: 复用未改动的 json_import.py + maptracker_compat.py) --------
# 前端只做收发: POST 文件文本 -> 后端算 -> 拿回归一化点位; POST 点位 -> 拿回 JSON 文本。
# 大文件 (含 PNG 亮度采样 / 目录遍历) 单一实现在 Python, 与 tk 工具字节一致 (见 DESIGN §5)。
# 惰性 import: 只在真正导入/导出时才加载 json_import (它会读 maptracker_coordinate_transforms.json),
# 从而纯导航/编辑用户即使缺该文件也能启动服务。
def _write_temp_json(text: str) -> Path:
    """把上传文本写到临时 .json, 以复用 load_*_from_json_file(path) —— json_import.py 零改动。"""
    import tempfile

    fd, tmp = tempfile.mkstemp(suffix=".json", prefix="mapnav_import_")
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        handle.write(text)
    return Path(tmp)


# 导入是「分析 -> (可选)区域指定 -> 定稿」两阶段, 逐字节复刻 app_tk.import_json /
# _try_import_assert_json / _prompt_zone_assignment_for_import / _validate_zone_assignments,
# 仅把 tk 的模态对话框换成前端对话框; 所有判定仍在 Python 内, 与 tk 工具一致。
def _dominant_zone_of(points: list) -> str:
    """片段主导 zone (app_tk._dominant_zone)。"""
    from model import normalize_zone_id

    counts: dict[str, int] = {}
    for point in points:
        zone_name = normalize_zone_id(point.get("zone", ""))
        if not zone_name:
            continue
        counts[zone_name] = counts.get(zone_name, 0) + 1
    if not counts:
        return ""
    return max(counts.items(), key=lambda item: item[1])[0]


def _segment_summary(points: list, start: int, end: int) -> str:
    """片段摘要文案 (app_tk._format_import_segment_summary)。"""
    seg = points[start:end]
    xs = [point["x"] for point in seg]
    ys = [point["y"] for point in seg]
    return (
        f"{start:02d}-{end - 1:02d} / {end - start:02d}点 "
        f"[{min(xs):.0f},{min(ys):.0f}]~[{max(xs):.0f},{max(ys):.0f}]"
    )


def _unresolved_zone_ids(points: list) -> list[str]:
    """无法映射到底图的 zone (app_tk._validate_zone_assignments)。"""
    from model import normalize_zone_id

    zone_ids = sorted(
        {normalize_zone_id(p.get("zone", "")) for p in points if normalize_zone_id(p.get("zone", ""))}
    )
    return [z for z in zone_ids if resolve_zone_image(z, MAP_IMAGE_DIR) is None]


def _unresolved_zone_message(unresolved: list[str]) -> str:
    text = "、".join(unresolved[:6])
    if len(unresolved) > 6:
        text += "..."
    return f"以下 zone 无法映射到底图：{text}"


@app.post("/api/import/analyze")
async def api_import_analyze(payload: dict[str, Any] = Body(default_factory=dict)) -> Any:
    """复刻 import_json 的头段: 先按路线解析(不做 zone 推断), 失败再退回 Assert 解析。

    返回 kind='path' 时: needs_assignment 决定是否需要前端弹「区域映射」对话框;
    needs_assignment=False 直接给归一化后的 points; True 则回 raw_points + segments 供定稿。
    kind='assert' 时直接给 zone_id + target。ok=False 时 error 为原样中文文案。
    """
    text = str(payload.get("text", "") or "")

    def _run() -> dict[str, Any]:
        from json_import import (
            infer_missing_zones,
            list_available_zone_ids,
            load_assert_location_from_json_file,
            load_points_from_json_file,
            split_route_into_segments,
        )
        from model import normalize_path_points

        tmp = _write_temp_json(text)
        try:
            # 先按路线导入 (apply_zone_inference=False, apply_maptracker_compat 用默认 True —— 与 tk 一致)
            try:
                route = load_points_from_json_file(tmp, apply_zone_inference=False)
            except Exception as route_exc:  # noqa: BLE001 —— tk import_json 捕获全部异常再试 Assert
                try:
                    location = load_assert_location_from_json_file(tmp)
                except Exception:  # noqa: BLE001 —— Assert 也失败 -> 回原始路线错误 (tk 行为)
                    return {"ok": False, "error": str(route_exc)}
                if resolve_zone_image(location.zone_id, MAP_IMAGE_DIR) is None:
                    return {"ok": False, "error": f"Assert zone 无法映射到底图：{location.zone_id}"}
                x, y, width, height = location.target
                return {
                    "ok": True,
                    "kind": "assert",
                    "zone_id": location.zone_id,
                    "target": [float(x), float(y), float(width), float(height)],
                    "condition_count": int(location.condition_count),
                    "converted_from_maptracker": bool(location.converted_from_maptracker),
                }

            imported_points = route.points
            converted_count = route.converted_maptracker_point_count
            if not route.source_has_zone_info:
                segments = split_route_into_segments(imported_points)
                zone_options = list_available_zone_ids()
                if segments and zone_options:
                    # 需要交互式区域指定 (tk _prompt_zone_assignment_for_import)
                    suggested_points = infer_missing_zones(imported_points)
                    seg_infos: list[dict[str, Any]] = []
                    for idx, (start, end) in enumerate(segments):
                        dominant = _dominant_zone_of(suggested_points[start:end])
                        if dominant not in zone_options:
                            dominant = zone_options[0]
                        seg_infos.append(
                            {
                                "index": idx,
                                "start": start,
                                "end": end,
                                "summary": _segment_summary(imported_points, start, end),
                                "suggested_zone": dominant,
                            }
                        )
                    return {
                        "ok": True,
                        "kind": "path",
                        "needs_assignment": True,
                        "raw_points": imported_points,
                        "segments": seg_infos,
                        "zone_options": zone_options,
                        "route_count": route.route_count,
                        "converted_count": converted_count,
                    }
                # 无片段/无可选区域 -> tk 直接沿用原点位, 进入 infer+normalize

            final_points = normalize_path_points(infer_missing_zones(imported_points))
            unresolved = _unresolved_zone_ids(final_points)
            if unresolved:
                return {"ok": False, "error": _unresolved_zone_message(unresolved)}
            return {
                "ok": True,
                "kind": "path",
                "needs_assignment": False,
                "points": final_points,
                "route_count": route.route_count,
                "converted_count": converted_count,
            }
        finally:
            try:
                tmp.unlink()
            except OSError:
                pass

    return await run_in_threadpool(_run)


@app.post("/api/import/finalize")
async def api_import_finalize(payload: dict[str, Any] = Body(default_factory=dict)) -> Any:
    """复刻 confirm() + 其后的转换尾段: 给 raw_points 按片段赋 zone, 再 convert_maptracker
    -> infer -> normalize -> 校验。converted_count 只是本阶段新增, 前端与 analyze 的相加。
    """
    raw_points = payload.get("raw_points", [])
    assignments = payload.get("zone_assignments", [])
    if not isinstance(raw_points, list) or not isinstance(assignments, list):
        raise HTTPException(status_code=400, detail="raw_points / zone_assignments 需为数组")

    def _run() -> dict[str, Any]:
        from json_import import infer_missing_zones
        from maptracker_compat import (
            convert_maptracker_points_to_mapnavigator,
            maptracker_base_map_name_from_zone,
        )
        from model import normalize_path_points

        assigned_points = [dict(point) for point in raw_points]
        selected_zone_names: list[str] = []
        for assignment in assignments:
            start = int(assignment.get("start", 0))
            end = int(assignment.get("end", 0))
            zone_name = str(assignment.get("zone", "") or "").strip()
            if not zone_name:
                return {"ok": False, "error": "请先为每个片段选择对应地图。"}
            zone_name = maptracker_base_map_name_from_zone(zone_name) or zone_name
            selected_zone_names.append(zone_name)
            for point_idx in range(start, end):
                if 0 <= point_idx < len(assigned_points):
                    assigned_points[point_idx]["zone"] = zone_name

        if not selected_zone_names:
            return {"ok": False, "error": "当前没有任何可用区域映射。"}

        points, converted_count = convert_maptracker_points_to_mapnavigator(assigned_points)
        final_points = normalize_path_points(infer_missing_zones(points))
        unresolved = _unresolved_zone_ids(final_points)
        if unresolved:
            return {"ok": False, "error": _unresolved_zone_message(unresolved)}
        return {"ok": True, "points": final_points, "converted_count": converted_count}

    return await run_in_threadpool(_run)


@app.post("/api/export/path")
async def api_export_path(payload: dict[str, Any] = Body(default_factory=dict)) -> Any:
    points = payload.get("points", [])
    if not isinstance(points, list):
        raise HTTPException(status_code=400, detail="points 需为数组")

    def _run() -> dict[str, Any]:
        from json_import import export_path_nodes

        nodes = export_path_nodes(points)
        # indent=4, ensure_ascii=False —— 与 tk 工具 (app_tk.py) 的复制格式逐字节一致
        text = json.dumps(nodes, indent=4, ensure_ascii=False)
        return {"nodes": nodes, "text": text}

    return await run_in_threadpool(_run)


@app.post("/api/export/assert")
async def api_export_assert(payload: dict[str, Any] = Body(default_factory=dict)) -> Any:
    zone_id = str(payload.get("zone_id", "") or "")
    target = payload.get("target", [])
    if not isinstance(target, list) or len(target) != 4:
        raise HTTPException(status_code=400, detail="target 需为 [x, y, w, h]")

    def _run() -> dict[str, Any]:
        from json_import import export_assert_location_node

        node = export_assert_location_node(zone_id, tuple(float(v) for v in target))
        text = json.dumps(node, indent=4, ensure_ascii=False)
        return {"node": node, "text": text}

    try:
        return await run_in_threadpool(_run)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc))


@app.get("/favicon.ico", include_in_schema=False)
async def favicon() -> Response:
    return Response(status_code=204)


@app.websocket("/ws/record")
async def ws_record(websocket: WebSocket) -> None:
    await websocket.accept()
    loop = asyncio.get_running_loop()

    def push(payload: dict[str, Any]) -> None:
        # 由录制线程调用, 把回调 marshal 回事件循环发送。ws 关闭时静默失败。
        try:
            asyncio.run_coroutine_threadsafe(websocket.send_json(payload), loop)
        except Exception:  # noqa: BLE001
            pass

    service: RecordingService | None = None
    acquired = False
    try:
        first = await websocket.receive_json()
        payload = first
        if isinstance(first, dict) and isinstance(first.get("start"), dict):
            payload = first["start"]
        if not isinstance(payload, dict):
            payload = {}

        runtime = get_runtime()
        if runtime is None:
            await websocket.send_json(
                {"type": "error", "message": "maafw 运行时不可用, 无法录制 (缺少 maafw 依赖或初始化失败)。"}
            )
            return

        # 权限检查仅在录制开始时进行 (绝不在服务启动时, 以免顶掉纯编辑用户)。
        try:
            privileges_ok = key_listener.ensure_privileges()
        except Exception as exc:  # noqa: BLE001
            _log(f"ensure_privileges 异常(放行): {exc}")
            privileges_ok = True
        if not privileges_ok:
            await websocket.send_json(
                {"type": "error", "message": "缺少全局按键监听所需权限 (需管理员/输入监控授权)。"}
            )
            return

        if not _recording_lock.acquire(blocking=False):
            await websocket.send_json({"type": "error", "message": "已有录制会话进行中, 请先停止。"})
            return
        acquired = True

        stop_event = asyncio.Event()

        def on_status(text: str, color: str) -> None:
            push({"type": "status", "text": text, "color": color})

        def on_finished(points: list) -> None:
            push({"type": "finished", "points": points})
            loop.call_soon_threadsafe(stop_event.set)

        def on_error(message: str) -> None:
            push({"type": "error", "message": message})
            loop.call_soon_threadsafe(stop_event.set)

        def on_locator(text: str) -> None:
            push({"type": "locator", "text": text})

        def on_clipboard(coord: str, status: str) -> None:
            _copy_to_clipboard(coord)  # 后端直接写系统剪贴板 (游戏持有焦点)
            push({"type": "toast", "coord": coord, "status": status})

        def on_force_waypoint(x: float, y: float, zone: str) -> None:
            push({"type": "force_waypoint", "x": x, "y": y, "zone": zone})

        service = RecordingService(
            runtime=runtime,
            on_status=on_status,
            on_finished=on_finished,
            on_error=on_error,
            on_locator_detail=on_locator,
            on_clipboard=on_clipboard,
            on_force_waypoint=on_force_waypoint,
        )
        service.start(_build_session_config(payload))

        async def read_client():
            try:
                while True:
                    msg = await websocket.receive_json()
                    if isinstance(msg, dict) and msg.get("type") == "stop":
                        if service is not None:
                            service.stop()
                        break
            except Exception:
                loop.call_soon_threadsafe(stop_event.set)

        client_task = asyncio.create_task(read_client())
        try:
            await stop_event.wait()
        finally:
            client_task.cancel()
    except WebSocketDisconnect:
        pass
    except Exception as exc:  # noqa: BLE001
        _log(f"录制 WS 异常: {exc}")
        try:
            await websocket.send_json({"type": "error", "message": str(exc)})
        except Exception:  # noqa: BLE001
            pass
    finally:
        if service is not None:
            try:
                service.stop()
            except Exception:  # noqa: BLE001
                pass
        if acquired:
            _recording_lock.release()
        try:
            await websocket.close()
        except Exception:  # noqa: BLE001
            pass


def do_locate_once(runtime: Any, session_config: Any) -> dict[str, Any]:
    """临时连接游戏并做一次定位: 采满 3 个有效帧后取第 3 帧的 (x, y, zone)。

    每次调用独立起一个 cpp Agent 子进程 + 临时 Tasker (与录制会话互不复用),
    结束时 finally 终止 Agent。取第 3 帧而非第 1 帧: 前两帧可能是切图/打开地图
    过程中的过渡画面, MapLocator 需要连续帧稳定后才可信。仅在 threadpool 中调用。
    """
    agent_process = None
    try:
        agent_id = f"MapLocatorOnceAgent_{int(time.time())}"
        if not CPP_AGENT_EXE.exists():
            raise FileNotFoundError(f"找不到 Agent 可执行文件: {CPP_AGENT_EXE}")

        env = get_agent_env()
        agent_process = subprocess.Popen([str(CPP_AGENT_EXE), agent_id], cwd=str(AGENT_DIR), env=env)

        time.sleep(2.0)
        if agent_process.poll() is not None:
            raise RuntimeError(f"Agent 启动失败，返回码: {agent_process.returncode}")

        try:
            runtime.Library.open(MAAFW_BIN_DIR)
        except Exception:  # noqa: BLE001 —— 已加载过时会抛, 幂等处理
            pass

        connector = build_recording_connector(runtime, session_config)
        controller = connector.connect()

        resource = runtime.Resource()
        connector.attach_resource(resource)
        client = runtime.AgentClient(identifier=agent_id)
        client.bind(resource)
        client.connect()
        if not client.connected:
            raise RuntimeError("Agent 连接失败")

        resource.override_pipeline(
            {"MapLocateNode": {"recognition": "Custom", "custom_recognition": "MapLocateRecognition"}}
        )

        tasker = runtime.Tasker()
        tasker.bind(resource, controller)
        if not tasker.inited:
            raise RuntimeError("Tasker 初始化失败")

        valid_frames = []
        for _attempt in range(25):
            tasker.post_task("MapLocateNode").wait()
            node = tasker.get_latest_node("MapLocateNode")
            if node and node.recognition and node.recognition.best_result:
                detail = node.recognition.best_result.detail
                if isinstance(detail, str):
                    try:
                        detail = json.loads(detail)
                    except json.JSONDecodeError:
                        detail = None
                if isinstance(detail, dict) and detail.get("status") == 0:
                    x = detail.get("x")
                    y = detail.get("y")
                    zone_id = normalize_zone_id(detail.get("mapName", ""))
                    if zone_id and isinstance(x, (int, float)) and isinstance(y, (int, float)):
                        valid_frames.append({"x": float(x), "y": float(y), "zone": zone_id})
                        if len(valid_frames) >= 3:
                            break
            time.sleep(0.04)

        if len(valid_frames) < 3:
            raise RuntimeError(f"未能获取到足够的有效定位帧 (仅获取到 {len(valid_frames)} 帧)")

        return {"ok": True, "x": valid_frames[2]["x"], "y": valid_frames[2]["y"], "zone": valid_frames[2]["zone"]}
    finally:
        if agent_process:
            agent_process.terminate()
            agent_process.wait()


@app.post("/api/locate-once")
async def api_locate_once(payload: dict[str, Any] = Body(default_factory=dict)) -> dict[str, Any]:
    runtime = get_runtime()
    if runtime is None:
        raise HTTPException(status_code=500, detail="maafw 运行时不可用，无法进行定位。")

    cfg_payload = payload.get("connection")
    if not isinstance(cfg_payload, dict):
        # 未显式给连接配置时回退持久化设置; 注意打平的设置结构需转成会话结构。
        current = settings_store.load()
        cfg_payload = {
            "kind": current.connection_kind,
            "win32": {"window_title": current.win32_window_title},
            "adb": {"adb_path": current.adb_path, "address": current.adb_address},
            "playcover": {"address": current.playcover_address, "uuid": current.playcover_uuid},
        }
    session_config = _build_session_config(cfg_payload)

    try:
        res = await run_in_threadpool(do_locate_once, runtime, session_config)
        return res
    except Exception as exc:
        raise HTTPException(status_code=500, detail=f"定位执行失败: {exc}")


# 静态站点挂在最后, 使显式 API 路由优先匹配。空目录也不会崩 (已在 lifespan 中 mkdir)。
app.mount("/", StaticFiles(directory=str(STATIC_DIR), html=True), name="static")


# --- 启动: 端口选取 / 浏览器 -----------------------------------------------------------
# 端口的唯一 owner 是本文件: 由本文件 bind、由本文件宣告 URL。main.py 只是 runpy 壳,
# 不再自行猜端口 —— 否则端口顺延后浏览器会开到错误的地址。


def _preferred_port() -> int:
    """首选端口: MAPNAV_PORT 覆盖 (非法值回退默认)。占用时仍会顺延, 见 bind_listen_socket。"""
    env_port = os.environ.get("MAPNAV_PORT")
    if not env_port:
        return DEFAULT_PORT
    try:
        port = int(env_port)
    except ValueError:
        _log(f"MAPNAV_PORT 非法 ({env_port!r}), 回退 {DEFAULT_PORT}")
        return DEFAULT_PORT
    if not 1 <= port <= 65535:
        _log(f"MAPNAV_PORT 越界 ({port}), 回退 {DEFAULT_PORT}")
        return DEFAULT_PORT
    return port


def _new_listen_socket() -> socket.socket:
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    if os.name != "nt":
        # POSIX: 允许复用 TIME_WAIT 残留 (快速重启不必换端口); bind 仍会拒绝正在监听的端口,
        # 所以冲突检测不受影响。Windows 语义相反 —— SO_REUSEADDR 允许抢占别人已绑的端口,
        # 会让下面的占用探测失效 (两个服务同绑一个口), 故 Windows 一律不设。
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    return sock


def bind_listen_socket(host: str, preferred_port: int) -> tuple[socket.socket, int]:
    """绑定一个可用端口, 返回 (已绑定的 socket, 实际端口)。

    从 preferred_port 起顺延试 PORT_SCAN_LIMIT 个端口; 全被占用则退到端口 0 (内核分配),
    使工具在任何情况下都能起来, 而不是像 uvicorn.run 那样直接 EADDRINUSE 退出。

    返回的 socket 直接交给 uvicorn (`Server.run(sockets=[sock])`), 因此「探测到的端口」
    与「实际服务的端口」是同一个 socket —— 不存在「探测完到真正 bind 之间被别人抢走」的竞态。
    """
    last_error: OSError | None = None
    for offset in range(PORT_SCAN_LIMIT):
        port = preferred_port + offset
        if port > 65535:
            break
        sock = _new_listen_socket()
        try:
            sock.bind((host, port))
        except OSError as exc:
            sock.close()
            last_error = exc
            continue
        if offset:
            _log(f"端口 {preferred_port} 已被占用, 顺延到 {port}")
        return sock, port

    sock = _new_listen_socket()
    try:
        sock.bind((host, 0))
    except OSError as exc:
        sock.close()
        raise RuntimeError(f"无法绑定任何端口 (最后错误: {last_error or exc})") from exc
    port = sock.getsockname()[1]
    _log(f"端口 {preferred_port} 起连续 {PORT_SCAN_LIMIT} 个均被占用, 改用系统分配的 {port}")
    return sock, port


def open_browser_when_ready(host: str, port: int, timeout: float = 30.0) -> None:
    """等服务真正开始 accept 再开浏览器 (取代固定 sleep: 慢机器上不会开出「无法连接」页)。"""
    url = f"http://{host}:{port}/"
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.5):
                break
        except OSError:
            time.sleep(0.2)
    else:
        _log(f"等待服务就绪超时 ({timeout:.0f}s), 仍尝试打开 {url}")
    webbrowser.open(url)


if __name__ == "__main__":
    import uvicorn
    import logging

    class EndpointFilter(logging.Filter):
        def filter(self, record: logging.LogRecord) -> bool:
            msg = record.getMessage()
            if "GET /api/load-status" in msg or "GET /favicon.ico" in msg:
                return False

            status_code = None
            if record.args and len(record.args) >= 5:
                try:
                    status_code = int(record.args[4])
                except (ValueError, TypeError, IndexError):
                    pass

            if status_code is not None:
                if 200 <= status_code < 400:
                    return False
            else:
                for ok_code in (" 200 ", " 204 ", " 304 ", " 200 OK", " 304 Not Modified"):
                    if ok_code in msg:
                        return False

            return True

    logging.getLogger("uvicorn.access").addFilter(EndpointFilter())

    listen_socket, listen_port = bind_listen_socket(LISTEN_HOST, _preferred_port())
    _log(f"服务地址: http://{LISTEN_HOST}:{listen_port}/")

    if not os.environ.get("MAPNAV_NO_BROWSER"):
        threading.Thread(
            target=open_browser_when_ready,
            args=(LISTEN_HOST, listen_port),
            daemon=True,
        ).start()

    # 交出已绑定的 socket (而非让 uvicorn 自己 bind), 端口即为上面宣告给浏览器的那个。
    uvicorn.Server(uvicorn.Config(app, host=LISTEN_HOST, port=listen_port)).run(sockets=[listen_socket])
