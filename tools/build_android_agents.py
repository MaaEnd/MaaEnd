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


def find_ndk(explicit: str | None) -> Path:
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
    die(
        "Android NDK not found. Set ANDROID_NDK_ROOT or pass --ndk. "
        "Need a complete NDK (build/cmake/android.toolchain.cmake)."
    )


def find_cmake() -> Path:
    sdk = os.environ.get("ANDROID_HOME") or os.environ.get("ANDROID_SDK_ROOT")
    extra: list[Path] = []
    if sdk:
        cmake_root = Path(sdk) / "cmake"
        if cmake_root.is_dir():
            extra.extend(sorted(cmake_root.glob("*/bin/cmake.exe"), reverse=True))
            extra.extend(sorted(cmake_root.glob("*/bin/cmake"), reverse=True))
    which = shutil.which("cmake")
    if which:
        extra.append(Path(which))
    for path in extra:
        if not path.is_file():
            continue
        try:
            out = subprocess.check_output([str(path), "--version"], text=True)
        except (OSError, subprocess.CalledProcessError):
            continue
        first = out.splitlines()[0] if out else ""
        # cmake version 3.31.6
        parts = first.split()
        if len(parts) >= 3:
            ver = parts[-1].split("-")[0]
            nums = [int(x) for x in ver.split(".")[:3] if x.isdigit()]
            if nums >= [3, 28, 0]:
                return path.resolve()
    die("CMake >= 3.28 not found. Install one or use the Android SDK cmake package.")


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
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    if args.skip_cpp and args.skip_go:
        die("nothing to build")

    utils = ROOT / "agent" / "cpp-algo" / "MaaUtils" / "MaaUtils.cmake"
    if not utils.is_file():
        die("agent/cpp-algo/MaaUtils is empty; run: git submodule update --init --recursive")

    ndk = find_ndk(args.ndk)
    os.environ["ANDROID_NDK_ROOT"] = str(ndk)
    log(f"[NDK] {ndk}")
    cmake = None if args.skip_cpp else find_cmake()
    if cmake:
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
