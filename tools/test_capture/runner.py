"""执行流程：等界面、截图、点击。"""

# pyright: reportMissingTypeStubs=false

from __future__ import annotations

import threading
import time
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any, cast

import click
import numpy as np
from PIL import Image as PILImage

from . import pretasks
from .agents import Agents, pi_environment
from .controllers import create_controller
from .flow import BASE_HEIGHT, BASE_WIDTH, Flow, FlowError, Step, as_dict, as_list
from .runtime import AGENT_DIR, LOG_DIR, controller_entry, load_task, maafw_dir, resource_bundles
from .testset import TestSetWriter
from .types import Box, Controller, Image, Point, Recognition, RecognitionDetail, Resource, Tasker

PROBE_NODE = "TestCaptureProbe"
ENTER_WORLD_NODE = "SceneAnyEnterWorld"
OPEN_GAME_NODE = "OpenGame"
OPEN_GAME_TASK = "AndroidOpenGame"
# 界面刚出来时可能有动画，截图前默认等 2 秒让画面稳定；只是等界面命中时不额外等待。
DEFAULT_WAIT = 2.0


@dataclass
class RunSettings:
    adb_path: str | None = None
    adb_serial: str | None = None
    dry_run: bool = False
    open_game: bool = True
    wait: float = DEFAULT_WAIT


def save_image(image: Image, path: Path) -> None:
    rgb = image[:, :, :3][:, :, ::-1]
    PILImage.fromarray(np.ascontiguousarray(rgb)).save(path)


def recognition_text(detail: RecognitionDetail | None) -> str:
    """从识别结果里取 OCR 文字；非 OCR 节点返回空串。"""
    if detail is None:
        return ""
    text = getattr(detail.best_result, "text", None)
    if isinstance(text, str):
        return text
    for key in ("text", "detail"):
        value = detail.raw_detail.get(key)
        if isinstance(value, str):
            return value
        if isinstance(value, dict):
            inner: Any = cast("dict[str, Any]", value).get("text")
            if isinstance(inner, str):
                return inner
    return ""


def box_center(box: Box | None) -> tuple[int, int] | None:
    if not box:
        return None
    x, y, width, height = box
    if width <= 0 or height <= 0:
        return None
    return x + width // 2, y + height // 2


