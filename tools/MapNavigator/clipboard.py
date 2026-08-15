"""系统剪贴板写入 (G 热键用)。

录制中游戏持有焦点, 前端拿不到 `navigator.clipboard` 权限, 只能由后端直接写。
serve.py 与提权录制子进程 record_worker.py 共用本模块。
"""

from __future__ import annotations

import subprocess
import sys
from typing import Callable


def copy_to_clipboard(text: str, log: Callable[[str], None] | None = None) -> bool:
    """写系统剪贴板。优先 pyperclip, 失败退到各平台命令行工具。"""
    try:
        import pyperclip

        pyperclip.copy(text)
        return True
    except Exception:  # noqa: BLE001
        pass
    try:
        if sys.platform == "darwin":
            subprocess.run(["pbcopy"], input=text.encode("utf-8"), check=True)
            return True
        if sys.platform == "win32":
            # Windows 'clip' 读 UTF-16LE
            subprocess.run(["clip"], input=text.encode("utf-16-le"), check=True)
            return True
        subprocess.run(["xclip", "-selection", "clipboard"], input=text.encode("utf-8"), check=True)
        return True
    except Exception as exc:  # noqa: BLE001
        if log is not None:
            log(f"剪贴板写入失败: {exc}")
        return False
