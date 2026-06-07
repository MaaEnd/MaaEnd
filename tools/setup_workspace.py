import argparse
import http.client
import json
import os
import shutil
import subprocess
import sys
import platform
import tempfile
import time
import traceback
import urllib.error
import urllib.request
from pathlib import Path
from urllib.parse import urlparse

from cli_support import Console, init_localization
import dep_3rdparty


PROJECT_BASE: Path = Path(__file__).parent.parent.resolve()
MFW_REPO: str = "MaaXYZ/MaaFramework"
MXU_REPO: str = "MistEO/MXU"
MAAEND_REPO: str = "MaaEnd/MaaEnd"


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
            print(Console.err(t("err_create_junction_failed", stderr=result.stderr)))
            return False
    else:
        dst.symlink_to(src)

    return True

LOCALS_DIR = Path(__file__).parent / "locals" / "setup_workspace"


_local_t = lambda key, **kwargs: key.format(**kwargs) if kwargs else key


def init_local() -> None:
    global _local_t
    t_func, load_error_path = init_localization(LOCALS_DIR)
    _local_t = t_func
    if load_error_path:
        print(Console.err(t("error_load_locale", path=load_error_path)))


def t(key: str, **kwargs) -> str:
    return _local_t(key, **kwargs)


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
CPP_ALGO_DIST_NAME: str = "cpp-algo.exe" if OS_KEYWORD == "win" else "cpp-algo"
CPP_ALGO_COMPANION_FILES: tuple[str, ...] = (
    ("WebView2Loader.dll",) if OS_KEYWORD == "win" else ()
)
ARCH_VARIANT_HINTS: dict[str, tuple[str, ...]] = {
    "x86_64": ("x86_64", "amd64", "x64"),
    "aarch64": ("aarch64", "arm64"),
}
TIMEOUT: int = 30
CACHE_DIR: Path = PROJECT_BASE / ".cache"
VERSION_FILE_NAME: str = "version.json"


def configure_token() -> None:
    """配置 GitHub Token，输出检测结果"""
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        print(Console.ok(t("inf_github_token_configured")))
    else:
        print(Console.warn(t("wrn_github_token_not_configured")))
        print(Console.info(t("inf_github_token_hint")))
    print("-" * 40)


def run_command(
    cmd: list[str] | str, cwd: Path | str | None = None, shell: bool = False
) -> bool:
    """执行命令并输出日志，返回是否成功"""
    cmd_str = " ".join(cmd) if isinstance(cmd, list) else str(cmd)
    print(f"{Console.info(t('cmd_prefix'))} {cmd_str}")
    try:
        subprocess.check_call(cmd, cwd=cwd or PROJECT_BASE, shell=shell)
        print(Console.ok(t("inf_command_success", cmd=cmd_str)))
        return True
    except subprocess.CalledProcessError as e:
        print(Console.err(t("err_command_failed", cmd=cmd_str, error=e)))
        return False


def update_submodules(skip_if_exist: bool = True) -> bool:
    print(Console.hdr(t("inf_check_submodules")))

    # 兼容旧版本：model 可能是普通文件夹而非子模块，需要删除以确保子模块正常 clone
    model_path = PROJECT_BASE / "assets" / "resource" / "model"
    if model_path.is_dir() and not (model_path / "LICENSE").exists():
        print(Console.warn(t("wrn_model_not_submodule", path=model_path)))
        shutil.rmtree(model_path)
        print(Console.ok(t("inf_model_dir_removed", path=model_path)))

    if (
        not skip_if_exist
        or not (model_path / "LICENSE").exists()
        or not (PROJECT_BASE / "agent" / "cpp-algo" / "MaaUtils" / "MaaUtils.cmake").exists()
    ):
        print(Console.info(t("inf_updating_submodules")))
        return run_command(["git", "submodule", "update", "--init", "--recursive"])
    print(Console.ok(t("inf_submodules_exist")))
    return True


def bootstrap_maadeps(skip_if_exist: bool = True) -> bool:
    """下载 MaaDeps 预编译依赖"""
    maadeps_dir = (
        PROJECT_BASE / "agent" / "cpp-algo" / "MaaUtils" / "MaaDeps" / "vcpkg" / "installed"
    )
    if skip_if_exist and maadeps_dir.exists() and any(maadeps_dir.iterdir()):
        print(Console.ok(t("inf_maadeps_exist")))
        return True

    print(Console.info(t("inf_bootstrap_maadeps")))
    script_path = PROJECT_BASE / "tools" / "maadeps-download.py"
    return run_command([sys.executable, str(script_path)])


_dep_3rdparty_inited = False


