"""与 maafw 打交道的类型。

maa 包没有类型存根，框架对象在这里统一收敛成 Protocol，其余模块只依赖这些接口。
"""

from __future__ import annotations

from collections.abc import Callable
from pathlib import Path
from typing import Any, Protocol

import numpy as np
from numpy.typing import NDArray

from .node_names import NodeName

Image = NDArray[np.uint8]
NodeData = dict[str, Any]
Recognition = str | NodeData
Box = tuple[int, int, int, int]


Point = tuple[int, int]


class Capture(Protocol):
    """流程函数拿到的操作对象：等界面、截图、点击、转跳。

    节点名都填 MaaEnd Pipeline 里现成的节点，模板匹配、OCR、自定义识别都行；
    节点名写成 Nodes.节点名，生成过 node_names.pyi 的话编辑器会补全节点名、显示节点说明、标出写错的。
    坐标和识别框都按 1280x720 写，脚本不做分辨率换算，换平台要自己核对。
    开了 --dry-run 时点击和滑动会跳过，识别和截图照常做。
    """

    def wait(self, node: NodeName, *, timeout: float = ..., interval: float = ...) -> Box:
        """等节点命中，返回它的识别框 (x, y, w, h)；只等命中，不额外等画面稳定。

        每隔 interval 秒看一次，超过 timeout 秒还没命中就报错。
        """
        ...

    def hit(self, node: NodeName) -> bool:
        """看一眼现在命不命中，不等待、不重试。用来写 if 判断界面状态。"""
        ...

    def text(self, node: NodeName, *, timeout: float = ..., interval: float = ...) -> str:
        """等节点命中，返回它 OCR 认出来的文字。"""
        ...

    def ocr(self, roi: Box, *, expected: str = ...) -> str:
        """临时在 roi 这一小块上认一次字，用来读界面上的文字；返回认到的全部文字。"""
        ...

    def find_text(self, needle: str | list[str], *, roi: Box) -> Point | None:
        """在 roi 里找包含这些字的一段文字，返回它的中心点；找不到返回 None。"""
        ...

    def number(self, roi: Box) -> int:
        """读 roi 里的数字，只留数字字符；读不出来当 0。"""
        ...

    def shot_when(self, node: NodeName, name: str, *, settle: float | None = ...) -> None:
        """等这个节点命中，再截当前画面存成 name。

        绝大多数图都是「某个界面出来了才拍」，写成一行比 wait 加 shot 短。
        """
        ...

    def shot(self, name: str, *, settle: float | None = ...) -> None:
        """截当前画面存进输出目录，文件名带 name；截之前先等画面稳定。

        settle 不写就等 --wait 秒（默认 2 秒），界面动得慢就单独给这张图调大。
        开了 --testset 时还会顺手脱敏导入测试集。
        """
        ...

    def click(self, node: NodeName, *, timeout: float = ..., interval: float = ...) -> None:
        """等节点命中，点它识别框的中心；命中了但没有能点的区域会报错。"""
        ...

    def click_at(self, point: Point, *, offset: Point = ...) -> None:
        """点固定坐标（以 1280x720 为基准），offset 用来微调。"""
        ...

    def swipe(self, begin: Point, end: Point, *, duration: int = ...) -> None:
        """从 begin 滑到 end，duration 是毫秒；用来拖滑条、翻列表。"""
        ...

    def sub_task(self, node: NodeName) -> None:
        """跑一个现成节点的整条流程（会跟 next），让它自己把界面转跳到目标。"""
        ...

    def goto(self, enter: NodeName, route: NodeName | Callable[[Capture], None] | None = ...) -> None:
        """先确认在 enter 这个界面；不在就 sub_task(route) 转过去，到了再往下走。

        route 不写就退回 SceneAnyEnterWorld 回大世界。每个步骤开跑前会自动调一次。
        """
        ...

    def run(self, node: NodeName) -> None:
        """按节点自己的识别和动作执行一次，不跟它的 next。"""
        ...

    def sleep(self, seconds: float) -> None:
        """硬等若干秒。能不用就不用，等界面优先用 wait。"""
        ...


class Job(Protocol):
    def wait(self) -> Job: ...

    @property
    def done(self) -> bool: ...

    @property
    def succeeded(self) -> bool: ...


class ScreencapJob(Protocol):
    def wait(self) -> ScreencapJob: ...

    @property
    def done(self) -> bool: ...

    def get(self) -> Image: ...


class RecognitionDetail(Protocol):
    hit: bool
    box: Box | None
    # OCR 的结果是 OCRResult（有 .text），其他算法是别的类型，所以用 Any。
    best_result: Any
    all_results: list[Any]
    raw_detail: dict[str, Any]
    # 只有调试模式下才有这一帧画面，所以可能是 None。
    raw_image: Image | None


class Resource(Protocol):
    @property
    def node_list(self) -> list[str]: ...

    def post_bundle(self, path: Path) -> Job: ...

    def get_node_data(self, name: str) -> NodeData | None: ...


class Controller(Protocol):
    def post_connection(self) -> Job: ...

    def post_screencap(self) -> ScreencapJob: ...

    def post_click(self, x: int, y: int) -> Job: ...


class Tasker(Protocol):
    def bind(self, resource: Resource, controller: Controller) -> bool: ...

    @property
    def inited(self) -> bool: ...

    def get_recognition_detail(self, reco_id: int) -> RecognitionDetail | None: ...

    def post_task(self, entry: str, pipeline_override: dict[str, Any] | None = None) -> Job: ...

    def post_stop(self) -> Job: ...

    def add_context_sink(self, sink: Any) -> int | None: ...

    def remove_context_sink(self, sink_id: int) -> None: ...


class AgentClient(Protocol):
    def bind(self, resource: Resource) -> bool: ...

    def connect(self) -> bool: ...

    def disconnect(self) -> None: ...

    @property
    def custom_recognition_list(self) -> list[str]: ...
