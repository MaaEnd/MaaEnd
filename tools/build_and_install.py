import argparse
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path

from cli_support import Console, init_localization

LOCALS_DIR = Path(__file__).parent / "locals" / "build_and_install"


_local_t = lambda key, **kwargs: key.format(**kwargs) if kwargs else key


def init_local() -> None:
    global _local_t
    t_func, load_error_path = init_localization(LOCALS_DIR)
    _local_t = t_func
    if load_error_path:
        print(Console.err(t("error_load_locale", path=load_error_path)))


def t(key: str, **kwargs) -> str:
    return _local_t(key, **kwargs)


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
            print(
                f"  {Console.err(t('error'))} {t('create_junction_failed')}: {result.stderr}"
            )
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
                print(
                    f"  {Console.err(t('error'))} {t('create_file_link_failed')}: {result.stderr}"
                )
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
            print(f"  {Console.info(t('go_version'))}: {result.stdout.strip()}")
            return True
    except FileNotFoundError:
        pass

    print(f"  {Console.err(t('error'))} {t('go_not_found')}")
    print()
    print(f"  {Console.info(t('go_install_prompt'))}")
    print(f"    - {Console.info(t('go_install_official'))}")
    print(f"    - {Console.info(t('go_install_windows'))}")
    print(f"    - {Console.info(t('go_install_macos'))}")
    print(f"    - {Console.info(t('go_install_linux'))}")
    print()
    print(f"  {Console.info(t('go_install_path'))}")
    return False


def configure_ocr_model(assets_dir: Path) -> bool:
    """配置 OCR 模型，逐个复制文件，已存在则跳过"""
    assets_ocr_src = assets_dir / "MaaCommonAssets" / "OCR" / "ppocr_v5" / "zh_cn"
    if not assets_ocr_src.exists():
        print(f"  {Console.err(t('error'))} {t('ocr_not_found')}: {assets_ocr_src}")
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

    print(f"  {Console.ok('->')} {ocr_dir}")
    print(f"  {t('ocr_copied', copied=copied_count, skipped=skipped_count)}")
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
        print(f"  {Console.err(t('error'))} {t('go_source_not_found')}: {go_service_dir}")
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

    print(f"  {Console.info(t('target_platform'))}: {goos}/{goarch}")
    print(f"  {Console.info(t('output_path'))}: {output_path}")

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
        print(f"  {Console.err(t('error'))} {t('go_mod_tidy_failed')}: {result.stderr}")
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

    build_mode_text = t("build_mode_ci") if ci_mode else t("build_mode_dev")
    print(f"  {Console.warn(t('build_mode'))}: {build_mode_text}")
    print(f"  {Console.info(t('build_command'))}: {' '.join(build_cmd)}")

    result = subprocess.run(
        build_cmd,
        cwd=go_service_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        env=env,
    )
    if result.stdout:
        print(result.stdout)
    if result.returncode != 0:
        print(f"  {Console.err(t('error'))} {t('go_build_failed')}:")
        if result.stderr:
            print(result.stderr)
        return False
    if result.stderr:
        print(result.stderr)

    print(f"  {Console.ok('->')} {output_path}")
    return True


def check_cmake_environment() -> bool:
    """检查 CMake 环境是否可用"""
    try:
        result = subprocess.run(
            ["cmake", "--version"],
            capture_output=True,
            text=True,
        )
        if result.returncode == 0:
            version_line = result.stdout.strip().splitlines()[0]
            print(f"  {t('cmake_version')}: {version_line}")
            return True
    except FileNotFoundError:
        pass

    print(f"  {t('error')} {t('cmake_not_found')}")
    return False


