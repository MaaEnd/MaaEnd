"""
Fill resource.hash and version into interface.json.

Usage (in CI):
    python -m pip install maafw
    python tools/fill_interface.py \
        --version v2.0.0 \
        --interface install/interface.json
"""

import argparse
import json
import re
import sys
from pathlib import Path


def strip_jsonc_comments(text: str) -> str:
    """Remove single-line // comments (outside strings) from JSONC text."""
    return re.sub(r"^\s*//.*$", "", text, flags=re.MULTILINE)


def load_jsonc(path: Path) -> dict:
    raw = path.read_text(encoding="utf-8")
    cleaned = strip_jsonc_comments(raw)
    return json.loads(cleaned)


def save_json(path: Path, data: dict) -> None:
    text = json.dumps(data, indent=4, ensure_ascii=False) + "\n"
    path.write_text(text, encoding="utf-8")


def log(msg: str) -> None:
    sys.stdout.buffer.write((msg + "\n").encode("utf-8"))
    sys.stdout.buffer.flush()


def compute_resource_hashes(
    interface_data: dict,
    base_dir: Path,
    maafw_lib_dir: Path | None,
) -> None:
    from maa.library import Library
    from maa.resource import Resource

    if maafw_lib_dir:
        Library.open(maafw_lib_dir)

    for res_item in interface_data.get("resource", []):
        paths = res_item.get("path", [])
        if not paths:
            continue

        resource = Resource()
        for p in paths:
            clean = re.sub(r"^\.[\\/]", "", p)
            full_path = base_dir / clean
            if not full_path.exists():
                log(f"  ERROR: resource path does not exist: {full_path}")
                sys.exit(1)
            job = resource.post_bundle(str(full_path))
            job.wait()

        h = resource.hash
        res_item["hash"] = h
        log(f"  {res_item['name']}: hash = {h}")


def main() -> None:
    parser = argparse.ArgumentParser(description="Fill interface.json with version and resource hashes")
    parser.add_argument("--version", required=True, help="Version tag to set")
    parser.add_argument("--maafw-lib-dir", help="Path to MaaFramework shared libraries (optional; uses pip-installed library if omitted)")
    parser.add_argument("--interface", required=True, help="Path to interface.json to modify")
    args = parser.parse_args()

    interface_path = Path(args.interface)
    maafw_lib_dir = Path(args.maafw_lib_dir) if args.maafw_lib_dir else None
    base_dir = interface_path.parent

    log(f"Loading {interface_path}")
    data = load_jsonc(interface_path)

    log(f"Setting version = {args.version}")
    data["version"] = args.version

    log("Computing resource hashes...")
    compute_resource_hashes(data, base_dir, maafw_lib_dir)

    log(f"Writing {interface_path}")
    save_json(interface_path, data)
    log("Done.")


if __name__ == "__main__":
    main()
