"""流程文件的定义与加载。

流程就是一段 Python：用 `@flow.step` 标出每一步，运行时拿到一个 `Capture` 对象，
调它的 `wait` / `shot` / `click` 等方法。想加判断就直接写 `if` / `for`。

写法见 tools/test_capture/README.md，例子见 tests/OutpostTrading/capture_flow.py。
"""

from __future__ import annotations

import importlib.util
import sys
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Any, cast

from .types import Capture

BASE_WIDTH, BASE_HEIGHT = 1280, 720


def load_jsonc(path: Path) -> dict[str, Any]:
    """读允许注释的 JSON（仓库里 interface.json 带 // 注释）。"""
    import json5

    return cast("dict[str, Any]", json5.loads(path.read_text(encoding="utf-8")))


def as_list(value: Any) -> list[Any]:
    """校验成列表；不是列表就当空。"""
    return cast("list[Any]", value) if isinstance(value, list) else []


def as_dict(value: Any) -> dict[str, Any]:
    """校验成对象；不是对象就当空。"""
    return cast("dict[str, Any]", value) if isinstance(value, dict) else {}


class FlowError(RuntimeError):
    """流程执行失败，消息直接展示给用户。"""


StepFunc = Callable[[Capture], None]


@dataclass
class Step:
    """流程里的一步：一个函数，加一个给人看的名字。"""

    name: str
    func: StepFunc


class Flow:
    """一份流程：按顺序执行的一组步骤。"""

    def __init__(self, name: str) -> None:
        self.name = name
        self.steps: list[Step] = []

    def step(self, func: StepFunc) -> StepFunc:
        """把一个函数登记成一步，步骤名取函数名。"""
        if any(item.name == func.__name__ for item in self.steps):
            raise FlowError(f"步骤名重复了：{func.__name__}")
        self.steps.append(Step(name=func.__name__, func=func))
        return func

    def named(self, name: str) -> Callable[[StepFunc], StepFunc]:
        """名字里不能写下的字符（空格之类）可以用这个指定步骤名。"""

        def register(func: StepFunc) -> StepFunc:
            if any(item.name == name for item in self.steps):
                raise FlowError(f"步骤名重复了：{name}")
            self.steps.append(Step(name=name, func=func))
            return func

        return register


def load_flow(path: Path) -> Flow:
    """把流程文件当模块执行，取出里面的 root flow。"""
    if not path.is_file():
        raise FlowError(f"找不到流程文件：{path}")
    module_name = f"test_capture_flow_{path.stem}"
    spec = importlib.util.spec_from_file_location(module_name, path)
    if spec is None or spec.loader is None:
        raise FlowError(f"没法把 {path} 当模块加载")
    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module
    try:
        spec.loader.exec_module(module)
    except FlowError:
        raise
    except Exception as error:  # 流程文件自己的语法/导入错误，原样抛给用户看
        raise FlowError(f"加载 {path.name} 失败：{error}") from error
    flow = getattr(module, "flow", None)
    if not isinstance(flow, Flow):
        raise FlowError(f'{path.name} 里没有 flow 对象，写成 flow = Flow("流程名") 再往上挂步骤')
    if not flow.steps:
        raise FlowError(f"{path.name} 里一步都没登记，用 @flow.step 标出步骤")
    return flow
