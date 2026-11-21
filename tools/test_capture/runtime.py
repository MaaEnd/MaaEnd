"""MaaFramework 运行时：动态库、调试模式、资源目录。"""

# maa 包没有类型存根，这里的框架对象都当 Any 用，作用域尽量小。
# pyright: reportMissingTypeStubs=false

from __future__ import annotations

import os
from pathlib import Path
from typing import Any

from .flow import FlowError, as_dict, as_list, load_jsonc

ROOT = Path(__file__).resolve().parents[2]
ASSETS = ROOT / "assets"
INTERFACE_PATH = ASSETS / "interface.json"
LOG_DIR = ROOT / ".cache/test-capture/logs"
AGENT_DIR = ROOT / "install/agent"
PRETASK_DIR = ASSETS / "tasks" / "pretasks"

_maafw_dir: Path | None = None


def framework_library_dir(explicit: Path | None) -> Path | None:
    """优先用仓库构建出来的 maafw 动态库，和 MXU 跑的是同一份。"""
    if explicit is not None:
        return explicit
    candidate = ROOT / "install/maafw"
    return candidate if candidate.is_dir() else None


def prepare_framework(explicit: Path | None) -> None:
    """加载框架动态库，并打开调试模式。

    只有调试模式才拿得到识别用的那一帧画面，所以这里不提供开关。
    """
    library_dir = framework_library_dir(explicit)
    if library_dir is not None:
        os.environ["MAAFW_BINARY_PATH"] = str(library_dir)
    from maa.library import Library

    if library_dir is not None:
        # open() 没有返回值，加载失败会让后面的 import 直接抛错。
        Library.open(library_dir)
    from maa.tasker import Tasker
    from maa.toolkit import Toolkit

    global _maafw_dir
    _maafw_dir = library_dir
    Toolkit.init_option(LOG_DIR)
    Tasker.set_debug_mode(True)
    Tasker.set_save_draw(False)
    Tasker.set_reco_image_cache_limit(64)


def maafw_dir() -> Path:
    """prepare_framework 之后才知道的框架目录，Agent 也要用它。"""
    if _maafw_dir is None:
        raise FlowError("框架还没初始化，先调用 prepare_framework")
    return _maafw_dir


def load_task(name: str) -> dict[str, Any]:
    """读 assets/tasks 里的任务定义。"""
    path = ASSETS / "tasks" / f"{name}.json"
    if not path.is_file():
        raise FlowError(f"找不到任务定义：{path}")
    return load_jsonc(path)


def load_interface() -> dict[str, Any]:
    return load_jsonc(INTERFACE_PATH)


def pretask_for(controller_name: str) -> dict[str, Any] | None:
    """这个控制器登记的启动前任务（PI 的 pretask），没有就返回 None。

    CloudWeb 的云网页外壳、Win32-Front 的游戏都由它在连接控制器之前拉起来。
    """
    if not PRETASK_DIR.is_dir():
        return None
    for path in sorted(PRETASK_DIR.glob("*.json")):
        pretask = as_dict(load_jsonc(path).get("pretask"))
        if controller_name in [str(item) for item in as_list(pretask.get("controller"))]:
            return pretask
    return None


def controller_entry(interface: dict[str, Any], name: str) -> dict[str, Any]:
    entries = [as_dict(item) for item in as_list(interface.get("controller"))]
    for entry in entries:
        if entry.get("name") == name:
            return entry
    available = [entry.get("name") for entry in entries]
    raise FlowError(f"interface.json 里没有控制器 {name}，可选：{available}")


def resource_bundles(interface: dict[str, Any], controller_name: str) -> list[Path]:
    """按 interface 的资源定义 + 控制器附加目录，得到要加载的 resource 目录。"""
    bundles: list[Path] = []
    for item in as_list(interface.get("resource")):
        for path in as_list(as_dict(item).get("path")):
            bundles.append(ASSETS / str(path).lstrip("./"))
    entry = controller_entry(interface, controller_name)
    for path in as_list(entry.get("attach_resource_path")):
        bundles.append(ASSETS / str(path).lstrip("./"))
    unique: list[Path] = []
    for bundle in bundles:
        if bundle.is_dir() and bundle not in unique:
            unique.append(bundle)
    return unique
