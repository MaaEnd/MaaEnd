---
name: docs-guide
description: MaaEnd 文档与 Markdown 编写指南：中英文文档要一起改、GitHub 渲染陷阱、markdownlint 格式化与检查、修复被格式化工具改坏的 Markdown。在新增、修改、翻译、审查任何 .md 文件，或处理 markdownlint 报错时使用。
---

# MaaEnd 文档编写指南

适用于仓库里所有 Markdown：`docs/`、`.agents/skills/`、`README*.md` 和各目录的 `README.md`。

## 中英文同步

`docs/zh_cn/**` 与 `docs/en_us/**` 按相同路径一一对应，都由人工维护，没有自动翻译。

- 新增、修改、删除文档时，在同一个改动里把中英文两份都改好；修错字、链接、格式这类小改动也一样。根目录的 `README.md` 与 `README.en.md` 同理。
- 新增开发者文档时，中英文 `developers/README.md` 的文档索引都要登记。
- 英文版与中文版逐一对应：标题、段落、列表、表格、代码块、提示块的数量和顺序都一致，不多不少。
- 翻译正文、标题、表格和代码块里的注释；代码、节点名、字段、路径、命令，以及游戏内文字（OCR 的 `expected`、物品名、截图文件名等）保持原样。任务名、选项名用 `assets/locales/interface/en_us.json` 里的官方英文。
- 只有中文界面的工具（如 MapNavigator 网页工具）：英文文档里的按钮、标签写界面上的中文原文，后面括号里给英文释义，如 `` `复制路径` (Copy Path) ``；标题里不放中文，以免改变锚点。
- en_us 里的链接指向 en_us 的对应文档，`#锚点` 按英文标题生成（见下节）；只有中文版的文件（如 `.agents/skills/**`）保留中文锚点。有英文版的中文外链换成英文版，如 `conventionalcommits.org/zh-hans/` 换成 `/en/`。

## 写作约定

- 开发者文档放 `docs/zh_cn/developers/`（组件放 `components/`，任务维护文档放 `tasks/<task>-maintain.md`），用户文档放 `docs/zh_cn/users/`；标题沿用同目录已有文档的写法。
- 任务维护文档开头写一句本文说明和更新日期，两行之间用行尾两个空格硬换行；改了内容就更新日期。
- 内容以代码和实际运行结果为准，拿不准的地方标出来，不要编。
- 节点名、字段、路径、命令、标识符写成行内代码，这样 `_` 不会被当成强调，`<…>` 不会被当成 HTML 标签。
- 一个段落写在一行里，不按宽度手动折行：中文段落中间的换行在 GitHub 上会显示成空格。需要换行时用行尾两个空格，或空一行分段。
- 粗体用 `**…**`，斜体用 `_…_`。
- 链接用相对路径，如 `./custom.md`、`../../../.github/workflows/check.yml`。不要用 `<a id>` 手写锚点，直接链接标题的锚点。
- 截图拖进 GitHub 的 issue 或 PR 评论框上传，用得到的 `https://github.com/user-attachments/assets/…` 链接，不要把截图提交进仓库。

## 标题锚点

GitHub 的规则：取标题文字（行内代码保留内容），转小写；文字（字母、汉字等）、数字、`_`、`-`、空格保留，其余字符全部删掉（包括全角的 `：（），、`）；再把每个空格换成 `-`，连续空格不合并。重名的标题依次加 `-1`、`-2`。

| 标题 | 锚点 |
| --- | --- |
| `` ## `i18n`：多语言文案 `` | `#i18n多语言文案` |
| `` ### `iconrecognition` / `iconqty` `` | `#iconrecognition--iconqty` |
| `## Quick Start` | `#quick-start` |
| 同一文件里第二个 `### 推荐写法` | `#推荐写法-1` |

lint 只检查同一文件内的锚点（MD051），跨文件的 `xxx.md#锚点` 写完要自己核对。

## GitHub 渲染陷阱

下面这些写错了大多不会报错，但显示会坏：

