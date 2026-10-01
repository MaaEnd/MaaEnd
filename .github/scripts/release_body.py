"""Build the GitHub Release body: download table on top, changelog below.

The table is generated from the files that are actually about to be uploaded,
so a link can never point at an asset that does not exist.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path
from urllib.parse import quote

# 下游（Mirror酱 更新说明、Android 应用内更新）按这对标记剔除下载区块，改动需同步
BLOCK_START = "<!-- downloads:start -->"
BLOCK_END = "<!-- downloads:end -->"

MIRRORCHYAN_LINK = (
    "[已有 Mirror酱 CDK？点击前往高速下载]"
    "(https://mirrorchyan.com/zh/projects?rid=MaaEnd&source=maaend-release)"
)

# 列顺序即表格里的平台顺序
PLATFORMS = [
    ("win", "Windows"),
    ("android", "Android"),
    ("macos", "macOS"),
    ("linux", "Linux"),
]
ROWS = [
    ("x86_64", "x86-64 (64-bit)"),
    ("aarch64", "AArch64 (ARM64)"),
]

# Android 的 ABI 名落到哪一行；universal 两行都放
ANDROID_ROW = {"x86_64": "x86_64", "arm64-v8a": "aarch64"}
ANDROID_UNIVERSAL = "universal"

LINK_TEXT = {"win": "ZIP", "macos": "DMG", "linux": "tar.gz"}
MACOS_NOTE = {"x86_64": "Intel", "aarch64": "Apple Silicon"}


def collect(artifacts: Path, tag: str):
    """Return ({(platform, arch): filename}, [unrecognized filenames])."""
    pattern = re.compile(
        rf"^MaaEnd-(?P<platform>[a-z]+)-(?P<arch>.+)-{re.escape(tag)}"
        r"\.(?:zip|dmg|tar\.gz|apk)$"
    )
    platforms = {key for key, _ in PLATFORMS}
    rows = {key for key, _ in ROWS}

    found: dict[tuple[str, str], str] = {}
    unknown: list[str] = []
    for path in sorted(artifacts.iterdir()):
        if not path.is_file():
            continue
        match = pattern.match(path.name)
        platform = match["platform"] if match else None
        arch = match["arch"] if match else None
        if platform == "android":
            known = arch == ANDROID_UNIVERSAL or arch in ANDROID_ROW
        else:
            known = platform in platforms and arch in rows
        if known:
            found[(platform, arch)] = path.name
        else:
            unknown.append(path.name)
    return found, unknown


def build_table(found: dict[tuple[str, str], str], unknown: list[str], base_url: str) -> str:
    def link(text: str, name: str) -> str:
        return f"[{text}]({base_url}/{quote(name)})"

    def cell(platform: str, row: str) -> str:
        if platform == "android":
            parts = []
            if universal := found.get(("android", ANDROID_UNIVERSAL)):
                parts.append(f"{link('**Universal**', universal)}（通用）")
            for abi, abi_row in ANDROID_ROW.items():
                if abi_row == row and (name := found.get(("android", abi))):
                    parts.append(link(abi, name))
            return " · ".join(parts)

        name = found.get((platform, row))
        if not name:
            return ""
        text = link(LINK_TEXT[platform], name)
        if platform == "macos":
            text += f" ({MACOS_NOTE[row]})"
        return text

    # 整个平台都没有产物时不留空列
    columns = [(key, title) for key, title in PLATFORMS if any(k[0] == key for k in found)]

    lines = []
    if columns:
        lines.append("| 架构 | " + " | ".join(title for _, title in columns) + " |")
        lines.append("| --- |" + " --- |" * len(columns))
        for row, row_title in ROWS:
            cells = " | ".join(cell(key, row) for key, _ in columns)
            lines.append(f"| {row_title} | {cells} |")
    if ("android", ANDROID_UNIVERSAL) in found:
        lines += ["", "> Android 推荐下载 Universal 包，无需区分架构。"]
    if unknown:
        lines += ["", "其他文件：" + " · ".join(link(name, name) for name in unknown)]
    lines += ["", MIRRORCHYAN_LINK]
    return "\n".join(lines).strip()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifacts", type=Path, required=True, help="directory holding the release files")
    parser.add_argument("--tag", required=True, help="release tag, e.g. v2.31.0")
    parser.add_argument("--repo", required=True, help="owner/repo")
    parser.add_argument("--changelog", type=Path, required=True, help="changelog markdown file")
    parser.add_argument("--output", type=Path, required=True, help="release body file to write")
    args = parser.parse_args()

    found, unknown = collect(args.artifacts, args.tag)
    for name in unknown:
        # 不让未登记的产物卡住发版：照常发布，但在表格下方列出并提醒补表
        print(f"::warning::{name} 不在下载表的平台/架构定义里，已列入「其他文件」")
    if not found:
        print(f"::warning::{args.artifacts} 下没有可识别的发布文件，下载表为空")

    base_url = f"https://github.com/{args.repo}/releases/download/{quote(args.tag)}"
    table = build_table(found, unknown, base_url)
    changelog = args.changelog.read_text(encoding="utf-8").strip()

    body = f"{BLOCK_START}\n\n{table}\n\n{BLOCK_END}\n\n{changelog}\n"
    args.output.write_text(body, encoding="utf-8", newline="\n")
    print(body)
    return 0


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    sys.exit(main())
