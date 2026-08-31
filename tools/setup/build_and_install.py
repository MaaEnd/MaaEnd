import argparse
import os
import platform
import shutil
import subprocess
import sys
import time
from pathlib import Path

from .cli_support import Console, init_localization
from .path_utils import remove_directory_or_link

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


def _timing(label: str, start: float) -> None:
    print(f"  [timing] {label}: {time.monotonic() - start:.1f}s", flush=True)


def _find_ccache(root_dir: Path) -> str | None:
    """找可用的 ccache: 优先工作区官方版 (.cache/ccache-bin), 退回 PATH; 需 >= 4.6 (MSVC 支持)。"""
    candidates: list[Path] = []
    # 1) 工作区官方版本（最可靠，规避 Strawberry Perl 自带的旧 3.x）
    ws_bin = root_dir / ".cache" / "ccache-bin" / "ccache.exe"
    if ws_bin.exists():
        candidates.append(ws_bin)
    # 2) PATH 中的 ccache
    found = shutil.which("ccache")
    if found:
        candidates.append(Path(found))

    for cand in candidates:
        try:
            proc = subprocess.run(
                [str(cand), "--version"],
                capture_output=True,
                text=True,
                encoding="utf-8",
                errors="replace",
                timeout=15,
            )
            if proc.returncode != 0:
                continue
            first = proc.stdout.splitlines()[0] if proc.stdout else ""
            # 例如 "ccache version 4.14" 或 "ccache version 3.7.12"
            import re as _re

            m = _re.search(r"ccache version (\d+)\.(\d+)", first)
            if not m:
                continue
            major, minor = int(m.group(1)), int(m.group(2))
            if major > 4 or (major == 4 and minor >= 6):
                return str(cand)
        except (OSError, subprocess.SubprocessError, ValueError):
            continue
    return None


