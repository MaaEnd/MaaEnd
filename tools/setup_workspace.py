import argparse
import os
import sys
import shutil
import subprocess
import platform
import urllib.request
import urllib.error
import json
import tempfile
from pathlib import Path
import time
import locale


# Localization (i18n)

TRANSLATIONS = {
    "zh_CN": {
        "token_configured": "[INF] 已配置 GitHub Token，将用于 API 请求",
        "token_not_configured": "[WRN] 未配置 GitHub Token，将使用匿名 API 请求（可能限流）",
        "token_rate_limit_hint": "[INF] 如遇 API 速率限制，请设置环境变量 GITHUB_TOKEN/GH_TOKEN",
        "cmd": "[CMD] {cmd}",
        "cmd_success": "[INF] 命令执行成功: {cmd}",
        "cmd_failed": "[ERR] 命令执行失败: {cmd}\n  错误: {error}",
        "checking_submodules": "[INF] 检查子模块...",
        "updating_submodules": "[INF] 正在更新子模块...",
        "submodules_exist": "[INF] 子模块已存在",
        "running_build_script": "[INF] 执行 build_and_install.py ...",
        "unrecognized_os": "Unrecognized operating system: {os}",
        "unrecognized_arch": "Unrecognized architecture: {arch}",
        "unsupported_os_mfw": "Unsupported OS for MaaFramework: {os}",
        "getting_release_info": "[INF] 获取 {repo} 的最新发布信息...",
        "matched_asset": "[INF] 匹配到资源: {name}",
        "get_release_failed": "[ERR] 获取发布信息失败: {error_type} - {error}",
        "read_version_failed": "[WRN] 读取版本文件失败，将忽略本地版本: {error}",
        "version_file_written": "\n[INF] 已写入版本文件: {path}",
        "current_version_info": "[INF] 当前版本信息: {versions}",
        "write_version_failed": "[WRN] 写入版本文件失败: {error}",
        "download_start": "[INF] 开始下载: {url}",
        "download_connecting": "[INF] 正在连接...",
        "download_progress": "[INF] 正在下载... {progress}   ",
        "download_complete": "[INF] 下载完成: {path}",
        "download_network_error": "[ERR] 网络错误: {reason}",
        "download_failed": "[ERR] 下载失败: {error_type} - {error}",
        "maafw_installed_skip": "[INF] MaaFramework 已安装，跳过（如需更新，请使用 --update 参数）",
        "maafw_download_link_not_found": "[ERR] 未找到 MaaFramework 下载链接",
        "maafw_latest_skip": "[INF] MaaFramework 已是最新版本 ({version})，跳过下载",
        "removing_old_dir": "[INF] 正在尝试删除旧目录: {path}",
        "permission_denied": "\n[ERR] 访问被拒绝 (PermissionError): {error}",
        "cannot_delete_dir": "[!] 无法删除 {path}，请确保该程序已完全退出。",
        "retry_or_quit": "[?] 请手动处理后按 Enter 重试，或输入 'q' 退出: ",
        "unknown_error_cleanup": "[ERR] 清理目录时发生未知错误: {error}",
        "extracting_maafw": "[INF] 解压 MaaFramework...",
        "copying_components": "[INF] 复制组件到 {dest}",
        "bin_not_found": "[ERR] 解压后未找到 bin 目录",
        "maafw_install_complete": "[INF] MaaFramework 安装完成\n",
        "maafw_install_failed": "[ERR] MaaFramework 安装失败: {error}",
        "mxu_installed_skip": "[INF] MXU 已安装，跳过",
        "mxu_download_link_not_found": "[ERR] 未找到 MXU 下载链接",
        "mxu_latest_skip": "[INF] MXU 已是最新版本 ({version})，跳过下载",
        "removing_old_file": "[INF] 正在尝试删除旧文件: {path}",
        "cannot_delete_file": "[!] 无法删除 {name}，请确保该程序已完全退出。",
        "unknown_error_delete": "[ERR] 删除文件时发生未知错误: {error}",
        "extracting_mxu": "[INF] 解压并安装 MXU...",
        "file_updated": "[INF] 已更新: {name}",
        "mxu_not_found": "[ERR] 未能找到 {name}",
        "mxu_install_complete": "[INF] MXU 安装完成",
        "mxu_install_failed": "[ERR] MXU 安装失败: {error}",
        "argparse_desc": "MaaEnd 构建工具：初始化并安装依赖项",
        "argparse_update": "当依赖项已存在时，是否进行更新操作",
        "argparse_ci": "CI 模式：不生成本地版本文件",
        "init_header": "========== MaaEnd Workspace 初始化 ==========",
        "submodule_update_failed": "[FATAL] 子模块更新失败，退出",
        "build_go_header": "\n========== 构建 Go Agent ==========",
        "build_script_failed": "[FATAL] 构建脚本执行失败，退出",
        "download_deps_header": "\n========== 下载依赖项 ==========",
        "maafw_install_failed_fatal": "[FATAL] MaaFramework 安装失败，退出",
        "mxu_install_failed_fatal": "[FATAL] MXU 安装失败，退出",
        "setup_complete_header": "\n========== 设置完成 ==========",
        "workspace_initialized": "[INF] 工作区已经初始化/更新完毕，可运行 {path} 来验证安装结果",
        "usage_hint": "[INF] 后续使用相关工具编辑、调试等，都基于 {dir} 文件夹",
        "read_dev_guide": "[INF] 请阅读开发手册 ({doc}) 中的「开发技巧」和「代码规范」后，再进行相关开发工作",
        "get_repo_infos": "[INF] 获取 {repo} 的最新发布信息...",
    },
    "en_US": {
        "token_configured": "[INF] GitHub Token configured, will be used for API requests",
        "token_not_configured": "[WRN] GitHub Token not configured, using anonymous API requests (may be rate limited)",
        "token_rate_limit_hint": "[INF] If you encounter API rate limit, please set environment variable GITHUB_TOKEN/GH_TOKEN",
        "cmd": "[CMD] {cmd}",
        "cmd_success": "[INF] Command executed successfully: {cmd}",
        "cmd_failed": "[ERR] Command execution failed: {cmd}\n  Error: {error}",
        "checking_submodules": "[INF] Checking submodules...",
        "updating_submodules": "[INF] Updating submodules...",
        "submodules_exist": "[INF] Submodules already exist",
        "running_build_script": "[INF] Running build_and_install.py ...",
        "unrecognized_os": "Unrecognized operating system: {os}",
        "unrecognized_arch": "Unrecognized architecture: {arch}",
        "unsupported_os_mfw": "Unsupported OS for MaaFramework: {os}",
        "getting_release_info": "[INF] Fetching latest release info for {repo}...",
        "matched_asset": "[INF] Matched asset: {name}",
        "get_release_failed": "[ERR] Failed to get release info: {error_type} - {error}",
        "read_version_failed": "[WRN] Failed to read version file, will ignore local version: {error}",
        "version_file_written": "\n[INF] Version file written: {path}",
        "current_version_info": "[INF] Current version info: {versions}",
        "write_version_failed": "[WRN] Failed to write version file: {error}",
        "download_start": "[INF] Starting download: {url}",
        "download_connecting": "[INF] Connecting...",
        "download_progress": "[INF] Downloading... {progress}   ",
        "download_complete": "[INF] Download complete: {path}",
        "download_network_error": "[ERR] Network error: {reason}",
        "download_failed": "[ERR] Download failed: {error_type} - {error}",
        "maafw_installed_skip": "[INF] MaaFramework already installed, skipping (use --update to update)",
        "maafw_download_link_not_found": "[ERR] MaaFramework download link not found",
        "maafw_latest_skip": "[INF] MaaFramework is already up to date ({version}), skipping download",
        "removing_old_dir": "[INF] Attempting to remove old directory: {path}",
        "permission_denied": "\n[ERR] Permission denied (PermissionError): {error}",
        "cannot_delete_dir": "[!] Cannot delete {path}, please ensure the program has fully exited.",
        "retry_or_quit": "[?] Press Enter to retry after manual handling, or type 'q' to quit: ",
        "unknown_error_cleanup": "[ERR] Unknown error during cleanup: {error}",
        "extracting_maafw": "[INF] Extracting MaaFramework...",
        "copying_components": "[INF] Copying components to {dest}",
        "bin_not_found": "[ERR] bin directory not found after extraction",
        "maafw_install_complete": "[INF] MaaFramework installation complete\n",
        "maafw_install_failed": "[ERR] MaaFramework installation failed: {error}",
        "mxu_installed_skip": "[INF] MXU already installed, skipping",
        "mxu_download_link_not_found": "[ERR] MXU download link not found",
        "mxu_latest_skip": "[INF] MXU is already up to date ({version}), skipping download",
        "removing_old_file": "[INF] Attempting to remove old file: {path}",
        "cannot_delete_file": "[!] Cannot delete {name}, please ensure the program has fully exited.",
        "unknown_error_delete": "[ERR] Unknown error during file deletion: {error}",
        "extracting_mxu": "[INF] Extracting and installing MXU...",
        "file_updated": "[INF] Updated: {name}",
        "mxu_not_found": "[ERR] Could not find {name}",
        "mxu_install_complete": "[INF] MXU installation complete",
        "mxu_install_failed": "[ERR] MXU installation failed: {error}",
        "argparse_desc": "MaaEnd build tool: Initialize and install dependencies",
        "argparse_update": "Whether to perform update operation when dependencies already exist",
        "argparse_ci": "CI mode: Do not generate local version file",
        "init_header": "========== MaaEnd Workspace Initialization ==========",
        "submodule_update_failed": "[FATAL] Submodule update failed, exiting",
        "build_go_header": "\n========== Building Go Agent ==========",
        "build_script_failed": "[FATAL] Build script execution failed, exiting",
        "download_deps_header": "\n========== Downloading Dependencies ==========",
        "maafw_install_failed_fatal": "[FATAL] MaaFramework installation failed, exiting",
        "mxu_install_failed_fatal": "[FATAL] MXU installation failed, exiting",
        "setup_complete_header": "\n========== Setup Complete ==========",
        "workspace_initialized": "[INF] Workspace has been initialized/updated, you can run {path} to verify the installation",
        "usage_hint": "[INF] Subsequent use of related tools for editing, debugging, etc., are all based on {dir} folder",
        "read_dev_guide": "[INF] Please read the 'Development Tips' and 'Code Standards' sections in the development manual ({doc}) before development work",
        "get_repo_infos": "[INF] Getting latest release info for {repo}...",
    },
}