def bootstrap_3rdparty(update: bool = False) -> bool:
    """委托给 tools/dep_3rdparty.py，统一拉取 3rdparty 二进制 SDK（目前仅 WebView2）。

    直接 in-process 调用，跳过情形下不再启动 Python 子进程；当依赖已经齐备时只产出
    一行日志，体感上对齐 maafw/mxu 那条路径。具体下载逻辑、缓存策略、平台判断仍在
    dep_3rdparty.py 内部，本函数只做编排。
    """
    global _dep_3rdparty_inited
    try:
        if not _dep_3rdparty_inited:
            # 常规 import 不会触发 dep_3rdparty 自身的 `if __name__ == '__main__'` 引导，
            # 需要手动给它初始化一次 locale，否则 t() 拿到的还是 raw key。
            dep_3rdparty.init_local()
            _dep_3rdparty_inited = True
        return dep_3rdparty.download_all(skip_if_exist=not update)
    except Exception as exc:
        traceback.print_exc()
        print(
            Console.err(
                t(
                    "err_bootstrap_3rdparty_failed",
                    exc_type=type(exc).__name__,
                    error=exc,
                )
            )
        )
        return False


def run_build_script() -> bool:
    print(Console.hdr(t("inf_run_build_script")))
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
        print(Console.info(t("inf_get_latest_release", repo=repo)))

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
                    print(Console.ok(t("inf_matched_asset", name=asset["name"])))
                    tag_name = tag.get("tag_name") or tag.get("name")
                    return asset["browser_download_url"], asset["name"], tag_name

        raise ValueError("No matching asset found in the latest release (GitHub API)")
    except Exception as e:
        print(Console.err(t("err_get_release_failed", error_type=type(e).__name__, error=e)))

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
        print(Console.warn(t("wrn_read_version_failed", error=e)))
    return {}


def write_versions_file(path: Path, versions: dict[str, str]) -> None:
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"versions": versions}, f, ensure_ascii=False, indent=4)
        print(Console.ok(t("inf_write_version_file", path=path)))
        print(Console.info(t("inf_current_versions", versions=versions)))
    except Exception as e:
        print(Console.warn(t("wrn_write_version_failed", error=e)))


def _retry_on_permission(operation, *, error_key: str = "", **fmt_args) -> bool:
    """执行 operation()，遇 PermissionError 提示重试/退出。

    Returns True 表示成功，False 表示用户选择退出。
    非 PermissionError 异常直接上抛。
    """
    while True:
        try:
            operation()
            return True
        except PermissionError as e:
            print(Console.err(t("err_permission_denied", error=e)))
            if error_key:
                print(Console.err(t(error_key, **fmt_args)))
            cmd = input(t("prompt_retry_or_quit")).strip().lower()
            if cmd == "q":
                return False


def _update_component_version(
    install_root: Path,
    component_key: str,
    version: str,
) -> None:
    """更新 version.json 中单个组件的版本号"""
    if not version:
        return
    version_file = install_root / VERSION_FILE_NAME
    versions = read_versions_file(version_file)
    versions[component_key] = version
    write_versions_file(version_file, versions)


def parse_semver(version: str) -> tuple[list[int], list[str]]:
    """Parse a semver string into (core_numbers, prerelease_identifiers).

    Implements SemVer 2.0.0 precedence essentials used by compare_semver:
    - Ignore leading 'v'/'V'
    - Ignore build metadata (+...)
    - Compare core version as numeric dot-separated identifiers
    - Handle prerelease precedence (alpha/beta/rc, numeric identifiers, etc.)
    """
    if not version:
        return [], []

    v = version.strip()
    if v.startswith(("v", "V")):
        v = v[1:]

    # Drop build metadata for precedence comparison.
    if "+" in v:
        v = v.split("+", 1)[0]

    # Split core and prerelease.
    core_part, pre_part = (v.split("-", 1) + [""])[:2] if "-" in v else (v, "")

    def parse_core_number(part: str) -> int:
        num = ""
        for ch in part:
            if ch.isdigit():
                num += ch
            else:
                break
        return int(num) if num else 0

    core_numbers = [parse_core_number(p) for p in core_part.split(".") if p != ""]
    prerelease = [p for p in pre_part.split(".") if p != ""] if pre_part else []
    return core_numbers, prerelease


def compare_semver(a: str | None, b: str | None) -> int:
    if not a and not b:
        return 0
    if a and not b:
        return 1
    if b and not a:
        return -1

    left_core, left_pre = parse_semver(a or "")
    right_core, right_pre = parse_semver(b or "")

    # Compare major.minor.patch (or longer) numerically.
    max_len = max(len(left_core), len(right_core))
    left_core += [0] * (max_len - len(left_core))
    right_core += [0] * (max_len - len(right_core))
    for l, r in zip(left_core, right_core):
        if l > r:
            return 1
        if l < r:
            return -1

    # Core equal: version without prerelease has higher precedence.
    if not left_pre and not right_pre:
        return 0
    if not left_pre and right_pre:
        return 1
    if left_pre and not right_pre:
        return -1

    # Both prerelease: compare dot-separated identifiers.
    def is_numeric_identifier(s: str) -> bool:
        return s.isdigit()

    for l, r in zip(left_pre, right_pre):
        l_num = is_numeric_identifier(l)
        r_num = is_numeric_identifier(r)

        if l_num and r_num:
            li, ri = int(l), int(r)
            if li > ri:
                return 1
            if li < ri:
                return -1
            continue

        if l_num and not r_num:
            return -1  # numeric < non-numeric
        if not l_num and r_num:
            return 1

        # both non-numeric: ASCII lexical compare
        if l > r:
            return 1
        if l < r:
            return -1

    # All shared identifiers equal: shorter prerelease has lower precedence.
    if len(left_pre) > len(right_pre):
        return 1
    if len(left_pre) < len(right_pre):
        return -1
    return 0


