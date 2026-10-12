"""Resolve the MaaFwApp shell version that the Android APK reports to Sentry.

The shell versions itself from `git describe --tags --always` inside its own
checkout (Android/MaaFwApp build-logic .../GitVersion.kt), and the APK puts that
string into the Sentry release as `MaaFwApp@<version>+<app name>@<app version>`.
A shallow checkout carries no tags, so describe degrades to a bare commit hash;
CI has to reproduce the gradle rules exactly, otherwise the release it creates
never matches the release the APK reports.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from collections.abc import Sequence
from pathlib import Path

# 与 GitVersion.kt 的 DESCRIBE / versionNameFromDescribe 保持一致
DESCRIBE = re.compile(
    r"v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.]+))?(?:-(\d+)-g[0-9a-f]+)?"
)
# 与 GitVersion.kt 的兜底一致
DEFAULT_VERSION = "0.0.0-dev"


def version_name_from_describe(described: str) -> str:
    """把 git describe 的输出规整成外壳版本号。"""
    match = DESCRIBE.fullmatch(described)
    if match is None:
        # 匹配不上时原样降级，一个 tag 都没有的仓库就报短哈希
        return described.removeprefix("v") or DEFAULT_VERSION

    major, minor, patch, prerelease, distance = match.groups()
    if distance is None and prerelease is None:
        return f"{major}.{minor}.{patch}"
    if distance is None:
        return f"{major}.{minor}.{patch}-{prerelease}"
    if prerelease is None:
        # 越过正式 tag：patch +1、距离作为 alpha 序号，版本仍排在正式版之前
        return f"{major}.{minor}.{int(patch) + 1}-alpha.{distance}"
    return f"{major}.{minor}.{patch}-{prerelease}.{distance}"


def describe(repository: Path) -> str:
    """在指定检出目录里执行 git describe --tags --always。"""
    result = subprocess.run(
        ["git", "-C", str(repository), "describe", "--tags", "--always"],
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout.strip()


def create_argument_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("repository", type=Path, help="MaaFwApp 检出目录")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    arguments = create_argument_parser().parse_args(argv)
    described = describe(arguments.repository)
    version = version_name_from_describe(described)
    if re.fullmatch(r"[0-9a-f]{7,}", version):
        print(
            f"::warning::{arguments.repository} 里 git describe 找不到 tag"
            f"（{described}），外壳版本退化成提交哈希，APK 上报的 release 也会是哈希",
            file=sys.stderr,
        )
    print(version)
    return 0


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    sys.exit(main())
