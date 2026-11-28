"""Tests for check_pr_title.py.

The judge() cases need no git-cliff binary; the others run the real tool on a scratch
repository so the check can never drift from what the release pipeline does.
"""

import contextlib
import io
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_pr_title as checker

VALID_GROUPS = checker.load_valid_groups(checker.DEFAULT_CLIFF_CONFIG)


def find_git_cliff() -> str | None:
    """Locate git-cliff: explicit env var first, then PATH."""
    return os.environ.get("GIT_CLIFF") or shutil.which("git-cliff")


GIT_CLIFF = find_git_cliff()


def injected(group: str | None, conventional: bool = True) -> dict:
    """Fake the context entry git-cliff returns for one synthetic commit."""
    return {"id": "", "conventional": conventional, "group": group, "scope": None}


def levels(problems: list[checker.Problem]) -> list[str]:
    return [problem.level for problem in problems]


class FormatTest(unittest.TestCase):
    """Rules that only look at the title text."""

    def test_allowed_types_pass(self):
        group = "<!-- 0 -->✨ 新功能"
        for name, _ in checker.ALLOWED_TYPES:
            with self.subTest(type=name):
                problems = checker.judge(f"{name}: 描述", injected(group), VALID_GROUPS)
                self.assertEqual(problems, [], f"{name} 应通过检查")

    def test_scope_and_breaking_marker_pass(self):
        group = "<!-- 0 -->✨ 新功能"
        for title in ("feat(OutpostTrading): 新增售卖", "feat(ui)!: 重写渲染", "refactor(a/b): 拆分模块"):
            with self.subTest(title=title):
                self.assertEqual(checker.judge(title, injected(group), VALID_GROUPS), [])

    def test_missing_space_after_colon_warns_only(self):
        problems = checker.judge("fix:修复启动崩溃", injected("<!-- 1 -->🐛 Bug修复"), VALID_GROUPS)
        self.assertEqual(levels(problems), [checker.WARNING])
        self.assertIn("冒号后建议空一格", problems[0].message)

    def test_legacy_types_warn_only(self):
        for title, group in (("rft: 重构", "<!-- 2 -->🚜 代码重构"), ("styles: 格式化", "<!-- 5 -->🎨 样式")):
            with self.subTest(title=title):
                problems = checker.judge(title, injected(group), VALID_GROUPS)
                self.assertEqual(levels(problems), [checker.WARNING])
                self.assertIn("旧写法", problems[0].message)

    def test_empty_description_is_error(self):
        problems = checker.judge("fix:", injected("<!-- 1 -->🐛 Bug修复", conventional=False), VALID_GROUPS)
        self.assertIn(checker.ERROR, levels(problems))
        self.assertIn("冒号后缺少描述", problems[0].message)

    def test_full_width_colon_is_error_with_specific_hint(self):
        problems = checker.judge(
            "fix：自动生态农场优化", injected("<!-- 1 -->🐛 Bug修复", conventional=False), VALID_GROUPS
        )
        self.assertEqual(levels(problems), [checker.ERROR])
        self.assertIn("全角", problems[0].message)

    def test_space_before_colon_is_error(self):
        problems = checker.judge("feat : 空格", injected("<!-- 0 -->✨ 新功能", conventional=False), VALID_GROUPS)
        self.assertIn("冒号前不能有空格", problems[0].message)

    def test_missing_colon_is_error(self):
        problems = checker.judge("feat(OutpostTrading)添加售卖", injected(None, conventional=False), VALID_GROUPS)
        self.assertEqual(levels(problems), [checker.ERROR])
        self.assertIn("从更新日志里消失", problems[0].message)

    def test_non_conventional_title_is_error(self):
        for title in ("去掉限制", "1", "文档可读性++", "fix：x"):
            with self.subTest(title=title):
                problems = checker.judge(title, injected(None, conventional=False), VALID_GROUPS)
                self.assertIn(checker.ERROR, levels(problems))

    def test_empty_title_is_error(self):
        self.assertIn(checker.ERROR, levels(checker.judge("   ", None, VALID_GROUPS)))

    def test_unknown_type_is_error_and_suggests(self):
        cases = {"pref: 抢委托送货": "perf", "doc: 补充文档": "docs", "build: 构建": "可用的类型"}
        for title, expected in cases.items():
            with self.subTest(title=title):
                problems = checker.judge(title, injected(title.split(":")[0]), VALID_GROUPS)
                self.assertEqual(levels(problems), [checker.ERROR])
                self.assertIn(expected, problems[0].hint)

    def test_unknown_type_is_error_even_with_a_real_group(self):
        """cliff.toml 用前缀正则匹配，`feature`、`featx` 这类野类型也能拿到合法分组，不能靠分组判断类型是否合法。"""
        group = "<!-- 0 -->✨ 新功能"
        for title in ("feature: x", "featx: x", "fixture: 修 bug", "docsx: 文档", "chores: 杂项", "perfume: 优化"):
            with self.subTest(title=title):
                problems = checker.judge(title, injected(group), VALID_GROUPS)
                self.assertEqual(levels(problems), [checker.ERROR])
                self.assertIn("不在允许的类型列表里", problems[0].message)

    def test_uppercase_type_is_error(self):
        for title in ("Fix: typo", "CI: 新增检查", "Rft: 重构"):
            with self.subTest(title=title):
                type_name = title.split(":")[0]
                problems = checker.judge(title, injected(type_name), VALID_GROUPS)
                self.assertEqual(levels(problems), [checker.ERROR])
                self.assertIn(type_name.lower(), problems[0].hint)

    def test_revert_is_warning(self):
        problems = checker.judge('Revert "chore: 某次提交"', injected(None, conventional=False), VALID_GROUPS)
        self.assertEqual(levels(problems), [checker.WARNING])
        self.assertIn("revert:", problems[0].hint)

    def test_skip_changelog_marker_is_warning(self):
        problems = checker.judge("chore: 同步 [skip changelog]", None, VALID_GROUPS)
        self.assertEqual(levels(problems), [checker.WARNING])
        self.assertIn("skip changelog", problems[0].message)