def create_directory_link(src: Path, dst: Path) -> bool:
    """
    在指定位置创建一个指定目录的链接
    - Windows：Junction
    - Unix/macOS：symlink
    """
    if dst.exists() or dst.is_symlink():
        remove_directory_or_link(dst)

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
        remove_directory_or_link(dst)
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
        print(
            f"  {Console.err(t('error'))} {t('go_source_not_found')}: {go_service_dir}"
        )
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
            else "arm64"
            if machine in ("aarch64", "arm64")
            else machine
        )

    ext = ".exe" if goos == "windows" else ""

    agent_dir = install_dir / "agent"
    agent_dir.mkdir(parents=True, exist_ok=True)
    output_path = agent_dir / f"go-service{ext}"

    print(f"  {Console.info(t('target_platform'))}: {goos}/{goarch}")
    print(f"  {Console.info(t('output_path'))}: {output_path}")

    env = {**os.environ, "GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0"}

    # 开发模式下自动同步 go.mod / go.sum；CI 模式下只校验是否已同步，避免静默改动依赖文件。
    tidy_cmd = ["go", "mod", "tidy"]
    if ci_mode:
        tidy_cmd.append("-diff")

    t_tidy_start = time.monotonic()
    tidy_result = subprocess.run(
        tidy_cmd,
        cwd=go_service_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        env=env,
    )
    if tidy_result.stdout:
        print(tidy_result.stdout)
    if tidy_result.returncode != 0:
        if ci_mode:
            print(f"  {Console.err(t('error'))} {t('go_mod_files_out_of_sync')}")
            if tidy_result.stderr:
                max_stderr_chars = 8 * 1024
                stderr_snippet = tidy_result.stderr.rstrip()
                if len(stderr_snippet) > max_stderr_chars:
                    stderr_snippet = stderr_snippet[-max_stderr_chars:]
                print(
                    f"  {Console.err(t('error'))} {t('go_mod_tidy_stderr')}:\n{stderr_snippet}"
                )
        else:
            print(
                f"  {Console.err(t('error'))} {t('go_mod_tidy_failed')}: {tidy_result.stderr}"
            )
        return False
    if tidy_result.stderr:
        print(tidy_result.stderr)
    _timing("go mod tidy", t_tidy_start)

    t_vendor_start = time.monotonic()
    vendor_result = subprocess.run(
        ["go", "mod", "vendor"],
        cwd=go_service_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        env=env,
    )
    if vendor_result.stdout:
        print(vendor_result.stdout)
    if vendor_result.returncode != 0:
        print(
            f"  {Console.err(t('error'))} {t('go_mod_vendor_failed')}: {vendor_result.stderr}"
        )
        return False
    if vendor_result.stderr:
        print(vendor_result.stderr)
    _timing("go mod vendor", t_vendor_start)

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

    t_build_start = time.monotonic()
    result = subprocess.run(
        build_cmd,
        cwd=go_service_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
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
    _timing("go build", t_build_start)

    print(f"  {Console.ok('->')} {output_path}")
    return True


def setup_windows_msvc_env(arch: str = "x86_64") -> bool:
    """初始化 MSVC 环境 (vswhere + vcvarsall), 供 Ninja + cl.exe 使用。失败返回 False。"""
    sys_root = os.environ.get("ProgramFiles(x86)", r"C:\Program Files (x86)")
    vswhere = Path(sys_root) / "Microsoft Visual Studio" / "Installer" / "vswhere.exe"
    if not vswhere.exists():
        # 兼容 VS 安装到非标准位置：尝试从 vswhere 注册表路径查找
        vswhere = Path("C:") / "Program Files (x86)" / "Microsoft Visual Studio" / "Installer" / "vswhere.exe"

    if not vswhere.exists():
        print(f"  {Console.warn(t('warning'))} {t('vswhere_not_found')}")
        return False

    try:
        result = subprocess.run(
            [
                str(vswhere),
                "-latest",
                "-products",
                "*",
                "-requires",
                "Microsoft.VisualStudio.Component.VC.Tools.x86.x64",
                "-property",
                "installationPath",
            ],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
    except (OSError, subprocess.SubprocessError) as exc:
        print(f"  {Console.warn(t('warning'))} {t('vswhere_query_failed', error=exc)}")
        return False

    vs_path = result.stdout.strip()
    if result.returncode != 0 or not vs_path:
        print(f"  {Console.warn(t('warning'))} {t('vswhere_not_found')}")
        return False

    vcvarsall = Path(vs_path) / "VC" / "Auxiliary" / "Build" / "vcvarsall.bat"
    if not vcvarsall.exists():
        print(
            f"  {Console.warn(t('warning'))} {t('vcvars_not_found', path=vcvarsall)}"
        )
        return False

    # x86_64 -> x64; aarch64 -> x64_arm64 (x64 host cross-compile to arm64 target).
    # Bare "arm64" means arm64 host -> arm64 target, which would break the
    # Hostx64/arm64 toolchain enforced below on x64 runners.
    msvc_arch = "x64_arm64" if arch == "aarch64" else "x64"
    # vcvarsall 必须在 shell 中执行 (cmd 对带空格路径的引号处理有坑)
    try:
        env_dump = subprocess.run(
            f'call "{vcvarsall}" {msvc_arch} >nul && set',
            shell=True,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
    except (OSError, subprocess.SubprocessError) as exc:
        print(f"  {Console.warn(t('warning'))} {t('vcvars_run_failed', error=exc)}")
        return False

    if env_dump.returncode != 0 or not env_dump.stdout:
        print(f"  {Console.warn(t('warning'))} {t('vcvars_run_failed', error=env_dump.stderr[:200])}")
        return False

    loaded = 0
    for line in env_dump.stdout.splitlines():
        if "=" not in line:
            continue
        key, _, value = line.partition("=")
        key = key.strip()
        value = value.strip()
        if not key:
            continue
        os.environ[key] = value
        loaded += 1

    print(f"  {Console.ok(t('msvc_env_loaded', count=loaded))}")

    # 确保 cl.exe 在 PATH 中（vcvarsall 在部分环境可能不把目标架构的 bin 加入 PATH，
    # 交叉编译时尤为明显）。无论如何，都把"最新版 MSVC 目标架构 bin + Windows Kits
    # 目标架构 bin"强制放到 PATH 最前，避免旧版本 cl 或 mingw/LLVM 抢占。
    tgt = "arm64" if arch == "aarch64" else "x64"
    vc_bin = Path(vs_path) / "VC" / "Tools" / "MSVC"
    prepend_dirs: list[str] = []

    if vc_bin.exists():
        msvc_dirs = sorted(vc_bin.iterdir(), key=lambda p: p.name, reverse=True)
        if msvc_dirs:
            latest_msvc = msvc_dirs[0]
            # Hostx64/<tgt> 是 CI 交叉编译的主要形态；HostARM64/<tgt> 供 ARM64 宿主
            for host in ["Hostx64", "HostARM64"]:
                cand_dir = latest_msvc / "bin" / host / tgt
                if (cand_dir / "cl.exe").exists():
                    prepend_dirs.append(str(cand_dir))
                    break

    # Windows Kits 的 bin：rc.exe 用宿主架构（x64）——arm64 的 rc.exe 是 ARM64 原生程序，
    # 在 x64 runner 上无法运行（Exec format error）。交叉编译时 manifest 由 x64 的 rc 处理。
    # 用 glob 直接找 <SDK bin>/<version>/x64/rc.exe，跨 runner 稳定
    import glob as _glob

    rc_exe_path = None
    for probe_root in [
        Path(os.environ.get("WindowsSdkDir", "")) / "bin",
        Path(r"C:\Program Files (x86)\Windows Kits\10\bin"),
    ]:
        if not probe_root.exists():
            continue
        matches = sorted(
            _glob.glob(str(probe_root / "*" / "x64" / "rc.exe")),
            reverse=True,
        )
        if matches:
            rc_exe_path = Path(matches[0])
            break
    if rc_exe_path is not None:
        prepend_dirs.append(str(rc_exe_path.parent))
        # 让 configure 阶段能拿到 x64 rc 的完整路径（CMake 的 Ninja+MSVC 会按目标架构选 arm64 rc，
        # 必须显式覆盖为宿主 x64 rc）
        os.environ["MAAEND_RC_COMPILER"] = str(rc_exe_path)
    else:
        os.environ.pop("MAAEND_RC_COMPILER", None)

    if prepend_dirs:
        old_path = os.environ.get("PATH", "")
        new_path = os.pathsep.join(prepend_dirs) + os.pathsep + old_path
        os.environ["PATH"] = new_path

    cl_in_path = shutil.which("cl.exe")
    if cl_in_path:
        print(f"  [diag] cl.exe in PATH: {cl_in_path}")
    else:
        print(f"  [diag] cl.exe STILL not in PATH; prepend_dirs={prepend_dirs}")
    print(f"  [diag] rc.exe in PATH: {shutil.which('rc.exe')}")
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


def cleanup_cmake_cache(build_dir: Path, interactive_retry: bool = False) -> bool:
    """清理 CMake 缓存，避免不同 generator 之间切换冲突。"""
    cache_paths = [build_dir / "CMakeCache.txt", build_dir / "CMakeFiles"]

    while True:
        cleanup_errors: list[tuple[Path, OSError]] = []

        for cache_path in cache_paths:
            if not cache_path.exists():
                continue

            try:
                if cache_path.is_dir():
                    shutil.rmtree(cache_path)
                else:
                    cache_path.unlink(missing_ok=True)
            except (PermissionError, OSError) as exc:
                cleanup_errors.append((cache_path, exc))

        if not cleanup_errors:
            return True

        for cache_path, exc in cleanup_errors:
            print(
                f"  {Console.warn(t('warning'))} {t('cmake_cache_cleanup_item_failed', path=cache_path, error=exc)}"
            )

        if interactive_retry and sys.stdin.isatty() and sys.stdout.isatty():
            choice = input(
                f"  {Console.warn(t('warning'))} {t('cmake_cache_cleanup_prompt')}"
            )
            if choice.strip().lower() == "q":
                return False
            continue

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
    configure_preset_candidates: list[str]
    if resolved_os == "win":
        if resolved_arch == "aarch64":
            # 优先 Ninja Multi-Config（多核并行编译，大幅缩短 MSVC 构建时间），
            # 失败时回退 VS generator（ARM 交叉编译）
            configure_preset_candidates = [
                "NinjaMulti Win32 ARM64",
                "MSVC 2026 ARM",
                "MSVC 2022 ARM",
            ]
        else:
            configure_preset_candidates = [
                "NinjaMulti Win32",
                "MSVC 2026",
                "MSVC 2022",
            ]
    elif resolved_os == "linux":
        if resolved_arch == "aarch64":
            configure_preset_candidates = ["NinjaMulti Linux arm64"]
        else:
            configure_preset_candidates = ["NinjaMulti Linux x64"]
    else:
        # macOS
        configure_preset_candidates = ["NinjaMulti"]

    # Windows 走 Ninja 时，需要先初始化 MSVC 开发环境（cl.exe/INCLUDE/LIB）
    if resolved_os == "win":
        if configure_preset_candidates[0].startswith("NinjaMulti Win32"):
            if not setup_windows_msvc_env(resolved_arch):
                print(
                    f"  {Console.warn(t('warning'))} {t('msvc_env_fail_fallback')}"
                )
                # 回退到 VS generator（CMake 可自动定位 VS，无需环境变量）
                if resolved_arch == "aarch64":
                    configure_preset_candidates = ["MSVC 2026 ARM", "MSVC 2022 ARM"]
                else:
                    configure_preset_candidates = ["MSVC 2026", "MSVC 2022"]

    # 构建 MAADEPS_TRIPLET: maa-{x64|arm64}-{windows|linux|osx}
    arch_part = "x64" if resolved_arch == "x86_64" else "arm64"
    os_part = {"win": "windows", "macos": "osx", "linux": "linux"}.get(
        resolved_os, resolved_os
    )
    maadeps_triplet = f"maa-{arch_part}-{os_part}"

    print(f"  {t('build_mode')}: {build_type}")
    print(f"  {t('target_platform')}: {resolved_os}/{resolved_arch}")
    print(
        f"  {t('cmake_configure_preset_candidates')}: {', '.join(configure_preset_candidates)}"
    )
    print(f"  {t('maadeps_triplet')}: {maadeps_triplet}")

    ccache_prog = _find_ccache(root_dir)
    if ccache_prog:
        os.environ["CCACHE_DIR"] = str(root_dir / ".cache" / "ccache")
        Path(os.environ["CCACHE_DIR"]).mkdir(parents=True, exist_ok=True)
        # 前置到 PATH, 确保 CMake 命中官方 4.14 而非 Strawberry 旧 3.x
        ccache_dir = str(Path(ccache_prog).parent)
        old_path = os.environ.get("PATH", "")
        if ccache_dir not in old_path.split(os.pathsep):
            os.environ["PATH"] = ccache_dir + os.pathsep + old_path
        print(f"  {Console.ok(t('ccache_status', path=os.environ['CCACHE_DIR']))}")
        print(f"  {t('ccache_compiler_launcher')}: {ccache_prog}")
    else:
        print(f"  {Console.warn(t('ccache_not_found'))}")

    # cmake --preset <configure_preset>（按候选列表依次尝试）
    configure_preset = configure_preset_candidates[0]
    build_dir = cpp_algo_dir / "build"
    if len(configure_preset_candidates) > 1 and not cleanup_cmake_cache(build_dir):
        print(
            f"  {Console.warn(t('warning'))} {t('cmake_cache_cleanup_first_try_hint')}"
        )

    t_configure_start = time.monotonic()
    for idx, preset in enumerate(configure_preset_candidates):
        if idx > 0 and not cleanup_cmake_cache(
            build_dir, interactive_retry=not ci_mode
        ):
            print(
                f"  {Console.err(t('error'))} {t('cmake_cache_cleanup_fallback_aborted')}"
            )
            return False

        enable_ccache = "ON" if ccache_prog else "OFF"
        configure_cmd = [
            "cmake",
            "--preset",
            preset,
            f"-DMAADEPS_TRIPLET={maadeps_triplet}",
            f"-DCMAKE_INSTALL_PREFIX={install_dir}",
            f"-DENABLE_CCACHE={enable_ccache}",
        ]

        # MSVC + ccache: /Zi 不可缓存, 改用 /Z7 (Embedded)
        if resolved_os == "win" and enable_ccache == "ON":
            configure_cmd.append(
                "-DCMAKE_MSVC_DEBUG_INFORMATION_FORMAT=Embedded"
            )

        # 交叉编译: 显式用 x64(宿主) rc.exe; -D 值里反斜杠是转义符, 必须用正斜杠
        if resolved_os == "win" and os.environ.get("MAAEND_RC_COMPILER"):
            rc_path = os.environ["MAAEND_RC_COMPILER"].replace("\\", "/")
            configure_cmd.append(f"-DCMAKE_RC_COMPILER={rc_path}")

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
            errors="replace",
        )

        if result.stdout:
            print(result.stdout)

        if result.returncode == 0:
            configure_preset = preset
            if idx > 0:
                print(
                    f"  {Console.warn(t('warning'))} {t('cmake_fallback_preset_used', preset=preset)}"
                )
            _timing("cmake configure", t_configure_start)
            break

        # 失败时：如果还有下一个候选，继续尝试；否则报错退出
        if result.stderr:
            print(result.stderr)
        if idx < len(configure_preset_candidates) - 1:
            print(
                f"  {Console.warn(t('warning'))} {t('cmake_preset_configure_retry', preset=preset)}"
            )
            continue

        print(f"  {t('error')} {t('cmake_configure_failed')}:")
        return False

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

    t_build_start = time.monotonic()
    result = subprocess.run(
        build_cmd,
        cwd=cpp_algo_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
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
    _timing("cmake build", t_build_start)

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

    t_install_start = time.monotonic()
    result = subprocess.run(
        install_cmd,
        cwd=cpp_algo_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
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
    _timing("cmake install", t_install_start)

    agent_dir = install_dir / "agent"
    print(f"  -> {agent_dir}")
    return True


def main() -> None:
    init_local()

    parser = argparse.ArgumentParser(prog="build-and-install", description=t("description"))
    parser.add_argument("--ci", action="store_true", help=t("arg_ci"))
    parser.add_argument("--os", dest="target_os", help=t("arg_os"))
    parser.add_argument("--arch", dest="target_arch", help=t("arg_arch"))
    parser.add_argument("--version", help=t("arg_version"))
    parser.add_argument("--cpp-algo", action="store_true", help=t("arg_cpp_algo"))
    args = parser.parse_args()

    use_copy = args.ci

    root_dir = Path(__file__).resolve().parents[2]
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

    # 1. 链接/复制 assets 目录内容 和 docs/img 目录
    print(Console.step(t("step_process_assets")))
    t_step1_start = time.monotonic()
    for item in assets_dir.iterdir():
        dst = install_dir / item.name
        if item.is_dir():
            if link_or_copy_dir(item, dst):
                print(f"  {Console.ok('->')} {dst}")
        elif item.is_file():
            if link_or_copy_file(item, dst):
                print(f"  {Console.ok('->')} {dst}")

    docs_img_dir = root_dir / "docs" / "img"
    if docs_img_dir.is_dir():
        docs_img_dst = install_dir / "docs" / "img"
        if link_or_copy_dir(docs_img_dir, docs_img_dst):
            print(f"  {Console.ok('->')} {docs_img_dst}")
        else:
            print(f"  {Console.warn(t('warning'))} {t('docs_copy_failed')}")
    _timing("step1 copy assets+docs", t_step1_start)

    # 2. 构建 Go Agent
    print(Console.step(t("step_build_go")))
    t_step2_start = time.monotonic()
    if not build_go_agent(
        root_dir, install_dir, args.target_os, args.target_arch, args.version, use_copy
    ):
        print(f"  {Console.err(t('error'))} {t('build_go_failed')}")
        sys.exit(1)
    _timing("step2 go agent", t_step2_start)

    # 3. 构建 C++ Algo Agent（仅在指定 --cpp-algo 时）
    if args.cpp_algo:
        print(Console.step(t("step_build_cpp")))
        t_step3_start = time.monotonic()
        if not build_cpp_algo(
            root_dir, install_dir, args.target_os, args.target_arch, use_copy
        ):
            print(f"  {t('error')} {t('build_cpp_failed')}")
            sys.exit(1)
        _timing("step3 cpp agent", t_step3_start)
    else:
        print(Console.step(t("step_skip_cpp")))

    # 4. 链接/复制项目根目录文件并创建 maafw 目录
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
    print(t("separator"))
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
