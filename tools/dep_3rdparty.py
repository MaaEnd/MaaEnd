#!/usr/bin/env python3
"""
下载并准备 3rdparty 依赖（不通过包管理器分发的二进制 SDK），统一安装到
当前唯一消费者 cpp-algo 自己的目录 agent/cpp-algo/3rdparty/ 下。

放在 cpp-algo 子目录里的好处：依赖与消费者就近、cmake 模块可以用纯相对
路径解析，根目录不再多出一个 3rdparty 文件夹。

当前支持：
  --webview2  Microsoft.Web.WebView2 NuGet SDK（仅 Windows，cpp-algo 链接所需）

调用约定：
  - setup_workspace.py 在 main flow 中直接 `from dep_3rdparty import ...` 调用，
    跳过情形下不再启动子进程
  - .github/workflows/install.yml 在 CI 中以 CLI 形式调用 `python tools/dep_3rdparty.py --webview2`

布局：
  agent/cpp-algo/3rdparty/webview2/                          -- 解压后的 NuGet 包根目录
  agent/cpp-algo/3rdparty/webview2/.maaend-webview2-version  -- 已安装版本边带文件
"""

import argparse
import platform
import socket
import shutil
import sys
import time
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

from cli_support import Console, init_localization

PROJECT_BASE: Path = Path(__file__).parent.parent.resolve()
# locale 资源仍沿用原目录名 3rdparty_download，避免迁徙文件；目录名只在本模块内部引用一次。
LOCALS_DIR: Path = Path(__file__).parent / "locals" / "3rdparty_download"
THIRDPARTY_DIR: Path = PROJECT_BASE / "agent" / "cpp-algo" / "3rdparty"
TIMEOUT: int = 30

# -------------------- WebView2 --------------------

# 最新版本可在 https://www.nuget.org/packages/Microsoft.Web.WebView2 查询。
WEBVIEW2_SDK_VERSION: str = "1.0.2210.55"
WEBVIEW2_NUGET_URL_TEMPLATE: str = (
    "https://www.nuget.org/api/v2/package/Microsoft.Web.WebView2/{version}"
)
WEBVIEW2_INSTALL_DIR: Path = THIRDPARTY_DIR / "webview2"
WEBVIEW2_VERSION_SENTINEL: Path = WEBVIEW2_INSTALL_DIR / ".maaend-webview2-version"
WEBVIEW2_HEADER_SENTINEL: Path = (
    WEBVIEW2_INSTALL_DIR / "build" / "native" / "include" / "WebView2.h"
)


_local_t = lambda key, **kwargs: key.format(**kwargs) if kwargs else key


def init_local() -> None:
    global _local_t
    t_func, load_error_path = init_localization(LOCALS_DIR)
    _local_t = t_func
    if load_error_path:
        print(Console.err(t("error_load_locale", path=load_error_path)))


def t(key: str, **kwargs) -> str:
    return _local_t(key, **kwargs)


def _format_size(size: int | None) -> str:
    if size is None or size < 0:
        return "--"
    s = float(size)
    for unit in ("B", "KB", "MB", "GB", "TB"):
        if s < 1024.0 or unit == "TB":
            return f"{s:.1f} {unit}"
        s /= 1024.0
    return "--"


def _format_speed(bps: float) -> str:
    if bps <= 0:
        return "--/s"
    s = float(bps)
    for unit in ("B/s", "KB/s", "MB/s", "GB/s"):
        if s < 1024.0 or unit == "GB/s":
            return f"{s:.1f} {unit}"
        s /= 1024.0
    return "--/s"


def _format_eta(sec: float | None) -> str:
    if sec is None or sec < 0:
        return "--:--:--"
    sec_int = int(sec)
    h = sec_int // 3600
    m = (sec_int % 3600) // 60
    s = sec_int % 60
    return f"{h:02d}:{m:02d}:{s:02d}"


