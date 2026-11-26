"""执行流程：等界面、截图、点击。"""

# pyright: reportMissingTypeStubs=false

from __future__ import annotations

import threading
import time
from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any, cast

import click
import numpy as np
from PIL import Image as PILImage

from . import cancel, pretasks, validate
from .agents import Agents, pi_environment
from .controllers import create_controller
from .flow import BASE_HEIGHT, BASE_WIDTH, Flow, FlowError, RouteFunc, Step, as_dict, as_list
from .runtime import AGENT_DIR, controller_entry, load_task, log_dir, maafw_dir, resource_bundles
from .testset import TestSetError, TestSetWriter
from .types import Box, Controller, Image, Point, Recognition, RecognitionDetail, Resource, Tasker
from .validate import NodeRefs

PROBE_NODE = "TestCaptureProbe"
ENTER_WORLD_NODE = "SceneAnyEnterWorld"
OPEN_GAME_NODE = "OpenGame"
OPEN_GAME_TASK = "AndroidOpenGame"
# 临时识别任务只认一次：MaaFramework 默认认不到就每秒重试、最多 20 秒，
# 那样 hit() 在节点不在时要白等 20 秒；要等就由 wait_for 自己按 interval 重试。
PROBE_TIMEOUT = 0
# 界面刚出来时可能有动画，截图前默认等 2 秒让画面稳定；只是等界面命中时不额外等待。
DEFAULT_WAIT = 2.0
# 失败现场图的后缀：它的前缀是步骤号，跟截图的序号不是一套，算最大号时要跳过。
FAILURE_SUFFIX = "_失败"


@dataclass
class RunSettings:
    adb_path: str | None = None
    adb_serial: str | None = None
    dry_run: bool = False
    open_game: bool = True
    wait: float = DEFAULT_WAIT


def last_shot_number(out_dir: Path) -> int:
    """这个平台目录里的截图已经编到几号。

    续跑、或者同一场里换了平台又换回来（会重连），编号都得接着目录里最大的往下数；
    从 01 重来会和已有的图撞号。失败现场图不算，它的前缀是步骤号。
    """
    best = 0
    for path in out_dir.glob("*.png"):
        if path.stem.endswith(FAILURE_SUFFIX):
            continue
        head = path.name.split("_", 1)[0]
        if head.isdigit():
            best = max(best, int(head))
    return best


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
        # 目录里可能已经有别的尝试拍的图（续跑、或者中途重连过），编号接着往下数。
        self.captured = last_shot_number(out_dir)
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
                "timeout": PROBE_TIMEOUT,
            }
        }
        self.wait_job(self.tasker.post_task(PROBE_NODE, harness))
        with self.lock:
            return self.details.get(PROBE_NODE)

    def wait_job[JobT: cancel.JobLike](self, job: JobT) -> JobT:
        """等作业完成；中途按 Ctrl+C 会先停掉当前任务，再抛 Cancelled。"""
        return cancel.wait_job(job, stop=self.tasker.post_stop)

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
        return self.wait_job(self.controller.post_screencap()).get()

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
            "timeout": PROBE_TIMEOUT,
        }
        with self.lock:
            self.details.pop(PROBE_NODE, None)
        self.wait_job(self.tasker.post_task(PROBE_NODE, {PROBE_NODE: node}))
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
            "timeout": PROBE_TIMEOUT,
        }
        with self.lock:
            self.details.pop(PROBE_NODE, None)
        self.wait_job(self.tasker.post_task(PROBE_NODE, {PROBE_NODE: node}))
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

    def shot_when(self, node: str, name: str, *, settle: float | None = None) -> None:
        """等这个节点命中，再截当前画面存成 name。"""
        self.wait(node)
        self.shot(name, settle=settle)

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
        if not self.wait_job(self.controller.post_click(x, y)).succeeded:
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
        if not self.wait_job(self.tasker.post_task(PROBE_NODE, {PROBE_NODE: node})).succeeded:
            raise FlowError(f"滑动 {begin} -> {end} 没成功")

    def run(self, node: str) -> None:
        """按这个节点自己的识别和动作执行一次，不跟它后面的 next。"""
        if self.resource.get_node_data(node) is None:
            raise FlowError(f"资源里没有节点 {node}")
        click.echo(f"[{self.name}] 执行 {node}")
        if self.dry_run:
            return
        if not self.wait_job(self.tasker.post_task(node, {node: {"next": []}})).succeeded:
            raise FlowError(f"节点 {node} 没执行成功")

    def sub_task(self, node: str) -> None:
        """跑一个现成的节点，让它自己把界面转跳到目标。"""
        if self.resource.get_node_data(node) is None:
            raise FlowError(f"资源里没有节点 {node}")
        click.echo(f"[{self.name}] 转跳 {node}")
        if self.dry_run:
            return
        if not self.wait_job(self.tasker.post_task(node)).succeeded:
            raise FlowError(f"节点 {node} 没跑成功，转跳可能被弹窗挡住了")

    def goto(self, enter: str, route: str | RouteFunc | None = None) -> None:
        """先确认在这个界面，不在就转跳过去，到了再往下走。

        route 写节点名就用现成节点转过去；写一段流程函数就自己一步步点过去——
        有的界面没有现成节点能一步到位（比如装箱界面只能从仓储节点点进去再选类型）。
        """
        if self.hit(enter):
            return
        if self.dry_run:
            raise FlowError(f"现在不在 {enter}，dry-run 不点击、转跳不了；先把游戏切到这个界面再跑")
        if callable(route):
            click.echo(f"[{self.name}] 不在 {enter}，按流程里的转法过去")
            route(self)
        else:
            target = route or ENTER_WORLD_NODE
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
            "step": index,
            "name": step.name,
            "shots": list(self.records),
            "clicked": [list(point) for point in self.clicked],
            "stopped": step.stop,
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
    if not cancel.wait_job(tasker.post_task(OPEN_GAME_NODE, override), stop=tasker.post_stop).succeeded:
        raise FlowError(f"{OPEN_GAME_NODE} 没跑成功；Agent 日志在 {log_dir()}/agent-*.log")


