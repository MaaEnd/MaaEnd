"""在 Linux/WSL 上按 MXU 预设顺序跑任务，逐项记录结果。

用法:
  python3 run_daily_linux.py --only 1,2,3          # 试点
  python3 run_daily_linux.py                       # 全量 14 项
  python3 run_daily_linux.py --from 5 --to 9
选项覆盖由 resolve_preset 解析（复现 MXU GUI 的行为），不是手写。
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import contextlib

from resolve_preset import load_preset

DIST = os.environ.get("MAAEND_DIST", os.path.expanduser("~/maaend"))
WEB = os.environ.get("MAAEND_WEB", "/mnt/d/桌面/常用/旧机改造服务器/MaaEnd适配/web")
_env_dirs = os.environ.get("MAAEND_RES_DIRS", "")
DIRS = (
    [d for d in _env_dirs.split(":") if d]
    if _env_dirs
    else [
        os.path.join(DIST, "resource"),
        os.path.join(DIST, "resource_web"),
        os.path.join(WEB, "linux", "resource_web_linux"),
    ]
)


def _find_socket():
    env = os.environ.get("WLR_SOCKET")
    if env:
        return env
    for cand in (
        os.path.join(os.environ.get("XDG_RUNTIME_DIR", ""), "wayland-1"),
        "/tmp/xdg-run/wayland-1",
        "/run/user/%d/wayland-1" % os.getuid(),
    ):
        if cand and os.path.exists(cand):
            return cand
    return "/tmp/xdg-run/wayland-1"


SOCK = _find_socket()
OUT_JSONL = os.path.join(WEB, "probe_out", "daily_linux_progress.jsonl")

# 单项超时（秒）：采集/送货/协议空间这类长任务给宽一点
TIMEOUTS = {
    "WebStartGame": 900,
    "AutoCollect": 2400,
    "DeliveryJobs": 2400,
    "SellProduct": 1800,
    "ProtocolSpace": 2400,
    "AutoStockpile": 1800,
    "AutoStockStable": 1800,
    "AutoStockStaple": 1800,
    "CreditShoppingN2": 1800,
    "VisitFriends": 1200,
    "DijiangRewards": 1200,
    "DailyRewards": 1200,
    "SeizeDeliveryJobs": 900,
    "AutoSell": 900,
    "EnvironmentMonitoring": 900,
}

AGENT_BIN = os.environ.get("MAA_AGENT_BIN", "/root/maaagent/go-service")
AGENT_DIR = os.path.dirname(AGENT_BIN)
AGENT_LIB = os.environ.get("MAA_AGENT_LIB", os.path.join(AGENT_DIR, "maafw"))


def start_agent(res, controller_name="Linux-Wlroots", resource_name="官服-网页端"):
    """拉起 go-service 并把它的自定义动作/识别绑到资源上。

    MaaEnd 的 SubTask / RepeatUntilFoundAction 等动作由 go-service 注册；
    不启动它，任务会立刻报 `Action is null` 失败。
    """
    import subprocess

    from maa.agent_client import AgentClient

    if not os.path.exists(AGENT_BIN):
        log("  [X] 找不到 agent: %s" % AGENT_BIN)
        return None, None

    client = AgentClient.create_tcp(0)
    ident = client.identifier
    # 顺序很关键：必须先 bind(resource) 再 connect()，否则 connect 直接返回 False
    # （框架日志：AgentClient::connect() "resource is not bound"）
    log("  agent bind=%s (identifier=%s)" % (client.bind(res), ident))
    env = os.environ.copy()
    import json as _json

    # 版本号从发行版的 interface.json 读，避免硬编码落后于实际资源
    _pi_ver, _iface_ver = "unknown", "2"
    try:
        with open(os.path.join(DIST, "interface.json"), encoding="utf-8-sig") as _f:
            _iface = _json.load(_f)
        _pi_ver = _iface.get("version") or "unknown"
        _iface_ver = str(_iface.get("interfaceVersion") or 2)
    except Exception:
        pass

    env.update(
        {
            "PI_CONTROLLER": _json.dumps(
                {
                    "name": controller_name,
                    "type": "Linux",
                    "linux": {"screencap": "Wlr", "input": "Wlr", "use_win32_vk_code": True},
                },
                ensure_ascii=False,
            ),
            "PI_RESOURCE": _json.dumps({"name": resource_name, "path": DIRS}, ensure_ascii=False),
            "PI_VERSION": _pi_ver,
            "PI_CLIENT_NAME": "MXU",
            "PI_CLIENT_VERSION": _pi_ver.lstrip("v").split("-")[0],
            "PI_CLIENT_LANGUAGE": "zh_cn",
            "PI_CLIENT_MAAFW_VERSION": "5.0.0",
            "PI_INTERFACE_VERSION": _iface_ver,
        }
    )
    os.makedirs(AGENT_DIR, exist_ok=True)
    logf = open(os.path.join(AGENT_DIR, "go-service.log"), "ab", buffering=0)
    env["LD_LIBRARY_PATH"] = AGENT_LIB + ((":" + env["LD_LIBRARY_PATH"]) if env.get("LD_LIBRARY_PATH") else "")
    proc = subprocess.Popen([AGENT_BIN, str(ident)], env=env, cwd=AGENT_DIR, stdout=logf, stderr=subprocess.STDOUT)
    ok = client.connect()
    log("  agent connect=%s alive=%s pid=%s" % (ok, client.alive, proc.pid))
    if not ok:
        return client, proc
    try:

        def _lst(name):
            v = getattr(client, name, [])
            return v() if callable(v) else (v or [])

        acts = _lst("custom_action_list")
        recs = _lst("custom_recognition_list")
        log("  自定义动作 %d 个 / 识别 %d 个" % (len(acts), len(recs)))
        log("    动作样本: %s" % ", ".join(sorted(acts)[:8]))
    except Exception as e:
        log("  (取列表失败: %s)" % e)
    return client, proc


def log(msg):
    line = "[%s] %s" % (time.strftime("%H:%M:%S"), msg)
    print(line, flush=True)


def record(obj):
    os.makedirs(os.path.dirname(OUT_JSONL), exist_ok=True)
    with open(OUT_JSONL, "a", encoding="utf-8") as f:
        f.write(json.dumps(obj, ensure_ascii=False) + "\n")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--only", default="")
    ap.add_argument("--tasks", default="", help="按名字或序号挑任务，支持 1-5 / WebStartGame,VisitFriends")
    ap.add_argument("--from", dest="frm", type=int, default=1)
    ap.add_argument("--to", dest="to", type=int, default=99)
    ap.add_argument("--instance", default="hyasuml")
    ap.add_argument("--skip-in-game-check", action="store_true")
    ap.add_argument("--no-agent", action="store_true")
    ap.add_argument("--dry-run", action="store_true", help="只打印要跑的任务，不连控制器")
    ap.add_argument("--controller", default=os.environ.get("MAAEND_CONTROLLER", "cdp"),
                    choices=["wlroots", "cdp"],
                    help="cdp=云串流原生帧（默认，锐度 +134%）；wlroots=合成器截图（兜底）")
    a = ap.parse_args()

    from maa.resource import Resource
    from maa.tasker import Tasker

    inst, plan = load_preset(a.instance)
    plan = [p for p in plan if p.get("entry") and p.get("enabled", True)]
    if a.tasks:
        tokens = [t.strip() for t in a.tasks.replace("，", ",").split(",") if t.strip()]
        picked = []
        for tok in tokens:
            if tok.isdigit():
                i = int(tok)
                if 1 <= i <= len(plan):
                    picked.append(plan[i - 1])
                else:
                    log("[X] 序号越界: %s" % tok)
            elif "-" in tok and all(x.strip().isdigit() for x in tok.split("-")):
                lo, hi = [int(x) for x in tok.split("-")]
                picked += plan[max(0, lo - 1) : hi]
            else:
                hit = [p for p in plan if p["name"].lower() == tok.lower()]
                if hit:
                    picked.append(hit[0])
                else:
                    log("[X] 未知任务: %s" % tok)
        plan = picked
    elif a.only:
        want = {int(x) for x in a.only.split(",") if x.strip()}
        plan = [p for i, p in enumerate(plan, 1) if i in want]
    else:
        plan = [p for i, p in enumerate(plan, 1) if a.frm <= i <= a.to]
    log("待跑 %d 项: %s" % (len(plan), "/".join(p["name"] for p in plan)))
    if a.dry_run:
        for i, p in enumerate(plan, 1):
            log("  %2d. %-22s -> %-32s 覆盖 %d 项" % (i, p["name"], p["entry"], p["n_override"]))
        return 0

    # CDP 路径的模板要单独一层：无视频时走 Page.captureScreenshot，
    # 渲染与合成器帧有细微差异，灰度匹配分数会掉到 0.90~0.92（阈值 0.95）。
    # 这一层只在 --controller cdp 下追加，homesrv 的合成器路径不受影响。
    dirs = list(DIRS)
    if a.controller == "cdp":
        _cdp_layer = os.path.join(os.path.dirname(os.path.abspath(__file__)), "resource_web_cdp")
        if os.path.isdir(_cdp_layer):
            dirs.append(_cdp_layer)

    res = Resource()
    missing = [d for d in dirs if not os.path.isdir(d)]
    if missing:
        for d in missing:
            log("  ! 资源目录不存在，已跳过: %s" % d)
    for d in [x for x in dirs if os.path.isdir(x)]:
        job = res.post_bundle(d)
        job.wait()
    log("资源加载: %s" % res.loaded)
    if not res.loaded:
        log("[X] 资源加载失败，任务不可能跑对，直接退出")
        return 3

    # --controller wlroots：合成器截图（1080→1440→720 两次重采样）
    # --controller cdp    ：直接从云串流 <video> 取原生 1080 帧，Python AREA 降到 720
    #                       （实测锐度 +30%；输入走 CDP 注入，不依赖 sway）
    if a.controller == "cdp":
        sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
        from cdp_controller import make_custom_controller

        ctrl = make_custom_controller()
    else:
        from maa.controller import WlRootsController

        # use_win32_vk_code=True：管线的动作节点用的是 Win32 虚拟键码（Alt=18），
        # 不加这个参数，18 会被当成 Linux 键码（=E）。
        ctrl = WlRootsController(SOCK, use_win32_vk_code=True)
    ctrl.post_connection().wait()
    _conn = getattr(ctrl, "connected", False)
    _conn = _conn() if callable(_conn) else bool(_conn)
    log("控制器: %s connected=%s" % (a.controller, _conn))
    if not _conn:
        if a.controller != "cdp":
            return 4
        # CDP 连不上（浏览器没起来 / 端口没开）不能让整批任务白跑：
        # 退回合成器截图，并在日志里显式标注，避免"静默降级"看不见。
        log("[!] CDP 控制器连接失败，回退到 wlroots（合成器截图）")
        from maa.controller import WlRootsController

        ctrl = WlRootsController(SOCK, use_win32_vk_code=True)
        ctrl.post_connection().wait()
        _conn = getattr(ctrl, "connected", False)
        _conn = _conn() if callable(_conn) else bool(_conn)
        log("控制器: wlroots(回退) connected=%s" % _conn)
        if not _conn:
            return 4

    tasker = Tasker()
    tasker.bind(res, ctrl)
    try:
        from maa.define import LoggingLevelEnum

        tasker.set_stdout_level(LoggingLevelEnum.Info)  # 4：打印节点识别/动作轨迹
        # 日志目录必须可写：给 /root 下路径时框架会抛 C++ 异常直接 abort
        log_dir = os.environ.get("MAA_LOG_DIR") or os.path.expanduser("~/maaend-log")
        os.makedirs(log_dir, exist_ok=True)
        tasker.set_log_dir(log_dir)
        # 识别可视化：MAA_SAVE_DRAW=1 时把每个节点的识别框/截图落盘到 <log_dir>/debug/
        # （默认关闭，一次全量会产出上百 MB）
        if os.environ.get("MAA_SAVE_DRAW") == "1":
            try:
                tasker.set_save_draw(True)
                tasker.set_save_on_error(True)
                tasker.set_draw_quality(85)
                log("  识别可视化已开启：draw/on_error -> %s/debug" % log_dir)
            except Exception as e:
                log("  (开启识别可视化失败: %s)" % str(e)[:60])
        log("  日志: stdout=Info  目录=%s" % log_dir)
    except Exception as e:
        log("  (设置日志级别失败: %s)" % e)

    agent_client, agent_proc = (None, None)
    if not a.no_agent:
        log("== 启动 agent (go-service) ==")
        agent_client, agent_proc = start_agent(res, "Linux-Wlroots", inst.get("resourceName") or "官服-网页端")
        if agent_client is None:
            log("  ! 没有 agent，自定义动作的任务会失败")

    def run_node(node, cap=90):
        """跑一个节点（当作 entry），返回是否 succeeded。"""
        try:
            jb = tasker.post_task(node)
        except Exception as e:
            log("    (节点 %s 提交失败: %s)" % (node, e))
            return False
        t0 = time.time()
        while not jb.done and time.time() - t0 < cap:
            time.sleep(1)
        if not jb.done:
            with contextlib.suppress(Exception):
                tasker.post_stop().wait()
            return False
        return bool(jb.succeeded)

    def save_failshot(tag):
        """失败现场留一帧：~/maaend-log/failshots/<日期>/<时间>-<tag>.png

        只读操作（多抓一帧），不改任何游戏状态。用于事后看“到底卡在哪个界面”，
        也是发现新 UI（签到页/活动中心这类）唯一可靠的来源。
        """
        try:
            from PIL import Image
        except Exception:
            return
        try:
            ctrl.post_screencap().wait()
            img = ctrl.cached_image
            if img is None:
                return
            d = os.path.join(os.path.expanduser("~/maaend-log"), "failshots", time.strftime("%Y-%m-%d"))
            os.makedirs(d, exist_ok=True)
            p = os.path.join(d, "%s-%s.png" % (time.strftime("%H%M%S"), tag))
            Image.fromarray(img[:, :, ::-1]).save(p)
            log("    [failshot] %s" % p)
        except Exception as e:
            log("    (failshot 失败: %s)" % str(e)[:60])

    def hard_reset(tag=""):
        """最后手段：把页面重置回启动页，再重新进游戏（约 60~90 秒）。

        上游没有这个动作（ADB 客户端不会掉云会话）；网页端有挂机超时/会话结束，
        才需要它。现在只在「启动那一刻」失败时兜底，任务之间不再调用。
        """
        log("    %s硬重置：页面回启动页并重新进游戏" % (tag + " " if tag else ""))
        try:
            subprocess.run(
                [
                    "python3",
                    os.path.join(WEB, "linux", "reset_page.py"),
                    "--port",
                    "9334",
                    "--url",
                    "https://endfield.hypergryph.com/cloud",
                    "--wait",
                    "12",
                ],
                timeout=90,
                capture_output=True,
            )
        except Exception as e:
            log("    (重置页面失败: %s)" % str(e)[:60])
        return run_node("WebStartGameEntry", cap=900)

    def ensure_in_game_once(tag="[启动]"):
        """开跑前跑一次 WebStartGameEntry，由它自己判断该不该启动。

        WebStartGameEntry = DirectHit + timeout 600s + next 链：
          WebAlreadyInGame（InWorld|InMenuList）→ 已在游戏里，命中即结束；
          WebLauncherStartButton → 点「开始游戏」；
          WebQueueDetected / WebTitleContinue / WebSessionBusy / WebMonthlyCard → 各自处理。
        这与上游把「启动」写成 pretask/pipeline 的做法一致：判断写在节点里，
        外层脚本不再做单帧判断（会漏判）或轮询（已在游戏里时白等）。
        """
        log("  %s 先跑 WebStartGameEntry（已在游戏内会自动跳过）" % tag)
        ok = run_node("WebStartGameEntry", cap=900)
        log("    WebStartGameEntry -> %s" % ok)
        if not ok:
            log("  ! 启动失败，硬重置一次再试")
            ok = hard_reset(tag)
            log("    硬重置后 WebStartGameEntry -> %s" % ok)
        return ok

    results = []
    # 只在开跑前做一次「进没进游戏」的判断；任务之间交给上游的 Enter/JumpBack 链自愈。
    if plan and plan[0]["name"] != "WebStartGame" and not a.skip_in_game_check:
        ensure_in_game_once("[启动]")
    for i, p in enumerate(plan, 1):
        name, entry = p["name"], p["entry"]
        attempts = 1 if name == "WebStartGame" else 2
        st = None
        last = None
        for attempt in range(1, attempts + 1):
            if attempt > 1:
                log("  ! %s 重试第 %d 次（不做恢复，靠任务自己的 Enter 链）" % (name, attempt))
            log(
                "--- (%d/%d) %s -> %s  覆盖 %d 项%s"
                % (i, len(plan), name, entry, p["n_override"], "" if attempt == 1 else " [重试]")
            )
            t0 = time.time()
            job = tasker.post_task(entry, p["override"])
            cap = TIMEOUTS.get(name, 1200)
            while not job.done and time.time() - t0 < cap:
                time.sleep(1)
            dur = time.time() - t0
            st = {
                "name": name,
                "entry": entry,
                "done": bool(job.done),
                "succeeded": bool(job.succeeded),
                "seconds": round(dur, 1),
                "last_node": last,
                "timeout": (not job.done),
                "attempt": attempt,
            }
            if not job.done:
                with contextlib.suppress(Exception):
                    tasker.post_stop().wait()
            log("    => done=%s succeeded=%s %.1fs  last_node=%s" % (st["done"], st["succeeded"], dur, last))
            if st["succeeded"]:
                break
            save_failshot("%s-att%d" % (name, attempt))
        record(st)
        results.append(st)

    log("==== 汇总 ====")
    for r in results:
        flag = "OK  " if r["succeeded"] else ("TIMEOUT" if r["timeout"] else "FAIL")
        log("  %-22s %-8s %6.1fs  %s" % (r["name"], flag, r["seconds"], r["last_node"] or ""))
    if agent_proc is not None:
        try:
            agent_proc.terminate()
            log("已关闭 agent (pid=%s)" % agent_proc.pid)
        except Exception:
            pass
    ok = sum(1 for r in results if r["succeeded"])
    log("通过 %d / %d" % (ok, len(results)))
    return 0 if ok == len(results) else 7


if __name__ == "__main__":
    sys.exit(main())
