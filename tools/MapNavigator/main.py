# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "fastapi",
#   "uvicorn",
#   "websockets",
#   "maafw",
#   "pynput",
#   "pyperclip",
#   "numpy",
# ]
# ///
"""MapNavigator 入口: 拉起 web 后端 (web/serve.py)。

依赖声明须与 web/serve.py 的 PEP 723 头保持一致 —— `uv run main.py` 只读取本文件的头。

端口选取与浏览器打开都在 serve.py: 端口被占用会顺延, 只有绑定方知道最终端口, 这里不能再猜。
"""

from __future__ import annotations

import runpy
from pathlib import Path

SERVE_PY = Path(__file__).resolve().parent / "web" / "serve.py"


def main() -> None:
    # run_name="__main__" 触发 serve.py 的启动块 (选端口 + 开浏览器 + uvicorn, 仅监听 127.0.0.1)
    runpy.run_path(str(SERVE_PY), run_name="__main__")


if __name__ == "__main__":
    main()
