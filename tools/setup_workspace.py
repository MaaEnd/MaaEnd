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

from cli_support import console, init_localization


PROJECT_BASE: Path = Path(__file__).parent.parent.resolve()
MFW_REPO: str = "MaaXYZ/MaaFramework"
MXU_REPO: str = "MistEO/MXU"

LOCALS_DIR = Path(__file__).parent / "locals" / "setup_workspace"


def hdr(text: str) -> str:
    return console.hdr(text)


def ok(text: str) -> str:
    return console.ok(text)


def warn(text: str) -> str:
    return console.warn(text)


def err(text: str) -> str:
    return console.err(text)


def info(text: str) -> str:
    return console.info(text)


_local_t = lambda key, **kwargs: key.format(**kwargs) if kwargs else key


def init_local() -> None:
    global _local_t
    t_func, load_error_path = init_localization(LOCALS_DIR)
    _local_t = t_func
    if load_error_path:
        print(err(t("error_load_locale", path=load_error_path)))


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
TIMEOUT: int = 30
VERSION_FILE_NAME: str = "version.json"


def configure_token() -> None:
    """配置 GitHub Token，输出检测结果"""
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        print(ok(t("inf_github_token_configured")))
    else:
        print(warn(t("wrn_github_token_not_configured")))
        print(t("inf_github_token_hint"))
    print("-" * 40)


def run_command(
    cmd: list[str] | str, cwd: Path | str | None = None, shell: bool = False
) -> bool:
    """执行命令并输出日志，返回是否成功"""
    cmd_str = " ".join(cmd) if isinstance(cmd, list) else str(cmd)
    print(f"{info(t('cmd_prefix'))} {cmd_str}")
    try:
        subprocess.check_call(cmd, cwd=cwd or PROJECT_BASE, shell=shell)
        print(ok(t("inf_command_success", cmd=cmd_str)))
        return True
    except subprocess.CalledProcessError as e:
        print(err(t("err_command_failed", cmd=cmd_str, error=e)))
        return False


def update_submodules(skip_if_exist: bool = True) -> bool:
    print(hdr(t("inf_check_submodules")))
    if (
        not skip_if_exist
        or not (PROJECT_BASE / "assets" / "MaaCommonAssets" / "LICENSE").exists()
    ):
        print(info(t("inf_updating_submodules")))
        return run_command(["git", "submodule", "update", "--init", "--recursive"])
    print(ok(t("inf_submodules_exist")))
    return True


def run_build_script() -> bool:
    print(hdr(t("inf_run_build_script")))
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
        print(info(t("inf_get_latest_release", repo=repo)))

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
                    print(ok(t("inf_matched_asset", name=asset["name"])))
                    tag_name = tag.get("tag_name") or tag.get("name")
                    return asset["browser_download_url"], asset["name"], tag_name

        raise ValueError("No matching asset found in the latest release (GitHub API)")
    except Exception as e:
        print(err(t("err_get_release_failed", error_type=type(e).__name__, error=e)))

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
        print(warn(t("wrn_read_version_failed", error=e)))
    return {}


