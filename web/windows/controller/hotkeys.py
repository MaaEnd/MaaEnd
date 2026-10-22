"""全局热键（Windows）：在没有标题栏/不在任务栏的窗口上，留一条键盘出口。

用 `RegisterHotKey(None, ...)` 把热键挂到**当前线程**上，线程里跑 `GetMessage` 循环接收
`WM_HOTKEY`——因此不需要真的存在一个窗口，也不受焦点影响（前台是别的程序时照样触发）。

    from hotkeys import install
    hk = install({"ctrl+alt+h": lambda: print("hide"), "ctrl+alt+q": quit_fn})
    hk.registered   # {"ctrl+alt+h": True, ...}  被占用的是 False，会在日志里说明
    hk.stop()

为什么不用 WinForms 的 ShowInTaskbar / 也不用 pywin32：前者会重建窗口句柄（热键跟着失效），
后者是一个额外依赖；这里只要 ctypes。
"""

from __future__ import annotations

import ctypes
import ctypes.wintypes as wt
import sys
import threading

MOD_ALT = 0x0001
MOD_CONTROL = 0x0002
MOD_SHIFT = 0x0004
MOD_NOREPEAT = 0x4000
WM_HOTKEY = 0x0312
WM_QUIT = 0x0012

_MODS = {"ctrl": MOD_CONTROL, "control": MOD_CONTROL, "alt": MOD_ALT, "shift": MOD_SHIFT}
_VK = {
    "h": 0x48, "q": 0x51, "t": 0x54, "b": 0x42, "g": 0x47, "p": 0x50,
    "s": 0x53, "w": 0x57, "c": 0x43, "[": 0xDB, "]": 0xDD,
}

user32 = ctypes.WinDLL("user32", use_last_error=True)
kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)


def parse(combo: str) -> tuple[int, int] | None:
    """'ctrl+alt+h' -> (modifiers, vk)；认不出就返回 None。"""
    parts = [p.strip().lower() for p in combo.split("+") if p.strip()]
    if not parts:
        return None
    mods = 0
    for p in parts[:-1]:
        if p not in _MODS:
            return None
        mods |= _MODS[p]
    key = parts[-1]
    if key not in _VK:
        return None
    return mods, _VK[key]


class Listener(threading.Thread):
    def __init__(self, bindings: dict, log=print) -> None:
        super().__init__(daemon=True, name="hotkeys")
        self._bindings = bindings
        self._log = log
        self.registered: dict[str, bool] = {}
        self.tid = 0
        self._ready = threading.Event()

    def run(self) -> None:
        self.tid = kernel32.GetCurrentThreadId()
        actions: dict[int, object] = {}
        for i, (combo, fn) in enumerate(self._bindings.items(), start=1):
            parsed = parse(combo)
            if not parsed:
                self.registered[combo] = False
                self._log(f"[hotkey] 认不出组合 {combo!r}")
                continue
            mods, vk = parsed
            ok = user32.RegisterHotKey(None, i, mods | MOD_NOREPEAT, vk)
            self.registered[combo] = bool(ok)
            if ok:
                actions[i] = fn
                self._log(f"[hotkey] {combo} 已注册")
            else:
                self._log(f"[hotkey] {combo} 注册失败（被别的程序占用？）")
        self._ready.set()

        msg = wt.MSG()
        while user32.GetMessageW(ctypes.byref(msg), None, 0, 0) > 0:
            if msg.message == WM_HOTKEY:
                fn = actions.get(int(msg.wParam))
                if fn is None:
                    continue
                try:
                    fn()
                except Exception as e:  # 热键回调不该把线程炸掉
                    self._log(f"[hotkey] 回调异常：{type(e).__name__}: {e}")

    def stop(self, timeout: float = 2.0) -> None:
        for i in range(1, len(self._bindings) + 1):
            user32.UnregisterHotKey(None, i)
        if self.tid:
            user32.PostThreadMessageW(self.tid, WM_QUIT, 0, 0)
        self.join(timeout)


def install(bindings: dict, log=print) -> Listener:
    """在当前进程装一组热键，返回 Listener（`registered` 里能看到谁没抢到）。"""
    if sys.platform != "win32":
        lst = Listener({}, log)
        lst.registered = dict.fromkeys(bindings, False)
        log("[hotkey] 仅 Windows 支持")
        return lst
    lst = Listener(bindings, log)
    lst.start()
    lst._ready.wait(3.0)
    return lst