class Session:
    """一次运行里的操作对象：流程函数通过它等界面、截图、点击。"""

    def __init__(
        self,
        tasker: Tasker,
        resource: Resource,
        controller: Controller,
        name: str,
        out_dir: Path,
        dry_run: bool,
        default_wait: float = DEFAULT_WAIT,
        testset: TestSetWriter | None = None,
    ) -> None:
        self.tasker = tasker
        self.resource = resource
        self.controller = controller
        self.name = name
        self.out_dir = out_dir
        self.dry_run = dry_run
        self.default_wait = default_wait
        self.testset = testset
        self.captured = 0
        self.records: list[dict[str, Any]] = []
        self.clicked: list[tuple[int, int]] = []
        self._fallback_image: Image | None = None
        self.details: dict[str, RecognitionDetail] = {}
        self.lock = threading.Lock()
        from maa.context import ContextEventSink
        from maa.event_sink import NotificationType

        self.succeeded = NotificationType.Succeeded
        runner = self

        class Sink(ContextEventSink):  # pyright: ignore[reportUntypedBaseClass]
            def on_node_recognition(self, context: Any, noti_type: Any, detail: Any) -> None:
                if noti_type != runner.succeeded:
                    return
                reco = runner.tasker.get_recognition_detail(detail.reco_id)
                if reco is None:
                    return
                with runner.lock:
                    runner.details[detail.name] = reco

        self.sink_id = tasker.add_context_sink(Sink())

    def close(self) -> None:
        if self.sink_id:
            self.tasker.remove_context_sink(self.sink_id)

    def recognition_of(self, node: str) -> Recognition:
        data = self.resource.get_node_data(node)
        if data is None:
            raise FlowError(f"资源里没有节点 {node}")
        recognition: Recognition = data.get("recognition") or "DirectHit"
        kind = recognition if isinstance(recognition, str) else recognition.get("type")
        if kind == "DirectHit":
            raise FlowError(f"节点 {node} 是 DirectHit，永远命中，当不了界面判断；换个有识别的节点")
        return recognition

    def probe(self, node: str) -> RecognitionDetail | None:
        """跑一次该节点的识别，返回识别结果（含命中框和那一帧画面）。"""
        recognition = self.recognition_of(node)
        with self.lock:
            self.details.pop(PROBE_NODE, None)
        harness: dict[str, Any] = {
            PROBE_NODE: {
                "recognition": recognition,
                "action": "DoNothing",
                "next": [],
                "pre_delay": 0,
                "post_delay": 0,
            }
        }
        self.tasker.post_task(PROBE_NODE, harness).wait()
        with self.lock:
            return self.details.get(PROBE_NODE)

    def wait_for(self, node: str, timeout: float, interval: float) -> tuple[RecognitionDetail, int]:
        deadline = time.monotonic() + timeout
        attempt = 0
        while True:
            attempt += 1
            detail = self.probe(node)
            if detail is not None and detail.hit:
                return detail, attempt
            if time.monotonic() >= deadline:
                raise FlowError(f"{timeout:g} 秒内没等到节点 {node} 命中（试了 {attempt} 次）")
            time.sleep(interval)

    def capture(self) -> Image:
        return self.controller.post_screencap().wait().get()

    # ---- 流程里能用的接口 -------------------------------------------------

    def sleep(self, seconds: float) -> None:
        """硬等若干秒；等界面优先用 wait，这里只给动画之类确实需要等的场景。"""
        if seconds > 0:
            time.sleep(seconds)

    def hit(self, node: str) -> bool:
        """看一眼这个节点现在命不命中，不等待。"""
        detail = self.probe(node)
        return detail is not None and detail.hit

    def text(self, node: str, *, timeout: float = 20.0, interval: float = 0.5) -> str:
        """等节点命中，返回它 OCR 认出来的文字。"""
        detail, _ = self.wait_for(node, timeout, interval)
        return recognition_text(detail)

    def ocr(self, roi: Box, *, expected: str = ".+") -> str:
        """临时在一小块区域上做一次文字识别，用来读界面上的数字。"""
        x, y, width, height = roi
        node: dict[str, Any] = {
            "recognition": "OCR",
            "roi": [int(x), int(y), int(width), int(height)],
            "expected": [expected],
            "action": "DoNothing",
            "next": [],
            "pre_delay": 0,
            "post_delay": 0,
        }
        with self.lock:
            self.details.pop(PROBE_NODE, None)
        self.tasker.post_task(PROBE_NODE, {PROBE_NODE: node}).wait()
        with self.lock:
            detail = self.details.get(PROBE_NODE)
        return recognition_text(detail)

    def find_text(self, needle: str | list[str], *, roi: Box) -> Point | None:
        """在 roi 里找包含这些字的一段文字，返回它的中心点；找不到返回 None。"""
        x, y, width, height = roi
        node: dict[str, Any] = {
            "recognition": "OCR",
            "roi": [int(x), int(y), int(width), int(height)],
            "expected": [".+"],
            "action": "DoNothing",
            "next": [],
            "pre_delay": 0,
            "post_delay": 0,
        }
        with self.lock:
            self.details.pop(PROBE_NODE, None)
        self.tasker.post_task(PROBE_NODE, {PROBE_NODE: node}).wait()
        with self.lock:
            detail = self.details.get(PROBE_NODE)
        if detail is None:
            return None
        needles = [needle] if isinstance(needle, str) else list(needle)
        for result in detail.all_results:
            text = getattr(result, "text", "")
            if not isinstance(text, str) or not any(item in text for item in needles):
                continue
            center = box_center(cast("Box | None", getattr(result, "box", None)))
            if center is not None:
                return center
        return None

    def number(self, roi: Box) -> int:
        """读一小块区域里的数字，读不出来当 0。"""
        digits = "".join(char for char in self.ocr(roi) if char.isdigit())
        return int(digits) if digits else 0

    def wait(self, node: str, *, timeout: float = 20.0, interval: float = 0.5) -> Box:
        """等节点命中，返回它的识别框；不额外等待，要等画面稳定用 shot 的 settle。"""
        detail, _ = self.wait_for(node, timeout, interval)
        self._fallback_image = detail.raw_image
        return detail.box if detail.box is not None else (0, 0, 0, 0)

    def shot(self, name: str, *, settle: float | None = None) -> None:
        """截当前画面存进输出目录；截之前先等画面稳定（默认 --wait 秒，可传 settle 改）。

        开了 --testset 就再脱敏一份进测试集。
        """
        self.sleep(self.default_wait if settle is None else settle)
        image = self.capture()
        if image.size == 0 and self._fallback_image is not None:
            image = self._fallback_image
        self._fallback_image = None
        if image.size == 0:
            raise FlowError("截图返回空画面")
        height, width = int(image.shape[0]), int(image.shape[1])
        if (width, height) != (BASE_WIDTH, BASE_HEIGHT):
            click.echo(f"[{self.name}] 注意：画面是 {width}x{height}，不是 {BASE_WIDTH}x{BASE_HEIGHT}")
        self.captured += 1
        path = self.out_dir / f"{self.captured:02d}_{name}.png"
        save_image(image, path)
        click.echo(f"[{self.name}] 截图 {name} -> {path.name}")
        record: dict[str, Any] = {
            "name": name,
            "image": path.name,
            "size": [width, height],
            "testsetImage": None,
            "capturedAt": datetime.now().isoformat(timespec="seconds"),
        }
        if self.testset is not None:
            record["testsetImage"] = self.testset.add(path, name)
            click.echo(f"[{self.name}] 测试图 -> {record['testsetImage']}")
        self.records.append(record)

    def click(self, node: str, *, timeout: float = 20.0, interval: float = 0.5) -> None:
        """点节点识别到的位置。"""
        detail, _ = self.wait_for(node, timeout, interval)
        center = box_center(detail.box)
        if center is None:
            raise FlowError(f"节点 {node} 命中了，但没有能点的区域")
        self.click_at(center)

    def click_at(self, point: Point, *, offset: Point = (0, 0)) -> None:
        """点固定的坐标（以 1280x720 为基准）。"""
        x, y = int(point[0]) + int(offset[0]), int(point[1]) + int(offset[1])
        if self.dry_run:
            click.echo(f"[{self.name}] 跳过点击 ({x}, {y})（dry-run）")
            return
        if not self.controller.post_click(x, y).wait().succeeded:
            raise FlowError(f"点击 ({x}, {y}) 失败")
        self.clicked.append((x, y))
        click.echo(f"[{self.name}] 点击 ({x}, {y})")

    def swipe(self, begin: Point, end: Point, *, duration: int = 500) -> None:
        """从 begin 滑动到 end，用来拖滑条、翻列表。"""
        if self.dry_run:
            click.echo(f"[{self.name}] 跳过滑动 {begin} -> {end}（dry-run）")
            return
        node: dict[str, Any] = {
            "recognition": "DirectHit",
            "action": "Swipe",
            "begin": [int(begin[0]), int(begin[1])],
            "end": [int(end[0]), int(end[1])],
            "duration": int(duration),
            "next": [],
            "pre_delay": 0,
            "post_delay": 0,
        }
        click.echo(f"[{self.name}] 滑动 {begin} -> {end}")
        if not self.tasker.post_task(PROBE_NODE, {PROBE_NODE: node}).wait().succeeded:
            raise FlowError(f"滑动 {begin} -> {end} 没成功")

    def run(self, node: str) -> None:
        """按这个节点自己的识别和动作执行一次，不跟它后面的 next。"""
        if self.resource.get_node_data(node) is None:
            raise FlowError(f"资源里没有节点 {node}")
        click.echo(f"[{self.name}] 执行 {node}")
        if self.dry_run:
            return
        if not self.tasker.post_task(node, {node: {"next": []}}).wait().succeeded:
            raise FlowError(f"节点 {node} 没执行成功")

    def sub_task(self, node: str) -> None:
        """跑一个现成的节点，让它自己把界面转跳到目标。"""
        if self.resource.get_node_data(node) is None:
            raise FlowError(f"资源里没有节点 {node}")
        click.echo(f"[{self.name}] 转跳 {node}")
        if self.dry_run:
            return
        if not self.tasker.post_task(node).wait().succeeded:
            raise FlowError(f"节点 {node} 没跑成功，转跳可能被弹窗挡住了")

    def goto(self, enter: str, route: str | None = None) -> None:
        """先确认在这个界面，不在就转跳过去，到了再往下走。"""
        if self.hit(enter):
            return
        target = route or ENTER_WORLD_NODE
        if self.dry_run:
            raise FlowError(f"现在不在 {enter}，dry-run 不点击、转跳不了；先把游戏切到这个界面再跑")
        click.echo(f"[{self.name}] 不在 {enter}，转跳 {target}")
        self.sub_task(target)
        self.wait(enter)

    def run_step(self, index: int, step: Step) -> dict[str, Any]:
        """跑一步：先到它要求的界面，再把操作对象交给流程函数。"""
        click.echo(f"[{self.name}] 第 {index} 步 {step.name}")
        if step.enter is not None:
            self.goto(step.enter, step.route)
        self.records.clear()
        self.clicked.clear()
        step.func(self)
        return {
            "index": index,
            "step": step.name,
            "shots": list(self.records),
            "clicked": [list(point) for point in self.clicked],
        }


