"""跑控制器自己的启动前任务：连控制器之前先把要连的窗口准备好。

CloudWeb 要先有云网页外壳的窗口，Win32-Front 要先拉起游戏。MXU 的做法是在连接控制器之前
跑一次 `go-service --pretask <名字>`（定义在 assets/tasks/pretasks/*.json），这里照同一套来：
退出码 0 算成功，外壳/游戏已经在跑时预任务会直接过。
"""

from __future__ import annotations

import os
import subprocess
from pathlib import Path
from typing import Any

import click

from .agents import agent_env
from .flow import FlowError, as_list
from .runtime import AGENT_DIR, LOG_DIR, maafw_dir, pretask_for

# 预任务自己会等窗口（外壳、启动器、游戏各最多 5 分钟），这里只兜住卡死的情况。
PRETASK_TIMEOUT = 900.0


def executable_of(spec: dict[str, Any]) -> Path:
    """预任务的 exec 写成 agent/go-service 这种仓库路径，装好之后在 install/agent 下。"""
    name = Path(str(spec.get("exec") or "")).name
    if not name:
        raise FlowError("预任务定义里没写 exec")
    return AGENT_DIR / f"{name}{'.exe' if os.name == 'nt' else ''}"


def run(controller_name: str, extra_env: dict[str, str]) -> bool:
    """跑这个控制器的启动前任务；这个控制器没登记预任务就返回 False。"""
    spec = pretask_for(controller_name)
    if spec is None:
        return False
    name = str(spec.get("name") or "")
    executable = executable_of(spec)
    if not executable.is_file():
        raise FlowError(f"缺少 {executable}，先跑 uv run build-and-install")
    args = [str(item) for item in as_list(spec.get("args"))]
    LOG_DIR.mkdir(parents=True, exist_ok=True)
    log_path = LOG_DIR / f"pretask-{name}.log"
    click.echo(f"[{controller_name}] 执行预任务 {name}：{executable.name} {' '.join(args)}")
    with log_path.open("wb") as log:
        try:
            done = subprocess.run(
                [str(executable), *args],
                cwd=executable.parent.parent,
                env=agent_env(executable.parent, maafw_dir(), extra_env),
                stdout=log,
                stderr=subprocess.STDOUT,
                timeout=PRETASK_TIMEOUT,
                check=False,
            )
        except subprocess.TimeoutExpired as error:
            raise FlowError(f"预任务 {name} 超过 {PRETASK_TIMEOUT:g} 秒还没结束，看日志 {log_path}") from error
    if done.returncode != 0:
        raise FlowError(f"预任务 {name} 没成功（退出码 {done.returncode}），看日志 {log_path}")
    click.echo(f"[{controller_name}] 预任务 {name} 完成")
    return True