def detect_locale() -> str:
    try:
        # 尝试多种方式获取系统语言
        system_locale = None

        try:
            loc = locale.getdefaultlocale()[0]
            if loc:
                system_locale = loc
        except Exception:
            pass

        if not system_locale:
            try:
                loc = locale.getlocale()[0]
                if loc:
                    system_locale = loc
            except Exception:
                pass

        if system_locale:
            # 标准化 locale 格式（将 - 替换为 _）
            lang = system_locale.replace("-", "_")

            # 检查是否直接支持（例如 zh_CN）
            if lang in TRANSLATIONS:
                return lang

            # 尝试基础语言匹配（例如 zh_CN.UTF-8 -> zh_CN）
            base_lang = lang.split(".")[0]
            if base_lang in TRANSLATIONS:
                return base_lang

            # 尝试主语言匹配（例如 zh_CN -> zh, Chinese_China -> zh）
            main_lang = base_lang.split("_")[0].lower()

            # 处理 Windows 风格的语言名称（如 Chinese (Simplified)_China -> zh_CN）
            if "chinese" in main_lang:
                if (
                    "traditional" in system_locale.lower()
                    or "taiwan" in system_locale.lower()
                ):
                    return "zh_TW"
                return "zh_CN"

            # 通过语言代码前缀匹配（例如 zh -> zh_CN）
            for supported_lang in TRANSLATIONS.keys():
                if supported_lang.lower().startswith(main_lang + "_"):
                    return supported_lang
    except Exception:
        pass

    # 回退到英语
    return "en_US"


