"""独立运行自动采集定位截图正例，不依赖 maa-tools 或其测试加载器。"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import uuid

import json5
import numpy as np
from PIL import Image


def collect_nodes(root: Path) -> dict:
    """列出 Route1–17、CommonRoute1–8 的定位节点，排除未注册的 Route999。"""
    nodes = {}
    for file in sorted((root / "assets/resource/pipeline/AutoCollect").glob("*.json")):
        for name, node in json5.loads(file.read_text(encoding="utf-8")).items():
            if (
                re.fullmatch(r"AutoCollect(?:CommonRoute[1-8]|Route(?:[1-9]|1[0-7]))AssertLocation\d*", name)
                and node.get("custom_recognition") == "MapLocateAssertLocation"
            ):
                nodes[name] = node["custom_recognition_param"]
    if not nodes:
        raise ValueError("没有找到正式自动采集路线的定位节点")
    return nodes


def prepare_cases(definition: dict, image_root: Path, nodes: dict) -> list[dict]:
    """校验独立用例文件，只展开每张图片显式列出的 hits 正例。"""
    config = definition["configs"]
    # Custom 控制器使用桌面小地图 ROI；本批只支持 Win32/官服，不展开矩阵。
    if config["controller"] != "Win32" or config["resource"] != "官服":
        raise ValueError("定位测试仅支持 controller=Win32、resource=官服")
    if not isinstance(definition["cases"], list) or not definition["cases"]:
        raise ValueError("定位测试 cases 必须为非空数组")
    cases, seen = [], set()
    image_root = image_root.resolve()
    for case in definition["cases"]:
        name = case["image"]
        if (
            not isinstance(name, str) or not name.endswith(".png")
            or "/" in name or "\\" in name or ":" in name or name in seen
        ):
            raise ValueError(f"图片名称无效或重复: {name}")
        seen.add(name)
        image = (image_root / name).resolve()
        if not image.is_relative_to(image_root) or not image.is_file():
            raise ValueError(f"测试图片不存在或超出测试集: {image}")
        with Image.open(image) as photo:
            if photo.format != "PNG" or photo.size != (1280, 720):
                raise ValueError(f"测试图片必须为 1280×720 PNG: {image} {photo.size}")
        hits = case["hits"]
        if not isinstance(hits, list) or not hits:
            raise ValueError(f"定位测试只执行正例，hits 必须为非空数组: {name}")
        seen_hits = set()
        for node in hits:
            if not isinstance(node, str) or node not in nodes:
                raise ValueError(f"不是正式自动采集定位节点: {node}")
            if node in seen_hits:
                raise ValueError(f"重复的 hits 节点: {name} {node}")
            seen_hits.add(node)
            cases.append({"image": name, "imagePath": str(image), "node": node})
    return cases


def recognition_error(succeeded: bool, detail, error: str | None) -> str | None:
    if error:
        return error
    if not succeeded or detail is None:
        return "缺少识别结果或测试识别任务失败"
    if not detail.hit:
        return "正例未命中"
    return None


def run_cases(root: Path, log_dir: Path, cases: list[dict], report: dict) -> None:
    install = root / "install"
    executable = install / "agent" / ("cpp-algo.exe" if os.name == "nt" else "cpp-algo")
    if not executable.is_file():
        raise FileNotFoundError(f"缺少 C++ Agent，请先构建当前源码: {executable}")
    if not (install / "resource/image/MapLocator").is_dir():
        raise FileNotFoundError("缺少 install/resource/image/MapLocator，请先准备安装目录资源")
    # maa 包导入时即加载动态库，必须在导入前选定与 Agent 相同的运行库。
    os.environ["MAAFW_BINARY_PATH"] = str(install / "maafw")
    from maa.agent_client import AgentClient
    from maa.controller import CustomController
    from maa.custom_action import CustomAction
    from maa.library import Library
    from maa.resource import Resource
    from maa.tasker import Tasker
    from maa.toolkit import Toolkit

    class ImageController(CustomController):
        def __init__(self):
            self.image = np.zeros((720, 1280, 3), dtype=np.uint8)
            super().__init__()

        def connect(self):
            return True

        def request_uuid(self):
            return "maaend-map-locate-offline"

        def screencap(self):
            return self.image.copy()

        # 测试仅允许截图，任何输入或应用操作均拒绝，不连接实际游戏。
        def deny_input(self, *args):
            return False

        start_app = stop_app = click = swipe = touch_down = touch_move = touch_up = deny_input
        click_key = input_text = key_down = key_up = scroll = relative_move = deny_input

        def shell(self, *args):
            return None

    class RecognizeNode(CustomAction):
        def __init__(self):
            super().__init__()
            self.node = ""
            self.result = None
            self.error = None

        def run(self, context, argv):
            try:
                # 只执行原节点识别，不执行它的 action、next 或路线移动。
                self.result = context.run_recognition(self.node, controller.image)
            except Exception as exc:
                self.error = str(exc)
            return self.result is not None

    report["agent"] = str(executable)
    report["agentModifiedNs"] = executable.stat().st_mtime_ns
    Library.open(install / "maafw")
    report["frameworkVersion"] = Library.version()
    Toolkit.init_option(log_dir)
    identifier = f"map-locate-{uuid.uuid4().hex}"
    env = os.environ.copy()
    env["PATH"] = os.pathsep.join([str(install / "agent"), str(install / "maafw"), env.get("PATH", "")])
    env["LD_LIBRARY_PATH"] = os.pathsep.join([str(install / "maafw"), env.get("LD_LIBRARY_PATH", "")])
    options = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
    client, process = None, None
    with (log_dir / "agent.log").open("w", encoding="utf-8") as agent_log:
        try:
            controller = ImageController()
            if not controller.post_connection().wait().succeeded:
                raise RuntimeError("离线截图控制器连接失败")
            resource = Resource()
            if not resource.post_bundle(root / "assets/resource").wait().succeeded:
                raise RuntimeError("加载当前仓库资源失败")
            for case in cases:
                if not resource.get_node_data(case["node"]):
                    raise ValueError(f"加载的资源中节点不存在: {case['node']}")
            client = AgentClient(identifier)
            if not client.bind(resource) or not client.set_timeout(30_000):
                raise RuntimeError("C++ Agent 客户端初始化失败")
            process = subprocess.Popen(
                [str(executable), identifier], cwd=log_dir, env=env,
                stdout=agent_log, stderr=subprocess.STDOUT, **options,
            )
            if not client.connect() or process.poll() is not None:
                raise RuntimeError(f"C++ Agent 连接失败，退出码: {process.poll()}，详见 agent.log")
            if "MapLocateAssertLocation" not in client.custom_recognition_list:
                raise RuntimeError("C++ Agent 未注册 MapLocateAssertLocation")
            # 这是 IPC 超时，不修改生产定位节点自身的追踪重置、轮询或等待行为。
            if not client.set_timeout(60_000):
                raise RuntimeError("设置 C++ Agent 识别超时失败")
            tasker = Tasker()
            if not tasker.bind(resource, controller) or not tasker.inited:
                raise RuntimeError("离线 Tasker 初始化失败")
            action = RecognizeNode()
            if not resource.register_custom_action("MapLocateTestRecognition", action):
                raise RuntimeError("注册测试识别入口失败")
            harness = {"MapLocateTestRecognition": {
                "recognition": "DirectHit", "action": "Custom",
                "custom_action": "MapLocateTestRecognition", "next": [], "post_delay": 0,
            }}
            for index, case in enumerate(cases, 1):
                if process.poll() is not None:
                    raise RuntimeError(f"C++ Agent 意外退出: {process.returncode}")
                with Image.open(case["imagePath"]) as photo:
                    controller.image = np.array(photo.convert("RGB"))[:, :, ::-1].copy()
                action.node, action.result, action.error = case["node"], None, None
                job = tasker.post_task("MapLocateTestRecognition", harness).wait()
                detail = action.result
                error = recognition_error(job.succeeded, detail, action.error)
                report["results"].append({
                    **case, "passed": error is None, "error": error,
                    "hit": bool(detail.hit) if detail is not None else None,
                    "detail": detail.raw_detail if detail is not None else None,
                })
                save_report(log_dir, report)
                print(f"[{index}/{len(cases)}] {'PASS' if not error else 'FAIL'} {case['node']} {case['image']}", flush=True)
        finally:
            # 先结束 Agent，防止它回调已销毁的 Tasker/Resource。
            if process is not None:
                if process.poll() is None:
                    process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            if client is not None:
                client.disconnect()


def save_report(log_dir: Path, report: dict) -> None:
    report["executedRecognitions"] = len(report["results"])
    report["passedNodes"] = sorted({r["node"] for r in report["results"] if r["passed"]})
    (log_dir / "report.json").write_text(
        json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8",
    )


def main(argv: list[str] | None = None) -> int:
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
    root = Path(__file__).resolve().parents[2]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cases", type=Path, default=Path(__file__).with_name("map_locate_assert_location.json"))
    parser.add_argument("--output", type=Path, default=Path(__file__).with_name("output"))
    args = parser.parse_args(argv)
    log_dir = args.output.resolve()
    log_dir.mkdir(parents=True, exist_ok=True)
    report = {
        "passed": False, "results": [], "errors": [], "sampledNodes": [],
        "missingNodes": {}, "totalNodes": 0, "expectedRecognitions": 0,
    }
    try:
        nodes = collect_nodes(root)
        report["totalNodes"] = len(nodes)
        report["missingNodes"] = nodes.copy()
        definition = json5.loads(args.cases.read_text(encoding="utf-8"))
        cases = prepare_cases(definition, root / "tests/MaaEndTestset/Win32/Official_CN/MapLocateAssertLocation", nodes)
        report["sampledNodes"] = sorted({case["node"] for case in cases})
        report["missingNodes"] = {name: param for name, param in nodes.items() if name not in report["sampledNodes"]}
        report["expectedRecognitions"] = len(cases)
        save_report(log_dir, report)
        run_cases(root, log_dir, cases, report)
        if len(report["results"]) != len(cases):
            raise RuntimeError("定位识别执行数量与用例数量不一致")
    except Exception as exc:
        report["errors"].append(str(exc))
        print(f"map-locate: {exc}", file=sys.stderr)
    report["passed"] = not report["errors"] and bool(report["results"]) and all(r["passed"] for r in report["results"])
    save_report(log_dir, report)
    print(f"样本覆盖: {len(report['sampledNodes'])}/{report['totalNodes']}，实际通过节点: {len(report['passedNodes'])}")
    for node in report["missingNodes"]:
        print(f"待补图: {node}")
    print(f"报告: {log_dir / 'report.json'}")
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
