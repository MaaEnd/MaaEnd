"""Check that a PR title is a Conventional Commit that git-cliff can group.

Release notes come from git-cliff and .github/cliff.toml. A title whose type matches no
commit_parsers entry produces a stray group such as "### Pref"; a title that is not a
Conventional Commit at all disappears from the changelog without a trace. Instead of
re-implementing git-cliff's parser, this script feeds the title to git-cliff itself as a
synthetic commit and reads back the resulting group.

Exit codes: 0 pass (warnings allowed), 1 at least one error, 2 git-cliff failed.
"""

import argparse
import difflib
import json
import re
import subprocess
import sys
import tomllib
from dataclasses import dataclass
from pathlib import Path

# 与 orhun/git-cliff-action@v4 的默认版本一致，判定期与发版期不能漂移
GIT_CLIFF_VERSION = "2.14.2"
RELEASES_URL = f"https://github.com/orhun/git-cliff/releases/tag/v{GIT_CLIFF_VERSION}"

REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_CLIFF_CONFIG = REPO_ROOT / ".github" / "cliff.toml"

# 报错文案里的人类可读类型表，顺序与文档的表格一致；与 cliff.toml 的一致性由测试锁定
ALLOWED_TYPES = [
    ("feat", "新增功能"),
    ("fix", "修复 Bug"),
    ("refactor", "重构：调整代码结构，不改变功能"),
    ("perf", "优化现有功能（速度、准确性、容错等）"),
    ("docs", "仅文档更改"),
    ("style", "不影响代码含义的更改（空白、格式等）"),
    ("test", "新增或修改测试"),
    ("ci", "CI 工作流与配置"),
    ("chore", "日常构建过程或辅助工具的变动（不涉及生产代码）"),
    ("chore(deps)", "依赖升级"),
    ("ai", "AI 协作相关（skill、AGENTS.md、AI 工作流等）"),
    ("revert", "撤销之前的提交"),
]
BASE_TYPE_NAMES = tuple(dict.fromkeys(name.split("(")[0] for name, _ in ALLOWED_TYPES))

# cliff.toml 仍兼容这两种旧写法，检查时只提示不阻断
LEGACY_TYPES = {"rft": "refactor", "styles": "style"}

ERROR = "error"
WARNING = "warning"
SYMBOL = {ERROR: "❌", WARNING: "⚠️"}

TITLE_RE = re.compile(r"^(?P<type>[A-Za-z]+)(?:\((?P<scope>[^()\r\n]*)\))?(?P<bang>!)?:(?P<sep> *)(?P<desc>.*)$")
# 分组串带 "<!-- N -->" 排序前缀，例如 "<!-- 0 -->✨ 新功能"
GROUP_PREFIX_RE = re.compile(r"^<!--\s*\d+\s*-->\s*")
REVERT_RE = re.compile(r'^Revert "')
# 与 cliff.toml 的 skip 正则保持一致（大小写敏感）；写在标题里时先剥掉再校验格式，否则可以拿它绕过检查
SKIP_MARKER_RE = re.compile(r"\[skip changelog\]")
# 大小写写错时 git-cliff 不会跳过这条提交，单独提示
SKIP_MARKER_VARIANT_RE = re.compile(r"\[skip changelog\]", re.IGNORECASE)


class CliffError(Exception):
    """git-cliff 或 cliff.toml 无法正常使用，属于环境/配置故障，不是标题不合规。"""


@dataclass(frozen=True, slots=True)
class Problem:
    level: str
    message: str
    hint: str = ""


def load_valid_groups(cliff_config: Path) -> set[str]:
    """从 cliff.toml 的 [git].commit_parsers 收集所有 group，即更新日志的全部合法分组。"""
    data = tomllib.loads(cliff_config.read_text(encoding="utf-8"))
    parsers = data.get("git", {}).get("commit_parsers", [])
    if not isinstance(parsers, list):
        raise CliffError(f"{cliff_config} 的 [git].commit_parsers 不是数组")
    groups = {parser["group"] for parser in parsers if isinstance(parser, dict) and "group" in parser}
    if not groups:
        raise CliffError(f"{cliff_config} 的 [git].commit_parsers 里没有任何 group")
    return groups