# 全局语言设置
CURRENT_LOCALE = detect_locale()


def _(key: str, **kwargs) -> str:
    """
    获取本地化文本。

    Args:
        key: 翻译键
        **kwargs: 格式化参数

    Returns:
        本地化后的文本
    """
    translation = TRANSLATIONS.get(CURRENT_LOCALE, TRANSLATIONS["en_US"]).get(
        key, TRANSLATIONS["en_US"].get(key, key)
    )
    if kwargs:
        return translation.format(**kwargs)
    return translation


PROJECT_BASE: Path = Path(__file__).parent.parent.resolve()
MFW_REPO: str = "MaaXYZ/MaaFramework"
MXU_REPO: str = "MistEO/MXU"

try:
    OS_KEYWORD: str = {
        "windows": "win",
        "linux": "linux",
        "darwin": "macos",
    }[platform.system().lower()]
except KeyError as e:
    raise RuntimeError(_("unrecognized_os", os=platform.system().lower())) from e

try:
    ARCH_KEYWORD: str = {
        "amd64": "x86_64",
        "x86_64": "x86_64",
        "aarch64": "aarch64",
        "arm64": "aarch64",
    }[platform.machine().lower()]
except KeyError as e:
    raise RuntimeError(_("unrecognized_arch", arch=platform.machine().lower())) from e

