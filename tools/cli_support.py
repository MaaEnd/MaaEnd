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

        # GetStdHandle returns NULL (0) for an invalid handle and INVALID_HANDLE_VALUE (-1) on error.
        # Treat both cases as "no usable console output handle" (for example, in a GUI app without a console).
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
    """
    Return True if ANSI color output should be used on the current stdout.  

    This helper centralizes the logic for deciding whether to emit ANSI escape  
    sequences. It respects common environment variables and platform-specific  
    behavior:  

    * `NO_COLOR` (if set) unconditionally disables color output.  
    * `FORCE_COLOR` (if set) unconditionally enables color output.  
    * Color is only enabled when `sys.stdout` is a TTY (interactive terminal).  
    * On Windows, virtual terminal processing must be available or successfully  
      enabled via `_enable_windows_virtual_terminal()`.  
    * On non-Windows platforms, `TERM` must be set to a non-empty value other  
      than `"dumb"` for color to be considered supported.  
    """
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
    """
    Helper for producing optionally colorized console text using ANSI escape codes.  

    The console can be configured to enable or disable color output. When disabled,  
    all helpers return the original text without any ANSI sequences.  
    """ 
    enabled = supports_color()

    @classmethod
    def colorize(cls, text: str, color: str) -> str:  
        """Wrap ``text`` in the given ANSI ``color`` code if color output is enabled.  

        Args:  
            text: The text to be colorized.  
            color: The ANSI color escape sequence to prefix the text with.  

        Returns:  
            The colorized text when colors are enabled; otherwise the original text.  
        """  
        if not cls.enabled:  
            return text  
        return f"{color}{text}{Ansi.RESET}"  

    @classmethod
    def hdr(cls, text: str) -> str:  
        """Return a header-style string, typically used for section titles."""  
        return cls.colorize(text, Ansi.MAGENTA)  

    @classmethod
    def step(cls, text: str) -> str:  
        """Return a step label string, e.g. for multi-step CLI workflows."""  
        return cls.colorize(text, Ansi.MAGENTA)  

    @classmethod
    def ok(cls, text: str) -> str:  
        """Return a success-style string."""  
        return cls.colorize(text, Ansi.GREEN)  

    @classmethod
    def warn(cls, text: str) -> str:  
        """Return a warning-style string."""  
        return cls.colorize(text, Ansi.YELLOW)  

    @classmethod
    def err(cls, text: str) -> str:  
        """Return an error-style string."""  
        return cls.colorize(text, Ansi.RED)  

    @classmethod
    def info(cls, text: str) -> str:  
        """Return an informational-style string."""  
        return cls.colorize(text, Ansi.CYAN)



def init_localization(
    locals_dir: Path,
    lang_map: dict[str, str] = LANG_MAP,
    default_lang: str = "en_us",
) -> tuple[Callable[..., str], str | None]:
    """
    Initialize localization by loading language resources for the current locale.
    This function determines the active language from the system locale and the
    provided `lang_map`, then attempts to load a corresponding JSON file from
    `locals_dir`. The file name is expected to be `<lang>.json`, where `<lang>`
    is the resolved language code (for example, ``en_us`` or ``zh_cn``).
    Parameters
    ----------
    locals_dir:
        Directory containing localization JSON files.
    lang_map:
        Mapping from system locale identifiers (e.g. ``"English_United States"``)
        or language codes (e.g. ``"en_us"``) to normalized language codes used
        to select the JSON file.
    default_lang:
        Language code to fall back to when the system locale cannot be mapped.
    Returns
    -------
    tuple[Callable[..., str], str | None]
        A pair ``(t, load_error_path)`` where:
        * ``t`` is a translation function that takes a string key and optional
          keyword arguments and returns a localized, ``str.format``-formatted
          string. If the key is missing or formatting fails, it returns the key
          or unformatted template.
        * ``load_error_path`` is the path to the locale file that failed to load,
          or ``None`` if the localization file was loaded successfully.
    """
    loc = locale.getlocale()
    lang = (loc[0] or "") if loc else ""

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
    except FileNotFoundError:
        load_error_path = str(locale_file)
        print(Console.err(f"[localization] locale file not found: {locale_file}"))
    except json.JSONDecodeError as e:
        load_error_path = str(locale_file)
        print(Console.err(f"[localization] failed to decode locale json: {locale_file}: {e}"))
    except OSError as e:
        load_error_path = str(locale_file)
        print(Console.err(f"[localization] failed to read locale file: {locale_file}: {e}"))
    except Exception as e:
        load_error_path = str(locale_file)
        print(Console.err(f"[localization] unexpected error while loading locale file: {locale_file}: {e}"))

    def t(key: str, **kwargs) -> str:
        template = lang_res.get(key, key)
        try:
            return template.format(**kwargs)
        except Exception:
            return template

    return t, load_error_path