def run_cliff(repo: Path, config: Path, message: str, git_cliff: str = "git-cliff") -> dict | None:
    """把 message 当作一条提交注入 git-cliff，返回它的上下文条目；被 skip 时返回 None。"""
    command = [
        git_cliff,
        "--repository",
        str(repo),
        "--config",
        str(config),
        "--offline",
        "--context",
        # 只渲染未发布的一段：完整仓库有 200 多个 release，不加要跑上百秒
        "--latest",
        f"--with-commit={message}",  # 等号形式，否则 "-x: ..." 这类标题会被当成参数
    ]
    try:
        done = subprocess.run(command, capture_output=True, text=True, encoding="utf-8", errors="replace", check=False)
    except FileNotFoundError:
        raise CliffError(f"未找到 git-cliff，请先安装 v{GIT_CLIFF_VERSION}：{RELEASES_URL}") from None
    if done.returncode != 0:
        detail = (done.stderr or done.stdout).strip().splitlines()
        raise CliffError(f"git-cliff 退出码 {done.returncode}：{detail[-1] if detail else '无输出'}")

    try:
        releases = json.loads(done.stdout)
    except json.JSONDecodeError as exc:
        raise CliffError(f"git-cliff 输出的不是合法 JSON：{exc}") from exc
    if not isinstance(releases, list) or not releases:
        raise CliffError("git-cliff 输出的上下文不是非空数组")

    # 合成提交的 id 是空字符串，真实提交是 40 位 SHA
    injected = [
        commit
        for release in releases
        if isinstance(release, dict)
        for commit in release.get("commits") or []
        if commit.get("id") == ""
    ]
    if len(injected) > 1:
        raise CliffError(f"预期注入 1 条提交，git-cliff 返回了 {len(injected)} 条")
    return injected[0] if injected else None


def display_group(group: str) -> str:
    """去掉分组串里的 "<!-- N -->" 排序前缀。"""
    return GROUP_PREFIX_RE.sub("", group)


def format_types() -> str:
    return " / ".join(f"`{name}`" for name in BASE_TYPE_NAMES)


def suggest_type(type_name: str) -> str:
    """按相似度给出最可能的正确类型，都不像时列出全部类型。入参须为小写类型名。"""
    close = difflib.get_close_matches(type_name, BASE_TYPE_NAMES, n=1, cutoff=0.6)
    if close:
        return f"是否想写 `{close[0]}`？可用的类型：{format_types()}"
    return f"可用的类型：{format_types()}"


def describe_format_issue(title: str) -> str:
    """标题不匹配 类型(范围): 描述 时，指出具体是哪里写错了。"""
    title = SKIP_MARKER_RE.sub("", title)
    if len(title.splitlines()) > 1:
        return "标题不能包含换行"
    if "：" in title:
        return "冒号须为半角 `:`，检测到全角 `：`"
    if re.match(r"^[A-Za-z]+ *:", title):
        return "冒号前不能有空格"
    if re.match(r"^[A-Za-z]+(?:\([^()]*\))?!? +\S", title):
        return "类型与描述之间缺少半角冒号 `:`"
    if not re.match(r"^[A-Za-z]", title):
        return "标题须以英文类型开头，例如 `feat`、`fix`"
    return "标题须为 `类型(范围): 描述`，范围可省略"


