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
from typing import Any, cast, overload

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
# 转跳也可以是一段动作：有些界面没有现成节点能一步到位，只能自己一步步点进去。
RouteFunc = Callable[[Capture], None]


@dataclass
class Step:
    """流程里的一步。

    enter 是这个步骤要求的界面：开跑前先看一眼在不在，不在就转跳过去；
    route 是「不在这个界面时怎么过去」：一般写现成节点名（SceneEnter... 这类从任意界面
    进某个菜单的节点），也可以写一段流程函数，自己一步步点过去；
    不写就退回 SceneAnyEnterWorld 回大世界。
    stop 默认就是 True：每一步都是本段的结尾，所有平台跑完这一步才进下一步；
    想连着跑几步再切段，给后面的步骤写 stop=False。
    once 表示这一步只有第一次能做：第一个跑到的平台做完，后面到的平台跳过它。
    """

    name: str
    func: StepFunc
    enter: str | None = None
    route: str | RouteFunc | None = None
    stop: bool = True
    once: bool = False


class Flow:
    """一份流程：按顺序执行的一组步骤。"""

    def __init__(self, name: str) -> None:
        self.name = name
        self.steps: list[Step] = []
        # 流程文件路径，由 load_flow 填；开跑前照着它扫一遍节点名。
        self.source: Path | None = None

    @overload
    def step(
        self,
        func: StepFunc,
        /,
        *,
        enter: str | None = ...,
        route: str | RouteFunc | None = ...,
        stop: bool = ...,
        once: bool = ...,
    ) -> StepFunc: ...

    @overload
    def step(
        self,
        func: None = ...,
        /,
        *,
        enter: str,
        route: str | RouteFunc | None = ...,
        stop: bool = ...,
        once: bool = ...,
    ) -> Callable[[StepFunc], StepFunc]: ...

    def step(
        self,
        func: StepFunc | None = None,
        /,
        *,
        enter: str | None = None,
        route: str | RouteFunc | None = None,
        stop: bool = True,
        once: bool = False,
    ) -> StepFunc | Callable[[StepFunc], StepFunc]:
        """登记一步。

        光写 @flow.step 也行；写 @flow.step(enter="InWorld") 就声明这步要求的界面。
        默认每步各自成段；写 stop=False 把这一步接到上一段里。
        只能做一次的步骤写 once=True。
        """

        def register(target: StepFunc) -> StepFunc:
            if any(item.name == target.__name__ for item in self.steps):
                raise FlowError(f"步骤名重复了：{target.__name__}")
            self.steps.append(Step(name=target.__name__, func=target, enter=enter, route=route, stop=stop, once=once))
            return target

        if func is None:
            return register
        return register(func)

    def named(
        self,
        name: str,
        *,
        enter: str | None = None,
        route: str | RouteFunc | None = None,
        stop: bool = True,
        once: bool = False,
    ) -> Callable[[StepFunc], StepFunc]:
        """名字里不能写下的字符（空格之类）可以用这个指定步骤名。"""

        def register(func: StepFunc) -> StepFunc:
            if any(item.name == name for item in self.steps):
                raise FlowError(f"步骤名重复了：{name}")
            self.steps.append(Step(name=name, func=func, enter=enter, route=route, stop=stop, once=once))
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
    flow.source = path
    return flow