def write_versions_file(path: Path, versions: dict[str, str]) -> None:
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"versions": versions}, f, ensure_ascii=False, indent=4)
        print(ok(t("inf_write_version_file", path=path)))
        print(info(t("inf_current_versions", versions=versions)))
    except Exception as e:
        print(warn(t("wrn_write_version_failed", error=e)))


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
        print(info(t("inf_start_download", url=url)))
        print(info(t("inf_connecting")), end="", flush=True)
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
                    print(
                        f"\r{info(t('inf_downloading', progress=progress_str))}",
                        end="",
                        flush=True,
                    )
                    cached_progress_str = progress_str
            print()
        print(ok(t("inf_download_complete", path=dest_path)))
        return True
    except urllib.error.URLError as e:
        print(err(t("err_network_error", reason=e.reason)))
    except Exception as e:
        print(err(t("err_download_failed", error_type=type(e).__name__, error=e)))
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
        print(ok(t("inf_maafw_installed_skip")))
        return True, local_version, False

    url, filename, remote_version = get_latest_release_url(
        MFW_REPO, ["maa", OS_KEYWORD, ARCH_KEYWORD]
    )
    if not url or not filename:
        print(err(t("err_maafw_url_not_found")))
        return False, local_version, False

    if (
        update_mode
        and maafw_installed
        and local_version
        and remote_version
        and compare_semver(local_version, remote_version) >= 0
    ):
        print(ok(t("inf_maafw_latest_version", version=local_version)))
        return True, local_version, False

    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)
        download_path = tmp_path / filename
        if not download_file(url, download_path):
            return False, local_version, False

        if maafw_dest.exists():
            while True:
                try:
                    print(info(t("inf_delete_old_dir", path=maafw_dest)))
                    shutil.rmtree(maafw_dest)
                    break
                except PermissionError as e:
                    print(err(t("err_permission_denied", error=e)))
                    print(err(t("err_cannot_delete_maafw", path=maafw_dest)))
                    cmd = input(t("prompt_retry_or_quit")).strip().lower()
                    if cmd == "q":
                        return False, local_version, False
                except Exception as e:
                    print(err(t("err_unknown_error_delete", error=e)))
                    return False, local_version, False

        print(info(t("inf_extract_maafw")))
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
                    print(info(t("inf_copy_components", dest=maafw_dest)))
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
                print(err(t("err_bin_not_found")))
                return False, local_version, False
            print(ok(t("inf_maafw_install_complete")))
            return True, remote_version or local_version, True
        except Exception as e:
            print(err(t("err_maafw_install_failed", error=e)))
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
        print(ok(t("inf_mxu_installed_skip")))
        return True, local_version, False

    url, filename, remote_version = get_latest_release_url(
        MXU_REPO, ["mxu", OS_KEYWORD, ARCH_KEYWORD]
    )
    if not url or not filename:
        print(err(t("err_mxu_url_not_found")))
        return False, local_version, False

    if (
        update_mode
        and mxu_installed
        and local_version
        and remote_version
        and compare_semver(local_version, remote_version) >= 0
    ):
        print(ok(t("inf_mxu_latest_version", version=local_version)))
        return True, local_version, False

    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_path = Path(tmp_dir)
        download_path = tmp_path / filename
        if not download_file(url, download_path):
            return False, local_version, False

        if mxu_path.exists():
            while True:
                try:
                    print(info(t("inf_delete_old_file", path=mxu_path)))
                    mxu_path.unlink()
                    break
                except PermissionError as e:
                    print(err(t("err_permission_denied", error=e)))
                    print(err(t("err_cannot_delete_mxu", name=MXU_DIST_NAME)))
                    cmd = input(t("prompt_retry_or_quit")).strip().lower()
                    if cmd == "q":
                        return False, local_version, False
                except Exception as e:
                    print(err(t("err_unknown_error_delete_file", error=e)))
                    return False, local_version, False

        print(info(t("inf_extract_install_mxu")))
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
                    print(ok(t("inf_updated_file", name=item.name)))
                    if item.name.lower() == MXU_DIST_NAME.lower():
                        copied = True

            if not copied:
                print(err(t("err_mxu_not_found", name=MXU_DIST_NAME)))
                return False, local_version, False
            print(ok(t("inf_mxu_install_complete")))
            return True, remote_version or local_version, True
        except Exception as e:
            print(err(t("err_mxu_install_failed", error=e)))
            return False, local_version, False


def main() -> None:
    init_local()

    parser = argparse.ArgumentParser(description=t("description"))
    parser.add_argument("--update", action="store_true", help=t("arg_update"))
    parser.add_argument("--ci", action="store_true", help=t("arg_ci"))
    args = parser.parse_args()

    install_dir = PROJECT_BASE / "install"
    version_file = install_dir / VERSION_FILE_NAME
    local_versions = read_versions_file(version_file)
    print(hdr(t("header_workspace_init")))
    configure_token()
    if not update_submodules(skip_if_exist=not args.update):
        print(err(t("fatal_submodule_failed")))
        sys.exit(1)
    print(hdr(t("header_build_go")))
    if not run_build_script():
        print(err(t("fatal_build_failed")))
        sys.exit(1)
    print(hdr(t("header_download_deps")))
    versions: dict[str, str] = dict(local_versions)
    any_downloaded = False
    maafw_ok, maafw_version, maafw_downloaded = install_maafw(
        install_dir,
        skip_if_exist=not args.update,
        update_mode=args.update,
        local_version=local_versions.get("maafw"),
    )
    if not maafw_ok:
        print(err(t("fatal_maafw_failed")))
        sys.exit(1)
    if maafw_version:
        versions["maafw"] = maafw_version
    any_downloaded = any_downloaded or maafw_downloaded

    mxu_ok, mxu_version, mxu_downloaded = install_mxu(
        install_dir,
        skip_if_exist=not args.update,
        update_mode=args.update,
        local_version=local_versions.get("mxu"),
    )
    if not mxu_ok:
        print(err(t("fatal_mxu_failed")))
        sys.exit(1)
    if mxu_version:
        versions["mxu"] = mxu_version
    any_downloaded = any_downloaded or mxu_downloaded

    if not args.ci and any_downloaded:
        write_versions_file(version_file, versions)
    print(ok(t("header_setup_complete")))
    print(info(t("inf_workspace_ready", mxu_path=install_dir / MXU_DIST_NAME)))
    print(info(t("inf_install_dir_hint", install_dir=install_dir)))

    dev_doc = PROJECT_BASE / "docs/developers/development.md"
    print(info(t("inf_read_dev_doc", doc_path=dev_doc)))


if __name__ == "__main__":
    main()
