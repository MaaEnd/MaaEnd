from pathlib import Path
from zipfile import ZIP_LZMA, ZipFile, ZipInfo


DIST_DIR = Path(__file__).resolve().parent.parent / "dist"
ARCHIVE_PATH = DIST_DIR / "static_files.zip"
TEMP_ARCHIVE_PATH = DIST_DIR / ".static_files.zip.tmp"
FIXED_TIMESTAMP = (1980, 1, 1, 0, 0, 0)


def main() -> None:
    if not (DIST_DIR / "index.html").is_file():
        raise FileNotFoundError("dist/index.html does not exist; run the Vite build first")

    excluded = {ARCHIVE_PATH, TEMP_ARCHIVE_PATH}
    files = sorted(path for path in DIST_DIR.rglob("*") if path.is_file() and path not in excluded)

    TEMP_ARCHIVE_PATH.unlink(missing_ok=True)
    try:
        with ZipFile(
            TEMP_ARCHIVE_PATH,
            mode="w",
            compression=ZIP_LZMA,
        ) as archive:
            for path in files:
                entry = ZipInfo(path.relative_to(DIST_DIR).as_posix(), FIXED_TIMESTAMP)
                entry.compress_type = ZIP_LZMA
                entry.create_system = 3
                entry.external_attr = 0o100644 << 16
                archive.writestr(entry, path.read_bytes())
        TEMP_ARCHIVE_PATH.replace(ARCHIVE_PATH)
    finally:
        TEMP_ARCHIVE_PATH.unlink(missing_ok=True)

    print(f"Packed {len(files)} files into {ARCHIVE_PATH.name}")


if __name__ == "__main__":
    main()