def ensure_cache_dir() -> Path:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    return CACHE_DIR


def cleanup_cache_file(path: Path) -> None:
    try:
        if path.exists():
            path.unlink()
            print(Console.ok(t("inf_cache_cleaned", path=path)))
        meta = Path(str(path) + ".url")
        if meta.exists():
            meta.unlink()
    except OSError as e:
        print(Console.warn(t("wrn_cache_clean_failed", path=path, error=e)))


def clean_cache() -> None:
    if not CACHE_DIR.exists():
        print(Console.info(t("inf_cache_empty")))
        return
    total_size = 0
    count = 0
    for f in CACHE_DIR.iterdir():
        if f.is_file():
            total_size += f.stat().st_size
            count += 1
    if count == 0:
        print(Console.info(t("inf_cache_empty")))
        return
    size_mb = total_size / (1024 * 1024)
    print(Console.info(t("inf_cache_summary", count=count, size=f"{size_mb:.1f} MB")))
    try:
        shutil.rmtree(CACHE_DIR)
        print(Console.ok(t("inf_cache_purged")))
    except OSError as e:
        print(Console.warn(t("wrn_cache_clean_failed", path=CACHE_DIR, error=e)))


def download_file(
    url: str,
    dest_path: Path,
    resume: bool = False,
    extra_headers: dict[str, str] | None = None,
) -> bool:
    """下载文件到指定路径。extra_headers 仅在初始请求携带，不跟随重定向。"""

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
        print(Console.info(t("inf_start_download", url=url)))

        url_meta = Path(str(dest_path) + ".url")

        if resume and dest_path.exists() and dest_path.stat().st_size > 0:
            if url_meta.exists():
                try:
                    cached_url = url_meta.read_text(encoding="utf-8").strip()
                except OSError:
                    cached_url = ""
                if cached_url and cached_url != url:
                    print(Console.warn(t("wrn_cache_url_mismatch")))
                    cleanup_cache_file(dest_path)
                    if dest_path.exists():
                        resume = False

        existing_size = 0
        if resume and dest_path.exists():
            existing_size = dest_path.stat().st_size
            if existing_size > 0:
                print(Console.info(t("inf_resume_detected", size=to_file_size(existing_size))))

        req = urllib.request.Request(url)
        req.add_header("User-Agent", "MaaEnd-setup")
        if extra_headers:
            for k, v in extra_headers.items():
                req.add_header(k, v)
        if existing_size > 0:
            req.add_header("Range", f"bytes={existing_size}-")

        print(Console.info(t("inf_connecting")), end="", flush=True)
        try:
            res = urllib.request.urlopen(req, timeout=TIMEOUT)
        except urllib.error.HTTPError as he:
            if he.code == 416 and existing_size > 0:
                # 416 means cached file is already complete — no need to re-download
                print()
                print(Console.ok(t("inf_cache_file_complete", path=dest_path)))
                return True
            raise

        with res:
            status_code = res.getcode()
            if status_code == 206:
                content_range = res.headers.get("Content-Range", "")
                size_total = 0
                if "/" in content_range:
                    total_str = content_range.rsplit("/", 1)[-1].strip()
                    if total_str != "*":
                        try:
                            size_total = int(total_str)
                        except (ValueError, TypeError):
                            size_total = 0
                file_mode = "ab"
                size_received = existing_size
                print(Console.info(
                    t("inf_resuming_download",
                      downloaded=to_file_size(existing_size),
                      total=to_file_size(size_total))
                ))
            else:
                size_total = int(res.headers.get("Content-Length", 0) or 0)
                file_mode = "wb"
                size_received = 0
                if existing_size > 0:
                    print(Console.warn(t("wrn_resume_not_supported")))

            session_received = 0
            cached_progress_str = ""
            start_ts = time.time()

            with open(dest_path, file_mode) as out_file:
                while True:
                    chunk = res.read(8192)
                    if not chunk:
                        break
                    out_file.write(chunk)
                    size_received += len(chunk)
                    session_received += len(chunk)

                    elapsed = max(1e-6, time.time() - start_ts)
                    speed = session_received / elapsed
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
                            f"\r{Console.info(t('inf_downloading', progress=progress_str))}",
                            end="",
                            flush=True,
                        )
                        cached_progress_str = progress_str
        print()
        print(Console.ok(t("inf_download_complete", path=dest_path)))
        try:
            url_meta.write_text(url, encoding="utf-8")
        except OSError:
            pass
        return True
    except urllib.error.HTTPError as e:
        print(Console.err(t("err_network_error_with_code", reason=e.reason, code=e.code)))
    except urllib.error.URLError as e:
        print(Console.err(t("err_network_error", reason=e.reason)))
    except Exception as e:
        print(Console.err(t("err_download_failed", error_type=type(e).__name__, error=e)))
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
    maafw_deps = PROJECT_BASE / "deps"
    maafw_installed = maafw_deps.exists() and any(maafw_deps.iterdir())

    if skip_if_exist and maafw_installed:
        print(Console.ok(t("inf_maafw_installed_skip")))
        return True, local_version, False

    url, filename, remote_version = get_latest_release_url(
        MFW_REPO, ["maa", OS_KEYWORD, ARCH_KEYWORD]
    )
    if not url or not filename:
        print(Console.err(t("err_maafw_url_not_found")))
        return False, local_version, False

    if (
        update_mode
        and maafw_installed
        and local_version
        and remote_version
        and compare_semver(local_version, remote_version) >= 0
    ):
        print(Console.ok(t("inf_maafw_latest_version", version=local_version)))
        return True, local_version, False

    cache_dir = ensure_cache_dir()
    download_path = cache_dir / filename
    if not download_file(url, download_path, resume=True):
        return False, local_version, False

    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)

        maafw_dest_is_link = maafw_dest.is_symlink()
        if hasattr(maafw_dest, 'is_junction'):
            maafw_dest_is_link = maafw_dest_is_link or maafw_dest.is_junction()

        if maafw_dest_is_link:
            print(Console.ok(t("inf_link_already_exists", path=maafw_dest)))
        elif maafw_dest.exists():
            if maafw_dest.is_dir():
                def _delete_maafw_dest():
                    print(Console.info(t("inf_delete_old_dir", path=maafw_dest)))
                    shutil.rmtree(maafw_dest)
                try:
                    if not _retry_on_permission(_delete_maafw_dest, error_key="err_cannot_delete_maafw", path=maafw_dest):
                        return False, local_version, False
                except Exception as e:
                    print(Console.err(t("err_unknown_error_delete", error=e)))
                    return False, local_version, False
            else:
                maafw_dest.unlink(missing_ok=True)

        print(Console.info(t("inf_extract_maafw")))
        try:
            extract_root = tmp_path / "extracted"
            extract_root.mkdir(parents=True, exist_ok=True)

            # 使用 shutil.unpack_archive 自动识别格式进行解压
            shutil.unpack_archive(str(download_path), extract_root)

            # 找到包含 bin 目录的 SDK 根目录
            sdk_root = None
            for root, dirs, _ in os.walk(extract_root):
                if "bin" in dirs:
                    sdk_root = Path(root)
                    break

            if not sdk_root:
                print(Console.err(t("err_bin_not_found")))
                return False, local_version, False

            # 先将完整 SDK 复制到项目根目录 deps/
            print(Console.info(t("inf_copying_sdk", dest=maafw_deps)))
            def _copy_sdk():
                if maafw_deps.exists():
                    shutil.rmtree(maafw_deps)
                shutil.copytree(sdk_root, maafw_deps)
            if not _retry_on_permission(_copy_sdk, error_key="err_cannot_access_deps", path=maafw_deps):
                return False, local_version, False
            print(Console.ok(t("inf_sdk_copied", dest=maafw_deps)))

            if not maafw_dest_is_link:
                # 创建 install/maafw -> deps/bin 的目录链接
                bin_path = maafw_deps / "bin"
                print(Console.info(t("inf_creating_link", link=maafw_dest, target=bin_path)))
                if not create_directory_link(bin_path, maafw_dest):
                    print(Console.err(t("err_create_link_failed")))
                    return False, local_version, False

            print(Console.ok(t("inf_maafw_install_complete")))
            cleanup_cache_file(download_path)
            version_to_write = remote_version or local_version
            if version_to_write:
                _update_component_version(install_root, "maafw", version_to_write)
            return True, version_to_write, True
        except Exception as e:
            print(Console.err(t("err_maafw_install_failed", error=e)))
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
        print(Console.ok(t("inf_mxu_installed_skip")))
        return True, local_version, False

    url, filename, remote_version = get_latest_release_url(
        MXU_REPO, ["mxu", OS_KEYWORD, ARCH_KEYWORD]
    )
    if not url or not filename:
        print(Console.err(t("err_mxu_url_not_found")))
        return False, local_version, False

    if (
        update_mode
        and mxu_installed
        and local_version
        and remote_version
        and compare_semver(local_version, remote_version) >= 0
    ):
        print(Console.ok(t("inf_mxu_latest_version", version=local_version)))
        return True, local_version, False

    cache_dir = ensure_cache_dir()
    download_path = cache_dir / filename
    if not download_file(url, download_path, resume=True):
        return False, local_version, False

    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)

        if mxu_path.exists():
            def _delete_mxu():
                print(Console.info(t("inf_delete_old_file", path=mxu_path)))
                mxu_path.unlink()
            try:
                if not _retry_on_permission(_delete_mxu, error_key="err_cannot_delete_mxu", name=MXU_DIST_NAME):
                    return False, local_version, False
            except Exception as e:
                print(Console.err(t("err_unknown_error_delete_file", error=e)))
                return False, local_version, False

        print(Console.info(t("inf_extract_install_mxu")))
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
                    print(Console.ok(t("inf_updated_file", name=item.name)))
                    if item.name.lower() == MXU_DIST_NAME.lower():
                        copied = True

            if not copied:
                print(Console.err(t("err_mxu_not_found", name=MXU_DIST_NAME)))
                return False, local_version, False
            print(Console.ok(t("inf_mxu_install_complete")))
            cleanup_cache_file(download_path)
            version_to_write = remote_version or local_version
            if version_to_write:
                _update_component_version(install_root, "mxu", version_to_write)
            return True, version_to_write, True
        except Exception as e:
            print(Console.err(t("err_mxu_install_failed", error=e)))
            return False, local_version, False


