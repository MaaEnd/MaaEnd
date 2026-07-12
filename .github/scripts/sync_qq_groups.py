from __future__ import annotations

import json
import re
import sys
import urllib.request
from pathlib import Path

INDEX_URL = "https://end.maafw.com/index.json"
REPO_ROOT = Path(__file__).resolve().parents[2]

# Current invite link in any of the files, regardless of its actual value.
LINK = r"https://qm\.qq\.com/q/[^)\s]*"


def fetch_groups():
    """Return (user_num, user_link, dev_num, dev_link) or None to skip."""
    try:
        with urllib.request.urlopen(INDEX_URL, timeout=30) as resp:
            data = json.load(resp)
    except Exception as exc:  # network error, bad JSON, ...
        print(f"Failed to fetch {INDEX_URL}: {exc}")
        return None

    groups = data.get("qq_groups", {})
    user = groups.get("user", {})
    dev = groups.get("dev", {})
    values = (
        str(user.get("number", "")),
        str(user.get("link", "")),
        str(dev.get("number", "")),
        str(dev.get("link", "")),
    )
    if not all(values):
        print("Missing fields in index.json, skipping")
        return None
    return values


def build_rules(user_num, user_link, dev_num, dev_link):
    """Map each target file to its (compiled regex, replacement fn) rules."""

    def md(label, num, link):
        # **<label>**: [<num>](<link>)  -- README / troubleshooting markdown
        return (
            re.compile(rf"(\*\*{label}\*\*: )\[\d+\]\({LINK}\)"),
            lambda m, num=num, link=link: f"{m.group(1)}[{num}]({link})",
        )

    def inline_num(prefix, num):
        # <prefix> (<num>)  -- e.g. "QQ 群文件 (1097256935)"
        return (
            re.compile(rf"({re.escape(prefix)} \()\d+(\))"),
            lambda m, num=num: f"{m.group(1)}{num}{m.group(2)}",
        )

    def yaml_entry(label, num, link):
        # name: ... <label> (<num>)\n  url: <link>  -- issue template config
        return (
            re.compile(rf"({re.escape(label)} \()\d+(\)[^\n]*\n\s*url: ){LINK}"),
            lambda m, num=num, link=link: f"{m.group(1)}{num}{m.group(2)}{link}",
        )

    return {
        "README.md": [
            md("用户 QQ 群", user_num, user_link),
            md("开发 QQ 群", dev_num, dev_link),
        ],
        "README.en.md": [
            md("User QQ Group", user_num, user_link),
            md("Developer QQ Group", dev_num, dev_link),
        ],
        "docs/zh_cn/users/troubleshooting.md": [
            inline_num("QQ 群文件", user_num),
            md("用户 QQ 群", user_num, user_link),
            md("开发 QQ 群", dev_num, dev_link),
        ],
        "docs/en_us/users/troubleshooting.md": [
            inline_num("QQ Group File", user_num),
            md("User QQ Group", user_num, user_link),
            md("Developer QQ Group", dev_num, dev_link),
        ],
        ".github/ISSUE_TEMPLATE/config.yml": [
            yaml_entry("User QQ Group", user_num, user_link),
            yaml_entry("Dev QQ Group", dev_num, dev_link),
        ],
        ".github/ISSUE_TEMPLATE/other_issue.yml": [
            # [QQ 群 (<num>)](<link>)  -- only the user group is referenced here
            (
                re.compile(rf"(\[QQ 群 \()\d+(\)\]\(){LINK}(\))"),
                lambda m: f"{m.group(1)}{user_num}{m.group(2)}{user_link}{m.group(3)}",
            ),
        ],
    }


def apply_rules(rules):
    """Apply replacements in place. Returns True if any warning was emitted."""
    had_warning = False
    for rel_path, replacements in rules.items():
        path = REPO_ROOT / rel_path
        try:
            text = original = path.read_text(encoding="utf-8")
        except FileNotFoundError:
            print(f"::warning::{rel_path} not found, skipping")
            had_warning = True
            continue

        for pattern, repl in replacements:
            text, count = pattern.subn(repl, text)
            if count == 0:
                print(f"::warning::No match for /{pattern.pattern}/ in {rel_path}")
                had_warning = True

        if text != original:
            path.write_text(text, encoding="utf-8")
            print(f"Updated {rel_path}")
        else:
            print(f"No changes in {rel_path}")
    return had_warning


def main():
    groups = fetch_groups()
    if groups is None:
        return 0
    apply_rules(build_rules(*groups))
    return 0


if __name__ == "__main__":
    sys.exit(main())
