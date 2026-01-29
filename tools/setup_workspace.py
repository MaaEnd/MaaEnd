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
import time
from pathlib import Path


project_base: Path = Path(__file__).parent.parent.resolve()
MAA_FW_REPO: str = "MaaXYZ/MaaFramework"
MXU_REPO: str = "MistEO/MXU"
MAX_RETRIES: int = 3
RETRY_DELAY: int = 2  # 秒

_system = platform.system().lower()
_machine = platform.machine().lower()
OS_KEYWORD: str = {"windows": "win", "linux": "linux", "darwin": "macos"}.get(_system, _system)
ARCH_KEYWORD: str = {"amd64": "x86_64", "x86_64": "x86_64", "aarch64": "aarch64", "arm64": "aarch64"}.get(_machine, _machine)
MXU_NAME: str = "mxu.exe" if OS_KEYWORD == "win" else "mxu"
MAAFW_LIB: str = {"win": "MaaFramework.dll", "linux": "libMaaFramework.so", "macos": "libMaaFramework.dylib"}.get(OS_KEYWORD, "libMaaFramework.so")


def configure_token() -> None:
    """配置 GitHub Token，输出检测结果"""
    print("[TIP] 如遇 API 速率限制，请设置环境变量 GITHUB_TOKEN/GH_TOKEN")
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        print("[INF] 检测到 GitHub Token，将用于 API 请求。")
    else:
        print("[WRN] 未检测到 GitHub Token，将使用匿名 API 请求（可能限流）。")
    print("-" * 40)


def get_platform_keywords() -> tuple[str, str]:
    """获取当前平台的操作系统和架构关键字"""
    return OS_KEYWORD, ARCH_KEYWORD


def run_command(
    cmd: list[str] | str, cwd: Path | str | None = None, shell: bool = False
) -> bool:
    """执行命令并输出日志，返回是否成功"""
    cmd_str = ' '.join(cmd) if isinstance(cmd, list) else str(cmd)
    print(f"[CMD] {cmd_str}")
    try:
        subprocess.check_call(cmd, cwd=cwd or project_base, shell=shell)
        print(f"[INF] 命令执行成功: {cmd_str}")
        return True
    except subprocess.CalledProcessError as e:
        print(f"[ERR] 命令执行失败: {cmd_str}\n  错误: {e}")
        return False


def check_and_update_submodules() -> bool:
    print("[INF] 检查子模块...")
    if not (project_base / "assets" / "MaaCommonAssets" / "LICENSE").exists():
        print("[WRN] 子模块缺失，尝试更新...")
        return run_command(["git", "submodule", "update", "--init", "--recursive"])
    print("[INF] 子模块已存在。")
    return True


def run_build_script() -> bool:
    print("[INF] 执行 build_and_install.py ...")
    script_path = project_base / "tools" / "build_and_install.py"
    return run_command([sys.executable, str(script_path)])


def get_latest_release_url(
    repo: str, keywords: list[str], retries: int = MAX_RETRIES
) -> tuple[str | None, str | None]:
    """获取指定 GitHub 仓库最新 release 中匹配关键字的资源下载链接和文件名，支持自动重试"""
    api_url = f"https://api.github.com/repos/{repo}/releases/latest"
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    
    for attempt in range(1, retries + 1):
        try:
            if attempt > 1:
                print(f"[INF] 重试获取 {repo} 发布信息 ({attempt}/{retries})...")
            else:
                print(f"[INF] 获取 {repo} 的最新发布信息...")
            
            req = urllib.request.Request(api_url)
            if token:
                req.add_header("Authorization", f"Bearer {token}")
            req.add_header("User-Agent", "MaaEnd-setup")
            req.add_header("Accept", "application/vnd.github+json")
            
            with urllib.request.urlopen(req, timeout=30) as response:
                data = json.loads(response.read().decode())
            assets = data.get("assets", [])
            for asset in assets:
                name = asset["name"].lower()
                if all(k.lower() in name for k in keywords):
                    print(f"[INF] 匹配到资源: {asset['name']}")
                    return asset["browser_download_url"], asset["name"]
            print(f"[WRN] 未找到包含关键词 {keywords} 的资源。")
            return None, None
        except Exception as e:
            print(f"[ERR] 获取发布信息失败: {e}")
            if attempt < retries:
                print(f"[INF] {RETRY_DELAY} 秒后重试...")
                time.sleep(RETRY_DELAY)
    
    print(f"[ERR] 获取发布信息失败，已重试 {retries} 次")
    return None, None


