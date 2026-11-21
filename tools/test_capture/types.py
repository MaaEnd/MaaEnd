"""与 maafw 打交道的类型。

maa 包没有类型存根，框架对象在这里统一收敛成 Protocol，其余模块只依赖这些接口。
"""

from __future__ import annotations

from pathlib import Path
from typing import Any, Protocol

import numpy as np
from numpy.typing import NDArray

Image = NDArray[np.uint8]
NodeData = dict[str, Any]
Recognition = str | NodeData
Box = tuple[int, int, int, int]


Point = tuple[int, int]


class Capture(Protocol):
    """流程函数拿到的操作对象：等界面、截图、点击、转跳。"""

    def wait(self, node: str, *, timeout: float = ..., interval: float = ...) -> Box: ...

    def hit(self, node: str) -> bool: ...

    def text(self, node: str, *, timeout: float = ..., interval: float = ...) -> str: ...

    def ocr(self, roi: Box, *, expected: str = ...) -> str: ...

    def find_text(self, needle: str | list[str], *, roi: Box) -> Point | None: ...

    def number(self, roi: Box) -> int: ...

    def shot(self, name: str, *, settle: float | None = ...) -> None: ...

    def click(self, node: str, *, timeout: float = ..., interval: float = ...) -> None: ...

    def click_at(self, point: Point, *, offset: Point = ...) -> None: ...

    def swipe(self, begin: Point, end: Point, *, duration: int = ...) -> None: ...

    def sub_task(self, node: str) -> None: ...

    def run(self, node: str) -> None: ...

    def sleep(self, seconds: float) -> None: ...


class Job(Protocol):
    def wait(self) -> Job: ...

    @property
    def succeeded(self) -> bool: ...


class ScreencapJob(Protocol):
    def wait(self) -> ScreencapJob: ...

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

    def add_context_sink(self, sink: Any) -> int | None: ...

    def remove_context_sink(self, sink_id: int) -> None: ...


class AgentClient(Protocol):
    def bind(self, resource: Resource) -> bool: ...

    def connect(self) -> bool: ...

    def disconnect(self) -> None: ...

    @property
    def custom_recognition_list(self) -> list[str]: ...