def find_cpp_algo_binary(search_root: Path) -> Path | None:
    preferred_names = (
        ["cpp-algo.exe", "cpp-algo"] if OS_KEYWORD == "win" else ["cpp-algo", "cpp-algo.exe"]
    )
    candidates: list[Path] = []
    for name in preferred_names:
        candidates.extend(path for path in search_root.rglob(name) if path.is_file())

    if not candidates:
        return None

    def _arch_rank(path_parts: list[str]) -> int:
        joined_path = "/".join(path_parts)
        preferred_hints = set(ARCH_VARIANT_HINTS.get(ARCH_KEYWORD, ()))
        all_hints = {hint for hints in ARCH_VARIANT_HINTS.values() for hint in hints}
        has_preferred_arch = any(hint in joined_path for hint in preferred_hints)
        has_other_arch = any(hint in joined_path for hint in (all_hints - preferred_hints))
        if has_preferred_arch:
            return 0
        if has_other_arch:
            return 2
        return 1

    def _score(path: Path) -> tuple[int, int, int, int]:
        path_parts = [part.lower() for part in path.parts]
        in_agent_dir = "agent" in path_parts
        agent_dir_rank = 0 if in_agent_dir else 1
        preferred_name_rank = 0 if path.name.lower() == preferred_names[0] else 1
        return agent_dir_rank, preferred_name_rank, _arch_rank(path_parts), len(path_parts)

    candidates.sort(key=_score)
    return candidates[0]


