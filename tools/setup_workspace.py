import argparse
import os
import sys
import shutil
import zipfile
import subprocess
import platform
import urllib.request
import urllib.error
import json
import tempfile
from pathlib import Path
import time


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
    raise RuntimeError(
        f"Unrecognized operating system: {platform.system().lower()}"
    ) from e

try:
    ARCH_KEYWORD: str = {
        "amd64": "x86_64",
        "x86_64": "x86_64",
        "aarch64": "aarch64",
        "arm64": "aarch64",
    }[platform.machine().lower()]
except KeyError as e:
    raise RuntimeError(
        f"Unrecognized architecture: {platform.machine().lower()}"
    ) from e

try:
    MFW_DIST_NAME: str = {
        "win": "MaaFramework.dll",
        "linux": "libMaaFramework.so",
        "macos": "libMaaFramework.dylib",
    }[OS_KEYWORD]
except KeyError as e:
    raise RuntimeError(f"Unsupported OS for MaaFramework: {OS_KEYWORD}") from e

MXU_DIST_NAME: str = "mxu.exe" if OS_KEYWORD == "win" else "mxu"
TIMEOUT: int = 30
VERSION_FILE_NAME: str = "version.json"


def configure_token() -> None:
    """配置 GitHub Token，输出检测结果"""
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        print("[INF] 已配置 GitHub Token，将用于 API 请求")
    else:
        print("[WRN] 未配置 GitHub Token，将使用匿名 API 请求（可能限流）")
        print("[INF] 如遇 API 速率限制，请设置环境变量 GITHUB_TOKEN/GH_TOKEN")
    print("-" * 40)


def run_command(
    cmd: list[str] | str, cwd: Path | str | None = None, shell: bool = False
) -> bool:
    """执行命令并输出日志，返回是否成功"""
    cmd_str = " ".join(cmd) if isinstance(cmd, list) else str(cmd)
    print(f"[CMD] {cmd_str}")
    try:
        subprocess.check_call(cmd, cwd=cwd or PROJECT_BASE, shell=shell)
        print(f"[INF] 命令执行成功: {cmd_str}")
        return True
    except subprocess.CalledProcessError as e:
        print(f"[ERR] 命令执行失败: {cmd_str}\n  错误: {e}")
        return False


def update_submodules(skip_if_exist: bool = True) -> bool:
    print("[INF] 检查子模块...")
    if (
        not skip_if_exist
        or not (PROJECT_BASE / "assets" / "MaaCommonAssets" / "LICENSE").exists()
    ):
        print("[INF] 正在更新子模块...")
        return run_command(["git", "submodule", "update", "--init", "--recursive"])
    print("[INF] 子模块已存在")
    return True


def run_build_script() -> bool:
    print("[INF] 执行 build_and_install.py ...")
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
        print(f"[INF] 获取 {repo} 的最新发布信息...")

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
                    print(f"[INF] 匹配到资源: {asset['name']}")
                    tag_name = tag.get("tag_name") or tag.get("name")
                    return asset["browser_download_url"], asset["name"], tag_name

        raise ValueError("No matching asset found in the latest release (GitHub API)")
    except Exception as e:
        print(f"[ERR] 获取发布信息失败: {type(e).__name__} - {e}")

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
        print(f"[WRN] 读取版本文件失败，将忽略本地版本: {e}")
    return {}


