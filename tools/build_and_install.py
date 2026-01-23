"""
创建 install 目录，支持两种模式：
- 本地开发模式（默认）：使用链接，方便调试
- CI 模式（--ci）：使用复制，用于打包分发

Windows 使用 mklink /J 创建目录 Junction（不需要管理员权限）
Unix/macOS 使用 symlink 创建符号链接
"""

import argparse
import os
import platform
import shutil
import subprocess
from pathlib import Path


def create_directory_link(src: Path, dst: Path) -> bool:
    """创建目录链接（Junction/symlink）"""
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
                print(f"  [ERROR] 创建链接失败: {result.stderr}")
                return False
    else:
        try:
            dst.hardlink_to(src)
        except (OSError, NotImplementedError):
            dst.symlink_to(src)

    return True


def copy_directory(src: Path, dst: Path) -> bool:
    """复制目录"""
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


def configure_ocr_model(assets_dir: Path, use_copy: bool = False) -> bool:
    """配置 OCR 模型"""
    assets_ocr_src = assets_dir / "MaaCommonAssets" / "OCR" / "ppocr_v5" / "zh_cn"
    if not assets_ocr_src.exists():
        print(f"  [ERROR] OCR 资源不存在: {assets_ocr_src}")
        print("  请确保已初始化 submodule: git submodule update --init")
        return False

    ocr_dir = assets_dir / "resource" / "model" / "ocr"
    if ocr_dir.exists():
        print("  [SKIP] OCR 目录已存在")
        return True

    if use_copy:
        shutil.copytree(assets_ocr_src, ocr_dir)
    else:
        create_directory_link(assets_ocr_src, ocr_dir)

    print(f"  -> {ocr_dir}")
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
        ["go", "build", f"-ldflags={ldflags}", "-o", str(output_path), "."],
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
    parser = argparse.ArgumentParser(description="创建 install 目录")
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

    # 1. 链接/复制 assets 目录内容（排除 MaaCommonAssets）
    print("[1/5] 处理 assets 目录...")
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

    # 2. 配置 OCR 模型
    print("[2/5] 配置 OCR 模型...")
    configure_ocr_model(assets_dir, use_copy)

    # 3. 构建 Go Agent
    print("[3/5] 构建 Go Agent...")
    build_go_agent(
        root_dir, install_dir, args.target_os, args.target_arch, args.version
    )

    # 4. 链接/复制项目根目录文件
    print("[4/5] 处理项目文件...")
    for filename in ["README.md", "LICENSE"]:
        src = root_dir / filename
        dst = install_dir / filename
        if src.exists():
            if link_or_copy_file(src, dst):
                print(f"  -> {dst}")

    # 5. 创建 maafw 目录
    print("[5/5] 创建 maafw 目录...")
    maafw_dir = install_dir / "maafw"
    maafw_dir.mkdir(parents=True, exist_ok=True)
    print(f"  -> {maafw_dir}")

    print()
    print("=" * 50)
    print("安装目录准备完成！")

    if not use_copy:
        print()
        print("后续步骤：")
        print("  1. 下载 MaaFramework 并解压 bin 内容到 install/maafw/")
        print("     https://github.com/MaaXYZ/MaaFramework/releases")
        print("  2. 下载 MXU 并解压到 install/")
        print("     https://github.com/MistEO/MXU/releases")

    print()


if __name__ == "__main__":
    main()