class ReportTest(unittest.TestCase):
    def test_render_pass_and_fail(self):
        report, code = checker.render("feat: x", [])
        self.assertEqual(code, 0)
        self.assertIn("通过", report)

        report, code = checker.render("pref: x", [checker.Problem(checker.ERROR, "坏标题")])
        self.assertEqual(code, 1)
        self.assertIn("不符合", report)
        self.assertIn("坏标题", report)

    def test_warning_keeps_exit_code_zero(self):
        report, code = checker.render("fix:x", [checker.Problem(checker.WARNING, "冒号后建议空一格")])
        self.assertEqual(code, 0)
        self.assertIn("建议", report)

    def test_command_escapes_newlines(self):
        self.assertEqual(checker.escape_command("a\nb%c"), "a%0Ab%25c")


@unittest.skipUnless(GIT_CLIFF, "需要 git-cliff，可设置 GIT_CLIFF 指向二进制")
class GitCliffIntegrationTest(unittest.TestCase):
    """Run the real tool so the check cannot drift from the release pipeline."""

    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        cls.repo = Path(cls.tmp.name)
        git = ["git", "-C", str(cls.repo)]
        subprocess.run(["git", "init", "-q", str(cls.repo)], check=True)
        subprocess.run(
            [
                *git,
                "-c",
                "user.name=ci",
                "-c",
                "user.email=ci@local",
                "commit",
                "-q",
                "--allow-empty",
                "-m",
                "chore: ci baseline",
            ],
            check=True,
        )

    @classmethod
    def tearDownClass(cls):
        cls.tmp.cleanup()

    def run_cliff(self, title: str):
        return checker.run_cliff(self.repo, checker.DEFAULT_CLIFF_CONFIG, title, GIT_CLIFF)

    def test_injects_single_commit(self):
        entry = self.run_cliff("feat(OutpostTrading): 新增售卖")
        self.assertIsNotNone(entry)
        self.assertTrue(entry["conventional"])
        self.assertIn(entry["group"], VALID_GROUPS)

    def test_skip_marker_removes_commit(self):
        self.assertIsNone(self.run_cliff("chore: 同步 [skip changelog]"))

    def test_leading_dash_title_is_not_parsed_as_flag(self):
        # "--with-commit=<值>" 的等号形式让以横线开头的标题原样传进来；git-conventional 认为 "-x" 是合法类型
        entry = self.run_cliff("-x: 坏标题")
        self.assertIsNotNone(entry)
        self.assertEqual(entry["group"], "-x")
        self.assertNotIn(entry["group"], VALID_GROUPS)

    def test_allowed_types_are_grouped(self):
        """Guard against editing cliff.toml without updating ALLOWED_TYPES."""
        for name, _ in checker.ALLOWED_TYPES:
            with self.subTest(type=name):
                entry = self.run_cliff(f"{name}: 描述")
                self.assertIsNotNone(entry)
                self.assertIn(entry["group"], VALID_GROUPS, f"{name} 没有对应的 changelog 分组")

    def test_every_group_is_reachable(self):
        reached = {self.run_cliff(f"{name}: x")["group"] for name, _ in checker.ALLOWED_TYPES}
        self.assertEqual(reached, VALID_GROUPS)

    def test_step_summary_blocks_stay_separated(self):
        summary = self.repo / "summary.md"
        for title in ("feat: 新增", "pref: 抢委托"):
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                status = checker.main(
                    [
                        "--title",
                        title,
                        "--repo",
                        str(self.repo),
                        "--cliff-config",
                        str(checker.DEFAULT_CLIFF_CONFIG),
                        "--git-cliff",
                        GIT_CLIFF,
                        "--step-summary",
                        str(summary),
                    ]
                )
            self.assertEqual(status, 0 if title.startswith("feat") else 1)
        text = summary.read_text(encoding="utf-8")
        self.assertNotIn("\n###", text.replace("\n\n###", ""))
        self.assertEqual(text.count("### "), 2)

    def test_whole_judge_pipeline(self):
        cases = {"feat: 新增": 0, "pref: 抢委托": 1, "feature: 新增": 1, "去掉限制": 1, 'Revert "chore: x"': 0}
        for title, expected in cases.items():
            with self.subTest(title=title):
                problems = checker.judge(title, self.run_cliff(title), VALID_GROUPS)
                self.assertEqual(checker.render(title, problems)[1], expected)


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    unittest.main()
