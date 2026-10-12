---
name: docs-guide
description: MaaEnd 文档与 Markdown 编写、维护指南。覆盖 docs/zh_cn → docs/en_us 镜像同步、markdownlint 格式化与检查、GitHub 渲染陷阱（嵌套代码块、提示块、表格里的竖线和尖括号、有序列表、标题锚点）、被格式化工具改坏的 Markdown 的修复流程，以及 README、.agents/skills、客户端描述文件等 docs 之外 Markdown 的约定。在新增、修改、翻译、审查任何 .md 文件，处理 markdownlint 报错，或排查文档在 GitHub 或客户端上显示异常时使用。
---

# MaaEnd 文档编写与维护指南

适用于仓库内所有 Markdown：`docs/`、`.agents/skills/`、`README*.md`、各目录的 `README.md`，以及客户端读取的 `assets/locales/**/*.md`。规则来自仓库现有配置和实际踩过的坑，历史案例与规则速查见 [reference.md](./reference.md)。

## 先确认改的是哪类文件

| 位置 | 谁读、怎么渲染 | 注意 |
| --- | --- | --- |
| `docs/zh_cn/**` | GitHub 网页、编辑器预览 | 文档的源文件，改动先落在这里 |
| `docs/en_us/**` | 同上 | 由 `docs-sync` 从 zh_cn 生成的镜像，见「中英文同步」 |
| `.agents/skills/**` | AI 直接读源码；`issue-ai-analysis.yml` 会读 `maaend-issue-log-analysis` | 源码要好读，GitHub 上也要显示正确 |
| `README.md`、`README.en.md` | GitHub 仓库首页 | 文件头关闭了 MD033、MD041，可以用 HTML 排版 |
| `assets/locales/LICENSE_SHORT.md`、`assets/locales/interface/DESCRIPTION/*.md` | MXU 客户端：marked（`gfm`、`breaks: true`）+ DOMPurify | 是片段，没有一级标题；单个换行在客户端里显示为换行；HTML 注释不显示 |
| `tools/**`、`agent/**`、`tests/**` 下的 `README.md` | GitHub | 按 docs 的写法 |
| `.github/**/*.md`、`.github/ISSUE_TEMPLATE/*.yml` | GitHub、Copilot | 不在 markdownlint 范围内；issue 模板里行尾两个空格是硬换行，不要删 |

## 写作约定

### 放在哪、怎么登记

- 开发者文档放 `docs/zh_cn/developers/`：高级组件放 `components/<name>.md`，任务维护文档放 `tasks/<task>-maintain.md`（kebab-case），文件格式协议放 `protocol/`；面向用户的文档放 `docs/zh_cn/users/`。
- 新增文档后，在 `docs/zh_cn/developers/README.md` 的「文档索引」对应表格里加一行；属于阅读主线的，同时更新开头的 mermaid 阅读路线。en_us 的 README 一起更新。
- 标题沿用同目录的写法：任务维护文档多为 `# 开发手册 - <名称>维护文档`，组件文档多为 `# 开发手册 - <组件名> <一句话说明>`。
- 任务维护文档的开头写一句本文说明和更新日期，两行之间用硬换行；改了内容就更新日期。常见章节有：文件路径、执行流程、特殊处理、新增 XX 时需改的路径、维护要点。

下面示例里的 `␣␣` 代表行尾的两个空格：

```markdown
# 开发手册 - 信用点商店维护文档

本文说明 `CreditShopping` 的文件分布与执行流程。␣␣
该文档更新于 2026 年 7 月 13 日。
```

### 行文

- 内容以代码、Pipeline 和实际运行结果为准；拿不准的地方明确标出来，不要编。
- 节点名、字段、文件路径、命令、标识符都写成行内代码，例如 `AutoStockStapleQuantityControl`、`max_hit`。这样也能避免下划线被当成强调、`<…>` 被当成 HTML 标签。
- 不要按宽度手动折行（MD013 已关闭），一个段落写成一行。需要换行时用行尾两个空格（硬换行），或空一行分段。中文段落中间的普通换行，在 GitHub 上会显示成一个多余的空格。
- 粗体用 `**…**`，斜体用 `_…_`（仓库里绝大多数文件如此）。markdownlint 按文件里第一次出现的符号统一（MD049、MD050），混用会被 `--fix` 改写。