def _download_to_file(url: str, dest_path: Path) -> bool:
    """流式下载到目标文件，带就地刷新的进度条。

    附 User-Agent 避免被 CDN 拒绝；目标体量很小（WebView2 NuGet 约 5MB），
    所以不做断点续传 / 缓存。如有需要请参考 setup_workspace.download_file()。
    """
    request = urllib.request.Request(
        url, headers={"User-Agent": "MaaEnd-3rdparty-download/1.0"}
    )
    try:
        with urllib.request.urlopen(request, timeout=TIMEOUT) as response, open(
            dest_path, "wb"
        ) as out_file:
            size_total = int(response.headers.get("Content-Length", 0) or 0)
            size_received = 0
            start_ts = time.time()
            cached_progress = ""

            while True:
                chunk = response.read(8192)
                if not chunk:
                    break
                out_file.write(chunk)
                size_received += len(chunk)

                elapsed = max(1e-6, time.time() - start_ts)
                speed = size_received / elapsed
                eta = None
                if size_total > 0 and speed > 0:
                    eta = (size_total - size_received) / speed

                percent = (
                    f"{size_received / size_total * 100:.1f}%"
                    if size_total > 0
                    else "--"
                )
                progress = (
                    f"{_format_size(size_received)}/{_format_size(size_total)} "
                    f"({percent}) | {_format_speed(speed)} | ETA {_format_eta(eta)}"
                )

                # 仅在文案变化时刷新，避免高频 chunk 把终端打爆。
                if progress != cached_progress:
                    print(
                        f"\r{Console.info(t('inf_webview2_downloading_progress', progress=progress))}",
                        end="",
                        flush=True,
                    )
                    cached_progress = progress
            # 把进度行收尾到下一行，避免与后续日志同行混排。
            print()
    except (urllib.error.URLError, OSError, TimeoutError) as exc:
        # 异常情况下进度行可能未换行，先补一个换行再打错误，避免错误信息黏在尾部。
        print()
        is_timeout = isinstance(exc, (TimeoutError, socket.timeout)) or (
            isinstance(exc, urllib.error.URLError)
            and isinstance(exc.reason, (TimeoutError, socket.timeout))
        )
        error_message = (
            f"request timed out after {TIMEOUT}s"
            if is_timeout
            else str(exc)
        )
        print(Console.err(t("err_download_failed", url=url, error=error_message)))
        dest_path.unlink(missing_ok=True)
        return False
    return True


def _read_installed_webview2_version() -> str:
    """读取本地 sentinel 文件中的版本号；缺失或不可读返回空串。"""
    if not WEBVIEW2_VERSION_SENTINEL.exists():
        return ""
    try:
        return WEBVIEW2_VERSION_SENTINEL.read_text(encoding="utf-8").strip()
    except OSError:
        return ""