def download_file(url: str, dest_path: Path, timeout: int = 60, retries: int = MAX_RETRIES) -> bool:
    """下载文件，支持自动重试"""
    for attempt in range(1, retries + 1):
        try:
            if attempt > 1:
                print(f"[INF] 重试下载 ({attempt}/{retries}): {url}")
            else:
                print(f"[INF] 下载: {url}")
            with urllib.request.urlopen(url, timeout=timeout) as response, open(dest_path, "wb") as out_file:
                shutil.copyfileobj(response, out_file)
            print(f"[INF] 下载完成: {dest_path}")
            return True
        except urllib.error.URLError as e:
            print(f"[ERR] 网络错误: {e.reason}")
        except Exception as e:
            print(f"[ERR] 下载失败: {e}")
        if attempt < retries:
            print(f"[INF] {RETRY_DELAY} 秒后重试...")
            time.sleep(RETRY_DELAY)
    print(f"[ERR] 下载失败，已重试 {retries} 次")
    return False


def install_maafw(install_root: Path) -> bool:
    """安装 MaaFramework，成功返回 True，失败返回 False"""
    real_install_root = install_root.resolve()
    maafw_dest = real_install_root / "maafw"
    if (maafw_dest / MAAFW_LIB).exists():
        print("[INF] MaaFramework 已安装，跳过。")
        return True
    os_kw, arch_kw = get_platform_keywords()
    url, filename = get_latest_release_url(MAA_FW_REPO, [os_kw, arch_kw])
    if not url or not filename:
        print("[ERR] 未找到 MaaFramework 下载链接，请手动安装。")
        return False
    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)
        download_path = tmp_path / filename
        if not download_file(url, download_path):
            print("[ERR] MaaFramework 下载失败。")
            return False
        print("[INF] 解压 MaaFramework...")
        try:
            extract_root = tmp_path / "extracted"
            with zipfile.ZipFile(download_path, "r") as zip_ref:
                zip_ref.extractall(extract_root)
            maafw_dest.mkdir(parents=True, exist_ok=True)
            bin_found = False
            for root, dirs, files in os.walk(extract_root):
                if "bin" in dirs:
                    bin_path = Path(root) / "bin"
                    print(f"[INF] 在 {bin_path} 找到 bin 目录，复制到 {maafw_dest}")
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
                print(f"[ERR] 未找到 bin 目录，可用目录: {[d.name for d in extract_root.iterdir() if d.is_dir()]}")
                return False
            print("[INF] MaaFramework 安装完成")
            return True
        except Exception as e:
            print(f"[ERR] MaaFramework 解压失败: {e}")
            return False


def install_mxu(install_root: Path) -> bool:
    """安装 MXU，成功返回 True，失败返回 False"""
    real_install_root = install_root.resolve()
    os_kw, arch_kw = get_platform_keywords()
    mxu_path = real_install_root / MXU_NAME
    if mxu_path.exists():
        print("[INF] MXU 已安装，跳过。")
        return True
    url, filename = get_latest_release_url(MXU_REPO, ["mxu", os_kw, arch_kw])
    if not url or not filename:
        print("[ERR] 未找到 MXU 下载链接，请手动安装。")
        return False
    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)
        download_path = tmp_path / filename
        if not download_file(url, download_path):
            print("[ERR] MXU 下载失败。")
            return False
        print("[INF] 解压 MXU...")
        try:
            extract_root = tmp_path / "extracted"
            with zipfile.ZipFile(download_path, "r") as zip_ref:
                zip_ref.extractall(extract_root)
            real_install_root.mkdir(parents=True, exist_ok=True)
            target_files = [MXU_NAME]
            if OS_KEYWORD == "win":
                target_files.append("mxu.pdb")
            copied = False
            for item in extract_root.iterdir():
                if item.name.lower() in [f.lower() for f in target_files]:
                    dest = real_install_root / item.name
                    shutil.copy2(item, dest)
                    print(f"[INF] 复制 {item.name} 到 {real_install_root}")
                    if item.name.lower() == MXU_NAME.lower():
                        copied = True
            if not copied:
                print(f"[ERR] 未找到 {MXU_NAME}")
                return False
            print("[INF] MXU 安装完成")
            return True
        except Exception as e:
            print(f"[ERR] MXU 解压失败: {e}")
            return False


def main() -> None:
    install_dir = project_base / "install"
    print("========== MaaEnd Workspace 初始化 ==========")
    configure_token()
    if not check_and_update_submodules():
        print("[FATAL] 子模块更新失败，退出。")
        sys.exit(1)
    print("========== 构建 Agent ==========")
    if not run_build_script():
        print("[FATAL] 构建脚本执行失败，退出。")
        sys.exit(1)
    print("\n========== 下载依赖项 ==========")
    if not install_maafw(install_dir):
        print("[FATAL] MaaFramework 安装失败，退出。")
        sys.exit(1)
    if not install_mxu(install_dir):
        print("[FATAL] MXU 安装失败，退出。")
        sys.exit(1)
    print("\n========== 设置完成 ==========")
    print(f"[INF] 请运行 {install_dir / MXU_NAME} 来检查是否安装成功。后续使用相关工具编辑、调试等，都基于该文件夹。")


if __name__ == "__main__":
    main()