def _replace_file_with_retry(src_path: Path, target_path: Path) -> None:
    tmp_target = target_path.with_name(f".{target_path.name}.tmp")
    def _do_replace():
        tmp_target.unlink(missing_ok=True)
        shutil.copy2(src_path, tmp_target)
        os.replace(tmp_target, target_path)
    try:
        if not _retry_on_permission(_do_replace):
            raise PermissionError(t("err_user_declined_replace", path=target_path))
    finally:
        tmp_target.unlink(missing_ok=True)


def _is_supported_archive(path: Path) -> bool:
    lower_name = path.name.lower()
    for _, extensions, _ in shutil.get_unpack_formats():
        if any(lower_name.endswith(ext.lower()) for ext in extensions):
            return True
    return False


def copy_cpp_algo_binary(src_path: Path, install_root: Path) -> None:
    agent_dir = install_root / "agent"
    agent_dir.mkdir(parents=True, exist_ok=True)

    target_path = agent_dir / CPP_ALGO_DIST_NAME
    _replace_file_with_retry(src_path, target_path)
    print(Console.ok(t("inf_updated_file", name=target_path.name)))

    if OS_KEYWORD != "win":
        target_path.chmod(target_path.stat().st_mode | 0o111)

    pdb_src = src_path.with_suffix(".pdb")
    if pdb_src.exists():
        pdb_target = agent_dir / f"{Path(CPP_ALGO_DIST_NAME).stem}.pdb"
        _replace_file_with_retry(pdb_src, pdb_target)
        print(Console.ok(t("inf_updated_file", name=pdb_target.name)))

    for companion_name in CPP_ALGO_COMPANION_FILES:
        companion_src = src_path.parent / companion_name
        if not companion_src.exists():
            print(Console.warn(t("wrn_cpp_algo_companion_missing", name=companion_name)))
            continue
        companion_target = agent_dir / companion_name
        _replace_file_with_retry(companion_src, companion_target)
        print(Console.ok(t("inf_updated_file", name=companion_target.name)))


def _github_auth_headers() -> dict[str, str] | None:
    """Return GitHub API auth headers, or None if no token is configured."""
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN") or ""
    token = token.strip()
    if not token:
        return None
    return {
        "Authorization": f"Bearer {token}",
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    }


def _is_git_sha(version: str | None) -> bool:
    """Return True if version looks like a short git SHA (7-40 hex chars)."""
    if not version:
        return False
    v = version.strip().lower()
    return 7 <= len(v) <= 40 and all(c in "0123456789abcdef" for c in v)