def download_webview2(skip_if_exist: bool = True) -> bool:
    """
    在 Windows 平台下载 Microsoft.Web.WebView2 SDK 并解压到 agent/cpp-algo/3rdparty/webview2/。
    其它平台 no-op。

    跳过语义对齐 setup_workspace 中的 maafw/mxu 模块：
      - skip_if_exist=True（非 --update）：sentinel 头文件存在即跳过，不查版本
      - skip_if_exist=False（带 --update）：本地版本 == 期望版本时跳过，否则升级
      - 期望版本就是脚本里 hardcode 的 WEBVIEW2_SDK_VERSION
        （NuGet 这边不像 GitHub Release 有"远端 latest"的概念）
    """
    if platform.system().lower() != "windows":
        print(Console.ok(t("inf_webview2_skip_non_windows")))
        return True

    installed = WEBVIEW2_HEADER_SENTINEL.exists()
    installed_version = _read_installed_webview2_version() if installed else ""

    # 非 --update 模式：已安装就跳过，行为对齐 maafw/mxu 在 skip_if_exist=True 下的"目录非空即跳过"。
    if skip_if_exist and installed:
        print(
            Console.ok(
                t(
                    "inf_webview2_already_installed",
                    version=installed_version or "<unknown>",
                )
            )
        )
        return True

    # --update 模式：本地版本 == 期望版本就跳过，相当于 maafw 中"本地 >= 远端则不更新"。
    if installed and installed_version == WEBVIEW2_SDK_VERSION:
        print(
            Console.ok(
                t("inf_webview2_already_installed", version=installed_version)
            )
        )
        return True

    if installed:
        # 走到这说明 --update 且本地版本与期望不一致，告诉用户为什么重下。
        print(
            Console.info(
                t(
                    "inf_webview2_version_mismatch",
                    installed=installed_version or "<unknown>",
                    expected=WEBVIEW2_SDK_VERSION,
                )
            )
        )

    if WEBVIEW2_INSTALL_DIR.exists():
        try:
            shutil.rmtree(WEBVIEW2_INSTALL_DIR)
        except OSError as exc:
            print(Console.err(t("err_webview2_cleanup_failed", error=exc)))
            return False
    WEBVIEW2_INSTALL_DIR.mkdir(parents=True, exist_ok=True)

    nupkg_path = THIRDPARTY_DIR / f"webview2-{WEBVIEW2_SDK_VERSION}.nupkg"
    url = WEBVIEW2_NUGET_URL_TEMPLATE.format(version=WEBVIEW2_SDK_VERSION)

    print(Console.info(t("inf_webview2_downloading", version=WEBVIEW2_SDK_VERSION)))
    if not _download_to_file(url, nupkg_path):
        return False

    print(Console.info(t("inf_webview2_extracting", dest=WEBVIEW2_INSTALL_DIR)))
    try:
        with zipfile.ZipFile(nupkg_path) as archive:
            archive.extractall(WEBVIEW2_INSTALL_DIR)
    except (zipfile.BadZipFile, OSError) as exc:
        print(Console.err(t("err_webview2_extract_failed", error=exc)))
        nupkg_path.unlink(missing_ok=True)
        return False

    nupkg_path.unlink(missing_ok=True)

    if not WEBVIEW2_HEADER_SENTINEL.exists():
        print(Console.err(t("err_webview2_invalid", path=WEBVIEW2_HEADER_SENTINEL)))
        return False

    try:
        WEBVIEW2_VERSION_SENTINEL.write_text(WEBVIEW2_SDK_VERSION, encoding="utf-8")
    except OSError as exc:
        print(Console.warn(t("wrn_webview2_version_write_failed", error=exc)))

    print(Console.ok(t("inf_webview2_install_complete", path=WEBVIEW2_INSTALL_DIR)))
    return True


# -------------------- High-level API --------------------


def download_all(skip_if_exist: bool = True) -> bool:
    """下载所有支持的 3rdparty 依赖。

    供 setup_workspace.py 等其它脚本以 in-process 方式直接调用，
    避免每次都启动一个 Python 子进程；CLI `--all` 走的也是同一个入口。
    各依赖内部各自做"已存在则跳过"判断，因此重复调用代价很小。
    """
    return download_webview2(skip_if_exist=skip_if_exist)


# -------------------- CLI --------------------


def main() -> None:
    init_local()

    parser = argparse.ArgumentParser(description=t("description"))
    parser.add_argument("--webview2", action="store_true", help=t("arg_webview2"))
    parser.add_argument("--all", action="store_true", help=t("arg_all"))
    parser.add_argument("--update", action="store_true", help=t("arg_update"))
    args = parser.parse_args()

    if not (args.webview2 or args.all):
        parser.print_help()
        sys.exit(1)

    skip_if_exist = not args.update
    # --all 与 --webview2 在目前只有 WebView2 一个依赖时等价，留 --webview2 以兼容旧脚本。
    if args.all:
        ok = download_all(skip_if_exist=skip_if_exist)
    else:
        ok = download_webview2(skip_if_exist=skip_if_exist)
    if not ok:
        sys.exit(1)


if __name__ == "__main__":
    main()