def judge(title: str, injected: dict | None, valid_groups: set[str]) -> list[Problem]:
    """判定一条标题。injected 为 None 表示 git-cliff 没收到这条提交（被 skip 掉了）。"""
    text = title.strip()
    if not text:
        return [
            Problem(ERROR, "PR 标题是空的", "请按 `类型(范围): 描述` 填写，例如 `feat(OutpostTrading): 新增自动售卖`")
        ]
    # 标记会让整条提交不进更新日志，所以剥掉后照样校验格式，否则加个标记就能绕过检查
    skip_marker = SKIP_MARKER_RE.search(text) is not None
    marker_problems: list[Problem] = []
    if skip_marker:
        text = SKIP_MARKER_RE.sub("", text).strip()
    elif SKIP_MARKER_VARIANT_RE.search(text):
        # 大小写与 cliff.toml 的正则不一致时标记不生效，别让作者以为提交被跳过了
        marker_problems.append(
            Problem(
                WARNING,
                "`[skip changelog]` 区分大小写，写成这样不会跳过这条提交，它仍会出现在更新日志里",
                "要跳过更新日志就写成全小写的 `[skip changelog]`，并写在 PR 描述里",
            )
        )
    if not text:
        return [Problem(ERROR, "PR 标题只有 `[skip changelog]` 标记，缺少实际内容", "示例：`chore: 同步资源`")]
    if injected is None:
        return [
            Problem(
                WARNING,
                "标题含 `[skip changelog]`，这个 PR 不会出现在更新日志里",
                "这个标记一般只给 bot 的自动提交用，请写在 PR 描述里而不是标题里",
            ),
        ]
    if REVERT_RE.match(text):
        return [
            Problem(
                WARNING,
                'GitHub 生成的 `Revert "..."` 标题不符合约定式提交，这个 PR 不会出现在更新日志里',
                "建议改成 `revert: 被撤销的提交标题`",
            ),
        ]

    conventional = bool(injected.get("conventional"))
    group = injected.get("group")
    match = TITLE_RE.match(text)
    problems: list[Problem] = []

    if match is None:
        message = f"标题不符合 `类型(范围): 描述` 格式：{describe_format_issue(text)}"
        if group is None and not skip_marker:
            message += "；合并后这条提交会从更新日志里消失"
        return [Problem(ERROR, message, "示例：`feat(OutpostTrading): 新增自动售卖`")]

    type_name = match.group("type")
    desc = match.group("desc").strip()
    lowered = type_name.lower()
    # commit_parsers 是前缀正则（^feat、^fix...），`feature`、`featx` 这类野类型也会命中并拿到合法分组，
    # 所以只能按类型名单判断，不能看分组是否合法
    known_type = type_name in BASE_TYPE_NAMES or type_name in LEGACY_TYPES

    if not desc:
        problems.append(Problem(ERROR, "冒号后缺少描述", "例如 `fix: 修复启动崩溃`"))
    if match.group("sep") == "" and desc:
        problems.append(Problem(WARNING, "冒号后建议空一格", f"`{type_name}: {desc}`"))
    if type_name in LEGACY_TYPES:
        problems.append(Problem(WARNING, f"`{type_name}` 是旧写法，建议改用 `{LEGACY_TYPES[type_name]}`"))
    if not known_type:
        if lowered in BASE_TYPE_NAMES or lowered in LEGACY_TYPES:
            problems.append(Problem(ERROR, f"类型须全小写，`{type_name}` 应写作 `{lowered}`", f"改成 `{lowered}` 即可"))
        else:
            problems.append(Problem(ERROR, f"类型 `{type_name}` 不在允许的类型列表里", suggest_type(lowered)))
    elif group is None:
        problems.append(
            Problem(ERROR, "无法归入任何分组，合并后这条提交会从更新日志里消失", f"可用的类型：{format_types()}")
        )
    elif group not in valid_groups:
        problems.append(Problem(ERROR, f"`{type_name}` 匹配到的分组「{display_group(group)}」不在 `cliff.toml` 里"))
    if not conventional and desc and match is not None:
        problems.append(Problem(ERROR, "git-cliff 无法把这条标题解析为约定式提交", describe_format_issue(text)))
    if skip_marker:
        problems.append(
            Problem(
                WARNING,
                "标题含 `[skip changelog]`，这个 PR 不会出现在更新日志里",
                "这个标记一般只给 bot 的自动提交用，请写在 PR 描述里而不是标题里",
            )
        )
    problems.extend(marker_problems)
    return problems


