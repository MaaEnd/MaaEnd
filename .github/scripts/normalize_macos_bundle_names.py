"""Normalize every file and directory name in a macOS app bundle to Unicode NFD.

Why: codesign seals each resource under the exact bytes of its file name. Finder (and
other copy paths on macOS) rewrite names to NFD when a user drags MaaEnd.app out of the
DMG, so a resource sealed under an NFC name such as "Seš'qamam..." shows up as
"file added" + "file missing" after installation and the signature is broken
(Gatekeeper then hangs on "Verifying"). If every name is already NFD before signing,
that rewrite is a no-op and the seal survives the copy. File contents are untouched,
and APFS looks names up normalization-insensitively, so runtime lookups keep working.

Usage:
    normalize_macos_bundle_names.py MaaEnd.app           # rename to NFD in place
    normalize_macos_bundle_names.py --check MaaEnd.app   # fail if any name is not NFD

Exit codes: 0 ok, 1 --check found non-NFD names or a rename would collide, 2 bad input.
"""

import argparse
import os
import sys
import unicodedata
import uuid
from pathlib import Path


def non_nfd_entries(root: Path) -> list[Path]:
    """Return every path below root whose own name is not NFD, deepest first."""
    found = []
    for dirpath, dirnames, filenames in os.walk(root, topdown=False):
        for name in filenames + dirnames:
            if not unicodedata.is_normalized("NFD", name):
                found.append(Path(dirpath) / name)
    return found


def normalize(root: Path) -> list[tuple[str, str]]:
    """Rename every non-NFD entry below root to its NFD form.

    Entries are visited deepest first, so a directory is renamed only after its contents.
    Each rename goes through a temporary name: APFS treats canonically equivalent names
    as the same entry, so a direct NFC -> NFD rename can be silently ignored.
    """
    renamed = []
    for path in non_nfd_entries(root):
        target = path.with_name(unicodedata.normalize("NFD", path.name))
        siblings = {
            unicodedata.normalize("NFD", other) for other in os.listdir(path.parent) if other != path.name
        }
        if target.name in siblings:
            raise FileExistsError(f"normalizing {path} would collide with an existing entry")
        temporary = path.with_name(f".nfd-{uuid.uuid4().hex}")
        os.rename(path, temporary)
        os.rename(temporary, target)
        renamed.append((str(path.relative_to(root)), target.name))
    return renamed


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("bundle", type=Path, help="path to the .app bundle (or any directory)")
    parser.add_argument("--check", action="store_true", help="only report names that are not NFD")
    args = parser.parse_args(argv)

    if not args.bundle.is_dir():
        print(f"error: {args.bundle} is not a directory", file=sys.stderr)
        return 2

    if args.check:
        offenders = non_nfd_entries(args.bundle)
        for path in offenders:
            print(f"not NFD: {path.relative_to(args.bundle)}", file=sys.stderr)
        if offenders:
            print(f"error: {len(offenders)} name(s) are not NFD", file=sys.stderr)
            return 1
        print("all names are NFD")
        return 0

    try:
        renamed = normalize(args.bundle)
    except FileExistsError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    for old, new in renamed:
        print(f"normalized to NFD: {old} -> {new}")
    print(f"{len(renamed)} name(s) normalized")
    return 0


if __name__ == "__main__":
    sys.exit(main())