### 链接、锚点与图片

- 文档之间、文档到仓库文件都用相对路径，例如 `./custom.md`、`../coding-standards.md#提交前检查`、`../../../.github/workflows/check.yml`。
- 不要用 `<a id>`、`<a name>` 手写锚点（MD033 不允许 `<a>`），直接链接标题自动生成的锚点。
- GitHub 生成锚点的规则：取标题文字（行内代码保留内容），转小写，删掉标点（包括全角的 `：（），、` 和半角的 `/ + & .` 等），空格换成 `-`，连续空格不合并；同名标题依次加 `-1`、`-2`。容易写错的例子：

| 标题 | 锚点 |
| --- | --- |
| `` ## `i18n`：多语言文案 `` | `#i18n多语言文案` |
| `` ### `iconrecognition` / `iconqty` `` | `#iconrecognition--iconqty` |
| `## 其它高复用包（简表 + 用法要点）` | `#其它高复用包简表--用法要点` |
| 同一文件里第二个 `### 推荐写法` | `#推荐写法-1` |

- MD051 只校验同一文件里的 `#锚点`，`--fix` 也只纠正大小写；跨文件的 `xxx.md#锚点` 没有工具检查，写完要自己确认。
- 截图上传到 GitHub（拖进 issue 或 PR 的评论框）后，用 `https://github.com/user-attachments/assets/…` 链接；现有文档基本都这样，图片不提交进 `docs/`。

### 代码块

- 围栏一律写语言（MD040）；目录树、日志、纯文本用 `text`。
- 代码块里还要放代码块时（例如给 AI 的输出模板），外层围栏要比里面所有围栏都长：

`````markdown
````markdown
## 关键证据

```text
Frame 0: ...
```
````
`````

- 结束围栏紧跟内容写上。漏写的话，后面的标题和正文会全部被吞进代码块；没有规则会报这种错，只能靠渲染预览发现。
- 列表项里的代码块前后要有空行（MD031，`--fix` 会自动补）。补上后整个列表变成松散列表，条目间距变大，这是正常的。

### 列表

- 无序列表用 `-`，嵌套缩进 4 个空格（MD007）。
- 有序列表只能用数字 `1.`、`2.`；`a.`、`一、`、`1、` 都不是列表标记，会和上下行挤成一段。GitHub 把嵌套的有序列表显示为 i.、ii.。
- 修改已有列表时不要改变列表类型（有序变无序或反过来），也不要顺手改正文。

### 表格

- 按紧凑格式写：`| 列 | 列 |`，分隔行写 `| --- | --- |`。MD060 设成了 `compact`，`--fix` 会把对齐的表格压成这种格式。
- 单元格里的竖线一律写成 `\|`，在行内代码里也要转义：单元格里写 `` `\|\|` ``，显示为 `||`。没转义的竖线会被当成分列符，`--fix` 还会把它改成 `` ` | | ` ``。
- `list<string>` 这类带尖括号的文字要放进行内代码，或者把 `<` 转义成 `\<`。否则 `<string>` 会被当成 HTML 标签直接消失：单元格里写 `string \| list<string>`，GitHub 只显示 string | list。
- 每行的列数要和表头一致（MD056）。

### 提示块

```markdown
> [!IMPORTANT]
> 正文
```

- `[!NOTE]`、`[!TIP]`、`[!IMPORTANT]`、`[!WARNING]`、`[!CAUTION]` 必须单独占一行。写成 `> [!IMPORTANT] > 正文` 就只是普通引用，开头原样显示 `[!IMPORTANT] >`。
- 连续两个提示块之间空一行即可（MD028 已关闭）。

### HTML

- 只能用 `<details>`、`<summary>`、`<br>`（MD033）。`<summary>` 所在行之后要空一行，折叠区里的 Markdown 才会渲染。
- 需要大量 HTML 排版的文件（如 README）才在文件头整体关闭 MD033。

## 中英文同步

