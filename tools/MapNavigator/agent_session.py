"""起 cpp Agent 并把它接成一个可用的 Tasker: 进程 -> 运行时 -> 控制器 -> Resource/AgentClient -> Tasker。

录制、实机试跑、单次定位、navmesh 查询都走这一整套, 步骤与顺序完全一样, 只有控制器从哪来、
Agent 叫什么、要不要预置节点不同。热键不在这里: key_listener 是模块级单例, 由各自的服务自己管。
"""

from __future__ import annotations

import os
import subprocess
import time
from typing import Any, Protocol

from runtime import AGENT_DIR, CPP_AGENT_EXE, MAAFW_BIN_DIR, MaaRuntime, get_agent_env, new_agent_id

# Agent 起来到能接受连接的等待时间。
BOOT_WAIT_SECONDS = 2.0

# go-service: 正式包的第二个 agent。挂索链 MapNavigatorZiplineMount 的
# custom_action=AutoAltClickAction (按住 Alt 点击交互提示) 注册在这里,
# 缺了它 OCR 识别到提示也点不了。与 cpp-algo 同一 Resource, 双 client 并挂。
GO_SERVICE_EXE = AGENT_DIR / "go-service.exe"


def _agent_process_options() -> dict[str, Any]:
    """让 Agent 不继承启动终端的 Ctrl+C，由会话生命周期负责关闭。"""
    if os.name == "nt":
        return {"creationflags": subprocess.CREATE_NEW_PROCESS_GROUP}
    return {"start_new_session": True}


class Connector(Protocol):
    """会话对连接方式的全部要求: 交出一个连好的控制器, 并把该挂的资源挂到 Resource 上。"""

    def connect(self) -> Any: ...

    def attach_resource(self, resource: Any) -> None: ...


class AgentSession:
    """一次 Agent + Tasker 的生命周期。open 成功后 tasker/resource 可用, close 反向拆掉。

    诊断全程走 print: 提权子进程里 stdout 被接到父进程日志, 删了就什么都看不见。
    """

    def __init__(self, runtime: MaaRuntime) -> None:
        self._runtime = runtime
        self._process: subprocess.Popen | None = None
        # 控制器与 AgentClient 析构即销毁底层句柄, 必须由会话持有到 close, 否则 Tasker 拿着野句柄。
        self._controller: Any = None
        self._client: Any = None
        self._aux_processes: list[subprocess.Popen] = []
        self._aux_clients: list[Any] = []
        self.tasker: Any = None
        self.resource: Any = None

    @property
    def agent_exit_code(self) -> int | None:
        """Agent 进程的返回码; 还活着 (或尚未启动) 为 None。它死了框架只会静默给不出结果。"""
        return None if self._process is None else self._process.poll()

    def open(
        self,
        connector: Connector,
        *,
        agent_name: str,
        pipeline_override: dict | None = None,
        resource_dirs: list | None = None,
        aux_agents: list | None = None,
    ) -> None:
        agent_id = new_agent_id(agent_name)
        if not CPP_AGENT_EXE.exists():
            raise FileNotFoundError(f"找不到 Agent 可执行文件: {CPP_AGENT_EXE}")

        print(f"Starting Agent process: {CPP_AGENT_EXE} {agent_id}")
        self._process = subprocess.Popen(
            [str(CPP_AGENT_EXE), agent_id],
            cwd=str(AGENT_DIR),
            env=get_agent_env(),
            **_agent_process_options(),
        )

        print(f"Waiting {BOOT_WAIT_SECONDS}s for Agent to boot...")
        time.sleep(BOOT_WAIT_SECONDS)
        if self._process.poll() is not None:
            raise RuntimeError(f"Agent 启动失败，进程已退出，返回码: {self._process.returncode}")

        print("Opening runtime library...")
        try:
            self._runtime.Library.open(MAAFW_BIN_DIR)
        except Exception as exc:
            # 兼容重复初始化场景，不影响后续流程。
            print(f"Opening runtime library at {MAAFW_BIN_DIR}... Error: {exc}")

        print("Connecting controller...")
        controller = connector.connect()
        print("Controller connected.")

        print("Connecting AgentClient...")
        resource = self._runtime.Resource()
        connector.attach_resource(resource)
        # 资源目录 (pipeline/OCR 模型等) 在 bind 给 agent 前挂上。滑索挂索链
        # (MapNavigatorZiplineMount*) 是正式包 pipeline 节点, cpp 端到达塔脚后要
        # GetNodeData 读扫描配置、RunTask 跑挂索任务 —— 没有这份资源就只会走路上索提示。
        for resource_dir in resource_dirs or []:
            print(f"Loading resource dir: {resource_dir}")
            job = resource.post_bundle(str(resource_dir))
            job.wait()
            if not job.succeeded:
                raise RuntimeError(f"资源目录装载失败: {resource_dir}")
        client = self._runtime.AgentClient(identifier=agent_id)
        client.bind(resource)
        client.connect()
        if not client.connected:
            raise RuntimeError("Agent 连接失败。")
        print("AgentClient connected.")

        # 辅助 agent (go-service 等): 各自起进程、bind 同一份 Resource。Tasker 执行
        # pipeline 时按注册名分发 custom action/recognition, 谁注册谁接单。
        # go-service 按 cwd/maafw 找 MAA DLL (agent.go libDir=cwd/maafw), 正式包主程序
        # 以包根为 cwd 拉起它; 对应这里就是 install\ (MAAFW_BIN_DIR 的父目录)。
        aux_cwd = MAAFW_BIN_DIR.parent
        for aux_exe in aux_agents or []:
            if not aux_exe.exists():
                print(f"Aux agent missing, skipped: {aux_exe}")
                continue
            aux_id = new_agent_id(aux_exe.stem)
            print(f"Starting aux agent process: {aux_exe} {aux_id}")
            aux_process = subprocess.Popen(
                [str(aux_exe), aux_id],
                cwd=str(aux_cwd),
                env=get_agent_env(),
                **_agent_process_options(),
            )
            self._aux_processes.append(aux_process)
            aux_client = self._runtime.AgentClient(identifier=aux_id)
            aux_client.bind(resource)
            aux_client.connect()
            if not aux_client.connected:
                raise RuntimeError(f"辅助 Agent 连接失败: {aux_exe.name}")
            self._aux_clients.append(aux_client)
            print(f"Aux agent connected: {aux_exe.name}")

        if pipeline_override:
            resource.override_pipeline(pipeline_override)

        print("Initializing Tasker...")
        tasker = self._runtime.Tasker()
        tasker.bind(resource, controller)
        if not tasker.inited:
            raise RuntimeError("Tasker 初始化失败。")
        print("Tasker initialized.")

        self._controller = controller
        self._client = client
        self.resource = resource
        self.tasker = tasker

    def close(self) -> None:
        # 先停进程再放句柄: Agent 还活着时销毁 Tasker/Resource 会让它对着已拆的 sink 回调。
        process, self._process = self._process, None
        if process is not None:
            process.terminate()
            process.wait()
        for aux_process in self._aux_processes:
            aux_process.terminate()
        for aux_process in self._aux_processes:
            aux_process.wait()
        self._aux_processes = []
        self._aux_clients = []
        self.tasker = None
        self.resource = None
        self._client = None
        self._controller = None