def write_versions_file(path: Path, versions: dict[str, str]) -> None:
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"versions": versions}, f, ensure_ascii=False, indent=4)
        print(f"\n[INF] 已写入版本文件: {path}")
        print(f"[INF] 当前版本信息: {versions}")
    except Exception as e:
        print(f"[WRN] 写入版本文件失败: {e}")


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
        print(f"[INF] 开始下载: {url}")
        print(f"[INF] 正在连接...", end="", flush=True)
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
                    print(f"\r[INF] 正在下载... {progress_str}   ", end="", flush=True)
                    cached_progress_str = progress_str
            print()
        print(f"[INF] 下载完成: {dest_path}")
        return True
    except urllib.error.URLError as e:
        print(f"[ERR] 网络错误: {e.reason}")
    except Exception as e:
        print(f"[ERR] 下载失败: {type(e).__name__} - {e}")
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
        print("[INF] MaaFramework 已安装，跳过（如需更新，请使用 --update 参数）")
        return True, local_version, False

    url, filename, remote_version = get_latest_release_url(
        MFW_REPO, ["maa", OS_KEYWORD, ARCH_KEYWORD]
    )
    if not url or not filename:
        print("[ERR] 未找到 MaaFramework 下载链接")
        return False, local_version, False

    if (
        update_mode
        and maafw_installed
        and local_version
        and remote_version
        and compare_semver(local_version, remote_version) >= 0
    ):
        print(f"[INF] MaaFramework 已是最新版本 ({local_version})，跳过下载")
        return True, local_version, False

    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)
        download_path = tmp_path / filename
        if not download_file(url, download_path):
            return False, local_version, False

        if maafw_dest.exists():
            while True:
                try:
                    print(f"[INF] 正在尝试删除旧目录: {maafw_dest}")
                    shutil.rmtree(maafw_dest)
                    break
                except PermissionError as e:
                    print(f"\n[ERR] 访问被拒绝 (PermissionError): {e}")
                    print(f"[!] 无法删除 {maafw_dest}，请确保该程序已完全退出。")
                    cmd = (
                        input("[?] 请手动处理后按 Enter 重试，或输入 'q' 退出: ")
                        .strip()
                        .lower()
                    )
                    if cmd == "q":
                        return False, local_version, False
                except Exception as e:
                    print(f"[ERR] 清理目录时发生未知错误: {e}")
                    return False, local_version, False

        print("[INF] 解压 MaaFramework...")
        try:
            extract_root = tmp_path / "extracted"
            extract_root.mkdir(parents=True, exist_ok=True)

            # 使用 shutil.unpack_archive 自动识别格式进行解压
            shutil.unpack_archive(str(download_path), extract_root)

            maafw_dest.mkdir(parents=True, exist_ok=True)
            bin_found = False
            for root, dirs, _ in os.walk(extract_root):
                if "bin" in dirs:
                    bin_path = Path(root) / "bin"
                    print(f"[INF] 复制组件到 {maafw_dest}")
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
                print("[ERR] 解压后未找到 bin 目录")
                return False, local_version, False
            print("[INF] MaaFramework 安装完成\n")
            return True, remote_version or local_version, True
        except Exception as e:
            print(f"[ERR] MaaFramework 安装失败: {e}")
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
        print("[INF] MXU 已安装，跳过")
        return True, local_version, False

    url, filename, remote_version = get_latest_release_url(
        MXU_REPO, ["mxu", OS_KEYWORD, ARCH_KEYWORD]
    )
    if not url or not filename:
        print("[ERR] 未找到 MXU 下载链接")
        return False, local_version, False

    if (
        update_mode
        and mxu_installed
        and local_version
        and remote_version
        and compare_semver(local_version, remote_version) >= 0
    ):
        print(f"[INF] MXU 已是最新版本 ({local_version})，跳过下载")
        return True, local_version, False

    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)
        download_path = tmp_path / filename
        if not download_file(url, download_path):
            return False, local_version, False

        if mxu_path.exists():
            while True:
                try:
                    print(f"[INF] 正在尝试删除旧文件: {mxu_path}")
                    mxu_path.unlink()
                    break
                except PermissionError as e:
                    print(f"\n[ERR] 访问被拒绝 (PermissionError): {e}")
                    print(f"[!] 无法删除 {MXU_DIST_NAME}，请确保该程序已完全退出。")
                    cmd = (
                        input("[?] 请手动处理后按 Enter 重试，或输入 'q' 退出: ")
                        .strip()
                        .lower()
                    )
                    if cmd == "q":
                        return False, local_version, False
                except Exception as e:
                    print(f"[ERR] 删除文件时发生未知错误: {e}")
                    return False, local_version, False

        print("[INF] 解压并安装 MXU...")
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
                    print(f"[INF] 已更新: {item.name}")
                    if item.name.lower() == MXU_DIST_NAME.lower():
                        copied = True

            if not copied:
                print(f"[ERR] 未能找到 {MXU_DIST_NAME}")
                return False, local_version, False
            print("[INF] MXU 安装完成")
            return True, remote_version or local_version, True
        except Exception as e:
            print(f"[ERR] MXU 安装失败: {e}")
            return False, local_version, False


def main() -> None:
    parser = argparse.ArgumentParser(description="MaaEnd 构建工具：初始化并安装依赖项")
    parser.add_argument(
        "--update", action="store_true", help="当依赖项已存在时，是否进行更新操作"
    )
    parser.add_argument("--ci", action="store_true", help="CI 模式：不生成本地版本文件")
    args = parser.parse_args()

    install_dir = PROJECT_BASE / "install"
    version_file = install_dir / VERSION_FILE_NAME
    local_versions = read_versions_file(version_file)
    print("========== MaaEnd Workspace 初始化 ==========")
    configure_token()
    if not update_submodules(skip_if_exist=not args.update):
        print("[FATAL] 子模块更新失败，退出")
        sys.exit(1)
    print("\n========== 构建 Go Agent ==========")
    if not run_build_script():
        print("[FATAL] 构建脚本执行失败，退出")
        sys.exit(1)
    print("\n========== 下载依赖项 ==========")
    versions: dict[str, str] = dict(local_versions)
    any_downloaded = False
    ok, maafw_version, maafw_downloaded = install_maafw(
        install_dir,
        skip_if_exist=not args.update,
        update_mode=args.update,
        local_version=local_versions.get("maafw"),
    )
    if not ok:
        print("[FATAL] MaaFramework 安装失败，退出")
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
        print("[FATAL] MXU 安装失败，退出")
        sys.exit(1)
    if mxu_version:
        versions["mxu"] = mxu_version
    any_downloaded = any_downloaded or mxu_downloaded

    if not args.ci and any_downloaded:
        write_versions_file(version_file, versions)
    print("\n========== 设置完成 ==========")
    print(
        f"[INF] 工作区已经初始化/更新完毕，请运行 {install_dir / MXU_DIST_NAME} 来验证安装结果"
    )
    print(f"[INF] 后续使用相关工具编辑、调试等，都基于 {install_dir} 文件夹")
    print(
        f"[INF] 请阅读开发手册 (docs/developers/development.md) 中的「开发技巧」和「代码规范」后，再进行相关开发工作"
    )


if __name__ == "__main__":
    main()
