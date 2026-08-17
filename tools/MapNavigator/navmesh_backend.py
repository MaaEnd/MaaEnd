"""navmesh 查询后端: 把 cpp-algo agent 当成一个常驻查询进程用。

几何解码、吸附、路线都在 agent 里算, 这边只负责拉起它、发查询、把结果交给端点。
查询节点不看画面也不发输入, 所以控制器是个空壳, 三项延迟一并归零。
"""

from __future__ import annotations

import json
import subprocess
import tempfile
import threading
from pathlib import Path
from typing import Any

from runtime import (
    AGENT_DIR,
    CPP_AGENT_EXE,
    MAAFW_BIN_DIR,
    get_agent_env,
    load_maa_runtime,
    new_agent_id,
)

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


class NavmeshBackend:
    """惰性拉起 agent 并保持连接; 首次查询时它才把 pack 读进内存。"""

    def __init__(self, navmesh_path: Path) -> None:
        self._path = navmesh_path
        self._proc: subprocess.Popen | None = None
        self._controller: Any = None
        self._resource: Any = None
        self._client: Any = None
        self._tasker: Any = None
        self._error: str | None = None
        self._ready = threading.Event()
        self._started = False
        self._start_lock = threading.Lock()
        self._query_lock = threading.Lock()
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
        if not CPP_AGENT_EXE.exists():
            raise FileNotFoundError(f"找不到 Agent 可执行文件: {CPP_AGENT_EXE}")
        try:
            runtime.Library.open(MAAFW_BIN_DIR)
        except Exception:  # noqa: BLE001 —— 已加载过时会抛, 幂等处理
            pass

        agent_id = new_agent_id("MapNavmeshAgent")
        self._proc = subprocess.Popen([str(CPP_AGENT_EXE), agent_id], cwd=str(AGENT_DIR), env=get_agent_env())
        self._controller = _make_null_controller()
        self._controller.post_connection().wait()

        self._resource = runtime.Resource()
        self._client = runtime.AgentClient(identifier=agent_id)
        self._client.bind(self._resource)
        self._client.connect()
        if not self._client.connected:
            raise RuntimeError("Agent 连接失败")

        self._tasker = runtime.Tasker()
        self._tasker.bind(self._resource, self._controller)
        if not self._tasker.inited:
            raise RuntimeError("Tasker 初始化失败")

    def close(self) -> None:
        proc, self._proc = self._proc, None
        if proc is not None and proc.poll() is None:
            proc.terminate()

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

    def _await_ready(self) -> None:
        self.ensure_loading()
        if not self._ready.wait(180.0):
            raise RuntimeError("navmesh agent 启动超时")
        if self._error is not None:
            raise RuntimeError(self._error)

    def _post(self, op: str, **params: Any) -> dict[str, Any]:
        payload = {"op": op, "navmesh_file": str(self._path), **params}
        with self._query_lock:
            # agent 死掉时框架只会静默给不出结果, 这里先把它认出来。
            if self._proc is not None and self._proc.poll() is not None:
                raise RuntimeError(f"navmesh agent 已退出 (返回码 {self._proc.returncode})")
            self._resource.override_pipeline({
                _NODE: {
                    "recognition": "Custom",
                    "custom_recognition": _NODE,
                    "custom_recognition_param": payload,
                    "pre_delay": 0,
                    "post_delay": 0,
                    "rate_limit": 0,
                },
            })
            job = self._tasker.post_task(_NODE)
            job.wait()
            detail = self._tasker.get_task_detail(job.job_id)
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
