import argparse
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path


def create_directory_link(src: Path, dst: Path) -> bool:
    """
    在指定位置创建一个指定目录的链接
    - Windows：Junction
    - Unix/macOS：symlink
    """
    if dst.exists() or dst.is_symlink():
        if dst.is_dir() and not dst.is_symlink():
            try:
                dst.rmdir()
            except OSError:
                shutil.rmtree(dst)
        else:
            dst.unlink(missing_ok=True)

    dst.parent.mkdir(parents=True, exist_ok=True)

    if platform.system() == "Windows":
        result = subprocess.run(
            ["cmd", "/c", "mklink", "/J", str(dst), str(src)],
            capture_output=True,
            text=True,
        )
        if result.returncode != 0:
            print(f"  [ERROR] 创建 Junction 失败: {result.stderr}")
            return False
    else:
        dst.symlink_to(src)

    return True


def create_file_link(src: Path, dst: Path) -> bool:
    """创建文件链接（硬链接优先）"""
    if dst.exists() or dst.is_symlink():
        dst.unlink(missing_ok=True)

    dst.parent.mkdir(parents=True, exist_ok=True)

    if platform.system() == "Windows":
        result = subprocess.run(
            ["cmd", "/c", "mklink", "/H", str(dst), str(src)],
            capture_output=True,
            text=True,
        )
        if result.returncode != 0:
            result = subprocess.run(
                ["cmd", "/c", "mklink", str(dst), str(src)],
                capture_output=True,
                text=True,
            )
            if result.returncode != 0:
                print(f"  [ERROR] 创建文件链接失败: {result.stderr}")
                return False
    else:
        try:
            dst.hardlink_to(src)
        except (OSError, NotImplementedError):
            dst.symlink_to(src)

    return True


def copy_directory(src: Path, dst: Path) -> bool:
    """复制目录（替换）"""
    if dst.exists():
        shutil.rmtree(dst)
    shutil.copytree(src, dst)
    return True


def copy_file(src: Path, dst: Path) -> bool:
    """复制文件"""
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(src, dst)
    return True


def check_go_environment() -> bool:
    """检查 Go 环境是否可用"""
    try:
        result = subprocess.run(
            ["go", "version"],
            capture_output=True,
            text=True,
        )
        if result.returncode == 0:
            print(f"  Go 版本: {result.stdout.strip()}")
            return True
    except FileNotFoundError:
        pass

    print("  [ERROR] 未检测到 Go 环境")
    print()
    print("  请安装 Go 后重试:")
    print("    - 官方下载: https://go.dev/dl/")
    print("    - Windows: winget install GoLang.Go")
    print("    - macOS:   brew install go")
    print("    - Linux:   参考发行版包管理器或官方指南")
    print()
    print("  安装后请确保 'go' 命令在 PATH 中可用")
    return False


def configure_ocr_model(assets_dir: Path) -> bool:
    """配置 OCR 模型，逐个复制文件，已存在则跳过"""
    assets_ocr_src = assets_dir / "MaaCommonAssets" / "OCR" / "ppocr_v5" / "zh_cn"
    if not assets_ocr_src.exists():
        print(f"  [ERROR] OCR 资源不存在: {assets_ocr_src}")
        print("  请确保已初始化 submodule: git submodule update --init")
        return False

    ocr_dir = assets_dir / "resource" / "model" / "ocr"
    ocr_dir.mkdir(parents=True, exist_ok=True)

    copied_count = 0
    skipped_count = 0

    for src_file in assets_ocr_src.iterdir():
        if not src_file.is_file():
            continue
        dst_file = ocr_dir / src_file.name
        if dst_file.exists():
            skipped_count += 1
        else:
            shutil.copy2(src_file, dst_file)
            copied_count += 1

    print(f"  -> {ocr_dir}")
    print(f"  复制 {copied_count} 个文件，跳过 {skipped_count} 个已存在文件")
    return True