- 代码块围栏都写上语言，目录树、日志用 `text`。结束围栏不能漏，漏了会把后面的内容全吞进代码块。
- 代码块里还要放代码块时，外层围栏要比里面的长，例如外层 4 个反引号、里面 3 个。
- 提示块标记（`> [!NOTE]`、`> [!TIP]`、`> [!IMPORTANT]`、`> [!WARNING]`、`> [!CAUTION]`）单独占一行，正文从下一行开始；和正文写在同一行就只是普通引用。
- 表格单元格里的 `|` 一律写成 `\|`，在行内代码里也要转义，例如单元格里写 `` `\|\|` `` 显示为 `||`。
- `list<string>` 这类带尖括号的文字放进行内代码，或把 `<` 写成 `\<`，否则会被当成 HTML 标签吞掉。
- 有序列表只能用 `1.`、`2.`，`a.`、`1、` 都不是列表标记。无序列表用 `-`，嵌套缩进 4 个空格。改已有列表时不要改变列表类型。
- 表格按紧凑格式写：`| 列 | 列 |`，分隔行 `| --- | --- |`；每行列数与表头一致。
- HTML 只能用 `<details>`、`<summary>`、`<br>`。`<summary>` 那行之后空一行，折叠区里的 Markdown 才会渲染。

## 格式化与检查

- Markdown 只由 markdownlint 处理而非 Prettier：`pnpm format:md` 自动修复，`pnpm format:md:check` 只检查，保持 0 条。
- PR 的 CI 不检查 Markdown，但 `format.yml` 每天两次对全仓库跑格式化并直接提交，没人 review。所以提交前自己先跑 `pnpm format:md`，再看 diff。
- `--fix` 不只改空白，下面几条会改变显示：
    - MD060 把表格压成紧凑格式，单元格里没转义的 `|` 会被当成分列符；
    - MD031 给列表里的代码块前后补空行，整个列表随之变松散，条目间距变大；
    - MD049、MD050 按文件里第一次出现的符号统一斜体、粗体；
    - MD038 删掉行内代码首尾的空格；
    - MD051 只修正锚点的大小写，写错的锚点不会帮你改。
- `assets/locales/LICENSE_SHORT.md` 和 `assets/locales/interface/DESCRIPTION/*.md` 由 MXU 客户端显示，不在 lint 范围内；客户端里单个换行就显示为换行。DESCRIPTION 有多种语言，各语言一起改。
- `.github/` 下的文件也不在 lint 范围内；issue 模板里行尾的两个空格是硬换行，不要删。

### 抑制报错

- 能改写法消除的报错就改写法，只有「预期如此」的报错才抑制。
- 格式是 `<!-- markdownlint-<动作> <规则号> -->`：`<动作>` 用 `disable-next-line`（下一行）或 `disable-line`（所在行）；一整段用 `disable` 开始、`restore` 结束。注释不要插在表格行之间或写在表格行末尾，会把表格拆开。一定写明规则号；规则号拼错、用逗号分隔都会被静默忽略，写完跑一次 `pnpm format:md:check` 确认。
- 这类注释写在代码块、行内代码里也会生效，在文档里举例时不要写出完整的注释。
- 不要加 `<!-- prettier-ignore -->`，prettier 已经不处理 Markdown。

## 修复被改坏的 Markdown

格式化工具会按坏掉的结构重排，起因往往是很早以前的一处错误写法：

1. 用 `git log -S'坏掉后的文本' -- <文件>` 找到改坏它的提交，多半是 `style: auto format code`。
2. 用 `git show <提交>^:<文件>` 取出改坏前的原文，按原文的结构和措辞还原；之后有意的修改要保留。
3. 写成真正合法的语法（例如 `a.` 改成 `1.`），不改变列表类型，不顺手改正文。
4. 跑 `pnpm format:md`，确认不会再被改写。中英文一起修。
5. 如果改坏它的提交在 `.git-blame-ignore-revs` 里（`format.yml` 的自动格式化提交都带 `[blame ignore]` 标记，会被自动收录），在同一个修复 PR 里删掉这一行：改坏和修好都是实际修改，blame 要能指到它们。还没被收录的话（`blame-ignore.yml` 每天 06:45 收录一次），等收录后再删。修复 PR 本身不要加这个标记。

## 提交前

1. 中英文都改了。
2. 跑 `pnpm format:md`，用 `git diff` 看它改了什么。
3. `pnpm format:md:check` 为 0 条。
4. 只调格式的改动，在 GitHub 上（PR 的 rich diff 或文件页面）确认显示没变；提示块、锚点也在那里确认。
5. 只改文档时不需要跑 `pnpm check`、`pnpm test`。

文档改动的提交用 `docs:`，skill 和 AI 提示词用 `ai:`。

## 编写 skill

- 放在 `.agents/skills/<name>/SKILL.md`。frontmatter 写 `name`（与目录名一致）和 `description`（用中文写清做什么、什么时候用，AI 靠它决定是否加载）。
- skill 主要给 AI 读，结构清楚、指令明确优先；同样受 markdownlint 约束。
- 给 AI 套用的输出模板放在外层 4 个反引号的 `markdown` 代码块里，参考 `maaend-issue-log-analysis` skill。
