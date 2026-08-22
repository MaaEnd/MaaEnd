"""提权游戏会话子进程: 由 serve.py 用 runas 拉起并回连它的 127.0.0.1 端口。

热键要求监听进程的权限不低于游戏, 而服务是长驻进程不能自己 runas 重启 (会另起一整套
服务并丢掉页面状态), 故只把会话这一段拆出来提权。--mode 选录制还是试跑, 两者的差异全
在 session_modes.MODES 里。协议 (NDJSON):
    -> {"type":"hello","token":...}   父进程校验后才继续
    <- {"type":"start","config":{连接配置},"path":[编辑器路点]}
    -> 会话事件, 与进程内会话推给前端的 JSON 完全一致
    <- 前端指令, 原样交给服务分发
"""

from __future__ import annotations

import sys
from pathlib import Path

# 与 serve.py 同样的 bare-name import 约定: 本目录必须在 sys.path 最前。
# 提权子进程的工作目录不可靠 (ShellExecuteW 给的是全新环境), 不能指望 cwd。
_TOOL_DIR = Path(__file__).resolve().parent
if str(_TOOL_DIR) not in sys.path:
    sys.path.insert(0, str(_TOOL_DIR))

from worker_runner import worker_main  # noqa: E402


if __name__ == "__main__":
    sys.exit(worker_main())
