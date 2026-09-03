"""navmesh 查询后端: 把 cpp-algo agent 当成一个常驻查询进程用。

几何解码、吸附、路线都在 agent 里算, 这边只负责拉起它、发查询、把结果交给端点。
查询节点不看画面也不发输入, 所以控制器是个空壳, 三项延迟一并归零。
"""

from __future__ import annotations

import json
import tempfile
import threading
from pathlib import Path
from typing import Any

from agent_session import AgentSession
from runtime import load_maa_runtime

_NODE = "MapNavmeshQuery"


def _make_null_controller() -> Any:
    import numpy as np
    from maa.controller import CustomController

    class NullController(CustomController):
        def __init__(self) -> None:
            super().__init__()
            self._frame = np.zeros((64, 64, 3), dtype=np.uint8)

        def connect(self) -> bool: return True
        def request_uuid(self) -> str: return "mapnavigator-navmesh"
        def start_app(self, intent) -> bool: return True
        def stop_app(self, intent) -> bool: return True
        def screencap(self): return self._frame
        def click(self, x, y) -> bool: return True
        def swipe(self, x1, y1, x2, y2, duration) -> bool: return True
        def touch_down(self, contact, x, y, pressure) -> bool: return True
        def touch_move(self, contact, x, y, pressure) -> bool: return True
        def touch_up(self, contact) -> bool: return True
        def click_key(self, keycode) -> bool: return True
        def input_text(self, text) -> bool: return True
        def key_down(self, keycode) -> bool: return True
        def key_up(self, keycode) -> bool: return True

    return NullController()


class _NullConnector:
    """查询会话的连接方式: 交出空壳控制器, Resource 无须附加任何东西。"""

    def connect(self) -> Any:
        controller = _make_null_controller()
        controller.post_connection().wait()
        return controller

    def attach_resource(self, resource: Any) -> None:
        return


class NavmeshBackend:
    """惰性拉起 agent 并保持连接; 首次查询时它才把 pack 读进内存。"""

    def __init__(self, navmesh_path: Path) -> None:
        self._path = navmesh_path
        self._session: AgentSession | None = None
        self._error: str | None = None
        self._ready = threading.Event()
        self._started = False
        self._start_lock = threading.Lock()
        self._query_lock = threading.Lock()
        self._latest_lock = threading.Lock()
        self._latest_generation: dict[str, int] = {}
        self._geom_of: dict[int, int] = {}
        self._mesh_cache: dict[int, bytes] = {}

    # --- 生命周期 ---------------------------------------------------------------------
    def ensure_loading(self) -> None:
        with self._start_lock:
            if self._started:
                return
            self._started = True
        threading.Thread(target=self._boot, name="navmesh-agent", daemon=True).start()

    def _boot(self) -> None:
        try:
            self._connect()
            # 顺带把 pack 读进 agent, 并留下 tier -> 几何区的映射供 mesh 缓存用。
            probe = self._post("zones")
            if not probe.get("ok"):
                raise RuntimeError(probe.get("error") or "navmesh 查询失败")
            self._geom_of = {int(z["zone_id"]): int(z["geometry_zone_id"]) for z in probe["zones"]}
        except Exception as exc:  # noqa: BLE001
            self._error = str(exc)
        finally:
            self._ready.set()

    def _connect(self) -> None:
        runtime = load_maa_runtime()
        if runtime is None:
            raise RuntimeError("maafw 不可用")
        self._session = AgentSession(runtime)
        self._session.open(_NullConnector(), agent_name="MapNavmeshAgent")

    def close(self) -> None:
        session, self._session = self._session, None
        if session is not None:
            session.close()

    def status(self) -> dict[str, Any]:
        ready = self._ready.is_set() and self._error is None
        return {
            "ready": ready,
            "loading": self._started and not self._ready.is_set(),
            "progress": 1.0 if ready else 0.0,
            "error": self._error,
            "path": str(self._path),
        }

    # --- 查询 -------------------------------------------------------------------------
    def query(self, op: str, **params: Any) -> dict[str, Any]:
        """发一次查询并返回 agent 的原始结果。仅在工作线程 (threadpool) 中调用。"""
        self._await_ready()
        return self._post(op, **params)

    def query_latest(self, key: str, op: str, **params: Any) -> dict[str, Any]:
        """同一 key 只执行等待队列里的最新查询；已经进入 Agent 的那次仍正常收尾。"""
        with self._latest_lock:
            generation = self._latest_generation.get(key, 0) + 1
            self._latest_generation[key] = generation

        self._await_ready()
        with self._query_lock:
            with self._latest_lock:
                if self._latest_generation.get(key) != generation:
                    return {"ok": False, "stale": True}
            return self._post_locked(op, **params)

    def _await_ready(self) -> None:
        self.ensure_loading()
        if not self._ready.wait(180.0):
            raise RuntimeError("navmesh agent 启动超时")
        if self._error is not None:
            raise RuntimeError(self._error)

    def _post(self, op: str, **params: Any) -> dict[str, Any]:
        with self._query_lock:
            return self._post_locked(op, **params)

    def _post_locked(self, op: str, **params: Any) -> dict[str, Any]:
        """调用方已经持有 _query_lock；保持所有 Agent 请求严格串行。"""
        payload = {"op": op, "navmesh_file": str(self._path), **params}
        session = self._session
        if session is None:
            raise RuntimeError("navmesh agent 未连接")
        # agent 死掉时框架只会静默给不出结果, 这里先把它认出来。
        exit_code = session.agent_exit_code
        if exit_code is not None:
            raise RuntimeError(f"navmesh agent 已退出 (返回码 {exit_code})")
        session.resource.override_pipeline({
                _NODE: {
                    "recognition": "Custom",
                    "custom_recognition": _NODE,
                    "custom_recognition_param": payload,
                    "pre_delay": 0,
                    "post_delay": 0,
                    "rate_limit": 0,
                },
        })
        job = session.tasker.post_task(_NODE)
        job.wait()
        detail = session.tasker.get_task_detail(job.job_id)
        node = detail.nodes[0] if detail and detail.nodes else None
        raw = node.recognition.best_result.detail if node and node.recognition and node.recognition.best_result else None

        if isinstance(raw, (str, bytes)):
            return json.loads(raw)
        if isinstance(raw, dict):
            return raw
        raise RuntimeError(f"navmesh agent 无响应 (op={op})")

    def mesh_bytes(self, zone_id: int) -> bytes | None:
        """某区所属几何区的 NMSH 缓冲 (tier 解析到父几何区); 无三角面时返回 None。"""
        # 缓存键要等 _geom_of 建起来才算得准, 否则同一份 mesh 会按各个 tier 再各存一份。
        self._await_ready()
        geom_id = self._geom_of.get(int(zone_id), int(zone_id))
        cached = self._mesh_cache.get(geom_id)
        if cached is not None:
            return cached

        with tempfile.NamedTemporaryFile(suffix=".nmsh", delete=False) as handle:
            out_path = Path(handle.name)
        try:
            result = self.query("mesh", zone_id=zone_id, out_file=str(out_path))
            if not result.get("ok"):
                return None
            data = out_path.read_bytes()
        finally:
            out_path.unlink(missing_ok=True)

        self._mesh_cache[geom_id] = data
        return data

    def warm(self, zone_id: int) -> None:
        """后台把某区的规划网格建起来, 免得首条路线冷吃这份开销。"""

        def _run() -> None:
            try:
                self.query("warm", zone_id=zone_id)
            except Exception:  # noqa: BLE001 —— 预热失败不影响后续查询
                pass

        threading.Thread(target=_run, name="navmesh-warm", daemon=True).start()
