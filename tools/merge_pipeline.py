import argparse
import json
import shutil
import sys
from pathlib import Path

import json5

def merge_pipeline(resource_dir: Path) -> None:
    pipeline_dir = resource_dir / "pipeline"
    if not pipeline_dir.is_dir():
        print(f"  跳过: {pipeline_dir} 不存在", flush=True)
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
                        # check.yml 已通过 maa-tools 检测跨文件重复键（conflict-task），
                        # 此处 dict.update 静默覆盖是安全的
                        merged.update(json5.load(f))
                    print(f"  已读取: {item.relative_to(pipeline_dir)}", flush=True)
                except (ValueError, OSError) as e:
                    raise RuntimeError(f"读取 {item.relative_to(pipeline_dir)} 时出错: {e}") from e

    collect(pipeline_dir)

    nodes_file = pipeline_dir / "nodes.json"
    with open(nodes_file, "w", encoding="utf-8") as f:
        json.dump(merged, f, ensure_ascii=False, indent=4, sort_keys=True)
    print(f"  已写入: {nodes_file} ({len(merged)} 个节点)", flush=True)

    # pipeline 目录仅包含 JSON pipeline 文件，可直接清理
    for item in sorted(pipeline_dir.iterdir(), reverse=True):
        if item.name == "nodes.json":
            continue
        if item.is_dir():
            shutil.rmtree(item)
            print(f"  已删除目录: {item.name}", flush=True)
        elif item.is_file():
            item.unlink()
            print(f"  已删除文件: {item.name}", flush=True)



def main():
    parser = argparse.ArgumentParser(description="合并各 resource 的 pipeline JSON 文件")
    parser.add_argument("install_dir", nargs="?", default="install", help="install 目录路径（默认: install）")
    args = parser.parse_args()

    install_dir = Path(args.install_dir)
    if not install_dir.is_dir():
        print(f"错误: 目录不存在: {install_dir}", flush=True)
        sys.exit(1)

    resource_dirs = sorted(
        [d for d in install_dir.iterdir() if d.is_dir() and d.name.startswith("resource")],
        key=lambda d: (d.name != "resource", d.name),
    )

    if not resource_dirs:
        print("错误: 未找到以 resource 开头的目录", flush=True)
        sys.exit(1)

    try:
        for resource_path in resource_dirs:
            print(f"\n处理: {resource_path}", flush=True)
            print("=" * 50, flush=True)
            merge_pipeline(resource_path)
            print("=" * 50, flush=True)
    except RuntimeError as e:
        print(f"\n错误: {e}", flush=True)
        sys.exit(1)


if __name__ == "__main__":
    main()
