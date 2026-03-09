from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any


PROJECT_ROOT = Path(__file__).resolve().parents[2]
INSTALL_DIR = PROJECT_ROOT / "install"
AGENT_DIR = INSTALL_DIR / "agent"
MAAFW_BIN_DIR = INSTALL_DIR / "maafw"
CPP_AGENT_EXE = AGENT_DIR / "cpp-algo.exe"
RESOURCE_DIR = PROJECT_ROOT / "assets" / "resource"
MAP_IMAGE_DIR = RESOURCE_DIR / "image"


def configure_runtime_env() -> None:
    """配置 maafw 运行所需环境变量。"""
    os.environ["MAAFW_BINARY_PATH"] = str(MAAFW_BIN_DIR)


@dataclass(frozen=True)
class MaaRuntime:
    """集中持有 maa Python API 引用，避免散落在业务代码中。"""

    Library: Any
    Resource: Any
    Win32Controller: Any
    Tasker: Any
    AgentClient: Any


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
    except ImportError:
        return None

    return MaaRuntime(
        Library=Library,
        Resource=Resource,
        Win32Controller=Win32Controller,
        Tasker=Tasker,
        AgentClient=AgentClient,
    )
