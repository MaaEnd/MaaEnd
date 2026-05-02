#!/usr/bin/env python3
"""
下载并准备 3rdparty 依赖（不通过包管理器分发的二进制 SDK），统一安装到
仓库根目录的 3rdparty/ 下。

当前支持：
  --webview2  Microsoft.Web.WebView2 NuGet SDK（仅 Windows，cpp-algo 链接所需）

调用约定：
  - setup_workspace.py 在 main flow 中通过 subprocess 调用本脚本
  - .github/workflows/install.yml 在 CI 中直接调用本脚本

布局：
  <repo>/3rdparty/webview2/                              -- 解压后的 NuGet 包根目录
  <repo>/3rdparty/webview2/.maaend-webview2-version      -- 已安装版本边带文件
"""

import argparse
import platform
import shutil
import sys
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

from cli_support import Console, init_localization

PROJECT_BASE: Path = Path(__file__).parent.parent.resolve()
LOCALS_DIR: Path = Path(__file__).parent / "locals" / "3rdparty_download"
THIRDPARTY_DIR: Path = PROJECT_BASE / "3rdparty"

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


def _download_to_file(url: str, dest_path: Path) -> bool:
    """简易下载器：流式写入到目标文件，附 User-Agent 避免被 CDN 拒绝。

    依赖目标足够小（WebView2 NuGet 约 5MB）才足够好用；如需断点续传/进度条，
    应当切回 setup_workspace.download_file() 那套实现。
    """
    request = urllib.request.Request(
        url, headers={"User-Agent": "MaaEnd-3rdparty-download/1.0"}
    )
    try:
        with urllib.request.urlopen(request) as response, open(
            dest_path, "wb"
        ) as out_file:
            shutil.copyfileobj(response, out_file)
    except (urllib.error.URLError, OSError) as exc:
        print(Console.err(t("err_download_failed", url=url, error=exc)))
        dest_path.unlink(missing_ok=True)
        return False
    return True


def download_webview2(skip_if_exist: bool = True) -> bool:
    """
    在 Windows 平台下载 Microsoft.Web.WebView2 SDK 并解压到 3rdparty/webview2/。
    其它平台 no-op。

    版本通过 .maaend-webview2-version 边带文件追踪：
      - sentinel 头文件 + 版本匹配 → skip
      - 否则清空目录、重新下载、重新解压、重写版本文件
    """
    if platform.system().lower() != "windows":
        print(Console.ok(t("inf_webview2_skip_non_windows")))
        return True

    if skip_if_exist and WEBVIEW2_HEADER_SENTINEL.exists():
        installed_version = ""
        if WEBVIEW2_VERSION_SENTINEL.exists():
            try:
                installed_version = WEBVIEW2_VERSION_SENTINEL.read_text(
                    encoding="utf-8"
                ).strip()
            except OSError:
                installed_version = ""
        if installed_version == WEBVIEW2_SDK_VERSION:
            print(
                Console.ok(
                    t("inf_webview2_already_installed", version=installed_version)
                )
            )
            return True
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
    if args.webview2 or args.all:
        if not download_webview2(skip_if_exist=skip_if_exist):
            sys.exit(1)


if __name__ == "__main__":
    main()
