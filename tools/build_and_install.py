import argparse
import locale
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path

# Localization (i18n)

TRANSLATIONS = {
    "zh_CN": {
        "root_dir": "项目根目录",
        "install_dir": "安装目录",
        "mode": "模式",
        "mode_ci": "CI (复制)",
        "mode_dev": "开发 (链接)",
        "error": "[ERROR]",
        "create_junction_failed": "创建 Junction 失败",
        "create_file_link_failed": "创建文件链接失败",
        "go_version": "Go 版本",
        "go_not_found": "未检测到 Go 环境",
        "go_install_prompt": "请安装 Go 后重试:",
        "go_install_official": "官方下载: https://go.dev/dl/",
        "go_install_windows": "Windows: winget install GoLang.Go",
        "go_install_macos": "macOS:   brew install go",
        "go_install_linux": "Linux:   参考发行版包管理器或官方指南",
        "go_install_path": "安装后请确保 'go' 命令在 PATH 中可用",
        "ocr_not_found": "OCR 资源不存在",
        "ocr_submodule_hint": "请确保已初始化 submodule: git submodule update --init",
        "ocr_copied": "复制 {0} 个文件，跳过 {1} 个已存在文件",
        "go_source_not_found": "Go 源码目录不存在",
        "target_platform": "目标平台",
        "output_path": "输出路径",
        "go_mod_tidy_failed": "go mod tidy 失败",
        "build_mode": "构建模式",
        "build_mode_ci": "CI (release with debug info)",
        "build_mode_dev": "开发 (debug)",
        "build_command": "构建命令",
        "go_build_failed": "go build 失败",
        "step_configure_ocr": "[1/4] 配置 OCR 模型...",
        "step_process_assets": "[2/4] 处理 assets 目录...",
        "step_build_go": "[3/4] 构建 Go Agent...",
        "step_prepare_files": "[4/4] 准备项目文件...",
        "configure_ocr_failed": "配置 OCR 模型失败",
        "build_go_failed": "构建 Go Agent 失败",
        "separator": "=" * 50,
        "install_complete": "安装目录准备完成！",
        "maafw_download_hint": "为了使用 MaaFramework，您还需要：",
        "maafw_download_step": "下载 MaaFramework 并解压 bin 内容到 install/maafw/",
        "maafw_download_url": "https://github.com/MaaXYZ/MaaFramework/releases",
        "mxu_download_hint": "为了使用 MXU，您还需要：",
        "mxu_download_step": "下载 MXU 并解压到 install/",
        "mxu_download_url": "https://github.com/MistEO/MXU/releases",
        "description": "MaaEnd 构建工具：处理构建所需资源并创建安装目录",
        "arg_ci": "CI 模式：复制文件而非链接",
        "arg_os": "目标操作系统 (win/macos/linux)",
        "arg_arch": "目标架构 (x86_64/aarch64)",
        "arg_version": "版本号（写入 Go Agent）",
    },
    "en_US": {
        "root_dir": "Project root",
        "install_dir": "Install directory",
        "mode": "Mode",
        "mode_ci": "CI (copy)",
        "mode_dev": "Development (link)",
        "error": "[ERROR]",
        "create_junction_failed": "Failed to create junction",
        "create_file_link_failed": "Failed to create file link",
        "go_version": "Go version",
        "go_not_found": "Go environment not detected",
        "go_install_prompt": "Please install Go and try again:",
        "go_install_official": "Official download: https://go.dev/dl/",
        "go_install_windows": "Windows: winget install GoLang.Go",
        "go_install_macos": "macOS:   brew install go",
        "go_install_linux": "Linux:   refer to your package manager or official guide",
        "go_install_path": "After installation, ensure 'go' command is available in PATH",
        "ocr_not_found": "OCR resources not found",
        "ocr_submodule_hint": "Please initialize submodule: git submodule update --init",
        "ocr_copied": "Copied {0} file(s), skipped {1} existing file(s)",
        "go_source_not_found": "Go source directory not found",
        "target_platform": "Target platform",
        "output_path": "Output path",
        "go_mod_tidy_failed": "go mod tidy failed",
        "build_mode": "Build mode",
        "build_mode_ci": "CI (release with debug info)",
        "build_mode_dev": "Development (debug)",
        "build_command": "Build command",
        "go_build_failed": "go build failed",
        "step_configure_ocr": "[1/4] Configuring OCR model...",
        "step_process_assets": "[2/4] Processing assets directory...",
        "step_build_go": "[3/4] Building Go Agent...",
        "step_prepare_files": "[4/4] Preparing project files...",
        "configure_ocr_failed": "Failed to configure OCR model",
        "build_go_failed": "Failed to build Go Agent",
        "separator": "=" * 50,
        "install_complete": "Installation directory prepared successfully!",
        "maafw_download_hint": "To use MaaFramework, you also need to:",
        "maafw_download_step": "Download MaaFramework and extract bin contents to install/maafw/",
        "maafw_download_url": "https://github.com/MaaXYZ/MaaFramework/releases",
        "mxu_download_hint": "To use MXU, you also need to:",
        "mxu_download_step": "Download MXU and extract to install/",
        "mxu_download_url": "https://github.com/MistEO/MXU/releases",
        "description": "MaaEnd build tool: Process build resources and create installation directory",
        "arg_ci": "CI mode: copy files instead of linking",
        "arg_os": "Target operating system (win/macos/linux)",
        "arg_arch": "Target architecture (x86_64/aarch64)",
        "arg_version": "Version number (to be written into Go Agent)",
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


def t(key: str, *args) -> str:
    """
    获取本地化文本
    Get localized text with fallback to en_US

    Args:
        key: 翻译键 / translation key
        *args: 格式化参数 / format arguments

    Returns:
        本地化后的文本 / localized text
    """
    text = TRANSLATIONS.get(CURRENT_LOCALE, {}).get(
        key, TRANSLATIONS["en_US"].get(key, key)
    )

    if args:
        return text.format(*args)
    return text


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
            print(f"  {t('error')} {t('create_junction_failed')}: {result.stderr}")
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
                print(f"  {t('error')} {t('create_file_link_failed')}: {result.stderr}")
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
            print(f"  {t('go_version')}: {result.stdout.strip()}")
            return True
    except FileNotFoundError:
        pass

    print(f"  {t('error')} {t('go_not_found')}")
    print()
    print(f"  {t('go_install_prompt')}")
    print(f"    - {t('go_install_official')}")
    print(f"    - {t('go_install_windows')}")
    print(f"    - {t('go_install_macos')}")
    print(f"    - {t('go_install_linux')}")
    print()
    print(f"  {t('go_install_path')}")
    return False


def configure_ocr_model(assets_dir: Path) -> bool:
    """配置 OCR 模型，逐个复制文件，已存在则跳过"""
    assets_ocr_src = assets_dir / "MaaCommonAssets" / "OCR" / "ppocr_v5" / "zh_cn"
    if not assets_ocr_src.exists():
        print(f"  {t('error')} {t('ocr_not_found')}: {assets_ocr_src}")
        print(f"  {t('ocr_submodule_hint')}")
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
    print(f"  {t('ocr_copied', copied_count, skipped_count)}")
    return True


def build_go_agent(
    root_dir: Path,
    install_dir: Path,
    target_os: str | None = None,
    target_arch: str | None = None,
    version: str | None = None,
    ci_mode: bool = False,
) -> bool:
    """构建 Go Agent"""
    if not check_go_environment():
        return False

    go_service_dir = root_dir / "agent" / "go-service"
    if not go_service_dir.exists():
        print(f"  {t('error')} {t('go_source_not_found')}: {go_service_dir}")
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

    print(f"  {t('target_platform')}: {goos}/{goarch}")
    print(f"  {t('output_path')}: {output_path}")

    env = {**os.environ, "GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0"}

    # go mod tidy
    result = subprocess.run(
        ["go", "mod", "tidy"],
        cwd=go_service_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        env=env,
    )
    if result.returncode != 0:
        print(f"  {t('error')} {t('go_mod_tidy_failed')}: {result.stderr}")
        return False

    # go build
    # CI 模式：release with debug info（保留 DWARF 调试信息，不使用 -s -w）
    # 开发模式：debug 构建（保留调试信息 + 禁用优化，便于断点调试）
    if ci_mode:
        # Release with debug info: 保留调试信息但启用优化
        ldflags = ""
        gcflags = ""
    else:
        # Debug 模式: 禁用优化和内联，便于断点调试
        ldflags = ""
        gcflags = "all=-N -l"

    if version:
        ldflags += f" -X main.Version={version}"
    ldflags = ldflags.strip()

    build_cmd = [
        "go",
        "build",
    ]

    if ci_mode:
        build_cmd.append("-trimpath")

    if gcflags:
        build_cmd.append(f"-gcflags={gcflags}")

    if ldflags:
        build_cmd.append(f"-ldflags={ldflags}")

    build_cmd.extend(["-o", str(output_path), "."])

    print(
        f"  {t('build_mode')}: {t('build_mode_ci') if ci_mode else t('build_mode_dev')}"
    )
    print(f"  {t('build_command')}: {' '.join(build_cmd)}")

    result = subprocess.run(
        build_cmd,
        cwd=go_service_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        env=env,
    )
    if result.returncode != 0:
        print(f"  {t('error')} {t('go_build_failed')}: {result.stderr}")
        return False

    print(f"  -> {output_path}")
    return True


def main():
    parser = argparse.ArgumentParser(description=t("description"))
    parser.add_argument("--ci", action="store_true", help=t("arg_ci"))
    parser.add_argument("--os", dest="target_os", help=t("arg_os"))
    parser.add_argument("--arch", dest="target_arch", help=t("arg_arch"))
    parser.add_argument("--version", help=t("arg_version"))
    args = parser.parse_args()

    use_copy = args.ci

    root_dir = Path(__file__).parent.parent.resolve()
    assets_dir = root_dir / "assets"
    install_dir = root_dir / "install"

    print(f"{t('root_dir')}: {root_dir}")
    print(f"{t('install_dir')}: {install_dir}")
    print(f"{t('mode')}: {t('mode_ci') if use_copy else t('mode_dev')}")
    print()

    install_dir.mkdir(parents=True, exist_ok=True)

    # 用于链接或复制的函数
    link_or_copy_dir = copy_directory if use_copy else create_directory_link
    link_or_copy_file = copy_file if use_copy else create_file_link

    # 1. 配置 OCR 模型
    print(t("step_configure_ocr"))
    if not configure_ocr_model(assets_dir):
        print(f"  {t('error')} {t('configure_ocr_failed')}")
        sys.exit(1)

    # 2. 链接/复制 assets 目录内容（排除 MaaCommonAssets）
    print(t("step_process_assets"))
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
    print(t("step_build_go"))
    if not build_go_agent(
        root_dir, install_dir, args.target_os, args.target_arch, args.version, use_copy
    ):
        print(f"  {t('error')} {t('build_go_failed')}")
        sys.exit(1)

    # 4. 链接/复制项目根目录文件并创建 maafw 目录
    print(t("step_prepare_files"))
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
    print(t("separator"))
    print(t("install_complete"))

    if not use_copy:
        if not any(maafw_dir.iterdir()):
            print()
            print(t("maafw_download_hint"))
            print(f"  {t('maafw_download_step')}")
            print(f"  {t('maafw_download_url')}")
        if (
            not (install_dir / "mxu").exists()
            and not (install_dir / "mxu.exe").exists()
        ):
            print()
            print(t("mxu_download_hint"))
            print(f"  {t('mxu_download_step')}")
            print(f"  {t('mxu_download_url')}")

    print()


if __name__ == "__main__":
    main()