def _github_api_get(url: str, auth_headers: dict[str, str]) -> dict:
    """Make an authenticated GET request to the GitHub API and return parsed JSON."""
    req = urllib.request.Request(url)
    req.add_header("User-Agent", "MaaEnd-setup")
    for k, v in auth_headers.items():
        req.add_header(k, v)
    with urllib.request.urlopen(req, timeout=TIMEOUT) as res:
        return json.loads(res.read())


def _find_cpp_algo_in_ci(
    auth_headers: dict[str, str] | None,
) -> tuple[str | None, str | None]:
    """Find the latest cpp-algo artifact from successful install.yml runs on v2.

    Only considers push events (not PRs) on the v2 branch, ensuring the artifact
    comes from merged code. auth_headers must be non-None (caller should verify).
    Returns (download_url, version_sha) or (None, None).
    """
    if auth_headers is None:
        print(Console.info(t("inf_ci_artifact_no_token")))
        return None, None

    artifact_name = f"cpp-algo-{OS_KEYWORD}-{ARCH_KEYWORD}"
    print(Console.info(t("inf_ci_artifact_search", name=artifact_name)))

    runs_url = (
        f"https://api.github.com/repos/{MAAEND_REPO}/actions/workflows/"
        f"install.yml/runs?branch=v2&status=success&event=push&per_page=10"
    )

    try:
        data = _github_api_get(runs_url, auth_headers)
    except urllib.error.HTTPError as e:
        if e.code in (403, 429):
            print(Console.warn(t("wrn_ci_artifact_rate_limited", code=e.code)))
        else:
            print(Console.warn(t("wrn_ci_artifact_list_runs_failed", error=e)))
        return None, None
    except urllib.error.URLError as e:
        print(Console.warn(t("wrn_ci_artifact_network_error", error=e.reason)))
        return None, None
    except Exception as e:
        print(Console.warn(t("wrn_ci_artifact_list_runs_failed", error=e)))
        return None, None

    runs = data.get("workflow_runs", [])
    if not runs:
        print(Console.info(t("inf_ci_artifact_no_runs")))
        return None, None

    for run in runs:
        run_id = run["id"]
        head_sha = run.get("head_sha", "")
        if not head_sha:
            continue

        artifacts_url = (
            f"https://api.github.com/repos/{MAAEND_REPO}/actions/"
            f"runs/{run_id}/artifacts"
        )
        try:
            artifacts_data = _github_api_get(artifacts_url, auth_headers)
        except urllib.error.HTTPError as e:
            if e.code in (403, 429):
                print(Console.warn(t("wrn_ci_artifact_rate_limited", code=e.code)))
                break
            else:
                print(Console.warn(t("wrn_ci_artifact_list_artifacts_failed", error=e)))
                continue
        except urllib.error.URLError as e:
            print(Console.warn(t("wrn_ci_artifact_network_error", error=e.reason)))
            continue
        except Exception as e:
            print(Console.warn(t("wrn_ci_artifact_list_artifacts_failed", error=e)))
            continue

        for artifact in artifacts_data.get("artifacts", []):
            if artifact.get("name") == artifact_name and not artifact.get("expired", False):
                artifact_id = artifact["id"]
                download_url = (
                    f"https://api.github.com/repos/{MAAEND_REPO}/actions/"
                    f"artifacts/{artifact_id}/zip"
                )
                print(Console.ok(t("inf_ci_artifact_found", sha=head_sha[:7])))
                return download_url, head_sha

    print(Console.info(t("inf_ci_artifact_not_found")))
    return None, None


