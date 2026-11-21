"""托管 install/agent 下的 go-service 与 cpp-algo。

流程里的步骤可以直接引用自定义识别的节点（MapFind 之类），回到大世界用的 SceneAnyEnterWorld
也依赖它们注册的识别和动作，所以整场运行期间都挂着，跑完再收掉。
"""

# pyright: reportMissingTypeStubs=false

from __future__ import annotations

import json
import os
import subprocess
import time
import uuid
from pathlib import Path
from typing import Any, cast

from .flow import FlowError
from .types import AgentClient as AgentClientProtocol
from .types import Resource

# 顺序无所谓，但两个都要起：场景导航用 go-service 的动作，地图相关识别在 cpp-algo。
AGENT_NAMES = ("go-service", "cpp-algo")
READY_TIMEOUT = 60.0


class Agents:
    """托管 install/agent 里的可执行文件及其 AgentClient。"""

    def __init__(
        self,
        agent_dir: Path,
        maafw_dir: Path,
        log_dir: Path,
        env_extra: dict[str, str],
    ) -> None:
        self.agent_dir = agent_dir
        self.maafw_dir = maafw_dir
        self.log_dir = log_dir
        self.env_extra = env_extra
        self.clients: list[AgentClientProtocol] = []
        self.processes: list[subprocess.Popen[bytes]] = []

    def executable(self, name: str) -> Path:
        suffix = ".exe" if os.name == "nt" else ""
        return self.agent_dir / f"{name}{suffix}"

    def _env(self) -> dict[str, str]:
        env = os.environ.copy()
        env["PATH"] = os.pathsep.join([str(self.agent_dir), str(self.maafw_dir), env.get("PATH", "")])
        env["LD_LIBRARY_PATH"] = os.pathsep.join([str(self.maafw_dir), env.get("LD_LIBRARY_PATH", "")])
        env.update(self.env_extra)
        return env

    def start(self, resource: Resource) -> None:
        from maa.agent_client import AgentClient

        self.log_dir.mkdir(parents=True, exist_ok=True)
        env = self._env()
        for name in AGENT_NAMES:
            executable = self.executable(name)
            if not executable.is_file():
                raise FlowError(f"缺少 Agent 可执行文件 {executable}，先跑 uv run build-and-install")
            identifier = f"test-capture-{name}-{uuid.uuid4().hex[:8]}"
            client = cast(AgentClientProtocol, AgentClient(identifier))
            if not client.bind(resource):
                raise FlowError(f"Agent {name} 绑定资源失败")
            log_file = (self.log_dir / f"agent-{name}.log").open("wb")
            creationflags = int(getattr(subprocess, "CREATE_NO_WINDOW", 0)) if os.name == "nt" else 0
            process = subprocess.Popen(
                [str(executable), identifier],
                cwd=self.agent_dir.parent,
                env=env,
                stdout=log_file,
                stderr=subprocess.STDOUT,
                creationflags=creationflags,
            )
            self.clients.append(client)
            self.processes.append(process)
            if not client.connect():
                raise FlowError(f"Agent {name} 连接失败，详见 {self.log_dir}")
            if process.poll() is not None:
                raise FlowError(f"Agent {name} 启动后立刻退出（{process.returncode}），详见 {self.log_dir}")

    def wait_ready(self) -> None:
        """等两个 Agent 把自定义组件注册完，否则第一个任务会找不到它们。"""
        deadline = time.monotonic() + READY_TIMEOUT
        while time.monotonic() < deadline:
            sizes = [len(client.custom_recognition_list) for client in self.clients]
            if all(size > 0 for size in sizes):
                time.sleep(0.5)
                if all(
                    len(client.custom_recognition_list) == size
                    for client, size in zip(self.clients, sizes, strict=True)
                ):
                    return
            time.sleep(0.3)
        raise FlowError(f"等 Agent 注册超时，详见 {self.log_dir}")

    def close(self) -> None:
        for process in self.processes:
            if process.poll() is None:
                process.terminate()
        for process in self.processes:
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
        for client in self.clients:
            client.disconnect()
        self.processes.clear()
        self.clients.clear()


def pi_environment(interface: dict[str, Any], controller_name: str, log_dir: Path) -> dict[str, str]:
    """按 PI 协议拼出 Agent 需要的环境变量（都是单行 JSON）。"""
    controller = next(
        (item for item in interface.get("controller", []) if item.get("name") == controller_name),
        None,
    )
    resource = next((item for item in interface.get("resource", []) if item.get("name") == "官服"), None)
    if controller is None or resource is None:
        raise FlowError("interface.json 里没有对应的控制器或资源定义")
    return {
        "PI_CONTROLLER": json.dumps(controller, ensure_ascii=False),
        "PI_RESOURCE": json.dumps(resource, ensure_ascii=False),
        "PI_USER_DATA_DIR": str(log_dir),
    }