zh_cn 是源，en_us 由 `.github/workflows/docs-sync.yml` 生成。它只能手动触发：按 `docs/en_us/.docs-sync-state.json` 里记录的 zh_cn 文件 sha256 找出变过的文件，整篇重新翻译并覆盖对应的 en_us 文件，再由 bot 提 PR「docs: sync en_us mirror from zh_cn」。由此：

1. 改动先落在 zh_cn。只改 en_us 的修复，下次同步该文件时会被覆盖：#3717 修好的英文提示块，就被 #3925 按未修的中文版同步坏了。
2. 修错字、格式、链接这类问题时，中英文在同一个 PR 里一起改。新增内容或大段改写可以只改 zh_cn，en_us 留给同步（目前就有几篇中文文档还没有英文版）。
3. 不要手改 `.docs-sync-state.json`，它只由同步工作流更新。

翻译脚本 `tools/docs/translate_with_llm.py` 会把指向 docs 文档标题的 `#锚点` 换成对应英文标题生成的锚点，同一文件和跨文件的链接都处理。对应关系的来源：

- 本次翻译的文档：翻译前给每个标题行末尾加编号标记，翻译后按标记对应，再去掉标记；模型丢了标记时，标题层级完全一致就按位置对应。
- 本次没翻译的文档：中文源文件自上次同步后没变，按标题位置对应。

对应不上的锚点保持原样，并在工作流日志里输出 `Warning: could not map anchor …`。所以：

- 处理同步 PR 时先看日志里的警告，再跑 `pnpm format:md:check`，MD051 会列出同一文件里仍然失效的锚点；
- 手写或手改 en_us 时，锚点要按英文标题写；
- 指向只有中文版的文档（例如 `agent/cpp-algo/source/IconRecognition/docs/*.md`）时，中文锚点本来就对，脚本不会改。

## 格式化与检查

- Markdown 只由 markdownlint 处理：`pnpm format:md`（即 `markdownlint --fix . .agents`）。`pnpm format` 的 prettier 只管 JSON、YAML，`.prettierignore` 里忽略了 `**/*.md`。
- `pnpm format:md:check`（即 `markdownlint . .agents`）只报告不修改。全仓库目前是 0 条，提交前保持 0 条。
- PR 的 CI 不检查 Markdown。`format.yml` 每天 0:00 和 6:00（UTC+8）对全仓库跑 `pnpm format:all`，直接提交 `style: auto format code`：没格式化的文件会被 bot 改写，而且没人 review。所以自己先格式化，看清 diff。
- `.` 展开时会跳过以点开头的目录，脚本里因此显式加了 `.agents`；`.github/` 仍不在范围内。把文件路径直接传给 markdownlint 时，任何位置的文件都会被处理。

### `--fix` 会改变显示的地方

`--fix` 改的不只是空白，下面几条会影响显示，跑完要看 diff：

| 规则 | 会做什么 | 要注意 |
| --- | --- | --- |
| MD060 | 把表格压成紧凑格式 | 单元格里没转义的 `\|` 会被改坏 |
| MD031 | 在代码块前后补空行 | 列表里的代码块补空行后，整个列表变松散 |
| MD049、MD050 | 统一斜体、粗体的符号 | 按文件里第一次出现的符号改写其余的 |
| MD051 | 纠正 `#锚点` 的大小写 | 只改大小写，锚点写错了不会帮你改 |
| MD038 | 删掉行内代码首尾的空格 | 想表达「`-` 加一个空格」这类带空格的代码会被删掉空格，改用文字说明 |

### 本仓库的规则配置

`.markdownlint.json` 只改了下面这些规则，其余都是默认值；常见的 MD040（代码块语言）、MD041（首行一级标题）、MD051（锚点）、MD056（表格列数）都开着。

| 规则 | 设置 | 含义 |
| --- | --- | --- |
| MD033 | 只允许 `details`、`summary`、`br` | 其他 HTML 一律报错 |
| MD024 | `siblings_only` | 不同章节下可以有同名标题 |
| MD007 | `indent: 4` | 无序列表嵌套缩进 4 个空格 |
| MD060 | `compact` | 表格用紧凑格式 |
| MD013 | 关闭 | 不限行长，不要手动折行 |
| MD028 | 关闭 | 引用块之间可以有空行 |
| MD036 | 关闭 | 允许整行粗体当小标题 |

### 抑制误报

