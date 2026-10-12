# docs-guide 参考：历史案例与规则速查

## 被改坏的 Markdown

2026-07-28 之前，Markdown 由 prettier 格式化；#4569 起改用 markdownlint，#4906（2026-08-10）让 prettier 忽略 `**/*.md`。`.agents/` 因为是点开头的目录，此后一直没被 markdownlint 处理，直到 `format:md` 显式加上它。

下面的问题都在仓库里真实发生过，SKILL.md 里对应的规则就是从这些案例来的。

| 提交 | 文件 | 发生了什么 | 对应规则 |
| --- | --- | --- | --- |
| 797ded03（2026-02-28） | en_us `map-tracker.md` | 原文写成 `**坐标系统***Coordinate System**`，prettier 转义后星号原样显示 | 粗体、斜体不要首尾紧贴 |
| 90614bcc（2026-03-25） | `maaend-issue-log-analysis/SKILL.md` | prettier 把 ```` ```markdown ```` 模板里的 `~~~text` 改成 ```` ```text ````，内层围栏提前关闭了外层 | 外层围栏比内层长 |
| 5a436fa1（2026-03-26） | 同上 | 按坏掉的结构重写围栏，并在内层代码块后插入结束围栏 | 同上 |
| 1eb84308（2026-03-28） | 同上 | 在文件末尾补了围栏，把 Reminders 整节包进代码块 | 同上 |
| 21a7b67a（2026-03-31） | 同上 | `a.` 不是列表标记，下面的 `-` 子项被并进同一行 | 有序列表用数字 |
| c9c735aa（2026-04-18） | `cpp-algo-style` 的 SKILL.md、reference.md | 下划线被当成强调：`` snake_case + 尾下划线 `_` `` 变成 `` snake*case + 尾下划线 `*` `` | 标识符写进行内代码 |
| 94666a83（2026-04-25） | `tools/pipeline-generate/BatchUseDetector/README.md` | 作者漏写结束围栏，格式化在文件末尾补上，后面的标题被吞进代码块 | 结束围栏紧跟内容 |
| 84a49def（2026-05-04） | `super-basic-introduction.md` | `> [!NOTE]` 与下一行被合并成 `> [!NOTE] > **外部资源**` | 提示块标记单独一行 |
| 739488ca（2026-05-22） | zh_cn `coding-standards.md` | `max_hit` 变成 `max*hit`，`_…_` 变成 `*…\_` | 标识符写进行内代码 |
| e9a885c5（#3431，2026-06-07） | zh_cn `auto-stockstaple-maintain.md` | 新增提示块时就写成了一行 | 提示块标记单独一行 |
| 064c28fc（#3925，2026-06-29） | en_us `coding-standards.md`、`auto-stockstaple-maintain.md` | #3717 修好的英文提示块，被按未修的中文版同步回坏的写法 | 先改 zh_cn，中英文一起修 |
| 3aa72421（2026-09-16） | zh_cn `go-service-pkg.md` | 表格里的 `` `\|\|` `` 被 MD060 改成 `` ` \| \| ` `` | 单元格里的竖线前加反斜杠转义 |
| 2affdeff、1caaf090 | issue 模板 | 删掉、挪动了行尾两个空格的硬换行，表单显示随之改变 | 行尾两个空格有意义 |

此外，`go-service-pkg.md` 的包一览表曾有 12 个锚点按「全角冒号变 `-`」的规则手写（如 `#i18n-多语言文案`），而 GitHub 会直接删掉全角冒号（`#i18n多语言文案`），点了不跳转；`pipeline-guide/field-reference.md` 的类型列里 `list<string>` 被当成 HTML 标签，GitHub 只显示 `list`。

## 检查同类问题

先跑 `pnpm format:md:check`：MD033 能抓出被当成标签的 `<…>`，MD056 能抓出被竖线拆错的表格，MD051 能抓出失效的同文件锚点。以下几类 lint 抓不到，用 grep 查：

```bash
# 提示块标记后面直接跟了内容（应单独占一行）
git grep -nE '^> *\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\] *[^ ]' -- '*.md' ':!.agents/skills/docs-guide'
# 历史上出现过的损坏特征：下划线标识符变星号、表格里的 || 被拆开
git grep -nE 'max\*hit|snake\*case|` \| \| `' -- '*.md' ':!.agents/skills/docs-guide'
# 用字母冒充的有序列表
git grep -nE '^ *[a-z]\. ' -- '*.md' ':!.agents/skills/docs-guide'
```

漏写结束围栏没有固定特征，在渲染预览里看「代码块是否吞掉了后面的标题」最快。

## markdownlint 规则速查

以下基于 markdownlint-cli 0.49.1（markdownlint 0.41.1）。

- `--fix` 会自动改写：MD004、MD005、MD007、MD009、MD010、MD011、MD012、MD014、MD018–MD023、MD026、MD027、MD029、MD030、MD031、MD032、MD034、MD037、MD038、MD039、MD044、MD047、MD049、MD050、MD051（只改大小写）、MD053、MD054、MD058、MD060。
- 只报告、需要手改：MD001、MD003、MD013、MD024、MD025、MD028、MD033、MD035、MD036、MD040、MD041、MD042、MD043、MD045、MD046、MD048、MD052、MD055、MD056、MD059。

本仓库常见的报错与处理：

| 规则 | 常见原因 | 处理 |
| --- | --- | --- |
| MD033 | 正文里的 `list<string>` 被当成 HTML 标签；手写 `<a id>` 锚点 | 放进行内代码或把 `<` 转义；锚点改为链接标题 |
| MD040 | 目录树、日志代码块没写语言 | 写 `text` |
| MD041 | 客户端读取的片段文件没有一级标题 | 预期如此，用 `disable-next-line` 抑制 |
| MD051 | 锚点不是按 GitHub 规则写的；en_us 里留着中文锚点 | 按 GitHub 规则改成标题实际生成的锚点 |
| MD056 | 单元格里有没转义的竖线 | 竖线前加反斜杠转义 |