try:
    MFW_DIST_NAME: str = {
        "win": "MaaFramework.dll",
        "linux": "libMaaFramework.so",
        "macos": "libMaaFramework.dylib",
    }[OS_KEYWORD]
except KeyError as e:
    raise RuntimeError(_("unsupported_os_mfw", os=OS_KEYWORD)) from e

MXU_DIST_NAME: str = "mxu.exe" if OS_KEYWORD == "win" else "mxu"
TIMEOUT: int = 30
VERSION_FILE_NAME: str = "version.json"


def configure_token() -> None:
    """配置 GitHub Token，输出检测结果"""
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        print(_("token_configured"))
    else:
        print(_("token_not_configured"))
        print(_("token_rate_limit_hint"))
    print("-" * 40)


def run_command(
    cmd: list[str] | str, cwd: Path | str | None = None, shell: bool = False
) -> bool:
    """执行命令并输出日志，返回是否成功"""
    cmd_str = " ".join(cmd) if isinstance(cmd, list) else str(cmd)
    print(_("cmd", cmd=cmd_str))
    try:
        subprocess.check_call(cmd, cwd=cwd or PROJECT_BASE, shell=shell)
        print(_("cmd_success", cmd=cmd_str))
        return True
    except subprocess.CalledProcessError as e:
        print(_("cmd_failed", cmd=cmd_str, error=e))
        return False


def update_submodules(skip_if_exist: bool = True) -> bool:
    print(_("checking_submodules"))
    if (
        not skip_if_exist
        or not (PROJECT_BASE / "assets" / "MaaCommonAssets" / "LICENSE").exists()
    ):
        print(_("updating_submodules"))
        return run_command(["git", "submodule", "update", "--init", "--recursive"])
    print(_("submodules_exist"))
    return True


def run_build_script() -> bool:
    print(_("running_build_script"))
    script_path = PROJECT_BASE / "tools" / "build_and_install.py"
    return run_command([sys.executable, str(script_path)])


def get_latest_release_url(
    repo: str, keywords: list[str], prerelease: bool = True
) -> tuple[str | None, str | None, str | None]:
    """
    获取指定 GitHub 仓库 Release 中首个符合是否预发布要求，且匹配所有关键字的资源下载链接和文件名。

    https://docs.github.com/en/rest/releases/releases?apiVersion=2022-11-28#list-releases
    """
    api_url = f"https://api.github.com/repos/{repo}/releases"
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")

    try:
        print(_("get_repo_infos", repo=repo))

        req = urllib.request.Request(api_url)
        if token:
            req.add_header("Authorization", f"Bearer {token}")
        req.add_header("Accept", "application/vnd.github+json")
        req.add_header("User-Agent", "MaaEnd-setup")
        req.add_header("X-GitHub-Api-Version", "2022-11-28")

        with urllib.request.urlopen(req, timeout=TIMEOUT) as res:
            tags = json.loads(res.read().decode())
            assert isinstance(tags, list)
            if not tags:
                raise ValueError("No releases found (GitHub API)")

        for tag in tags:
            assert isinstance(tag, dict)
            if (
                not prerelease
                and tag.get("prerelease", False)
                or tag.get("draft", False)
            ):
                continue
            assets = tag.get("assets", [])
            assert isinstance(assets, list)

            for asset in assets:
                assert isinstance(asset, dict)
                name = asset["name"].lower()
                if all(k.lower() in name for k in keywords):
                    print(_("matched_asset", name=asset["name"]))
                    tag_name = tag.get("tag_name") or tag.get("name")
                    return asset["browser_download_url"], asset["name"], tag_name

        raise ValueError("No matching asset found in the latest release (GitHub API)")
    except Exception as e:
        print(_("get_release_failed", error_type=type(e).__name__, error=e))

    return None, None, None


