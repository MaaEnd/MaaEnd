import argparse
import json
import shutil
import sys
from pathlib import Path

import json5


def merge_pipeline(resource_dir: Path) -> None:
    """合并 resource_dir/pipeline 下所有 .json 文件到 nodes.json，并删除原始文件"""
    pipeline_dir = resource_dir / "pipeline"
    if not pipeline_dir.is_dir():
        print(f"  跳过: {pipeline_dir} 不存在")
        return

    merged: dict = {}

    def collect(dir_path: Path) -> None:
        for item in sorted(dir_path.iterdir()):
            if item.name == "nodes.json":
                continue
            if item.is_dir():
                collect(item)
            elif item.suffix == ".json":
                try:
                    with open(item, "r", encoding="utf-8") as f:
                        merged.update(json5.load(f))
                    print(f"  已读取: {item.relative_to(pipeline_dir)}")
                except Exception as e:
                    print(f"  读取 {item.relative_to(pipeline_dir)} 时出错: {e}", file=sys.stderr)

    collect(pipeline_dir)

    nodes_file = pipeline_dir / "nodes.json"
    with open(nodes_file, "w", encoding="utf-8") as f:
        json.dump(merged, f, ensure_ascii=False, indent=4)
    print(f"  已写入: {nodes_file} ({len(merged)} 个节点)")

    for item in sorted(pipeline_dir.iterdir(), reverse=True):
        if item.name == "nodes.json":
            continue
        if item.is_dir():
            shutil.rmtree(item)
            print(f"  已删除目录: {item.name}")
        elif item.is_file():
            item.unlink()
            print(f"  已删除文件: {item.name}")


def main():
    parser = argparse.ArgumentParser(description="合并各 resource 的 pipeline JSON 文件")
    parser.add_argument("install_dir", nargs="?", default="install", help="install 目录路径（默认: install）")
    args = parser.parse_args()

    install_dir = Path(args.install_dir)
    if not install_dir.is_dir():
        print(f"错误: 目录不存在: {install_dir}", file=sys.stderr)
        sys.exit(1)

    resource_names = ["resource", "resource_adb", "resource_wlroots"]

    for name in resource_names:
        resource_path = install_dir / name
        print(f"\n处理: {resource_path}")
        print("=" * 50)
        merge_pipeline(resource_path)
        print("=" * 50)


if __name__ == "__main__":
    main()