def start_app_override(controller_name: str) -> dict[str, Any]:
    """从 AndroidOpenGame 任务的默认选项里取「点哪个包启动游戏」。"""
    task = load_task(OPEN_GAME_TASK)
    options = as_dict(task.get("option"))
    group = "ClientVersionCloudLocked" if controller_name.startswith("Cloud") else "ClientVersion"
    option = as_dict(options.get(group))
    default = option.get("default_case")
    for case in as_list(option.get("cases")):
        data = as_dict(case)
        if data.get("name") == default:
            return as_dict(data.get("pipeline_override"))
    raise FlowError(f"任务 {OPEN_GAME_TASK} 里没有 {group} 的默认客户端，不知道要用哪个包启动游戏")


def open_game(tasker: Tasker, resource: Resource, interface: dict[str, Any], controller_name: str) -> None:
    """跑现成的 OpenGame 任务：已经在游戏里就直接过，没开就按客户端包启动并等进登录界面。"""
    if resource.get_node_data(OPEN_GAME_NODE) is None:
        raise FlowError(f"资源里没有节点 {OPEN_GAME_NODE}")
    override = start_app_override(controller_name)
    click.echo(f"[{controller_name}] 执行 {OPEN_GAME_NODE} 打开游戏")
    if not tasker.post_task(OPEN_GAME_NODE, override).wait().succeeded:
        raise FlowError(f"{OPEN_GAME_NODE} 没跑成功；Agent 日志在 {LOG_DIR}/agent-*.log")


