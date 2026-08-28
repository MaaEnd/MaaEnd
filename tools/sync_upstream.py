"""安全地把 MaaEnd 上游分支同步到当前功能分支。

日常使用：

    pnpm 同步

脚本默认同步 ``upstream/v2``，自动保护并恢复未提交修改，再同步全部子模块。
除非显式提供 ``--push``，否则结果只保留在本地。
"""

from __future__ import annotations

import argparse
import shlex
import subprocess
import sys
from pathlib import Path
from typing import Sequence


class SyncError(RuntimeError):
    """可以向用户说明并安全停止的同步错误。"""


def configure_output_encoding() -> None:
    if sys.platform != "win32":
        return
    for stream in (sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if reconfigure is not None:
            reconfigure(encoding="utf-8", errors="replace")


def format_command(arguments: Sequence[str]) -> str:
    return shlex.join(["git", *arguments])


def run_git(
    root: Path,
    arguments: Sequence[str],
    *,
    capture: bool = False,
    check: bool = True,
    announce: bool = False,
) -> subprocess.CompletedProcess[str]:
    if announce:
        print(f"  执行：{format_command(arguments)}", flush=True)

    result = subprocess.run(
        ["git", *arguments],
        cwd=root,
        capture_output=capture,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if check and result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        suffix = f"\n{detail}" if detail else ""
        raise SyncError(f"命令执行失败：{format_command(arguments)}{suffix}")
    return result


def git_output(root: Path, arguments: Sequence[str], *, check: bool = True) -> str:
    return run_git(root, arguments, capture=True, check=check).stdout.strip()


def find_repository_root() -> Path:
    result = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if result.returncode != 0:
        raise SyncError("当前目录不是 Git 仓库，请在 MaaEnd 项目目录中运行。")
    return Path(result.stdout.strip()).resolve()


def git_path(root: Path, name: str) -> Path:
    path = Path(git_output(root, ["rev-parse", "--git-path", name]))
    return path if path.is_absolute() else root / path


def ensure_no_git_operation_in_progress(root: Path) -> None:
    operation_markers = {
        "MERGE_HEAD": "合并",
        "CHERRY_PICK_HEAD": "挑选提交",
        "REVERT_HEAD": "撤销提交",
        "rebase-merge": "变基",
        "rebase-apply": "变基",
    }
    active = [label for marker, label in operation_markers.items() if git_path(root, marker).exists()]
    if active:
        names = ", ".join(sorted(set(active)))
        raise SyncError(f"仓库正在进行其他 Git 操作（{names}），请先完成或取消该操作。")


def current_branch(root: Path) -> str:
    branch = git_output(root, ["symbolic-ref", "--quiet", "--short", "HEAD"], check=False)
    if not branch:
        raise SyncError("当前没有位于具体分支上，请先切换到你的功能分支。")
    return branch


def ensure_remote_exists(root: Path, remote: str) -> None:
    if run_git(root, ["remote", "get-url", remote], capture=True, check=False).returncode != 0:
        raise SyncError(f"没有找到远程仓库“{remote}”，请先检查 Git 远程配置。")


def ensure_valid_remote_ref(root: Path, remote: str, branch: str) -> str:
    remote_ref = f"refs/remotes/{remote}/{branch}"
    if run_git(root, ["check-ref-format", remote_ref], capture=True, check=False).returncode != 0:
        raise SyncError(f"远程分支名称不合法：{remote}/{branch}")
    return remote_ref


def configured_submodule_paths(repository: Path) -> list[Path]:
    gitmodules = repository / ".gitmodules"
    if not gitmodules.is_file():
        return []

    result = run_git(
        repository,
        ["config", "--file", str(gitmodules), "--get-regexp", r"^submodule\..*\.path$"],
        capture=True,
        check=False,
    )
    if result.returncode not in (0, 1):
        detail = result.stderr.strip()
        raise SyncError(f"无法读取子模块配置 {gitmodules}：{detail}")

    paths: list[Path] = []
    for line in result.stdout.splitlines():
        fields = line.split(maxsplit=1)
        if len(fields) == 2:
            paths.append(repository / fields[1])
    return paths


def initialized_submodules(root: Path) -> list[Path]:
    discovered: list[Path] = []
    pending = configured_submodule_paths(root)
    while pending:
        submodule = pending.pop()
        if not (submodule / ".git").exists():
            continue
        discovered.append(submodule)
        pending.extend(configured_submodule_paths(submodule))
    return discovered


def describe_status(code: str) -> str:
    if code == "??":
        return "新文件（尚未纳入 Git）"

    names = {
        "A": "新增",
        "M": "修改",
        "D": "删除",
        "R": "重命名",
        "C": "复制",
        "U": "冲突",
        "T": "类型变化",
    }
    descriptions: list[str] = []
    index_status, worktree_status = code
    if index_status != " ":
        descriptions.append(f"已暂存的{names.get(index_status, index_status)}")
    if worktree_status != " ":
        descriptions.append(f"未暂存的{names.get(worktree_status, worktree_status)}")
    return "、".join(descriptions) or "状态变化"


def working_tree_changes(root: Path) -> list[tuple[str, str]]:
    result = run_git(
        root,
        [
            "status",
            "--porcelain=v1",
            "-z",
            "--untracked-files=normal",
            "--ignore-submodules=all",
        ],
        capture=True,
    )
    records = result.stdout.split("\0")
    changes: list[tuple[str, str]] = []
    index = 0
    while index < len(records):
        record = records[index]
        index += 1
        if not record:
            continue
        code = record[:2]
        path = record[3:]
        if "R" in code or "C" in code:
            if index < len(records) and records[index]:
                source = records[index]
                index += 1
                path = f"{source} → {path}"
        changes.append((describe_status(code), path))
    return changes


def print_changes(changes: Sequence[tuple[str, str]], *, indent: str = "  ") -> None:
    for description, path in changes:
        print(f"{indent}- {path}（{description}）")


def ensure_submodules_clean(root: Path) -> None:
    dirty: list[str] = []
    for submodule in initialized_submodules(root):
        status = git_output(submodule, ["status", "--porcelain", "--untracked-files=normal"])
        if status:
            dirty.append(str(submodule.relative_to(root)))
    if dirty:
        listing = "\n".join(f"  - {path}" for path in sorted(dirty))
        raise SyncError(
            "以下子模块中有未提交修改。为避免覆盖，请先在对应子模块中提交或暂存："
            f"\n{listing}"
        )


def create_stash(root: Path, branch: str) -> str:
    before = git_output(root, ["rev-parse", "--quiet", "--verify", "refs/stash"], check=False)
    message = f"sync-upstream: preserve {branch} working tree"
    run_git(
        root,
        ["stash", "push", "--include-untracked", "--message", message],
        capture=True,
    )
    after = git_output(root, ["rev-parse", "--quiet", "--verify", "refs/stash"], check=False)
    if not after or after == before:
        raise SyncError("检测到了本地修改，但未能创建安全暂存。仓库内容没有被合并。")
    return after


def restore_stash(root: Path, stash_oid: str) -> None:
    result = run_git(
        root,
        ["stash", "pop", "--index", "stash@{0}"],
        capture=True,
        check=False,
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        suffix = f"\nGit 提示：\n{detail}" if detail else ""
        raise SyncError(
            "上游代码已经合并，但恢复本地修改时发生冲突。"
            f"请手动处理冲突；安全暂存仍保留在 {stash_oid}。{suffix}"
        )


def abort_merge(root: Path) -> bool:
    if not git_path(root, "MERGE_HEAD").exists():
        return True
    return run_git(root, ["merge", "--abort"], capture=True, check=False).returncode == 0


def divergence(root: Path, remote_ref: str) -> tuple[int, int]:
    output = git_output(root, ["rev-list", "--left-right", "--count", f"HEAD...{remote_ref}"])
    try:
        local_only, upstream_only = (int(value) for value in output.split())
    except (TypeError, ValueError) as error:
        raise SyncError(f"无法理解 Git 返回的分支差异：{output!r}") from error
    return local_only, upstream_only


def print_dry_run(root: Path, remote: str, branch: str, push: bool) -> None:
    remote_ref = f"refs/remotes/{remote}/{branch}"
    print("【预览模式】不会修改仓库，也不会连接网络。")
    print(f"项目目录：{root}")
    print(f"当前分支：{current_branch(root)}")
    print(f"准备同步：{remote}/{branch}")
    if run_git(root, ["show-ref", "--verify", "--quiet", remote_ref], check=False).returncode == 0:
        local_only, upstream_only = divergence(root, remote_ref)
        print(f"上次获取的记录：本地独有 {local_only} 个提交，上游独有 {upstream_only} 个提交。")
    else:
        print(f"本地还没有 {remote}/{branch} 的记录，正式运行时会自动获取。")

    changes = working_tree_changes(root)
    if changes:
        print("正式同步时会临时保管并在完成后恢复：")
        print_changes(changes)
    else:
        print("当前没有需要临时保管的本地修改。")

    print("正式运行时将依次完成：")
    print("  1. 获取上游最新代码")
    print("  2. 临时保护本地修改")
    print("  3. 合并上游代码")
    print("  4. 恢复本地修改")
    print("  5. 更新子模块")
    print(f"  6. {'推送到自己的远程仓库' if push else '保留在本地，不自动推送'}")


def tracking_divergence(root: Path) -> tuple[str, int, int] | None:
    tracking = git_output(
        root,
        ["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"],
        check=False,
    )
    if not tracking:
        return None
    local_only, remote_only = divergence(root, tracking)
    return tracking, local_only, remote_only


def print_summary(
    root: Path,
    branch_name: str,
    remote: str,
    branch: str,
    upstream_only: int,
    saved_changes: Sequence[tuple[str, str]],
    push: bool,
) -> None:
    print()
    print("=" * 56)
    print("同步已完成")
    print("=" * 56)
    print(f"当前分支：{branch_name}")
    print(f"上游分支：{remote}/{branch}")
    if upstream_only:
        print(f"上游代码：已合并本次发现的 {upstream_only} 个新提交")
    else:
        print("上游代码：运行前已经是最新，本次无需合并新提交")
    print("你的功能：已保留在当前分支中")
    print("子模块：已同步到当前仓库指定的版本")

    if saved_changes:
        print(f"本地修改：临时保管并成功恢复了 {len(saved_changes)} 项")
        print_changes(saved_changes, indent="    ")
    else:
        print("本地修改：运行前没有未提交修改，无需临时保管")

    tracking = tracking_divergence(root)
    if push:
        print("远程仓库：已执行推送")
    elif tracking is None:
        print("远程仓库：当前分支没有设置对应的远程分支，本次未推送")
    else:
        tracking_name, local_only, remote_only = tracking
        if local_only == 0 and remote_only == 0:
            print(f"远程仓库：与 {tracking_name} 的提交已经一致，本次未推送")
        elif remote_only == 0:
            print(f"远程仓库：本地比 {tracking_name} 多 {local_only} 个提交，本次未推送")
        else:
            print(
                f"远程仓库：相对 {tracking_name}，本地多 {local_only} 个提交、少 {remote_only} 个提交；本次未推送"
            )

    print("下次日常同步：pnpm 同步")
    if not push:
        print("需要同步后推送：pnpm 同步 --push")


def synchronize(root: Path, remote: str, branch: str, push: bool) -> None:
    branch_name = current_branch(root)
    remote_ref = f"refs/remotes/{remote}/{branch}"
    stash_oid: str | None = None

    saved_changes = working_tree_changes(root)

    print(f"开始同步：{branch_name} ← {remote}/{branch}")
    print("[1/5] 正在获取上游最新代码……", flush=True)
    run_git(root, ["fetch", remote, "--prune"], capture=True)
    if run_git(root, ["show-ref", "--verify", "--quiet", remote_ref], check=False).returncode != 0:
        raise SyncError(f"获取完成后仍找不到上游分支 {remote}/{branch}。")

    local_only, upstream_only = divergence(root, remote_ref)
    print(f"      检查完成：本地独有 {local_only} 个提交，上游新增 {upstream_only} 个提交。")

    print("[2/5] 正在保护本地未提交修改……", flush=True)
    if saved_changes:
        print_changes(saved_changes, indent="      ")
        stash_oid = create_stash(root, branch_name)
        print("      本地修改已安全保管。")
    else:
        print("      没有未提交修改，跳过临时保管。")

    print("[3/5] 正在合并上游代码……", flush=True)
    merge_result = run_git(
        root,
        ["merge", "--no-edit", f"{remote}/{branch}"],
        capture=True,
        check=False,
    )
    if merge_result.returncode != 0:
        detail = (merge_result.stderr or merge_result.stdout or "").strip()
        if not abort_merge(root):
            raise SyncError(
                "上游合并失败，并且无法自动取消合并。安全暂存尚未恢复，请先查看 git status。"
            )
        if stash_oid is not None:
            restore_stash(root, stash_oid)
        suffix = f"\nGit 提示：\n{detail}" if detail else ""
        raise SyncError(f"上游合并发生冲突，已自动取消合并并恢复原来的工作区。{suffix}")

    if upstream_only:
        print(f"      已合并上游的 {upstream_only} 个新提交。")
    else:
        print("      当前已经包含全部上游代码，无需新增合并。")

    print("[4/5] 正在恢复本地修改……", flush=True)
    if stash_oid is not None:
        restore_stash(root, stash_oid)
        print(f"      已恢复 {len(saved_changes)} 项本地修改。")
    else:
        print("      没有需要恢复的本地修改。")

    print("[5/5] 正在同步子模块……", flush=True)
    run_git(root, ["submodule", "sync", "--recursive"], capture=True)
    run_git(root, ["submodule", "update", "--init", "--recursive"], capture=True)
    print("      子模块同步完成。")

    if run_git(root, ["merge-base", "--is-ancestor", remote_ref, "HEAD"], check=False).returncode != 0:
        raise SyncError(f"最终校验失败：当前分支没有完整包含 {remote}/{branch}。")

    if push:
        print("正在推送到自己的远程仓库……", flush=True)
        run_git(root, ["push"], capture=True)

    print_summary(root, branch_name, remote, branch, upstream_only, saved_changes, push)


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="安全地把 MaaEnd 上游代码同步到当前功能分支。",
    )
    parser.add_argument("--remote", default="upstream", help="上游远程名称（默认：upstream）")
    parser.add_argument("--branch", default="v2", help="上游分支名称（默认：v2）")
    parser.add_argument("--push", action="store_true", help="同步成功后推送当前分支")
    parser.add_argument("--dry-run", action="store_true", help="只预览操作，不修改仓库")
    return parser.parse_args(argv)


def main(argv: Sequence[str] | None = None) -> int:
    configure_output_encoding()
    args = parse_args(argv)
    try:
        root = find_repository_root()
        ensure_no_git_operation_in_progress(root)
        ensure_remote_exists(root, args.remote)
        ensure_valid_remote_ref(root, args.remote, args.branch)
        ensure_submodules_clean(root)
        if args.dry_run:
            print_dry_run(root, args.remote, args.branch, args.push)
        else:
            synchronize(root, args.remote, args.branch, args.push)
    except SyncError as error:
        print(f"同步未完成：{error}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("\n同步已取消。重新运行前请先查看 git status 和 git stash list。", file=sys.stderr)
        return 130
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
