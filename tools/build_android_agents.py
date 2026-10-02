#!/usr/bin/env python3
"""Cross-compile MaaEnd agents for Android and drop them into MaaFwApp jniLibs.

Output layout (consumed by MaaFwApp syncAgentJniLibs):

    Android/agent-dist/<abi>/jniLibs/libgo-service.so
    Android/agent-dist/<abi>/jniLibs/libcpp-algo.so

MaaFramework .so stay in the MaaFwApp tree (setup_maa_framework.py). This script
only uses the Android SDK zip as a *link-time* DEPS_DIR.
"""

from __future__ import annotations

import argparse
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MFW_REPO = "MaaXYZ/MaaFramework"
DEFAULT_MAAFW_TAG = "v5.14.0"
DEFAULT_ABI = "arm64-v8a"
DEFAULT_API = 23
GOARCH_BY_ABI = {"arm64-v8a": "arm64", "x86_64": "amd64"}
CLANG_TRIPLE_BY_ABI = {
    "arm64-v8a": "aarch64-linux-android",
    "x86_64": "x86_64-linux-android",
}
MAADEPS_TARGET_BY_ABI = {
    "arm64-v8a": "arm64-android",
    "x86_64": "x64-android",
}


def log(msg: str) -> None:
    print(msg, flush=True)


def die(msg: str, code: int = 1) -> None:
    print(f"[ERR] {msg}", file=sys.stderr, flush=True)
    raise SystemExit(code)


def run(cmd: list[str] | str, *, cwd: Path | None = None, env: dict[str, str] | None = None) -> None:
    printable = cmd if isinstance(cmd, str) else " ".join(cmd)
    log(f"[RUN] {printable}")
    completed = subprocess.run(cmd, cwd=cwd, env=env, shell=isinstance(cmd, str))
    if completed.returncode != 0:
        die(f"command failed ({completed.returncode}): {printable}")


def github_json(url: str) -> dict:
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    req = urllib.request.Request(url)
    req.add_header("Accept", "application/vnd.github+json")
    req.add_header("User-Agent", "MaaEnd-android-agents")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as exc:
        if exc.code == 401 and token:
            req.remove_header("Authorization")
            with urllib.request.urlopen(req, timeout=60) as resp:
                return json.loads(resp.read().decode())
        raise