def read_versions_file(path: Path) -> dict[str, str]:
    if not path.exists():
        return {}
    try:
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)
        versions = data.get("versions", {})
        if isinstance(versions, dict):
            return {str(k): str(v) for k, v in versions.items()}
    except Exception as e:
        print(_("read_version_failed", error=e))
    return {}


def write_versions_file(path: Path, versions: dict[str, str]) -> None:
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"versions": versions}, f, ensure_ascii=False, indent=4)
        print(_("version_file_written", path=path))
        print(_("current_version_info", versions=versions))
    except Exception as e:
        print(_("write_version_failed", error=e))


def parse_semver(version: str) -> list[int]:
    if not version:
        return []
    v = version.strip()
    if v.startswith("v") or v.startswith("V"):
        v = v[1:]
    if "-" in v:
        v = v.split("-", 1)[0]
    parts = v.split(".")
    numbers: list[int] = []
    for part in parts:
        num = ""
        for ch in part:
            if ch.isdigit():
                num += ch
            else:
                break
        if num == "":
            numbers.append(0)
        else:
            numbers.append(int(num))
    return numbers


def compare_semver(a: str | None, b: str | None) -> int:
    if not a and not b:
        return 0
    if a and not b:
        return 1
    if b and not a:
        return -1
    left = parse_semver(a or "")
    right = parse_semver(b or "")
    max_len = max(len(left), len(right))
    left += [0] * (max_len - len(left))
    right += [0] * (max_len - len(right))
    for l, r in zip(left, right):
        if l > r:
            return 1
        if l < r:
            return -1
    return 0


def download_file(url: str, dest_path: Path) -> bool:
    """下载文件到指定路径。"""

    def to_percentage(current: float, total: float) -> str:
        return f"{(current / total) * 100:.1f}%" if total > 0 else ""

    def to_file_size(size: int | None) -> str:
        if size is None or size < 0:
            return "--"
        s = float(size)
        for unit in ["B", "KB", "MB", "GB", "TB"]:
            if s < 1024.0 or unit == "TB":
                return f"{s:.1f} {unit}"
            s /= 1024.0
        return "--"

    def to_speed(bps: float) -> str:
        if bps is None or bps <= 0:
            return "--/s"
        s = float(bps)
        for unit in ["B/s", "KB/s", "MB/s", "GB/s"]:
            if s < 1024.0 or unit == "GB/s":
                return f"{s:.1f} {unit}"
            s /= 1024.0
        return "--/s"

    def seconds_to_hms(sec: float | None) -> str:
        if sec is None or sec < 0:
            return "--:--:--"
        sec = int(sec)
        h = sec // 3600
        m = (sec % 3600) // 60
        s = sec % 60
        return f"{h:02d}:{m:02d}:{s:02d}"

    try:
        print(_("download_start", url=url))
        print(_("download_connecting"), end="", flush=True)
        with (
            urllib.request.urlopen(url, timeout=TIMEOUT) as res,
            open(dest_path, "wb") as out_file,
        ):
            size_total = int(res.headers.get("Content-Length", 0) or 0)
            size_received = 0
            cached_progress_str = ""
            start_ts = time.time()
            # read loop
            while True:
                chunk = res.read(8192)
                if not chunk:
                    break
                out_file.write(chunk)
                size_received += len(chunk)

                elapsed = max(1e-6, time.time() - start_ts)
                speed = size_received / elapsed
                eta = None
                if size_total > 0 and speed > 0:
                    eta = (size_total - size_received) / speed

                progress_str = (
                    f"{to_file_size(size_received)}/{to_file_size(size_total)} "
                    f"({to_percentage(size_received, size_total)}) | "
                    f"{to_speed(speed)} | ETA {seconds_to_hms(eta)}"
                )

                if progress_str != cached_progress_str:
                    print(
                        f"\r{_('download_progress', progress=progress_str)}",
                        end="",
                        flush=True,
                    )
                    cached_progress_str = progress_str
            print()
        print(_("download_complete", path=dest_path))
        return True
    except urllib.error.URLError as e:
        print(_("download_network_error", reason=e.reason))
    except Exception as e:
        print(_("download_failed", error_type=type(e).__name__, error=e))
    return False


