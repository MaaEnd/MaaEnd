"""文件系统路径/链接工具（供 tools.setup 各模块共享）。

集中管理 Windows Junction 与目录链接的识别、删除语义，避免各脚本各自
实现导致行为发散（例如把链接误判为普通目录并对其 shutil.rmtree，从而
穿透删除链接目标目录）。
"""

import os
import shutil
from pathlib import Path


def is_directory_link(path: Path) -> bool:
    """判断路径是否为链接（含 Windows Junction，兼容 Python < 3.12）。

    - 任意平台：Path.is_symlink() 识别符号链接。
    - Python 3.12+：Path.is_junction() 在所有平台存在（非 Windows 返回 False），
      专门识别 Windows Junction。
    - Python 3.8–3.11：Path.is_symlink()/os.path.islink() 不把 Windows Junction
      当作链接（实测 Python 3.11 下对 Junction 返回 False），仅在 Windows 上通过
      lstat().st_reparse_tag 识别（Junction 为 IO_REPARSE_TAG_MOUNT_POINT）；
      其它平台不探测，避免无谓的 lstat 系统调用与依赖 AttributeError 的平台探测。
    """
    if path.is_symlink():
        return True
    if hasattr(path, "is_junction"):
        try:
            return path.is_junction()
        except OSError:
            return False
    if os.name == "nt":
        try:
            return bool(path.lstat().st_reparse_tag)
        except OSError:
            return False
    return False


def remove_directory_or_link(path: Path) -> None:
    """删除目录或链接：链接/符号链接只删自身，真实目录递归删除。

    若误把链接当作普通目录调用 shutil.rmtree，shutil 会抛
    "Cannot call rmtree on a symbolic link"（为保护链接目标目录）。
    本函数保证：对链接只 unlink 自身，绝不穿透删除目标。
    """
    if is_directory_link(path):
        path.unlink(missing_ok=True)
    elif path.is_dir():
        shutil.rmtree(path)
    elif path.exists() or path.is_symlink():
        path.unlink(missing_ok=True)