只有「预期如此」的报错才抑制，例如客户端读取的片段文件本来就没有一级标题。能改写法消除的报错，先改写法。

markdownlint 识别的注释格式是 `<!-- markdownlint-<动作> [规则…] -->`，把 `<动作>` 换成下表中的一项：

| 动作 | 作用范围 |
| --- | --- |
| `disable-next-line` | 注释的下一行 |
| `disable-line` | 注释所在的行，注释写在行尾 |
| `disable`、`enable` | 从注释所在的行开始，到相反的注释或文件末尾为止 |
| `capture`、`restore` | 保存当前的规则开关状态、恢复到保存的状态；文件开头默认保存过一次，所以 `disable` 之后写 `restore` 就能回到原配置 |
| `disable-file`、`enable-file` | 整个文件，与注释的位置无关 |
| `configure-file` | 后面跟 JSON（如 `{ "MD060": { "style": "aligned" } }`），修改本文件的规则配置；与位置无关，可以跨多行，有多个时从上到下合并 |

规则列表的写法：

- 用空格隔开，可以写规则号（`MD033`）、别名（`no-inline-html`）或标签（`html`），不区分大小写。
- 不写规则，就是全部规则。
- 用逗号隔开、规则名拼错，都会被静默忽略，不报错。写完跑一次 `pnpm format:md:check`，确认报错确实消失了。

放的位置：

- 优先用 `disable-next-line` 或 `disable-line`，并写明规则，影响范围最小。
- 注释独占一行时，不能插在表格行之间或段落中间，否则会把表格、段落截成两半；在列表项里要缩进到该项正文所在的列。
- `disable-line` 不能写在表格行的末尾，注释会变成多出来的一个单元格（MD055、MD056）。要对整个表格关闭规则，在表格紧挨着的上一行写 `disable` 加规则，表格之后空一行写 `restore`。
- 整个文件关闭规则，只用在 README 这类大量 HTML 排版的文件上，而且要写明规则。

容易踩的坑：

- 这些注释是按行用正则识别的，**代码块和行内代码里的也会生效**。在文档里举例时不要写出完整的注释，像上面那样用 `<动作>` 模板写。`disable-file`、`enable-file`、`configure-file` 对整个文件生效，影响最大。
- 同一行里先 `disable` 再 `enable`，等于没写。
- 不写规则的 `disable` 会关掉全部规则；不写规则的 `enable`、`enable-file` 会打开全部规则，连配置里关掉的 MD013 等也会打开。
- `configure-file` 会让单个文件偏离仓库的统一配置，不要用。
- `<!-- prettier-ignore -->` 这类注释对 markdownlint 无效，prettier 也已经不处理 Markdown，不要再加；README 里的是历史遗留。

不写注释的办法：`.markdownlintignore` 排除整个路径（目前排除了 `node_modules/`、vendor、子模块等）；`.markdownlint.json` 修改全仓库的规则配置；命令行的 `--disable`、`--enable` 只影响当次运行。前两种影响所有文件，要先和维护者商量。

现有的抑制：`README*.md` 在文件开头用 `disable` 关闭了 MD033、MD041，另有一处行级的 MD036；`DESCRIPTION.*.md` 和 `LICENSE_SHORT.md` 用 `disable-next-line` 关闭了 MD041。

## 验证

按顺序做：

1. 跑 `pnpm format:md`，用 `git diff` 看它改了什么。
2. 跑 `pnpm format:md:check`，确认 0 条。
3. 只调格式、修结构的改动，用 `render-diff.mjs` 比较渲染结果。
4. 提示块、标题锚点这类 GitHub 独有的语法，按下文用 GitHub 确认。
5. 只改文档时不需要跑 `pnpm check`、`pnpm test`，交给 PR 的 CI（见 `AGENTS.md`）。

`render-diff.mjs` 用 markdownlint 自带的解析器（CommonMark + GFM 表格）渲染 git 版本（默认 `HEAD`）和工作区的文件，再做比较：

```bash
node .agents/skills/docs-guide/scripts/render-diff.mjs --ref origin/v2 docs/zh_cn/developers/xxx.md
```