def install_maafw(
    install_root: Path,
    skip_if_exist: bool = True,
    update_mode: bool = False,
    local_version: str | None = None,
) -> tuple[bool, str | None, bool]:
    """安装 MaaFramework，若遇占用则提示用户手动处理"""
    real_install_root = install_root.resolve()
    maafw_dest = real_install_root / "maafw"
    maafw_installed = (maafw_dest / MFW_DIST_NAME).exists()

    if skip_if_exist and maafw_installed:
        print(_("maafw_installed_skip"))
        return True, local_version, False

    url, filename, remote_version = get_latest_release_url(
        MFW_REPO, ["maa", OS_KEYWORD, ARCH_KEYWORD]
    )
    if not url or not filename:
        print(_("maafw_download_link_not_found"))
        return False, local_version, False

    if (
        update_mode
        and maafw_installed
        and local_version
        and remote_version
        and compare_semver(local_version, remote_version) >= 0
    ):
        print(_("maafw_latest_skip", version=local_version))
        return True, local_version, False

    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)
        download_path = tmp_path / filename
        if not download_file(url, download_path):
            return False, local_version, False

        if maafw_dest.exists():
            while True:
                try:
                    print(_("removing_old_dir", path=maafw_dest))
                    shutil.rmtree(maafw_dest)
                    break
                except PermissionError as e:
                    print(_("permission_denied", error=e))
                    print(_("cannot_delete_dir", path=maafw_dest))
                    cmd = input(_("retry_or_quit")).strip().lower()
                    if cmd == "q":
                        return False, local_version, False
                except Exception as e:
                    print(_("unknown_error_cleanup", error=e))
                    return False, local_version, False

        print(_("extracting_maafw"))
        try:
            extract_root = tmp_path / "extracted"
            extract_root.mkdir(parents=True, exist_ok=True)

            # 使用 shutil.unpack_archive 自动识别格式进行解压
            shutil.unpack_archive(str(download_path), extract_root)

            maafw_dest.mkdir(parents=True, exist_ok=True)
            bin_found = False
            for root, dirs, files in os.walk(extract_root):
                if "bin" in dirs:
                    bin_path = Path(root) / "bin"
                    print(_("copying_components", dest=maafw_dest))
                    for item in bin_path.iterdir():
                        dest_item = maafw_dest / item.name
                        if item.is_dir():
                            if dest_item.exists():
                                shutil.rmtree(dest_item)
                            shutil.copytree(item, dest_item)
                        else:
                            shutil.copy2(item, dest_item)
                    bin_found = True
                    break

            if not bin_found:
                print(_("bin_not_found"))
                return False, local_version, False
            print(_("maafw_install_complete"))
            return True, remote_version or local_version, True
        except Exception as e:
            print(_("maafw_install_failed", error=e))
            return False, local_version, False