def download(url: str, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    log(f"[DOWNLOAD] {url}")
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    req = urllib.request.Request(url)
    req.add_header("User-Agent", "MaaEnd-android-agents")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        resp_ctx = urllib.request.urlopen(req, timeout=600)
    except urllib.error.HTTPError as exc:
        if exc.code == 401 and token:
            req.remove_header("Authorization")
            resp_ctx = urllib.request.urlopen(req, timeout=600)
        else:
            raise
    with resp_ctx as resp, open(dest, "wb") as out:
        shutil.copyfileobj(resp, out)


def host_ndk_prebuilt() -> str:
    system = platform.system().lower()
    machine = platform.machine().lower()
    if system == "windows":
        return "windows-x86_64"
    if system == "darwin":
        return "darwin-arm64" if machine in {"arm64", "aarch64"} else "darwin-x86_64"
    return "linux-x86_64"


INSTALL_HINTS = {
    "jdk": {
        "Windows": "winget install Microsoft.OpenJDK.21",
        "Darwin": "brew install openjdk",
        "Linux": "apt install openjdk-21-jdk",
    },
    "cmake": {
        "Windows": "winget install Kitware.CMake",
        "Darwin": "brew install cmake",
        "Linux": "apt install cmake",
    },
    "ninja": {
        "Windows": "winget install Ninja-build.Ninja",
        "Darwin": "brew install ninja",
        "Linux": "apt install ninja-build",
    },
    "go": {
        "Windows": "winget install GoLang.Go",
        "Darwin": "brew install go",
        "Linux": "https://go.dev/dl/",
    },
}


def install_hint(tool: str) -> str:
    """按当前平台给出安装命令，免得在 macOS 上提示 winget。"""
    hints = INSTALL_HINTS[tool]
    return hints.get(platform.system(), hints["Linux"])


def find_ndk(explicit: str | None, *, fail: bool = True) -> Path | None:
    candidates: list[Path] = []
    if explicit:
        candidates.append(Path(explicit))
    for key in ("ANDROID_NDK_ROOT", "ANDROID_NDK_HOME", "NDK_ROOT"):
        if os.environ.get(key):
            candidates.append(Path(os.environ[key]))
    sdk = os.environ.get("ANDROID_HOME") or os.environ.get("ANDROID_SDK_ROOT")
    if sdk:
        ndk_root = Path(sdk) / "ndk"
        if ndk_root.is_dir():
            versions = sorted(
                (p for p in ndk_root.iterdir() if p.is_dir() and (p / "source.properties").exists()),
                key=lambda p: p.name,
                reverse=True,
            )
            candidates.extend(versions)
    for path in candidates:
        toolchain = path / "build" / "cmake" / "android.toolchain.cmake"
        if toolchain.is_file():
            return path.resolve()
    if not fail:
        return None
    die(
        "Android NDK not found. Set ANDROID_NDK_ROOT or pass --ndk. "
        "Need a complete NDK (build/cmake/android.toolchain.cmake)."
    )


def cmake_candidates() -> list[Path]:
    """按优先级列出候选 cmake：SDK 自带的（版本高的在前），再是 PATH 上的。"""
    candidates: list[Path] = []
    sdk = os.environ.get("ANDROID_HOME") or os.environ.get("ANDROID_SDK_ROOT")
    if sdk:
        cmake_root = Path(sdk) / "cmake"
        if cmake_root.is_dir():
            candidates.extend(sorted(cmake_root.glob("*/bin/cmake.exe"), reverse=True))
            candidates.extend(sorted(cmake_root.glob("*/bin/cmake"), reverse=True))
    which = shutil.which("cmake")
    if which:
        candidates.append(Path(which))
    return candidates


def cmake_version(path: Path) -> list[int] | None:
    """读 cmake --version 的版本号，读不出来返回 None。"""
    try:
        out = subprocess.check_output([str(path), "--version"], text=True)
    except (OSError, subprocess.CalledProcessError):
        return None
    first = out.splitlines()[0] if out else ""
    # cmake version 3.31.6
    parts = first.split()
    if len(parts) < 3:
        return None
    nums = [int(x) for x in parts[-1].split("-")[0].split(".")[:3] if x.isdigit()]
    return nums or None


def newest_cmake() -> tuple[list[int], Path] | None:
    """版本号最高的那个 cmake，只用于诊断“装了但太旧”。"""
    newest: tuple[list[int], Path] | None = None
    for path in cmake_candidates():
        if not path.is_file():
            continue
        nums = cmake_version(path)
        if nums and (newest is None or nums > newest[0]):
            newest = (nums, path)
    return newest


def find_cmake(*, fail: bool = True) -> Path | None:
    for path in cmake_candidates():
        if not path.is_file():
            continue
        nums = cmake_version(path)
        if nums and nums >= [3, 28, 0]:
            return path.resolve()
    if not fail:
        return None
    newest = newest_cmake()
    if newest:
        nums, path = newest
        version = ".".join(str(n) for n in nums)
        die(
            f"CMake >= 3.28 required, but the newest one found is {version} ({path}).\n"
            f"  {install_hint('cmake')}\n"
            '  Or: sdkmanager --install "cmake;3.31.6"'
        )
    die(f"CMake not found in PATH or <SDK>/cmake. {install_hint('cmake')}")


def ninja_host_name() -> str:
    return "ninja.exe" if os.name == "nt" else "ninja"


def find_ninja(*, verbose: bool = True) -> Path | None:
    """定位 ninja；CMake 的 Ninja 生成器只从 PATH 找它，找不到就直接配置失败。

    优先 PATH，其次 Android SDK 自带的 cmake/<ver>/bin（Gradle 装 SDK 的 CMake 时会
    顺带放一份），找到就把所在目录追加到 PATH，让后续 cmake --preset 能看见。
    """
    which = shutil.which("ninja")
    if which:
        return Path(which).resolve()

    # SDK 自带一份（Gradle 装 SDK 的 CMake 时会顺带放进去），用 ANDROID_HOME 找
    sdk_env = os.environ.get("ANDROID_HOME") or os.environ.get("ANDROID_SDK_ROOT")
    if not sdk_env:
        return None
    cmake_root = Path(sdk_env) / "cmake"
    if not cmake_root.is_dir():
        return None

    for path in sorted(cmake_root.glob(f"*/bin/{ninja_host_name()}"), reverse=True):
        if not path.is_file():
            continue
        append_path(path.parent)
        if verbose:
            log(f"[NINJA] {path.resolve()}")
        return path.resolve()
    return None


def append_path(directory: Path) -> None:
    """把目录追加到 PATH 末尾。

    只做"兜底"：SDK 里的 cmake/<ver>/bin 同时含 cmake 与 ninja，但那份 CMake 通常
    低于 3.28；前置会遮蔽用户自己装的 CMake，追加才不会。
    """
    current = os.environ.get("PATH", "")
    entry = str(directory)
    if entry not in current.split(os.pathsep):
        os.environ["PATH"] = current + os.pathsep + entry


def require_ninja() -> Path:
    ninja = find_ninja()
    if ninja:
        return ninja
    die(
        "Ninja not found; CMake's \"Ninja Multi-Config\" generator needs it on PATH.\n"
        f"  {install_hint('ninja')}\n"
        "  Or install the Android SDK cmake package, which ships a ninja under "
        "<SDK>/cmake/<version>/bin.\n"
        "  See docs/zh_cn/developers/android-build-env.md#4-装依赖时的报错"
    )


def prepare_maadeps(abi: str) -> None:
    script = ROOT / "tools" / "maadeps-download.py"
    if not script.is_file():
        die(f"missing {script}")
    target = MAADEPS_TARGET_BY_ABI[abi]
    installed = (
        ROOT
        / "agent"
        / "cpp-algo"
        / "MaaUtils"
        / "MaaDeps"
        / "vcpkg"
        / "installed"
        / f"maa-{target}"
    )
    if installed.is_dir() and any(installed.iterdir()):
        log(f"[SKIP] MaaDeps already present: {installed}")
        return
    run([sys.executable, str(script), target])


def find_sdk_root(extract_root: Path) -> Path:
    for dirpath, _dirnames, _filenames in os.walk(extract_root):
        current = Path(dirpath)
        if (current / "bin").is_dir() and (current / "share").is_dir():
            return current
    die(f"extracted MaaFramework SDK has no bin+share under {extract_root}")


def prepare_maafw(deps_dir: Path, tag: str, abi: str, skip: bool) -> None:
    marker = deps_dir / "share" / "cmake" / "MaaFramework"
    version_file = deps_dir / "MAAFW_VERSION"
    if skip and marker.is_dir():
        log(f"[SKIP] Android MaaFramework SDK already at {deps_dir}")
        return
    if marker.is_dir() and version_file.is_file() and version_file.read_text(encoding="utf-8").strip() == tag:
        log(f"[SKIP] Android MaaFramework SDK already {tag}")
        return

    arch = "aarch64" if abi == "arm64-v8a" else "x86_64"
    url = f"https://api.github.com/repos/{MFW_REPO}/releases/tags/{tag}"
    log(f"[FETCH] {url}")
    data = github_json(url)
    assets = data.get("assets") or []
    asset = next(
        (
            a
            for a in assets
            if isinstance(a, dict)
            and str(a.get("name", "")).endswith(".zip")
            and f"android-{arch}" in str(a.get("name", "")).lower()
        ),
        None,
    )
    if not asset:
        die(f"no MAA-android-{arch} zip on {tag}")

    cache = ROOT / ".cache" / "android-agents"
    cache.mkdir(parents=True, exist_ok=True)
    archive = cache / str(asset["name"])
    if not archive.is_file():
        download(str(asset["browser_download_url"]), archive)

    if deps_dir.exists():
        shutil.rmtree(deps_dir)
    with tempfile.TemporaryDirectory() as tmp:
        extract_root = Path(tmp) / "extracted"
        extract_root.mkdir()
        log(f"[EXTRACT] {archive.name}")
        with zipfile.ZipFile(archive) as zf:
            zf.extractall(extract_root)
        sdk_root = find_sdk_root(extract_root)
        shutil.copytree(sdk_root, deps_dir)
    version_file.write_text(tag + "\n", encoding="utf-8")
    log(f"[OK] Android MaaFramework SDK -> {deps_dir} ({tag})")


def build_cpp(
    *,
    cmake: Path,
    abi: str,
    out_so: Path,
) -> None:
    source = ROOT / "agent" / "cpp-algo"
    build_dir = source / "build" / "android" / abi
    preset = "NinjaMulti Android arm64" if abi == "arm64-v8a" else "NinjaMulti Android x64"
    # Same generator as CLion / CMakePresets.json. A leftover single-config Ninja
    # cache in this binaryDir will refuse to reconfigure.
    run([str(cmake), "--preset", preset], cwd=source)
    run(
        [
            str(cmake),
            "--build",
            "--preset",
            f"{preset} - RelWithDebInfo",
            "--target",
            "cpp-algo",
        ],
        cwd=source,
    )
    # cpp-algo 是可执行目标；按 jniLibs 约定改名为 lib*.so 才会被打进 APK
    built = build_dir / "bin" / "RelWithDebInfo" / "cpp-algo"
    if not built.is_file():
        die(f"cpp-algo not produced under {build_dir}")
    out_so.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(built, out_so)
    log(f"[OK] {out_so}")


def build_go(*, ndk: Path, abi: str, api: int, version: str, out_so: Path) -> None:
    go = shutil.which("go")
    if not go:
        die("go not found on PATH")
    prebuilt = ndk / "toolchains" / "llvm" / "prebuilt" / host_ndk_prebuilt() / "bin"
    clang = prebuilt / ("clang.exe" if os.name == "nt" else "clang")
    clangxx = prebuilt / ("clang++.exe" if os.name == "nt" else "clang++")
    if not clang.is_file():
        die(f"NDK clang not found: {clang}")
    triple = f"{CLANG_TRIPLE_BY_ABI[abi]}{api}"
    target = f"--target={triple}"
    env = os.environ.copy()
    env.update(
        {
            "GOOS": "android",
            "GOARCH": GOARCH_BY_ABI[abi],
            "CGO_ENABLED": "1",
            "CC": str(clang),
            "CXX": str(clangxx),
            "CGO_CFLAGS": target,
            "CGO_CXXFLAGS": target,
            "CGO_LDFLAGS": target,
            "GOTOOLCHAIN": env.get("GOTOOLCHAIN", "auto"),
        }
    )
    out_so.parent.mkdir(parents=True, exist_ok=True)
    cmd = [
        go,
        "build",
        "-mod=readonly",
        "-trimpath",
        "-buildvcs=false",
        f"-ldflags=-X main.Version={version}",
        "-o",
        str(out_so),
        ".",
    ]
    run(cmd, cwd=ROOT / "agent" / "go-service", env=env)
    log(f"[OK] {out_so}")


def _enable_windows_vt() -> bool:
    """Windows 控制台默认不认 ANSI，得先打开 VT 处理。"""
    try:
        import ctypes
        from ctypes import wintypes

        kernel32 = ctypes.windll.kernel32
        handle = kernel32.GetStdHandle(-11)
        mode = wintypes.DWORD()
        if not kernel32.GetConsoleMode(handle, ctypes.byref(mode)):
            return False
        return bool(kernel32.SetConsoleMode(handle, mode.value | 0x0004))
    except Exception:
        return False


_color_enabled: bool | None = None


def supports_color() -> bool:
    global _color_enabled
    if _color_enabled is None:
        if os.environ.get("NO_COLOR") is not None:
            _color_enabled = False
        elif os.environ.get("FORCE_COLOR") is not None:
            _color_enabled = True
        elif not (hasattr(sys.stdout, "isatty") and sys.stdout.isatty()):
            _color_enabled = False
        elif os.name == "nt":
            _color_enabled = _enable_windows_vt()
        else:
            _color_enabled = os.environ.get("TERM", "") not in ("", "dumb")
    return _color_enabled


RED = "\033[31m"
GREEN = "\033[32m"
DIM = "\033[2m"
BOLD = "\033[1m"
RESET = "\033[0m"


def paint(text: str, color: str) -> str:
    return f"{color}{text}{RESET}" if supports_color() else text


def jdk_major(java: str) -> int | None:
    """跑 java -version 读主版本；读不出来返回 None。

    输出走 stderr，且 JDK 8 报的是 "1.8.0_471"（主版本在第二段）。
    """
    try:
        out = subprocess.run(
            [java, "-version"], capture_output=True, text=True, errors="replace", timeout=30
        ).stderr
    except (OSError, subprocess.SubprocessError):
        return None
    m = re.search(r'version "(\d+)(?:\.(\d+))?', out)
    if not m:
        return None
    return int(m.group(2)) if m.group(1) == "1" and m.group(2) else int(m.group(1))


def java_check() -> tuple[bool, str, str]:
    """检查 java 是不是 17+，返回 (是否通过, 描述, 额外提示)。

    Gradle 认 JAVA_HOME，没设才用 PATH 上的 java。JAVA_HOME 设了却指不到 java 时
    直接报错、不回退：回退会拿 PATH 上另一个 JDK 判通过，把真正的配置问题盖掉。
    """
    java_home = os.environ.get("JAVA_HOME")
    if java_home:
        exe = Path(java_home) / "bin" / ("java.exe" if os.name == "nt" else "java")
        if not exe.is_file():
            return (
                False,
                f"JAVA_HOME points at {java_home}, but no java under {java_home}/bin",
                f"fix JAVA_HOME, or install JDK 17+ ({install_hint('jdk')})",
            )
        active = exe
    else:
        which = shutil.which("java")
        if not which:
            return (
                False,
                "java not found and JAVA_HOME is not set",
                f"install JDK 17+ ({install_hint('jdk')}), then set JAVA_HOME",
            )
        active = Path(which)

    major = jdk_major(str(active))
    if major is not None and major >= 17:
        return True, f"{active} (JDK {major})", ""

    detail = f"using JDK {major} ({active}), need 17+" if major else f"cannot read JDK version ({active})"
    return False, detail, f"install JDK 17+ ({install_hint('jdk')}) and set JAVA_HOME to it"


def check_environment(args: argparse.Namespace) -> int:
    """逐项体检 Android 构建环境，只报告不安装。返回进程退出码。"""
    log(paint("Android build environment check", BOLD))
    log(paint(f"  {platform.system()} {platform.machine()}, python {platform.python_version()}", DIM))
    log("")

    passed: list[str] = []
    problems: list[tuple[str, str, str]] = []  # (项目, 现状, 怎么修)

    # JDK
    good, detail, hint = java_check()
    if good:
        passed.append("JDK 17+")
    else:
        problems.append(("JDK 17+", detail, hint))

    # Android SDK
    sdk_env = os.environ.get("ANDROID_HOME") or os.environ.get("ANDROID_SDK_ROOT")
    if not sdk_env:
        problems.append((
            "Android SDK",
            "ANDROID_HOME / ANDROID_SDK_ROOT is not set",
            "set ANDROID_HOME to your SDK root (the one containing platforms/ and build-tools/)",
        ))
    elif not Path(sdk_env).is_dir():
        problems.append((
            "Android SDK",
            f"ANDROID_HOME points at {sdk_env}, which does not exist",
            "fix ANDROID_HOME (the SDK root containing platforms/ and build-tools/)",
        ))
    else:
        sdk = Path(sdk_env)
        lacking = [p for p in ("platforms", "build-tools") if not any((sdk / p).glob("*"))]
        if lacking:
            problems.append((
                "Android SDK components",
                f"{sdk} is missing: {', '.join(lacking)}",
                'sdkmanager --install "platforms;android-37.0" "build-tools;37.0.0"',
            ))
        else:
            passed.append("Android SDK")

    # NDK
    if find_ndk(args.ndk, fail=False):
        passed.append("Android NDK")
    else:
        problems.append((
            "Android NDK",
            "ANDROID_NDK_ROOT is not set (or points at an incomplete NDK)",
            'sdkmanager --install "ndk;29.0.13599879", then set ANDROID_NDK_ROOT to it',
        ))

    # CMake
    if find_cmake(fail=False):
        passed.append("CMake >= 3.28")
    else:
        newest = newest_cmake()
        if newest:
            nums, path = newest
            problems.append((
                "CMake >= 3.28",
                f"found {'.'.join(str(n) for n in nums)} ({path}), too old",
                f"{install_hint('cmake')}, or sdkmanager --install \"cmake;3.31.6\"",
            ))
        else:
            problems.append((
                "CMake >= 3.28",
                "not found in PATH or <SDK>/cmake",
                f"{install_hint('cmake')}, or sdkmanager --install \"cmake;3.31.6\"",
            ))

    # Ninja
    if find_ninja(verbose=False):
        passed.append("Ninja")
    else:
        problems.append((
            "Ninja",
            "not found on PATH or <SDK>/cmake/*/bin",
            f"{install_hint('ninja')}, or install the Android SDK cmake package",
        ))

    # Go
    if shutil.which("go"):
        passed.append("Go")
    else:
        problems.append(("Go", "not found", install_hint("go")))

    # MaaUtils 子模块
    if (ROOT / "agent" / "cpp-algo" / "MaaUtils" / "MaaUtils.cmake").is_file():
        passed.append("MaaUtils submodule")
    else:
        problems.append((
            "MaaUtils submodule",
            "empty (only needed to build cpp-algo)",
            "git submodule update --init --recursive",
        ))

    if problems:
        for name, detail, fix in problems:
            log(f"{paint('MISS', RED + BOLD)}  {paint(name, BOLD)}: {detail}")
            log(f"      {paint('-> ' + fix, DIM)}")
        log("")
        log(paint(f"{len(problems)} problem(s) to fix", RED))
        log(paint("guide: docs/zh_cn/developers/android-build-env.md", DIM))
        if passed:
            log(paint(f"already ok: {', '.join(passed)}", DIM))
        return 1

    log(paint(f"OK  environment ready: {', '.join(passed)}", GREEN))
    log(paint("run: uv run tools/build_android_agents.py", DIM))
    return 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build Android agents into MaaFwApp jniLibs layout")
    parser.add_argument("--ndk", help="Android NDK root")
    parser.add_argument(
        "--abi",
        action="append",
        choices=sorted(GOARCH_BY_ABI),
        help=f"target ABI, repeatable (default {DEFAULT_ABI})",
    )
    parser.add_argument("--api", type=int, default=DEFAULT_API, help="Android API level (default 23)")
    parser.add_argument(
        "--maafw-version",
        default=os.environ.get("MAAFW_VERSION", DEFAULT_MAAFW_TAG),
        help=f"MaaFramework release tag used as link-time SDK (default {DEFAULT_MAAFW_TAG})",
    )
    parser.add_argument(
        "--out",
        type=Path,
        default=ROOT / "Android" / "agent-dist",
        help="agent.sourceDir (default Android/agent-dist)",
    )
    parser.add_argument(
        "--version",
        default=os.environ.get("MAAEND_VERSION", "dev"),
        help="go-service main.Version (default dev)",
    )
    parser.add_argument("--skip-download", action="store_true")
    parser.add_argument("--skip-cpp", action="store_true")
    parser.add_argument("--skip-go", action="store_true")
    parser.add_argument(
        "--check-env",
        action="store_true",
        help="check the Android build environment and report what is missing, then exit",
    )
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    if args.check_env:
        raise SystemExit(check_environment(args))
    if args.skip_cpp and args.skip_go:
        die("nothing to build")

    utils = ROOT / "agent" / "cpp-algo" / "MaaUtils" / "MaaUtils.cmake"
    if not utils.is_file():
        die("agent/cpp-algo/MaaUtils is empty; run: git submodule update --init --recursive")

    ndk = find_ndk(args.ndk)
    os.environ["ANDROID_NDK_ROOT"] = str(ndk)
    log(f"[NDK] {ndk}")

    cmake = None
    if not args.skip_cpp:
        # 只有编 cpp-algo 才用 CMake，而 CMake 的 Ninja 生成器只从 PATH 找 ninja，
        # 缺了会在配置阶段报一句很难懂的错。提前定位并兜底补进 PATH
        # （其次选自 SDK 自带的 cmake/<ver>/bin）。--skip-cpp 时不该因此拦下构建。
        require_ninja()
        cmake = find_cmake()
        log(f"[CMAKE] {cmake}")

    out_dirs: list[Path] = []
    for abi in dict.fromkeys(args.abi or [DEFAULT_ABI]):
        log(f"[ABI] {abi}")
        out_dir = args.out.resolve() / abi / "jniLibs"
        out_dir.mkdir(parents=True, exist_ok=True)
        out_dirs.append(out_dir)

        if cmake:
            # 每个 ABI 一份链接期 SDK，与 CMakePresets.json 中 Android 预设的 DEPS_DIR 对应
            deps_dir = ROOT / "deps-android" / abi
            prepare_maadeps(abi)
            prepare_maafw(deps_dir, args.maafw_version, abi, args.skip_download)
            build_cpp(cmake=cmake, abi=abi, out_so=out_dir / "libcpp-algo.so")

        if not args.skip_go:
            build_go(
                ndk=ndk,
                abi=abi,
                api=args.api,
                version=args.version,
                out_so=out_dir / "libgo-service.so",
            )

    log("")
    for out_dir in out_dirs:
        log(f"[DONE] {out_dir}")
        for name in ("libgo-service.so", "libcpp-algo.so"):
            path = out_dir / name
            if path.is_file():
                log(f"       {path}  ({path.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