def enter_world(tasker: Tasker, resource: Resource, controller_name: str) -> None:
    """跑既有的 SceneAnyEnterWorld 节点，把游戏带回大世界。"""
    if resource.get_node_data(ENTER_WORLD_NODE) is None:
        raise FlowError(f"资源里没有节点 {ENTER_WORLD_NODE}，没法先回大世界")
    click.echo(f"[{controller_name}] 执行 {ENTER_WORLD_NODE} 回到大世界")
    job = cancel.wait_job(tasker.post_task(ENTER_WORLD_NODE), stop=tasker.post_stop)
    if not job.succeeded:
        raise FlowError(f"{ENTER_WORLD_NODE} 没跑成功，游戏可能停在弹窗或登录界面；Agent 日志在 {log_dir()}/agent-*.log")
    click.echo(f"[{controller_name}] 已回到大世界")


@dataclass
class Segment:
    """流程里的一段：从上一段结尾之后，到这一段结尾的那几步；默认每一步自己就是一段。"""

    index: int
    steps: list[tuple[int, Step]]


def split_segments(flow: Flow) -> list[Segment]:
    """按 stop 标记把流程切成几段。

    stop 默认就是 True，所以默认每一步各自成一段；写 stop=False 的步骤会并到下一段里。
    每段都在所有平台上各跑一遍：切段只决定先跑谁，不负责结束整场运行。
    """
    segments: list[Segment] = []
    current: list[tuple[int, Step]] = []
    for index, step in enumerate(flow.steps, 1):
        current.append((index, step))
        if step.stop:
            segments.append(Segment(index=len(segments) + 1, steps=current))
            current = []
    if current:
        segments.append(Segment(index=len(segments) + 1, steps=current))
    return segments