| 结果 | 含义 |
| --- | --- |
| `identical` | 渲染结果完全相同 |
| `whitespace-only` | 只差块级标签之间的空白，显示不变 |
| `tightness-only` | 只差列表松紧，条目间距会变 |
| `DIFFERENT` | 显示内容变了，会输出第一处差异；此时退出码为 1 |

GitHub 独有的语法这样确认：

- 提示块：`gh api markdown -f mode=gfm -f context=MaaEnd/MaaEnd -F text=@<file.md>`，写对的提示块会渲染成 `<div class="markdown-alert …">`。这会把内容发给 GitHub，只用于本来就要公开的文档。
- 标题锚点：同一文件内靠 MD051。跨文件的，把分支推上去后用 `gh api -H "Accept: application/vnd.github.html" "repos/MaaEnd/MaaEnd/contents/<path>?ref=<branch>"` 取页面，`id="user-content-…"` 里就是锚点（经过 URL 编码）；也可以在 PR 的 rich diff 里直接点。

## 修复被格式化工具改坏的 Markdown

1. 找到弄坏它的提交：`git log -L <起始行>,<结束行>:<file>` 或 `git log -S'<坏掉后的文本>'`，多半是 `style: auto format code`。
2. 用 `git show <作者的提交>:<path>` 取出原文，按原文的结构还原；之后有意的修改要保留（例如 `maa.log` 改成了 `maafw.log`）。
3. 写成真正合法的语法，不要原样恢复伪语法（例如 `a.` 改成 `1.`）；不改变列表类型，不改措辞。
4. 跑 `pnpm format:md`，确认它不会再改这些位置。
5. 中英文一起修。
6. 这类修复改的是正文。如果仓库启用了 `.git-blame-ignore-revs`，提交不要带 blame ignore 标记，否则这些行的 blame 会落回弄坏它们的提交。
7. 顺手查同类问题，常用的特征见 [reference.md](./reference.md)。

## 提交

- 文档改动的提交用 `docs:`，skill、AI 提示词用 `ai:`；`.github/cliff.toml` 分别把它们归到 changelog 的「📚 文档」「🤖 人工智能」。全部类型见 `docs/zh_cn/developers/getting-started.md` 的「提交规范」。
- 带 `[skip changelog]` 的提交不进 changelog，一般只有 bot 使用。PR 以 squash 合并时 PR 描述会成为提交正文，所以 PR 描述里也不要写这个标记。
- 提交前跑 `pnpm format:md`；由 AI 改完文档时，提醒用户跑（见 `AGENTS.md`）。

## 编写 `.agents/skills`

- 放在 `.agents/skills/<name>/SKILL.md`（`.claude/` 已被 gitignore）。frontmatter 只写 `name` 和 `description`，`name` 与目录名一致（现有例外：`dmp-analysis` 目录里的 name 是 `windows-dmp-analysis`）。
- `description` 用中文写清做什么、在什么场景下使用，AI 靠它决定是否加载这个 skill。
- skill 主要由 AI 读源码，结构清楚、指令明确优先；它同样受 markdownlint 约束，在 GitHub 上也要能正常显示。
- 篇幅大的参考资料放同目录的 `reference.md`，脚本放 `scripts/`，在仓库根目录运行（`node .agents/skills/<name>/scripts/x.mjs`、`uv run .agents/skills/<name>/scripts/x.py`）。
- 给 AI 套用的输出模板，放在外层 4 个反引号的 `markdown` 代码块里。

## 提交前检查清单

- [ ] 改动先落在 zh_cn；同一问题在 en_us 也改了
- [ ] 新文档已登记到 `docs/zh_cn/developers/README.md`（en_us 同步）
- [ ] 代码块都写了语言；嵌套时外层围栏更长；结束围栏没有漏
- [ ] 表格里的竖线写成 `\|`，`<…>` 放进行内代码或转义
- [ ] 提示块标记单独一行，有序列表用数字
- [ ] 锚点与标题一致，没有 `<a>` 锚点
- [ ] 抑制注释只用于预期如此的报错并写明了规则；文档里举例时没有写出完整的抑制注释
- [ ] `pnpm format:md` 不再产生改动，`pnpm format:md:check` 为 0 条
- [ ] 只调格式的改动，`render-diff.mjs` 的结果不是 `DIFFERENT`