def flatten(text: str) -> str:
    """把标题里的换行压平：Actions 会把输出里以 :: 开头的行当工作流命令，标题不能带换行。"""
    return " ".join(text.splitlines())


def render(title: str, problems: list[Problem]) -> tuple[str, int]:
    """渲染 Markdown 报告与退出码。"""
    errors = [problem for problem in problems if problem.level == ERROR]
    lines = ["### ❌ PR 标题不符合约定式提交规范" if errors else "### ✅ PR 标题检查通过", ""]
    lines.append(f"**PR 标题**：`{flatten(title)}`")
    if problems:
        lines.append("")
    for problem in problems:
        lines.append(f"- {SYMBOL[problem.level]} {problem.message}")
        if problem.hint:
            lines.append(f"  - 建议：{problem.hint}")
    if errors:
        lines += [
            "",
            "**格式**：`类型(范围): 描述`，范围可省略。可用的类型见 `docs/zh_cn/developers/getting-started.md` 的「提交规范」章节。",
        ]
    return "\n".join(lines), 1 if errors else 0


def escape_command(text: str) -> str:
    """转义 GitHub Actions 工作流命令里的特殊字符。"""
    return text.replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--title", action="append", required=True, metavar="TITLE", help="PR 标题，可重复传入")
    parser.add_argument("--repo", type=Path, default=REPO_ROOT, help="git 仓库路径，默认为脚本所在的仓库")
    parser.add_argument(
        "--cliff-config", type=Path, default=None, help="cliff.toml 路径，默认 <repo>/.github/cliff.toml"
    )
    parser.add_argument("--git-cliff", default="git-cliff", metavar="PATH", help="git-cliff 可执行文件，默认取 PATH")
    parser.add_argument("--github", action="store_true", help="输出 ::error:: / ::warning:: 工作流命令")
    parser.add_argument(
        "--step-summary",
        type=Path,
        default=None,
        metavar="FILE",
        help="追加 Markdown 汇总，通常传 $GITHUB_STEP_SUMMARY",
    )
    args = parser.parse_args(argv)

    config = args.cliff_config or args.repo / ".github" / "cliff.toml"
    try:
        valid_groups = load_valid_groups(config)
    except (OSError, tomllib.TOMLDecodeError, CliffError) as exc:
        message = f"读取 {config} 失败：{exc}"
        print(f"::error::{escape_command(message)}" if args.github else f"错误：{message}", file=sys.stderr)
        return 2

    reports: list[str] = []
    exit_code = 0
    for title in args.title:
        # 带 [skip changelog] 的标题会被 git-cliff 整条丢掉，用剥掉标记的版本去问分组，格式照样校验
        probe = SKIP_MARKER_RE.sub("", title).strip() or title
        try:
            injected = run_cliff(args.repo, config, probe, args.git_cliff)
        except CliffError as exc:
            print(f"::error::{escape_command(str(exc))}" if args.github else f"错误：{exc}", file=sys.stderr)
            return 2
        problems = judge(title, injected, valid_groups)
        report, code = render(title, problems)
        exit_code = max(exit_code, code)
        reports.append(report)
        if args.github:
            for problem in problems:
                print(f"::{problem.level}::{escape_command(problem.message)}")
        print(report)

    if args.step_summary is not None and reports:
        try:
            # 多次运行共用同一个汇总文件，块之间留一个空行，避免标题紧贴上一条
            prefix = "\n" if args.step_summary.exists() and args.step_summary.stat().st_size else ""
            with args.step_summary.open("a", encoding="utf-8") as summary:
                summary.write(prefix + "\n\n".join(reports) + "\n")
        except OSError as exc:
            # 汇总只是附带信息，写不进去不该改变判定结果
            message = f"写入 {args.step_summary} 失败：{exc}"
            print(f"::warning::{escape_command(message)}" if args.github else f"警告：{message}", file=sys.stderr)
    return exit_code


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    sys.exit(main())
