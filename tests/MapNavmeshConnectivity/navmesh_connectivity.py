"""独立运行连通性规划：按用例文件中的起点与终点调用 Agent 寻路，不依赖测试截图或定位测试。"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid

import json5
import numpy as np

QUERY_NODE = "MapNavmeshConnectivityQuery"
CASE_KEYS = {"name", "zone_id", "start", "goal", "goal_tier", "goal_deck_y"}


def is_point(value) -> bool:
    return (
        isinstance(value, list) and len(value) == 2
        and all(isinstance(v, (int, float)) and not isinstance(v, bool) for v in value)
    )


def prepare_cases(definition: dict) -> list[dict]:
    """校验用例文件，每条用例拼成 [ZONE, NAVMESH] 两步路径。"""
    if not isinstance(definition.get("cases"), list) or not definition["cases"]:
        raise ValueError("连通性测试 cases 必须为非空数组")
    cases, seen = [], set()
    for case in definition["cases"]:
        name = case.get("name")
        if not isinstance(name, str) or not name or name in seen:
            raise ValueError(f"用例名称无效或重复: {name}")
        seen.add(name)
        if set(case) - CASE_KEYS:
            raise ValueError(f"用例包含未知字段: {name} {sorted(set(case) - CASE_KEYS)}")
        zone = case.get("zone_id")
        if not isinstance(zone, str) or not zone:
            raise ValueError(f"zone_id 无效: {name}")
        if not is_point(case.get("start")) or not is_point(case.get("goal")):
            raise ValueError(f"start/goal 必须为 [x, y]: {name}")
        navmesh = {"action": "NAVMESH", "target": case["goal"]}
        # 终点为分层区坐标或需指定落脚高度时，与 Pipeline 中 NAVMESH 的同名字段含义一致。
        if "goal_tier" in case:
            if not isinstance(case["goal_tier"], str) or not case["goal_tier"]:
                raise ValueError(f"goal_tier 无效: {name}")
            navmesh["target_tier"] = case["goal_tier"]
        if "goal_deck_y" in case:
            if not isinstance(case["goal_deck_y"], (int, float)) or isinstance(case["goal_deck_y"], bool):
                raise ValueError(f"goal_deck_y 无效: {name}")
            navmesh["target_deck_y"] = case["goal_deck_y"]
        cases.append({
            **case,
            "customActionParam": {"path": [{"action": "ZONE", "zone_id": zone}, navmesh]},
        })
    return cases


def query_error(result) -> str | None:
    if not isinstance(result, dict):
        return "缺少规划结果或查询任务失败"
    if not result.get("ok"):
        return result.get("error") or "规划失败"
    return None


def run_cases(root: Path, log_dir: Path, cases: list[dict], report: dict) -> None:
    install = root / "install"
    executable = install / "agent" / ("cpp-algo.exe" if os.name == "nt" else "cpp-algo")
    if not executable.is_file():
        raise FileNotFoundError(f"缺少 C++ Agent，请先构建当前源码: {executable}")
    if not any((install / "resource/model/map/navmesh" / name).is_file() for name in ("base.nav.gz", "base.nav")):
        raise FileNotFoundError("缺少 install/resource/model/map/navmesh/base.nav(.gz)，请先准备安装目录资源")
    # maa 包导入时即加载动态库，必须在导入前选定与 Agent 相同的运行库。
    os.environ["MAAFW_BINARY_PATH"] = str(install / "maafw")
    from maa.agent_client import AgentClient
    from maa.controller import CustomController
    from maa.library import Library
    from maa.resource import Resource
    from maa.tasker import Tasker
    from maa.toolkit import Toolkit

    class NullController(CustomController):
        def connect(self):
            return True

        def request_uuid(self):
            return "maaend-navmesh-connectivity-offline"

        def screencap(self):
            return np.zeros((720, 1280, 3), dtype=np.uint8)

        # 规划查询不看画面，任何输入或应用操作均拒绝，不连接实际游戏。
        def deny_input(self, *args):
            return False

        start_app = stop_app = click = swipe = touch_down = touch_move = touch_up = deny_input
        click_key = input_text = key_down = key_up = scroll = relative_move = deny_input

        def shell(self, *args):
            return None

    report["agent"] = str(executable)
    report["agentModifiedNs"] = executable.stat().st_mtime_ns
    Library.open(install / "maafw")
    report["frameworkVersion"] = Library.version()
    Toolkit.init_option(log_dir)
    identifier = f"navmesh-connectivity-{uuid.uuid4().hex}"
    env = os.environ.copy()
    env["PATH"] = os.pathsep.join([str(install / "agent"), str(install / "maafw"), env.get("PATH", "")])
    env["LD_LIBRARY_PATH"] = os.pathsep.join([str(install / "maafw"), env.get("LD_LIBRARY_PATH", "")])
    options = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
    client, process = None, None
    with (log_dir / "agent.log").open("w", encoding="utf-8") as agent_log:
        try:
            controller = NullController()
            if not controller.post_connection().wait().succeeded:
                raise RuntimeError("离线控制器连接失败")
            resource = Resource()
            if not resource.post_bundle(root / "assets/resource").wait().succeeded:
                raise RuntimeError("加载当前仓库资源失败")
            client = AgentClient(identifier)
            if not client.bind(resource) or not client.set_timeout(30_000):
                raise RuntimeError("C++ Agent 客户端初始化失败")
            process = subprocess.Popen(
                [str(executable), identifier], cwd=log_dir, env=env,
                stdout=agent_log, stderr=subprocess.STDOUT, **options,
            )
            if not client.connect() or process.poll() is not None:
                raise RuntimeError(f"C++ Agent 连接失败，退出码: {process.poll()}，详见 agent.log")
            if "MapNavmeshQuery" not in client.custom_recognition_list:
                raise RuntimeError("C++ Agent 未注册 MapNavmeshQuery")
            if not client.set_timeout(60_000):
                raise RuntimeError("设置 C++ Agent 查询超时失败")
            tasker = Tasker()
            if not tasker.bind(resource, controller) or not tasker.inited:
                raise RuntimeError("离线 Tasker 初始化失败")
            for index, case in enumerate(cases, 1):
                if process.poll() is not None:
                    raise RuntimeError(f"C++ Agent 意外退出: {process.returncode}")
                # route_preview 与 MapNavigateAction 运行前的展开同源，分层区起点会先换算到底图。
                payload = {
                    "op": "route_preview", "position": case["start"],
                    "position_zone": case["zone_id"], "custom_action_param": case["customActionParam"],
                }
                harness = {QUERY_NODE: {
                    "recognition": "Custom", "custom_recognition": "MapNavmeshQuery",
                    "custom_recognition_param": payload, "action": "DoNothing", "next": [],
                    "pre_delay": 0, "post_delay": 0, "rate_limit": 0,
                }}
                job = tasker.post_task(QUERY_NODE, harness).wait()
                detail = tasker.get_task_detail(job.job_id)
                node = detail.nodes[0] if detail and detail.nodes else None
                best = node.recognition.best_result if node and node.recognition else None
                result = best.detail if best is not None else None
                if isinstance(result, (str, bytes)):
                    result = json.loads(result)
                error = query_error(result)
                result = result if isinstance(result, dict) else {}
                report["results"].append({
                    **case, "passed": error is None, "error": error,
                    "failure": result.get("failure"), "expandedWaypoints": result.get("expanded_waypoints"),
                })
                save_report(log_dir, report)
                print(
                    f"[{index}/{len(cases)}] {'PASS' if not error else 'FAIL'} {case['name']}"
                    + (f" ({error})" if error else ""),
                    flush=True,
                )
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
    report["executedQueries"] = len(report["results"])
    report["failedCases"] = [r["name"] for r in report["results"] if not r["passed"]]
    (log_dir / "report.json").write_text(
        json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8",
    )


def main(argv: list[str] | None = None) -> int:
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
    root = Path(__file__).resolve().parents[2]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cases", type=Path, default=Path(__file__).with_name("navmesh_connectivity.json"))
    parser.add_argument("--output", type=Path, default=Path(__file__).with_name("output"))
    args = parser.parse_args(argv)
    log_dir = args.output.resolve()
    log_dir.mkdir(parents=True, exist_ok=True)
    report = {"passed": False, "results": [], "errors": [], "expectedQueries": 0}
    try:
        cases = prepare_cases(json5.loads(args.cases.read_text(encoding="utf-8")))
        report["expectedQueries"] = len(cases)
        save_report(log_dir, report)
        run_cases(root, log_dir, cases, report)
        if len(report["results"]) != len(cases):
            raise RuntimeError("规划查询执行数量与用例数量不一致")
    except Exception as exc:
        report["errors"].append(str(exc))
        print(f"navmesh-connectivity: {exc}", file=sys.stderr)
    report["passed"] = not report["errors"] and bool(report["results"]) and all(r["passed"] for r in report["results"])
    save_report(log_dir, report)
    passed = sum(r["passed"] for r in report["results"])
    print(f"规划通过: {passed}/{report['expectedQueries']}")
    print(f"报告: {log_dir / 'report.json'}")
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