class Platform:
    """一台平台的一次连接：控制器、资源、Agent，加上跑步骤用的 Session。

    同一台平台连着跑好几段时复用这一个连接，不重连、不重新登录。
    """

    def __init__(
        self,
        name: str,
        out_dir: Path,
        controller: Controller,
        resource: Resource,
        tasker: Tasker,
        agents: Agents,
        session: Session,
    ) -> None:
        self.name = name
        self.out_dir = out_dir
        self.controller = controller
        self.resource = resource
        self.tasker = tasker
        self.agents = agents
        self.session = session

    def run_segment(
        self,
        segment: Segment,
        result: dict[str, Any],
        skip: dict[str, str | None] | None = None,
        notify: Callable[[], None] | None = None,
    ) -> None:
        """跑这一段，把每步的记录追加到 result["steps"]。

        skip 里的步骤不跑：值是这次跳过的原因、要记一笔；值是 None 表示上次已经跑过，
        这次不用再记。notify 在每次进度变化后调用，用来随时把进度写到磁盘。
        """
        skipped = skip or {}
        for index, step in segment.steps:
            if step.name in skipped:
                reason = skipped[step.name]
                if reason is not None:
                    result["steps"].append({"step": index, "name": step.name, "skipped": reason})
                if notify is not None:
                    notify()
                continue
            try:
                record = self.session.run_step(index, step)
            except FlowError as error:
                result["error"] = {"step": index, "name": step.name, "message": str(error)}
                failure_path = self.out_dir / f"{index:02d}_{step.name}{FAILURE_SUFFIX}.png"
                try:
                    image = self.session.capture()
                    if image.size:
                        save_image(image, failure_path)
                        result["error"]["image"] = failure_path.name
                except Exception:  # 失败截图失败就不再掩盖原始错误
                    pass
                raise FlowError(
                    f"[{self.name}] 第 {index} 步「{step.name}」失败：{error}；现场截图已存到 {failure_path}"
                ) from error
            result["steps"].append(record)
            if notify is not None:
                notify()
        result.setdefault("segments", []).append(segment.index)
        if notify is not None:
            notify()

    def close(self) -> None:
        self.session.close()
        self.agents.close()


def open_platform(
    interface: dict[str, Any],
    controller_name: str,
    out_dir: Path,
    settings: RunSettings,
    testset: TestSetWriter | None,
    refs: NodeRefs,
) -> Platform:
    """连上这台平台、装好资源与 Agent，并把它带回大世界。"""
    from maa.resource import Resource as MaaResource
    from maa.tasker import Tasker as MaaTasker

    kind = str(controller_entry(interface, controller_name).get("type") or "")
    env_extra = pi_environment(interface, controller_name, log_dir())

    resource = cast(Resource, MaaResource())
    for bundle in resource_bundles(interface, controller_name):
        if not cancel.wait_job(resource.post_bundle(bundle)).succeeded:
            raise FlowError(f"加载资源失败: {bundle}")
    # 资源装好就先把流程里写的节点名对一遍：写错了就别去拉游戏、连设备了。
    validate.check(refs, resource)
    click.echo(f"[{controller_name}] 节点名核对通过（{len(refs.nodes)} 个）")

    if settings.open_game:
        # 云网页版的外壳、桌面版客户端都由控制器自己的预任务拉起；已经在跑就直接过。
        pretasks.run(controller_name, env_extra)
    controller, description = create_controller(interface, controller_name, settings.adb_path, settings.adb_serial)
    click.echo(f"[{controller_name}] 连接 {description}")
    if not cancel.wait_job(controller.post_connection()).succeeded:
        raise FlowError(f"控制器 {controller_name} 连接失败")

    tasker = cast(Tasker, MaaTasker())
    if not tasker.bind(resource, controller) or not tasker.inited:
        raise FlowError("Tasker 初始化失败")

    # Agent 常驻：流程里引用的节点可能是自定义识别（MapFind 之类），也可能是内置识别，
    # 都走同一套探测逻辑，所以整场都让它们挂着。
    agents = Agents(AGENT_DIR, maafw_dir(), log_dir(), env_extra)
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
    session = Session(
        tasker,
        resource,
        controller,
        controller_name,
        out_dir,
        settings.dry_run,
        settings.wait,
        testset,
    )
    return Platform(controller_name, out_dir, controller, resource, tasker, agents, session)


