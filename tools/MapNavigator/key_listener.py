"""跨平台全局按键监听器，基于 pynput。

提供模块级 API：
- ``start()`` / ``stop()``：启停后台监听线程。
- ``register(key_name, callback)``：注册按键回调（按下瞬间触发，内置防抖）。
- ``unregister_all()``：清除所有注册的回调。
- ``ensure_privileges()``：检测并尝试获取必要的系统权限。

Windows 注意事项：
    监听全局按键需要以管理员身份运行。
    调用 ``ensure_privileges()`` 会自动检测并弹出 UAC 提权请求。

macOS 注意事项：
    首次运行时需要在 *系统设置 → 隐私与安全性 → 输入监控* 中授权终端或 Python。
"""

from __future__ import annotations

import os
import sys
import threading
import time
from typing import Callable

_lock = threading.Lock()
_listener = None
_started = False

# 回调注册表：key_name -> (callback, last_trigger_time)
_hotkeys: dict[str, tuple[Callable[[], None], float]] = {}
_hotkey_lock = threading.Lock()
_DEBOUNCE_SECONDS = 0.4


def register(key_name: str, callback: Callable[[], None]) -> None:
    """注册一个按键回调。按下瞬间在监听线程中触发，内置防抖。

    ``key_name`` 使用小写字母，例如 ``'g'``、``'x'``。
    ``callback`` 会在 pynput 的监听线程中被调用。
    """
    with _hotkey_lock:
        _hotkeys[key_name.lower()] = (callback, 0.0)


def unregister_all() -> None:
    """清除所有已注册的热键回调。"""
    with _hotkey_lock:
        _hotkeys.clear()


def start() -> None:
    """启动后台按键监听。重复调用安全，不会创建多个监听线程。"""
    global _listener, _started
    if _started:
        return

    try:
        from pynput import keyboard

        def on_press(key):
            name = _key_name(key)
            if not name:
                return
            with _hotkey_lock:
                entry = _hotkeys.get(name)
                if entry is None:
                    return
                callback, last_time = entry
                now = time.monotonic()
                if now - last_time < _DEBOUNCE_SECONDS:
                    return
                _hotkeys[name] = (callback, now)
            # 在锁外执行回调，避免死锁
            try:
                callback()
            except Exception as exc:
                print(f"Hotkey callback error [{name}]: {exc}")

        def on_release(key):
            pass

        _listener = keyboard.Listener(on_press=on_press, on_release=on_release)
        _listener.daemon = True
        _listener.start()
        _started = True
    except ImportError:
        print(
            "Warning: pynput is not installed. "
            "Recording hotkeys (G/X) will not work. "
            "Install with: pip install pynput"
        )
    except Exception as exc:
        print(f"Warning: Failed to start key listener: {exc}")


def stop() -> None:
    """停止后台按键监听并清空状态。"""
    global _listener, _started
    if _listener is not None:
        try:
            _listener.stop()
        except Exception:
            pass
        _listener = None
    _started = False
    unregister_all()


def ensure_privileges() -> bool:
    """检测当前进程是否具备全局按键监听所需的系统权限。

    - Windows：检测是否以管理员身份运行，若否则弹出 UAC 提权并重启。
    - macOS：提示用户授权输入监控权限。
    - Linux：通常不需要特殊权限（X11），直接放行。

    Returns:
        True 表示权限就绪，可以继续运行。
        False 表示已触发提权重启，当前进程应退出。
    """
    if sys.platform == "win32":
        return _ensure_windows_admin()
    if sys.platform == "darwin":
        _warn_macos_permissions()
        return True
    # Linux / 其他平台不需要特殊权限
    return True


def _ensure_windows_admin() -> bool:
    """Windows: 检测管理员权限，若不足则通过 UAC 提权重启。"""
    try:
        import ctypes
        if ctypes.windll.shell32.IsUserAnAdmin():
            return True
    except Exception:
        # 无法检测时假定有权限，让 pynput 自行报错
        return True

    print("当前未以管理员身份运行，正在请求提权...")
    try:
        import ctypes
        # 用 runas 动词触发 UAC 弹窗，以管理员身份重新启动当前脚本
        params = " ".join(f'"{arg}"' for arg in sys.argv)
        ret = ctypes.windll.shell32.ShellExecuteW(
            None, "runas", sys.executable, params, None, 1
        )
        # ShellExecuteW 返回值 > 32 表示成功
        if ret > 32:
            return False  # 提权成功，当前进程应退出
    except Exception as exc:
        print(f"提权失败: {exc}")

    # 提权失败或用户取消，继续以普通权限运行
    print("Warning: 未能获取管理员权限，全局热键可能在游戏窗口前台时不生效。")
    return True


def _warn_macos_permissions() -> None:
    """macOS: 提示用户需要授权输入监控权限。"""
    print(
        "提示: macOS 上全局按键监听需要授权。\n"
        "  如果热键不生效，请前往: 系统设置 → 隐私与安全性 → 输入监控\n"
        "  并授权当前终端应用或 Python 解释器。"
    )


def _key_name(key) -> str:
    """将 pynput 的 key 对象统一转成小写名称字符串。"""
    # 字符键 (a-z, 0-9 等)
    try:
        if hasattr(key, "char") and key.char:
            return key.char.lower()
    except AttributeError:
        pass

    # 特殊键 (shift, space, ctrl 等)
    try:
        if hasattr(key, "name") and key.name:
            return key.name.lower()
    except AttributeError:
        pass

    return ""
