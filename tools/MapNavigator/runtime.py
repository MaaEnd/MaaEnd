from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any


PROJECT_ROOT = Path(__file__).resolve().parents[2]
INSTALL_DIR = PROJECT_ROOT / "install"
AGENT_DIR = INSTALL_DIR / "agent"
MAAFW_BIN_DIR = INSTALL_DIR / "maafw"
RESOURCE_DIR = PROJECT_ROOT / "assets" / "resource"
MAP_IMAGE_DIR = RESOURCE_DIR / "image"
RESOURCE_ADB_DIR = PROJECT_ROOT / "assets" / "resource_adb"


def _resolve_cpp_agent_executable() -> Path:
    candidates = [
        AGENT_DIR / "cpp-algo.exe",
        AGENT_DIR / "cpp-algo",
    ]
    for candidate in candidates:
        if candidate.exists():
            return candidate
    return candidates[0]


CPP_AGENT_EXE = _resolve_cpp_agent_executable()


def get_agent_env() -> dict[str, str]:
    """获取启动 Agent 专用的环境变量，注入 DLL 搜索路径。"""
    import os
    env = os.environ.copy()
    paths = [str(AGENT_DIR), str(MAAFW_BIN_DIR), env.get("PATH", "")]
    env["PATH"] = os.pathsep.join(p for p in paths if p)
    return env


def configure_runtime_env() -> None:
    """配置 maafw 运行所需环境变量。"""
    os.environ["MAAFW_BINARY_PATH"] = str(MAAFW_BIN_DIR)


@dataclass(frozen=True)
class MaaRuntime:
    """集中持有 maa Python API 引用，避免散落在业务代码中。"""

    Library: Any
    Resource: Any
    Win32Controller: Any
    AdbController: Any
    Tasker: Any
    AgentClient: Any
    Toolkit: Any


def load_maa_runtime() -> MaaRuntime | None:
    """
    动态加载 maafw 依赖。

    返回 None 表示当前环境缺少 maafw，调用方应给出友好提示。
    """
    try:
        from maa.agent_client import AgentClient
        from maa.controller import Win32Controller
        from maa.library import Library
        from maa.resource import Resource
        from maa.tasker import Tasker
    except ImportError as exc:
        print(f"Error loading Maa runtime: {exc}")
        return None

    try:
        from maa.controller import AdbController
    except ImportError:
        AdbController = None

    try:
        from maa.toolkit import Toolkit
    except ImportError:
        Toolkit = None

    return MaaRuntime(
        Library=Library,
        Resource=Resource,
        Win32Controller=Win32Controller,
        AdbController=AdbController,
        Tasker=Tasker,
        AgentClient=AgentClient,
        Toolkit=Toolkit,
    )