def build_go_agent(
    root_dir: Path,
    install_dir: Path,
    target_os: str | None = None,
    target_arch: str | None = None,
    version: str | None = None,
) -> bool:
    """构建 Go Agent"""
    if not check_go_environment():
        return False

    go_service_dir = root_dir / "agent" / "go-service"
    if not go_service_dir.exists():
        print(f"  [ERROR] Go 源码目录不存在: {go_service_dir}")
        return False

    # 检测或使用指定的系统和架构
    if target_os:
        goos = {"win": "windows", "macos": "darwin", "linux": "linux"}.get(
            target_os, target_os
        )
    else:
        system = platform.system().lower()
        goos = {"windows": "windows", "darwin": "darwin"}.get(system, "linux")

    if target_arch:
        goarch = {"x86_64": "amd64", "aarch64": "arm64"}.get(target_arch, target_arch)
    else:
        machine = platform.machine().lower()
        goarch = (
            "amd64"
            if machine in ("x86_64", "amd64")
            else "arm64" if machine in ("aarch64", "arm64") else machine
        )

    ext = ".exe" if goos == "windows" else ""

    agent_dir = install_dir / "agent"
    agent_dir.mkdir(parents=True, exist_ok=True)
    output_path = agent_dir / f"go-service{ext}"

    print(f"  目标平台: {goos}/{goarch}")
    print(f"  输出路径: {output_path}")

    env = {**os.environ, "GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0"}

    # go mod tidy
    result = subprocess.run(
        ["go", "mod", "tidy"],
        cwd=go_service_dir,
        capture_output=True,
        text=True,
        env=env,
    )
    if result.returncode != 0:
        print(f"  [ERROR] go mod tidy 失败: {result.stderr}")
        return False

    # go build
    ldflags = "-s -w"
    if version:
        ldflags += f" -X main.Version={version}"

    result = subprocess.run(
        [
            "go",
            "build",
            "-trimpath",
            f"-ldflags={ldflags}",
            "-o",
            str(output_path),
            ".",
        ],
        cwd=go_service_dir,
        capture_output=True,
        text=True,
        env=env,
    )
    if result.returncode != 0:
        print(f"  [ERROR] go build 失败: {result.stderr}")
        return False

    print(f"  -> {output_path}")
    return True


def main():
    parser = argparse.ArgumentParser(description="MaaEnd 构建工具：处理构建所需资源并创建安装目录")
    parser.add_argument("--ci", action="store_true", help="CI 模式：复制文件而非链接")
    parser.add_argument("--os", dest="target_os", help="目标操作系统 (win/macos/linux)")
    parser.add_argument("--arch", dest="target_arch", help="目标架构 (x86_64/aarch64)")
    parser.add_argument("--version", help="版本号（写入 Go Agent）")
    args = parser.parse_args()

    use_copy = args.ci

    root_dir = Path(__file__).parent.parent.resolve()
    assets_dir = root_dir / "assets"
    install_dir = root_dir / "install"

    print(f"项目根目录: {root_dir}")
    print(f"安装目录:   {install_dir}")
    print(f"模式:       {'CI (复制)' if use_copy else '开发 (链接)'}")
    print()

    install_dir.mkdir(parents=True, exist_ok=True)

    # 用于链接或复制的函数
    link_or_copy_dir = copy_directory if use_copy else create_directory_link
    link_or_copy_file = copy_file if use_copy else create_file_link

    # 1. 配置 OCR 模型
    print("[1/4] 配置 OCR 模型...")
    if not configure_ocr_model(assets_dir):
        print("  [ERROR] 配置 OCR 模型失败")
        sys.exit(1)

    # 2. 链接/复制 assets 目录内容（排除 MaaCommonAssets）
    print("[2/4] 处理 assets 目录...")
    for item in assets_dir.iterdir():
        if item.name == "MaaCommonAssets":
            continue
        dst = install_dir / item.name
        if item.is_dir():
            if link_or_copy_dir(item, dst):
                print(f"  -> {dst}")
        elif item.is_file():
            if link_or_copy_file(item, dst):
                print(f"  -> {dst}")

    # 3. 构建 Go Agent
    print("[3/4] 构建 Go Agent...")
    if not build_go_agent(
        root_dir, install_dir, args.target_os, args.target_arch, args.version
    ):
        print("  [ERROR] 构建 Go Agent 失败")
        sys.exit(1)

    # 4. 链接/复制项目根目录文件并创建 maafw 目录
    print("[4/4] 准备项目文件...")
    for filename in ["README.md", "LICENSE"]:
        src = root_dir / filename
        dst = install_dir / filename
        if src.exists():
            if link_or_copy_file(src, dst):
                print(f"  -> {dst}")

    maafw_dir = install_dir / "maafw"
    maafw_dir.mkdir(parents=True, exist_ok=True)
    print(f"  -> {maafw_dir}")

    print()
    print("=" * 50)
    print("安装目录准备完成！")

    if not use_copy:
        if not any(maafw_dir.iterdir()):
            print()
            print("为了使用 MaaFramework，您还需要：")
            print("  下载 MaaFramework 并解压 bin 内容到 install/maafw/")
            print("  https://github.com/MaaXYZ/MaaFramework/releases")
        if not (install_dir / "mxu").exists() and not (install_dir / "mxu.exe").exists():
            print()
            print("为了使用 MXU，您还需要：")
            print("  下载 MXU 并解压到 install/")
            print("  https://github.com/MistEO/MXU/releases")

    print()


if __name__ == "__main__":
    main()