def run_flow(
    interface: dict[str, Any],
    flow: Flow,
    controllers: tuple[str, ...],
    run_dir: Path,
    settings: RunSettings,
    testset: TestSetWriter | None = None,
    previous: list[dict[str, Any]] | None = None,
    on_progress: Callable[[list[dict[str, Any]], dict[str, str]], None] | None = None,
    only: str | None = None,
) -> list[dict[str, Any]]:
    """按「段」和「平台」两层循环跑，返回每个平台汇总后的结果。

    外层是段、内层是平台：一段在所有平台上各跑一遍才进下一段，这样同一段里各平台的
    前置状态一致。平台顺序一段正、一段反（A B → B A → A B），让上一段的最后一台直接
    接着跑下一段，少一次重新登录；只有换平台那一次才需要顶号。

    任何一台平台报错就整场停下：后面的段、别的平台都不再跑，免得在状态不对的游戏上接着点。
    previous 是上次中断那场已经记下的进度，传进来就跳过已经跑完的步骤接着跑；
    on_progress 在每次进度变化后调用，用来随时把 manifest 写到磁盘。
    only 只在这台平台上跑，别的平台沿用 previous 里的记录、这次不连也不动它们。
    """
    segments = split_segments(flow)
    # 流程文件扫一遍节点名，每台平台装好资源后都会拿它对一次，写错了连设备都不连。
    refs = validate.collect(flow.source) if flow.source is not None else NodeRefs()
    results: dict[str, dict[str, Any]] = {
        name: {
            "controller": name,
            "dir": str(run_dir / name),
            "steps": [],
            "segments": [],
            "skippedSegments": [],
            "error": None,
        }
        for name in controllers
    }
    steps_by_name = {step.name: step for step in flow.steps}
    # 每个平台已经处理完的步骤（跑过或记过跳过），以及标了 once 的步骤归谁做的。
    finished: dict[str, set[str]] = {name: set() for name in controllers}
    done_once: dict[str, str] = {}
    for item in previous or []:
        name = str(item.get("controller") or "")
        if name not in results:
            continue
        results[name]["steps"] = list(item.get("steps") or [])
        results[name]["segments"] = list(item.get("segments") or [])
        results[name]["skippedSegments"] = list(item.get("skippedSegments") or [])
        results[name]["error"] = item.get("error")
        for record in results[name]["steps"]:
            # 早先的运行记录把步骤名写在 step 里、序号写在 index 里，读旧目录时两种都认。
            old_name = record.get("step") if isinstance(record.get("step"), str) else ""
            step_name = str(record.get("name") or old_name or "")
            finished[name].add(step_name)
            step = steps_by_name.get(step_name)
            if step is not None and step.once and not record.get("skipped"):
                done_once[step_name] = name
    if previous:
        done_steps = sum(len(names) for names in finished.values())
        click.echo(f"接着上次的进度跑：已经记下 {done_steps} 步，跑过的不再跑")

    def save() -> None:
        if on_progress is not None:
            on_progress([results[name] for name in controllers], dict(done_once))

    save()
    if len(segments) > 1:
        click.echo(f"流程切成 {len(segments)} 段（默认每一步一段），每段在 {len(controllers)} 个平台上各跑一遍")

    platform: Platform | None = None
    stopped = False
    try:
        for segment in segments:
            if stopped:
                break
            order = controllers if segment.index % 2 == 1 else tuple(reversed(controllers))
            for name in order:
                if only is not None and name != only:
                    continue
                skip: dict[str, str | None] = {}
                for _, step in segment.steps:
                    if step.name in finished[name]:
                        # 上次中断前已经跑过，这次不再跑，也不用再记一笔。
                        skip[step.name] = None
                    elif step.once and step.name in done_once and done_once[step.name] != name:
                        skip[step.name] = f"只能做一次，已经由 {done_once[step.name]} 做完"
                if len(skip) == len(segment.steps):
                    noted = False
                    for index, step in segment.steps:
                        reason = skip[step.name]
                        if reason is not None:
                            results[name]["steps"].append({"step": index, "name": step.name, "skipped": reason})
                            noted = True
                    if noted:
                        if segment.index not in results[name]["skippedSegments"]:
                            results[name]["skippedSegments"].append(segment.index)
                        click.echo(f"[{name}] 第 {segment.index} 段不用跑（上次跑过，或者只能做一次），跳过")
                    save()
                    continue
                # 这次真的要在这台平台上跑：上次留下的失败结论作废，成没成以这次的结果为准。
                results[name]["error"] = None
                if platform is not None and platform.name != name:
                    platform.close()
                    platform = None
                try:
                    if platform is None:
                        platform = open_platform(interface, name, run_dir / name, settings, testset, refs)
                    if testset is not None:
                        testset.start_controller(name)
                    platform.run_segment(segment, results[name], skip, save)
                    for _, step in segment.steps:
                        if step.name in skip:
                            continue
                        finished[name].add(step.name)
                        if step.once:
                            done_once[step.name] = name
                    save()
                except (FlowError, TestSetError) as error:
                    results[name]["error"] = results[name]["error"] or {"message": str(error)}
                    click.echo(str(error), err=True)
                    save()
                    if platform is not None and platform.name == name:
                        platform.close()
                        platform = None
                    if isinstance(error, validate.NodeNameError):
                        # 节点名写错跟平台无关，一台报出来就够了，不用接着试别的平台。
                        raise
                    click.echo(f"[{name}] 失败，整场停下，后面的段和别的平台都不再跑", err=True)
                    stopped = True
                    break
    finally:
        if platform is not None:
            platform.close()
    return [results[name] for name in controllers]
