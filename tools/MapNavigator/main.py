# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "fastapi>=0.129,<1.0",
#   "maafw>=5.13.0b4,<6.0",
#   "numpy",
#   "pydantic",
#   "pynput>=1.7.0",
#   "pyperclip",
#   "starlette",
#   "uvicorn>=0.41,<1.0",
#   "websockets",
# ]
# ///
"""MapNavigator 入口：拉起 Web 后端（web/serve.py）。

从项目根目录运行 `uv run map-navigator` 时使用 pyproject.toml 中的依赖；兼容的
`uv run main.py` 脚本模式只读取本文件的 PEP 723 声明。两处依赖须与
web/serve.py 保持一致。

端口选取与浏览器打开都在 serve.py：端口被占用会顺延，只有绑定方知道最终端口，
这里不能再猜。
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
