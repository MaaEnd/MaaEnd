import json
import locale
import os
import platform
import sys
from pathlib import Path
from typing import Callable


class Ansi:
    RESET = "\033[0m"
    RED = "\033[31m"
    GREEN = "\033[32m"
    YELLOW = "\033[33m"
    BLUE = "\033[34m"
    MAGENTA = "\033[35m"
    CYAN = "\033[36m"

LANG_MAP = {
    "Chinese (Simplified)_China": "zh_cn",
    "Chinese (Traditional)_Taiwan": "zh_tw",
    "English_United States": "en_us",
    "Japanese_Japan": "ja_jp",
    "Korean_Korea": "ko_kr",
    "zh_cn": "zh_cn",
    "zh_tw": "zh_tw",
    "en_us": "en_us",
    "ja_jp": "ja_jp",
    "ko_kr": "ko_kr",
}

def _enable_windows_virtual_terminal() -> bool:
    if platform.system() != "Windows":
        return False
    try:
        import ctypes

        kernel32 = ctypes.windll.kernel32
        handle = kernel32.GetStdHandle(-11)  # STD_OUTPUT_HANDLE
        if handle in (0, -1):
            return False
        mode = ctypes.c_uint32()
        if kernel32.GetConsoleMode(handle, ctypes.byref(mode)) == 0:
            return False
        enable_vt = 0x0004  # ENABLE_VIRTUAL_TERMINAL_PROCESSING
        if mode.value & enable_vt:
            return True
        return kernel32.SetConsoleMode(handle, mode.value | enable_vt) != 0
    except Exception:
        return False


def supports_color() -> bool:
    if os.environ.get("NO_COLOR") is not None:
        return False
    if os.environ.get("FORCE_COLOR") is not None:
        return True
    if not hasattr(sys.stdout, "isatty") or not sys.stdout.isatty():
        return False
    if platform.system() == "Windows":
        return _enable_windows_virtual_terminal()
    return os.environ.get("TERM", "") not in ("", "dumb")


class Console:
    def __init__(self, enabled: bool | None = None) -> None:
        self.enabled = supports_color() if enabled is None else enabled

    def colorize(self, text: str, color: str) -> str:
        if not self.enabled:
            return text
        return f"{color}{text}{Ansi.RESET}"

    def hdr(self, text: str) -> str:
        return self.colorize(text, Ansi.MAGENTA)

    def step(self, text: str) -> str:
        return self.colorize(text, Ansi.MAGENTA)

    def ok(self, text: str) -> str:
        return self.colorize(text, Ansi.GREEN)

    def warn(self, text: str) -> str:
        return self.colorize(text, Ansi.YELLOW)

    def err(self, text: str) -> str:
        return self.colorize(text, Ansi.RED)

    def info(self, text: str) -> str:
        return self.colorize(text, Ansi.CYAN)


def init_localization(
    locals_dir: Path,
    lang_map: dict[str, str] = LANG_MAP,
    default_lang: str = "en_us",
) -> tuple[Callable[..., str], str | None]:
    lang = str(locale.getlocale()[0])
    if lang in lang_map:
        lang = lang_map[lang]
    elif lang.lower() in lang_map:
        lang = lang_map[lang.lower()]
    else:
        lang = default_lang

    lang_res: dict[str, str] = {}
    locale_file = locals_dir / f"{lang}.json"
    load_error_path: str | None = None

    try:
        with open(locale_file, "r", encoding="utf-8") as f:
            data = json.load(f)
        if isinstance(data, dict):
            lang_res = {str(k): str(v) for k, v in data.items()}
    except Exception:
        load_error_path = str(locale_file)

    def t(key: str, **kwargs) -> str:
        template = lang_res.get(key, key)
        try:
            return template.format(**kwargs)
        except Exception:
            return template

    return t, load_error_path

console = Console()
