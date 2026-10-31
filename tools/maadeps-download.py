#!/usr/bin/env python3
import sys
from pathlib import Path

sub_path = Path(__file__).parent.parent / "agent" / "cpp-algo" / "MaaUtils" / "tools"
sys.path.append(str(sub_path))

# sys.path 上面才补上子模块路径，这两行 import 只能放在这里。
from maadeps_download import detect_host_triplet  # noqa: E402
from maadeps_download import main as download_main  # noqa: E402

REPO = "MaaXYZ/MaaDeps"
VERSION = "v3.0.0"

if __name__ == "__main__":
    target_triplet = sys.argv[1] if len(sys.argv) == 2 else detect_host_triplet()

    download_main(target_triplet, REPO, VERSION)