def enter_world(tasker: Tasker, resource: Resource, controller_name: str) -> None:
    """跑既有的 SceneAnyEnterWorld 节点，把游戏带回大世界。"""
    if resource.get_node_data(ENTER_WORLD_NODE) is None:
        raise FlowError(f"资源里没有节点 {ENTER_WORLD_NODE}，没法先回大世界")
    click.echo(f"[{controller_name}] 执行 {ENTER_WORLD_NODE} 回到大世界")
    job = tasker.post_task(ENTER_WORLD_NODE).wait()
    if not job.succeeded:
        raise FlowError(f"{ENTER_WORLD_NODE} 没跑成功，游戏可能停在弹窗或登录界面；Agent 日志在 {LOG_DIR}/agent-*.log")
    click.echo(f"[{controller_name}] 已回到大世界")


def run_controller(
    interface: dict[str, Any],
    flow: Flow,
    controller_name: str,
    out_dir: Path,
    settings: RunSettings,
    testset: TestSetWriter | None = None,
) -> dict[str, Any]:
    from maa.resource import Resource as MaaResource
    from maa.tasker import Tasker as MaaTasker

    result: dict[str, Any] = {
        "controller": controller_name,
        "dir": str(out_dir),
        "steps": [],
        "error": None,
    }
    kind = str(controller_entry(interface, controller_name).get("type") or "")
    env_extra = pi_environment(interface, controller_name, LOG_DIR)
    if settings.open_game:
        # 云网页版的外壳、桌面版客户端都由控制器自己的预任务拉起；已经在跑就直接过。
        pretasks.run(controller_name, env_extra)
    controller, description = create_controller(interface, controller_name, settings.adb_path, settings.adb_serial)
    click.echo(f"[{controller_name}] 连接 {description}")
    if not controller.post_connection().wait().succeeded:
        raise FlowError(f"控制器 {controller_name} 连接失败")

    resource = cast(Resource, MaaResource())
    for bundle in resource_bundles(interface, controller_name):
        if not resource.post_bundle(bundle).wait().succeeded:
            raise FlowError(f"加载资源失败: {bundle}")
    tasker = cast(Tasker, MaaTasker())
    if not tasker.bind(resource, controller) or not tasker.inited:
        raise FlowError("Tasker 初始化失败")

    # Agent 常驻：流程里引用的节点可能是自定义识别（MapFind 之类），也可能是内置识别，
    # 都走同一套探测逻辑，所以整场都让它们挂着。
    agents = Agents(AGENT_DIR, maafw_dir(), LOG_DIR, env_extra)
    agents.start(resource)
    agents.wait_ready()
    click.echo(f"[{controller_name}] Agent 已就绪，可以用任意节点做匹配和点击")
    try:
        if settings.open_game:
            if kind == "Adb":
                # 安卓端先跑 OpenGame：没开就按客户端包启动、顺手过掉登录和排队；已经在游戏里它自己会直接过。
                open_game(tasker, resource, interface, controller_name)
                enter_world(tasker, resource, controller_name)
            else:
                # 桌面端和云网页版的窗口已经由预任务拉起来了，这里只需要回到大世界。
                try:
                    enter_world(tasker, resource, controller_name)
                except FlowError as error:
                    raise FlowError(f"[{controller_name}] 没进到游戏：{error}") from error
    except Exception:
        agents.close()
        raise

    out_dir.mkdir(parents=True, exist_ok=True)
    runner = Session(
        tasker,
        resource,
        controller,
        controller_name,
        out_dir,
        settings.dry_run,
        settings.wait,
        testset,
    )
    try:
        for index, step in enumerate(flow.steps, 1):
            try:
                result["steps"].append(runner.run_step(index, step))
            except FlowError as error:
                result["error"] = {"step": index, "name": step.name, "message": str(error)}
                failure_path = out_dir / f"{index:02d}_{step.name}_失败.png"
                try:
                    image = runner.capture()
                    if image.size:
                        save_image(image, failure_path)
                        result["error"]["image"] = failure_path.name
                except Exception:  # 失败截图失败就不再掩盖原始错误
                    pass
                raise FlowError(
                    f"[{controller_name}] 第 {index} 步「{step.name}」失败：{error}；现场截图已存到 {failure_path}"
                ) from error
    finally:
        runner.close()
        agents.close()
    return result