def install_mxu(
    install_root: Path,
    skip_if_exist: bool = True,
    update_mode: bool = False,
    local_version: str | None = None,
) -> tuple[bool, str | None, bool]:
    """安装 MXU，若遇占用则提示用户手动处理"""
    real_install_root = install_root.resolve()
    mxu_path = real_install_root / MXU_DIST_NAME
    mxu_installed = mxu_path.exists()

    if skip_if_exist and mxu_installed:
        print(_("mxu_installed_skip"))
        return True, local_version, False

    url, filename, remote_version = get_latest_release_url(
        MXU_REPO, ["mxu", OS_KEYWORD, ARCH_KEYWORD]
    )
    if not url or not filename:
        print(_("mxu_download_link_not_found"))
        return False, local_version, False

    if (
        update_mode
        and mxu_installed
        and local_version
        and remote_version
        and compare_semver(local_version, remote_version) >= 0
    ):
        print(_("mxu_latest_skip", version=local_version))
        return True, local_version, False

    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)
        download_path = tmp_path / filename
        if not download_file(url, download_path):
            return False, local_version, False

        if mxu_path.exists():
            while True:
                try:
                    print(_("removing_old_file", path=mxu_path))
                    mxu_path.unlink()
                    break
                except PermissionError as e:
                    print(_("permission_denied", error=e))
                    print(_("cannot_delete_file", name=MXU_DIST_NAME))
                    cmd = input(_("retry_or_quit")).strip().lower()
                    if cmd == "q":
                        return False, local_version, False
                except Exception as e:
                    print(_("unknown_error_delete", error=e))
                    return False, local_version, False

        print(_("extracting_mxu"))
        try:
            extract_root = tmp_path / "extracted"
            extract_root.mkdir(parents=True, exist_ok=True)

            # 使用 shutil.unpack_archive 自动识别格式进行解压
            shutil.unpack_archive(str(download_path), extract_root)

            real_install_root.mkdir(parents=True, exist_ok=True)
            target_files = [MXU_DIST_NAME]
            if OS_KEYWORD == "win":
                target_files.append("mxu.pdb")

            copied = False
            for item in extract_root.iterdir():
                if item.name.lower() in [f.lower() for f in target_files]:
                    dest = real_install_root / item.name
                    shutil.copy2(item, dest)
                    print(_("file_updated", name=item.name))
                    if item.name.lower() == MXU_DIST_NAME.lower():
                        copied = True

            if not copied:
                print(_("mxu_not_found", name=MXU_DIST_NAME))
                return False, local_version, False
            print(_("mxu_install_complete"))
            return True, remote_version or local_version, True
        except Exception as e:
            print(_("mxu_install_failed", error=e))
            return False, local_version, False


def main() -> None:
    parser = argparse.ArgumentParser(description=_("argparse_desc"))
    parser.add_argument("--update", action="store_true", help=_("argparse_update"))
    parser.add_argument("--ci", action="store_true", help=_("argparse_ci"))
    args = parser.parse_args()

    install_dir = PROJECT_BASE / "install"
    version_file = install_dir / VERSION_FILE_NAME
    local_versions = read_versions_file(version_file)
    print(_("init_header"))
    configure_token()
    if not update_submodules(skip_if_exist=not args.update):
        print(_("submodule_update_failed"))
        sys.exit(1)
    print(_("build_go_header"))
    if not run_build_script():
        print(_("build_script_failed"))
        sys.exit(1)
    print(_("download_deps_header"))
    versions: dict[str, str] = dict(local_versions)
    any_downloaded = False
    ok, maafw_version, maafw_downloaded = install_maafw(
        install_dir,
        skip_if_exist=not args.update,
        update_mode=args.update,
        local_version=local_versions.get("maafw"),
    )
    if not ok:
        print(_("maafw_install_failed_fatal"))
        sys.exit(1)
    if maafw_version:
        versions["maafw"] = maafw_version
    any_downloaded = any_downloaded or maafw_downloaded

    ok, mxu_version, mxu_downloaded = install_mxu(
        install_dir,
        skip_if_exist=not args.update,
        update_mode=args.update,
        local_version=local_versions.get("mxu"),
    )
    if not ok:
        print(_("mxu_install_failed_fatal"))
        sys.exit(1)
    if mxu_version:
        versions["mxu"] = mxu_version
    any_downloaded = any_downloaded or mxu_downloaded

    if not args.ci and any_downloaded:
        write_versions_file(version_file, versions)
    print(_("setup_complete_header"))
    print(_("workspace_initialized", path=install_dir / MXU_DIST_NAME))
    print(_("usage_hint", dir=install_dir))

    dev_doc = PROJECT_BASE / "docs/developers/development.md"
    print(_("read_dev_guide", doc=dev_doc))


if __name__ == "__main__":
    main()