def build_cpp_algo(
    root_dir: Path,
    install_dir: Path,
    target_os: str | None = None,
    target_arch: str | None = None,
    ci_mode: bool = False,
) -> bool:
    """构建 C++ Algo Agent（使用 CMake Presets）"""
    if not check_cmake_environment():
        return False

    cpp_algo_dir = root_dir / "agent" / "cpp-algo"
    if not cpp_algo_dir.exists():
        print(f"  {t('error')} {t('cpp_source_not_found')}: {cpp_algo_dir}")
        return False

    build_type = "RelWithDebInfo"

    # 确定目标操作系统
    if target_os:
        resolved_os = target_os  # win, macos, linux
    else:
        system = platform.system().lower()
        resolved_os = {"windows": "win", "darwin": "macos"}.get(system, "linux")

    # 确定目标架构
    if target_arch:
        resolved_arch = target_arch  # x86_64, aarch64
    else:
        machine = platform.machine().lower()
        if machine in ("x86_64", "amd64"):
            resolved_arch = "x86_64"
        elif machine in ("aarch64", "arm64"):
            resolved_arch = "aarch64"
        else:
            resolved_arch = machine

    # 根据平台选择 configure preset，参考 MaaFramework build.yml
    if resolved_os == "win":
        if resolved_arch == "aarch64":
            configure_preset = "MSVC 2022 ARM"
        else:
            configure_preset = "MSVC 2022"
    elif resolved_os == "linux":
        if resolved_arch == "aarch64":
            configure_preset = "NinjaMulti Linux arm64"
        else:
            configure_preset = "NinjaMulti Linux x64"
    else:
        # macOS
        configure_preset = "NinjaMulti"

    # 构建 MAADEPS_TRIPLET: maa-{x64|arm64}-{windows|linux|osx}
    arch_part = "x64" if resolved_arch == "x86_64" else "arm64"
    os_part = {"win": "windows", "macos": "osx", "linux": "linux"}.get(
        resolved_os, resolved_os
    )
    maadeps_triplet = f"maa-{arch_part}-{os_part}"

    print(f"  {t('build_mode')}: {build_type}")
    print(f"  {t('target_platform')}: {resolved_os}/{resolved_arch}")
    print(f"  Configure preset: {configure_preset}")
    print(f"  MaaDeps triplet: {maadeps_triplet}")

    # cmake --preset <configure_preset>
    configure_cmd = [
        "cmake",
        "--preset",
        configure_preset,
        f"-DMAADEPS_TRIPLET={maadeps_triplet}",
        f"-DCMAKE_INSTALL_PREFIX={install_dir}",
    ]

    # macOS 需要额外的参数
    if resolved_os == "macos":
        osx_arch = "x86_64" if resolved_arch == "x86_64" else "arm64"
        configure_cmd.extend(
            [
                "-DCMAKE_OSX_SYSROOT=macosx",
                f"-DCMAKE_OSX_ARCHITECTURES={osx_arch}",
            ]
        )

    print(f"  {t('build_command')}: {' '.join(configure_cmd)}")

    result = subprocess.run(
        configure_cmd,
        cwd=cpp_algo_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    if result.stdout:
        print(result.stdout)
    if result.returncode != 0:
        print(f"  {t('error')} {t('cmake_configure_failed')}:")
        if result.stderr:
            print(result.stderr)
        return False
    if result.stderr:
        print(result.stderr)

    # cmake --build build --preset <build_preset>
    build_preset = f"{configure_preset} - {build_type}"
    build_cmd = [
        "cmake",
        "--build",
        "build",
        "--preset",
        build_preset,
    ]
    print(f"  {t('build_command')}: {' '.join(build_cmd)}")

    result = subprocess.run(
        build_cmd,
        cwd=cpp_algo_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    if result.stdout:
        print(result.stdout)
    if result.returncode != 0:
        print(f"  {t('error')} {t('cmake_build_failed')}:")
        if result.stderr:
            print(result.stderr)
        return False
    if result.stderr:
        print(result.stderr)

    # cmake --install build --prefix <install_dir> --config <build_type>
    install_cmd = [
        "cmake",
        "--install",
        "build",
        "--prefix",
        str(install_dir),
        "--config",
        build_type,
    ]
    print(f"  {t('build_command')}: {' '.join(install_cmd)}")

    result = subprocess.run(
        install_cmd,
        cwd=cpp_algo_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    if result.stdout:
        print(result.stdout)
    if result.returncode != 0:
        print(f"  {t('error')} {t('cmake_install_failed')}:")
        if result.stderr:
            print(result.stderr)
        return False
    if result.stderr:
        print(result.stderr)

    agent_dir = install_dir / "agent"
    print(f"  -> {agent_dir}")
    return True


def main():
    init_local()

    parser = argparse.ArgumentParser(description=t("description"))
    parser.add_argument("--ci", action="store_true", help=t("arg_ci"))
    parser.add_argument("--os", dest="target_os", help=t("arg_os"))
    parser.add_argument("--arch", dest="target_arch", help=t("arg_arch"))
    parser.add_argument("--version", help=t("arg_version"))
    parser.add_argument("--cpp-algo", action="store_true", help=t("arg_cpp_algo"))
    args = parser.parse_args()

    use_copy = args.ci

    root_dir = Path(__file__).parent.parent.resolve()
    assets_dir = root_dir / "assets"
    install_dir = root_dir / "install"

    mode_text = t("mode_ci") if use_copy else t("mode_dev")
    print(f"{Console.info(t('root_dir'))}: {root_dir}")
    print(f"{Console.info(t('install_dir'))}:   {install_dir}")
    print(f"{Console.warn(t('mode'))}:       {mode_text}")
    print()

    install_dir.mkdir(parents=True, exist_ok=True)

    # 用于链接或复制的函数
    link_or_copy_dir = copy_directory if use_copy else create_directory_link
    link_or_copy_file = copy_file if use_copy else create_file_link

    # 1. 配置 OCR 模型
    print(Console.step(t("step_configure_ocr")))
    if not configure_ocr_model(assets_dir):
        print(f"  {Console.err(t('error'))} {t('configure_ocr_failed')}")
        sys.exit(1)

    # 2. 链接/复制 assets 目录内容（排除 MaaCommonAssets）
    print(Console.step(t("step_process_assets")))
    for item in assets_dir.iterdir():
        if item.name == "MaaCommonAssets":
            continue
        dst = install_dir / item.name
        if item.is_dir():
            if link_or_copy_dir(item, dst):
                print(f"  {Console.ok('->')} {dst}")
        elif item.is_file():
            if link_or_copy_file(item, dst):
                print(f"  {Console.ok('->')} {dst}")

    # 3. 构建 Go Agent
    print(Console.step(t("step_build_go")))
    if not build_go_agent(
        root_dir, install_dir, args.target_os, args.target_arch, args.version, use_copy
    ):
        print(f"  {Console.err(t('error'))} {t('build_go_failed')}")
        sys.exit(1)

    # 4. 构建 C++ Algo Agent（仅在指定 --cpp-algo 时）
    if args.cpp_algo:
        print(Console.step(t("step_build_cpp")))
        if not build_cpp_algo(root_dir, install_dir, args.target_os, args.target_arch, use_copy):
            print(f"  {t('error')} {t('build_cpp_failed')}")
            sys.exit(1)
    else:
        print(Console.step(t("step_skip_cpp")))

    # 5. 链接/复制项目根目录文件并创建 maafw 目录
    print(Console.step(t("step_prepare_files")))
    for filename in ["README.md", "LICENSE"]:
        src = root_dir / filename
        dst = install_dir / filename
        if src.exists():
            if link_or_copy_file(src, dst):
                print(f"  {Console.ok('->')} {dst}")

    maafw_dir = install_dir / "maafw"
    maafw_dir.mkdir(parents=True, exist_ok=True)
    print(f"  {Console.ok('->')} {maafw_dir}")

    print()
    print("=" * 50)
    print(Console.ok(t("install_complete")))

    if not use_copy:
        if not any(maafw_dir.iterdir()):
            print()
            print(Console.warn(t("maafw_download_hint")))
            print(f"  {t('maafw_download_step')}")
            print(f"  {t('maafw_download_url')}")
        if (
            not (install_dir / "mxu").exists()
            and not (install_dir / "mxu.exe").exists()
        ):
            print()
            print(Console.warn(t("mxu_download_hint")))
            print(f"  {t('mxu_download_step')}")
            print(f"  {t('mxu_download_url')}")

    print()


if __name__ == "__main__":
    main()