def install_cpp_algo(
    install_root: Path,
    skip_if_exist: bool = True,
    update_mode: bool = False,
    local_version: str | None = None,
) -> tuple[bool, str | None, bool]:
    real_install_root = install_root.resolve()
    cpp_algo_path = real_install_root / "agent" / CPP_ALGO_DIST_NAME
    cpp_algo_installed = cpp_algo_path.exists()

    if skip_if_exist and cpp_algo_installed:
        print(Console.ok(t("inf_cpp_algo_installed_skip")))
        return True, local_version, False

    # ~~~ CI artifact fast path ~~~
    # Try to grab just the cpp-algo binary from a recent successful v2 push
    # workflow run. This avoids downloading the entire MaaEnd release package.
    auth_headers = _github_auth_headers()
    ci_url, ci_version = _find_cpp_algo_in_ci(auth_headers)
    if ci_url:
        ci_should_skip = (
            update_mode
            and cpp_algo_installed
            and _is_git_sha(local_version)
            and _is_git_sha(ci_version)
            and local_version == ci_version
        )
        if ci_should_skip:
            print(Console.ok(t("inf_cpp_algo_latest_version", version=local_version)))
            return True, local_version, False

        cache_dir = ensure_cache_dir()
        ci_download_path = (
            cache_dir / f"cpp-algo-{OS_KEYWORD}-{ARCH_KEYWORD}.zip"
        )
        ci_downloaded = False

        if auth_headers is not None:
            # GitHub artifact API returns a 302 redirect to Azure blob storage.
            # urllib's default redirect handler strips Authorization on cross-origin
            # redirects, causing a 401.  Resolve the redirect with http.client
            # (which does not auto-follow redirects) and then download from the
            # storage URL with auth headers intact.
            storage_url: str | None = None
            try:
                parsed = urlparse(ci_url)
                conn = http.client.HTTPSConnection(
                    parsed.hostname, timeout=TIMEOUT,
                )
                try:
                    path = parsed.path
                    if parsed.query:
                        path += "?" + parsed.query
                    request_headers = {"User-Agent": "MaaEnd-setup"}
                    request_headers.update(auth_headers)
                    conn.request("GET", path, headers=request_headers)
                    with conn.getresponse() as api_resp:
                        if 300 <= api_resp.status < 400:
                            storage_url = api_resp.getheader("Location")
                        else:
                            body = api_resp.read(512).decode("utf-8", errors="replace")
                            print(Console.warn(
                                t("wrn_ci_artifact_unexpected_status",
                                  status=api_resp.status,
                                  reason=api_resp.reason,
                                  body=body)
                            ))
                finally:
                    conn.close()

                if storage_url is not None:
                    # The storage URL is SAS-signed — no extra auth needed.
                    ci_downloaded = download_file(
                        storage_url, ci_download_path, resume=False,
                    )
            except Exception:
                pass  # warning is printed by the elif below

        if ci_downloaded:
            with tempfile.TemporaryDirectory() as tmp_dir:
                extract_root = Path(tmp_dir) / "extracted"
                extract_root.mkdir(parents=True, exist_ok=True)
                try:
                    shutil.unpack_archive(str(ci_download_path), extract_root)
                    cpp_algo_src = find_cpp_algo_binary(extract_root)
                    if not cpp_algo_src:
                        print(Console.warn(t("wrn_ci_artifact_no_binary")))
                    else:
                        copy_cpp_algo_binary(cpp_algo_src, real_install_root)
                        print(Console.ok(t("inf_cpp_algo_install_complete")))
                        cleanup_cache_file(ci_download_path)
                        version_to_write = ci_version or local_version
                        if version_to_write:
                            _update_component_version(install_root, "cpp_algo", version_to_write)
                        return True, version_to_write, True
                except PermissionError:
                    # User declined the retry prompt — release fallback would
                    # hit the same file-in-use problem, so bail out directly.
                    cleanup_cache_file(ci_download_path)
                    return False, local_version, False
                except Exception as e:
                    print(Console.warn(t("wrn_ci_artifact_extract_failed", error=e)))
            cleanup_cache_file(ci_download_path)
        elif auth_headers is not None:
            print(Console.warn(t("wrn_ci_artifact_download_failed")))

        # Fall through to release download on any failure
        if auth_headers is not None:
            print(Console.info(t("inf_fallback_to_release")))

    # ~~~ Release fallback (original logic) ~~~
    url, filename, remote_version = get_latest_release_url(
        MAAEND_REPO, ["maaend", OS_KEYWORD, ARCH_KEYWORD]
    )
    if not url or not filename:
        print(Console.err(t("err_cpp_algo_url_not_found")))
        return False, local_version, False

    if (
        update_mode
        and cpp_algo_installed
        and local_version
        and remote_version
        and not _is_git_sha(local_version)
        and not _is_git_sha(remote_version)
        and compare_semver(local_version, remote_version) >= 0
    ):
        print(Console.ok(t("inf_cpp_algo_latest_version", version=local_version)))
        return True, local_version, False

    cache_dir = ensure_cache_dir()
    download_path = cache_dir / filename
    if not download_file(url, download_path, resume=True):
        return False, local_version, False

    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)

        print(Console.info(t("inf_extract_install_cpp_algo")))
        try:
            lower_filename = filename.lower()
            if lower_filename.endswith(".dmg"):
                if platform.system() != "Darwin":
                    print(Console.err(t("err_cpp_algo_dmg_unsupported")))
                    return False, local_version, False

                mount_dir = tmp_path / "mounted"
                mount_dir.mkdir(parents=True, exist_ok=True)
                attach_result = subprocess.run(
                    [
                        "hdiutil",
                        "attach",
                        str(download_path),
                        "-nobrowse",
                        "-readonly",
                        "-mountpoint",
                        str(mount_dir),
                    ],
                    capture_output=True,
                    text=True,
                )
                if attach_result.returncode != 0:
                    error_message = (
                        attach_result.stderr.strip()
                        or attach_result.stdout.strip()
                        or str(attach_result.returncode)
                    )
                    print(Console.err(t("err_cpp_algo_dmg_attach_failed", error=error_message)))
                    return False, local_version, False

                try:
                    cpp_algo_src = find_cpp_algo_binary(mount_dir)
                    if not cpp_algo_src:
                        print(Console.err(t("err_cpp_algo_not_found", name=CPP_ALGO_DIST_NAME)))
                        return False, local_version, False
                    copy_cpp_algo_binary(cpp_algo_src, real_install_root)
                finally:
                    detach_result = subprocess.run(
                        ["hdiutil", "detach", str(mount_dir), "-force"],
                        capture_output=True,
                        text=True,
                    )
                    if detach_result.returncode != 0:
                        error_message = (
                            detach_result.stderr.strip()
                            or detach_result.stdout.strip()
                            or str(detach_result.returncode)
                        )
                        print(Console.warn(t("wrn_cpp_algo_dmg_detach_failed", error=error_message)))
            else:
                cpp_algo_src: Path | None = None
                if _is_supported_archive(download_path):
                    extract_root = tmp_path / "extracted"
                    extract_root.mkdir(parents=True, exist_ok=True)
                    shutil.unpack_archive(str(download_path), extract_root)
                    cpp_algo_src = find_cpp_algo_binary(extract_root)
                elif download_path.is_file():
                    cpp_algo_src = download_path

                if not cpp_algo_src:
                    print(Console.err(t("err_cpp_algo_not_found", name=CPP_ALGO_DIST_NAME)))
                    return False, local_version, False
                copy_cpp_algo_binary(cpp_algo_src, real_install_root)

            print(Console.ok(t("inf_cpp_algo_install_complete")))
            cleanup_cache_file(download_path)
            version_to_write = remote_version or local_version
            if version_to_write:
                _update_component_version(install_root, "cpp_algo", version_to_write)
            return True, version_to_write, True
        except Exception as e:
            traceback.print_exc()
            error_with_type = f"{type(e).__name__}: {e}"
            print(Console.err(t("err_cpp_algo_install_failed", error=error_with_type)))
            return False, local_version, False


def _is_cn_locale() -> bool:
    """检测当前系统语言是否为简体中文"""
    import locale as _locale

    loc = _locale.getlocale()
    lang = (loc[0] or "").lower()
    return lang in ("zh_cn", "chinese (simplified)_china")


def main() -> None:
    init_local()

    if _is_cn_locale():
        print(
            Console.warn(
                "[提示] 本脚本需要访问 GitHub，若出现下载超时或连接失败，可尝试配置系统代理"
            )
        )
        print("-" * 60)

    parser = argparse.ArgumentParser(description=t("description"))
    parser.add_argument("--update", action="store_true", help=t("arg_update"))
    parser.add_argument("--clean-cache", action="store_true", help=t("arg_clean_cache"))
    args = parser.parse_args()

    if args.clean_cache:
        clean_cache()
        return

    install_dir = PROJECT_BASE / "install"
    version_file = install_dir / VERSION_FILE_NAME
    local_versions = read_versions_file(version_file)
    print(Console.hdr(t("header_workspace_init")))
    configure_token()
    if not update_submodules(skip_if_exist=not args.update):
        print(Console.err(t("fatal_submodule_failed")))
        sys.exit(1)
    print(Console.hdr(t("header_bootstrap_maadeps")))
    if not bootstrap_maadeps(skip_if_exist=True):   # 这玩意太慢了，也不常更新，没必要每次下载
        print(Console.err(t("fatal_maadeps_failed")))
        sys.exit(1)
    print(Console.hdr(t("header_bootstrap_3rdparty")))
    if not bootstrap_3rdparty(update=args.update):
        print(Console.err(t("fatal_3rdparty_failed")))
        sys.exit(1)
    print(Console.hdr(t("header_build_go")))
    if not run_build_script():
        print(Console.err(t("fatal_build_failed")))
        sys.exit(1)
    print(Console.hdr(t("header_download_deps")))
    maafw_ok, _, _ = install_maafw(
        install_dir,
        skip_if_exist=not args.update,
        update_mode=args.update,
        local_version=local_versions.get("maafw"),
    )
    if not maafw_ok:
        print(Console.err(t("fatal_maafw_failed")))
        sys.exit(1)

    mxu_ok, _, _ = install_mxu(
        install_dir,
        skip_if_exist=not args.update,
        update_mode=args.update,
        local_version=local_versions.get("mxu"),
    )
    if not mxu_ok:
        print(Console.err(t("fatal_mxu_failed")))
        sys.exit(1)

    cpp_algo_ok, _, _ = install_cpp_algo(
        install_dir,
        skip_if_exist=not args.update,
        update_mode=args.update,
        local_version=local_versions.get("cpp_algo"),
    )
    if not cpp_algo_ok:
        print(Console.err(t("fatal_cpp_algo_failed")))
        sys.exit(1)

    print(Console.ok(t("header_setup_complete")))
    print(Console.info(t("inf_workspace_ready", mxu_path=install_dir / MXU_DIST_NAME)))
    print(Console.info(t("inf_install_dir_hint", install_dir=install_dir)))

    dev_doc = (
        PROJECT_BASE / "docs/zh_cn/developers/README.md"
        if _is_cn_locale()
        else PROJECT_BASE / "docs/en_us/developers/README.md"
    )
    print(Console.info(t("inf_read_dev_doc", doc_path=dev_doc)))


if __name__ == "__main__":
    main()
