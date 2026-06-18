# 超基础入门指南

> **这篇文档写给谁？**
>
> 你打开 [快速开始](./getting-started.md)，看到 `git clone`、`pnpm install`、`Pipeline`、`PR`……一头雾水，完全不知道从哪下手。
>
> 这篇文档就是为你写的——它不教你写代码，它教你"怎么看得懂别人在说什么"。
>
> 如果你已经会用 Git、终端、VS Code，这篇对你来说太基础了，直接去看 [README.md](./README.md) → [getting-started.md](./getting-started.md)。

---

## 第零章 · 先搞清楚你是哪类小白

| 你的情况                                     | 跳到哪里                                                             |
| -------------------------------------------- | -------------------------------------------------------------------- |
| 我只想**用** MaaEnd 挂机，不是来写代码的     | → [官网下载](https://maaend.com/)，你不需要开发者文档                |
| 我想帮忙写 Pipeline（JSON 配置，不用写代码） | → 读完这篇 → [getting-started.md](./getting-started.md)              |
| 我想写 Go Service / 改底层逻辑               | → 读完这篇 → 学 Go 基础 → [getting-started.md](./getting-started.md) |

**绝大多数贡献者只走 Pipeline 这条路。不需要会编程，也不需要写 Go 代码。**

---

## 第一章 · 这些黑话到底是什么意思

在开始之前，先用最白的话把常见术语解释一遍。不需要精确，能干活就行。

| 术语            | 人话解释                                                        |
| --------------- | --------------------------------------------------------------- |
| **Git**         | 代码的"存档系统"。每次存档都能写备注，随时回到旧版本            |
| **GitHub**      | "把 Git 存档放到网上"的网站，大家可以一起在上面协作             |
| **终端/命令行** | 那个黑框框。用打字代替鼠标来操作电脑                            |
| **VS Code**     | 加强版记事本，专门用来写代码和配置文件                          |
| **JSON**        | 一种填表格式。`{}` 是一张表，`[]` 是一个列表                    |
| **Pipeline**    | 流水线。按顺序：识别画面 → 操作 → 识别画面 → 操作……像按菜谱做菜 |
| **Fork**        | 把别人的仓库复制一份到自己名下                                  |
| **Clone**       | 把网上的代码下载到自己电脑                                      |
| **Branch**      | 分支。开一条自己的线，不乱修改别人的                            |
| **Commit**      | 存档。给当前改动拍个快照，写一行备注                            |
| **Push**        | 把本地的存档上传到 GitHub                                       |
| **PR**          | Pull Request。把你改的发给项目管理员审核                        |
| **模板匹配**    | 在大图里找一张小图。比如"在屏幕上找到这个按钮"                  |

---

## 第二章 · 你电脑上需要装的东西

### 2.1 Git

- 下载：[git-scm.com](https://git-scm.com/downloads)
- 一路点"下一步"，不用改任何设置
- 装好后桌面右键出现 "Git Bash Here" 就说明好了

**学 Git？推荐这两个，按顺序来：**

1. [Learn Git Branching](https://learngitbranching.js.org/)——最推荐！交互式闯关，边玩边学，项目文档也推荐这个
2. [廖雪峰 Git 教程](https://www.liaoxuefeng.com/wiki/896043488029600)——中文，讲得很细，不想玩闯关就看这个

### 2.2 终端

- **Windows 11**：在文件夹里右键 → "在终端中打开"
- **Windows 10**：装了 Git 后右键 → "Git Bash Here"
- **macOS**：`Command + 空格` → 输入 `Terminal` → 回车
- **Linux**：`Ctrl + Alt + T`

你只需要会三个命令：

```bash
cd 文件夹名    # 进入某个文件夹
ls             # 看看当前文件夹里有什么
# 复制粘贴     # 把教程里的命令粘贴进来按回车
```

够了。

### 2.3 VS Code

- 下载：[code.visualstudio.com](https://code.visualstudio.com/)
- 安装时建议勾上"添加到 PATH"和"将 Code 添加到右键菜单"
- VS Code 本身装好就行，插件等后面 Clone 完、打开了项目文件夹再装——因为 `@recommended` 工作区推荐得先有项目开着才有东西。详见 [B.2 Clone——下载到本地](#b2-clone下载到本地)
- 最重要的插件：**Maa Pipeline Support**——截图、框选识别区域全靠它（也可以先在扩展市场搜这个装上）

### 2.4 Node.js + pnpm

- [Node.js 官网](https://nodejs.org/) 下载 **LTS 版**（22.x 或更高），一路下一步
- 装完后打开终端，输入：

```bash
corepack enable pnpm
```

- 验证：`pnpm --version` 能看到版本号（需要 10+）就 OK

### 2.5 Python

- [Python 官网](https://www.python.org/) 下载 3.10 以上版本
- **安装时一定要勾上 "Add Python to PATH"！** 不然终端里找不到 python

### 2.6 Go（必装）

项目底层依赖 Go 编译运行，所以**必须安装**。好消息是：你不需要学 Go 语法，也不需要写 Go 代码，装好放着就行。去 [go.dev](https://go.dev/) 下载安装（1.25.6+）。

### 🎯 检查点

> - [ ] Git 装好了
> - [ ] 会打开终端了（会 `cd` 和 `ls`）
> - [ ] VS Code 装好了，推荐插件装上了
> - [ ] `node --version` 有输出
> - [ ] `pnpm --version` 有输出
> - [ ] `python --version` 有输出
> - [ ] `go version` 有输出

---

## 第三章 · GitHub 最小求生指南

> 目标：能 clone、能开分支、能 commit、能 push、能开 PR。
>
> 下面提供两条路，选一条走到底就行：
>
> - **线路 A：GitHub Desktop** ——图形界面，全程鼠标点击，适合不想碰命令行的纯新手。
> - **线路 B：Git 命令行**——在终端里敲 git 命令，学会了哪个项目都能用。
>
> VS Code 自带的 Git 界面也能完成大部分操作，介于两者之间，这里不展开。另外 GitHub 官方还有一个 `gh` 命令行工具（GitHub CLI），能简化 Fork / PR 等操作，有兴趣可以自己了解。

---

### 线路 A：GitHub Desktop（图形界面）

先去 [GitHub Desktop 官网](https://desktop.github.com/) 下载安装，打开后登录你的 GitHub 账号。

#### A.1 Fork——把仓库复制到自己名下

这一步在网页上操作：

1. 打开 [MaaEnd 仓库](https://github.com/MaaEnd/MaaEnd)，确认已登录
2. 点右上角 **Fork** 按钮
3. 不改任何东西，直接点 **Create fork**
4. 等几秒，页面跳到 `https://github.com/你的用户名/MaaEnd`——这是你自己的副本

#### A.2 Clone——下载到本地

1. 打开 GitHub Desktop，菜单栏 **File → Clone a repository**
2. 选 **GitHub.com** 标签页，找到 `你的用户名/MaaEnd` 仓库，点它
3. 选一个本地存放路径，点 **Clone**
4. 等它下载完，仓库就在你电脑上了

#### A.3 Branch——开一条工作分支

1. GitHub Desktop 上方有个分支选择框，点它
2. 选 **New Branch**
3. 分支名用英文，格式建议 `feat/描述`，比如 `feat/add-sell-button`，点 **Create Branch**

> Fork 是复制了一整个仓库，Branch 是在仓库里面再开一条工作分支。永远不要直接在 v2 分支上改东西——开条分支，改烂了删掉就行，v2 干干净净不受影响。

#### A.4 Commit——存档

1. 改完文件后，GitHub Desktop 左侧会列出所有变更
2. 勾选你要存档的文件
3. 左下角 **Summary** 输入框写 commit 消息（格式见下方"Commit 消息格式"）
4. 点 **Commit to 你的分支名**

#### A.5 Push——上传到 GitHub

GitHub Desktop 顶部会出现 **Push origin** 按钮，点一下就行。第一次 push 会稍慢，之后很快。

#### A.6 开 PR——请求审核

1. push 完后 GitHub Desktop 顶部会出现 **Create Pull Request** 按钮，点了直接跳浏览器
2. 或者手动打开 `https://github.com/你的用户名/MaaEnd`，页面顶部会有黄色提示条 "xxx had recent pushes"，点 **Compare & pull request**
3. 标题写清楚你改了什么
4. 没做完就勾上 **Create draft pull request**
5. 点 **Create pull request**

---

### 线路 B：Git 命令行

你已经在第二章装好了 Git。打开终端（文件夹右键 → "Git Bash Here" 或 "在终端中打开"），跟着敲就行。

#### B.1 Fork——把仓库复制到自己名下

这一步同样在网页上操作：

1. 打开 [MaaEnd 仓库](https://github.com/MaaEnd/MaaEnd)，确认已登录
2. 点右上角 **Fork** 按钮，直接点 **Create fork**
3. 等几秒，页面跳到 `https://github.com/你的用户名/MaaEnd`

#### B.2 Clone——下载到本地

```bash
git clone --recursive https://github.com/你的用户名/MaaEnd.git
```

把 `你的用户名` 换成你自己的 GitHub 用户名。等它跑完，当前目录下就多了一个 `MaaEnd` 文件夹。

如果你已经 clone 了但没有加 `--recursive`，可以在仓库目录里补这一句：

```bash
git submodule update --init --recursive
```

> **子模块是什么？** MaaEnd 引用了外部资源（比如模型文件），它们放在别的 Git 仓库里。`--recursive` 就是"把引用的外部仓库也一起下载下来"。不加的话，用到的部分文件会缺失，后续跑不起来。

#### B.3 Branch——开一条工作分支

```bash
cd MaaEnd                                    # 进入仓库目录
git checkout -b feat/你的分支名               # 创建并切换到新分支
```

分支名用英文，格式建议 `feat/描述`，比如 `feat/add-sell-button`。

> Fork 是复制了一整个仓库，Branch 是在仓库里面再开一条工作分支。永远不要直接在 v2 分支上改东西——开条分支，改烂了删掉就行，v2 干干净净不受影响。

#### B.4 Commit——存档

```bash
git add .                                                # 暂存所有改动
git commit -m "feat(任务名): 做了什么"                     # 存档 + 写备注
```

commit 消息格式见下方。如果只想存档某几个文件，把 `git add .` 换成 `git add 文件路径`。

#### B.5 Push——上传到 GitHub

```bash
git push -u origin feat/你的分支名
```

**为什么要 `-u`？** 你本地新建的分支，GitHub 那边还不存在。`-u`（`--set-upstream` 的缩写）做两件事：

1. 在 GitHub 上创建同名远程分支，把本地代码传上去
2. 让本地分支"记住"对应哪个远程分支——之后直接 `git push` 就行，不用再敲一长串

**忘了加 `-u` 会怎样？** push 时会报错：

```text
fatal: The current branch feat/xxx has no upstream branch.
```

别慌，按它提示的敲：

```bash
git push --set-upstream origin feat/你的分支名
```

效果跟 `-u` 一样。之后再 push 就只需要 `git push` 了。

#### B.6 开 PR——请求审核

push 完之后，打开浏览器访问 `https://github.com/你的用户名/MaaEnd`，页面顶部会有黄色提示条 "xxx had recent pushes"，点 **Compare & pull request**。标题写清楚，没做完勾上 **Create draft pull request**，点 **Create pull request**。

---

### Commit 消息格式（两条线通用）

本项目遵循 [约定式提交（Conventional Commits）](https://www.conventionalcommits.org/zh-hans/v1.0.0/)，详见 [getting-started.md § 0. 提交规范](./getting-started.md)。下面是常用前缀速查：

| 前缀     | 什么时候用                            |
| -------- | ------------------------------------- |
| `feat:`  | 新增功能（Pipeline 节点、识别模板等） |
| `fix:`   | 修复 Bug                              |
| `docs:`  | 仅文档更改                            |
| `style:` | 格式/空白调整（不影响代码含义）       |
| `chore:` | 构建、依赖等杂项                      |

示例：`feat(SellProduct): 添加售货按钮识别模板`、`fix: 修复启动崩溃`。

---

### 🎯 检查点

> - [ ] 能把仓库 clone 到本地
> - [ ] 能创建分支
> - [ ] 能 commit（写出符合格式的 message）
> - [ ] 能 push
> - [ ] 能在 GitHub 网页上开一个 Draft PR

---

## 第四章 · JSON 填表入门

> Pipeline 用 JSON 写。JSON 是什么？**一张填好的表。** 它不叫编程语言，它叫配置格式。

### 4.1 花括号 `{}` = 一张表

```json
{
    "姓名": "张三",
    "年龄": 25,
    "会写代码": false
}
```

- `{}` = 这是一张表（或者说一个"东西"）
- `"姓名"` = 表里的栏目名，**必须用双引号包起来**
- `"张三"` = 值，文字用双引号，数字不用，真假用 `true` / `false`

### 4.2 方括号 `[]` = 一个列表

```json
{
    "名字": "李四",
    "技能": [
        "吃饭",
        "睡觉",
        "写代码"
    ]
}
```

### 4.3 套娃——表里套表

```json
{
    "识别": {
        "类型": "模板匹配",
        "参数": {
            "模板": "SellProduct/按钮.png",
            "阈值": 0.7
        }
    },
    "操作": {
        "类型": "点击"
    },
    "下一步": [
        "卖东西",
        "退出"
    ]
}
```

这就是 Pipeline 节点的基本形状。

### 4.4 新手最常见的三个错误

#### 错误 1：最后一个元素后多了逗号

```json
// ❌ 错
{
    "a": 1,
    "b": 2,
}

// ✅ 对
{
    "a": 1,
    "b": 2
}
```

#### 错误 2：栏目名忘了用双引号

```json
// ❌ 错
{
    名字: "张三"
}

// ✅ 对
{
    "名字": "张三"
}
```

#### 错误 3：花括号数量不匹配

```json
// ❌ 错——少了一个 }
{
    "a": {
        "b": 1
    }
```

> VS Code 装了推荐插件后，这些问题都会自动标红。

### 学习资源

- [JSON 教程（菜鸟教程）](https://www.runoob.com/json/json-tutorial.html)——中文，短小精悍
- [MDN JSON 教程](https://developer.mozilla.org/zh-CN/docs/Learn/JavaScript/Objects/JSON)——更系统一些

### 🎯 检查点

> - [ ] 能一眼分辨 `{}` 和 `[]`
> - [ ] 知道名字必须用双引号
> - [ ] 知道最后不能多逗号
> - [ ] 在 VS Code 里写一段 JSON，确认不报红

---

## 第五章 · Pipeline 是怎么跑的

> 这一章让你能看懂别人写的 Pipeline。读完就去看 `getting-started.md`。

### 5.1 核心思想：先看，再动

每个 Pipeline 节点做三件事：

```text
┌─────────────────┐
│  识别（看屏幕）  │  "屏幕上有没有我要找的东西？"
├─────────────────┤
│  操作（动手）    │  "有？那就点/滑/按它！"
├─────────────────┤
│  下一步（跳转）  │  "然后去哪个节点继续？"
└─────────────────┘
```

> [!WARNING]
>
> ## **铁律：永远先识别再操作。**
>
> ## 不能假设"我点了按钮，下一个画面一定出现"——每次都要亲眼确认

### 5.2 拆解一个真实节点

```json
{
    "SellProductMain": {
        "desc": "在主界面时，识别地区建设入口并点击进入",

        "recognition": {
            "type": "TemplateMatch",
            "param": {
                "template": "SellProduct/RegionalDevelopmentEntry.png",
                "roi": [
                    400,
                    200,
                    480,
                    320
                ],
                "threshold": 0.7,
                "green_mask": true
            }
        },

        "action": {
            "type": "Click"
        },

        "pre_delay": 0,
        "post_delay": 0,
        "post_wait_freezes": 100,

        "next": ["SellProductLoop"]
    }
}
```

逐行翻译成人话：

| 字段                                        | 人话                                                                                     |
| ------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `"desc"`                                    | 给人看的注释，机器不管                                                                   |
| `"recognition"` → `"type": "TemplateMatch"` | 识别方式：模板匹配（在屏幕上找一张小图）                                                 |
| `"template"`                                | 要找的那张图存哪了                                                                       |
| `"roi"`                                     | 只在这个框里找——`[左上x, 左上y, 宽, 高]`，屏幕左上角是原点                               |
| `"threshold": 0.7`                          | 相似度 70% 就算命中                                                                      |
| `"green_mask": true`                        | 绿色掩码：若为 true，将图片中不希望匹配的部分涂绿 RGB: (0, 255, 0)，匹配时会跳过绿色区域 |
| `"action"` → `"type": "Click"`              | 识别到了就点击，默认点击识别到的位置                                                     |
| `"pre_delay": 0`                            | 识别到后、执行动作前等待多少毫秒。入口节点画面稳定，设 0                                 |
| `"post_delay": 0`                           | 执行动作后、开始识别 next 前等待多少毫秒。这里用 `post_wait_freezes` 代替了              |
| `"post_wait_freezes": 100`                  | 执行动作后等画面不动了，再多等 100 毫秒。比固定 `post_delay` 更靠谱                      |
| `"next": ["SellProductLoop"]`               | 做完后按顺序尝试 next 里的每个节点，只执行第一个识别到的                                 |

> 延迟字段只在必要时用：`pre_delay` 等画面出现，`post_delay` 等动画播完，`post_wait_freezes` 等画面稳定。大多数节点设 0 就行。SellProductMain 作为任务入口，画面本身就是稳定的，所以 pre/post_delay 都是 0。
>
> 这里只拆了最常用的字段，实际可用的远不止这些——遇到不认识的，自己去网上搜 **MaaFramework Pipeline 协议**，官方文档有完整列表（5.5 节有链接）。

### 5.3 常用识别方式速查

| 方式     | 关键词           | 什么时候用                             |
| -------- | ---------------- | -------------------------------------- |
| 模板匹配 | `TemplateMatch`  | 找固定图标、按钮——给张图，在屏幕上找   |
| 文字识别 | `OCR`            | 读屏幕上的文字——比如确认当前在哪个界面 |
| 颜色匹配 | `ColorMatch`     | 检测某个点的颜色                       |
| 同时满足 | `And` + `all_of` | 多个条件都满足才命中                   |
| 任意满足 | `Or` + `any_of`  | 满足一个就命中                         |

### 5.4 下一步的跳转逻辑

```json
"next": ["SellProductStartSelling", "SellProductTaskEnd"]
```

Pipeline 会**按顺序尝试**——先试第一个，不命中才试第二个。所以最可能出现的状态写在最前面。候选越多越好，能在一轮"截图 → 识别 → 动作"的循环中命中。

### 5.5 去哪查详细语法

- [MaaFramework Pipeline 协议](https://maafw.com/docs/3.1-PipelineProtocol/)——官方完整文档
- 最快的学习方式：打开 `assets/resource/pipeline/` 下别人写好的 JSON，看一行学一行

### 🎯 检查点

> - [ ] 知道每个节点 = 识别 → 操作 → 下一步
> - [ ] 知道 TemplateMatch 和 OCR 分别干什么
> - [ ] 知道 `next` 列表的尝试顺序
> - [ ] 打开项目里某个 Pipeline JSON，能大致看懂在干什么
> - [ ] 去看 `getting-started.md`，不再觉得是天书

---

## 第六章 · 你的第一次 PR（手把手）

> 任务：给项目贡献一张截图模板——零编程门槛，人人都能干。

### 第 1 步：Fork

1. 打开 [MaaEnd 仓库](https://github.com/MaaEnd/MaaEnd)
2. 点右上角 **Fork** 按钮
3. 不改任何东西，直接点 **Create fork**

等几秒就跳到了 `https://github.com/你的用户名/MaaEnd`——这是你自己的副本。

### 第 2 步：Clone 你自己的仓库

> 忘了 clone 是什么意思？→ [回第三章复习](#第三章--github-最小求生指南)

VS Code → `F1` → `Git: Clone` → 输入 **你 fork 的那个地址**，不是原版的那个。

### 第 3 步：创建分支

点左下角分支名 → "创建新分支" → `feat/add-template-xxx`

### 第 4 步：截图 + 放模板

1. 截图以 1280×720 为基准/推荐，但无需手动切换分辨率（framework 会自动缩放）
2. VS Code 里 `Ctrl+Shift+P` → `Maa: Screenshot`（需要装了 Maa Pipeline Support）
3. 在截图上框选你要识别的区域
4. 需要的话使用绿色掩码去除干扰识别的区域——将不希望匹配的部分涂绿 RGB: (0, 255, 0)，匹配时会跳过绿色区域。VS Code 装了 Maa Pipeline Support 插件后可以直接在截图上涂绿，不要手动 PS
5. 图片放到 `assets/resource/image/你的任务名/`

### 第 5 步：Commit

选一种你顺手的方式：

| 方式                 | 操作                                                       |
| -------------------- | ---------------------------------------------------------- |
| VS Code 界面         | `Ctrl + Shift + G` → 点 `+` 暂存 → 写 commit 消息 → 点 `✓` |
| 终端（第三章线路 B） | `git add .` 然后 `git commit -m "feat(任务名): 做了什么"`  |

### 第 6 步：Push

| 方式                 | 操作                                       |
| -------------------- | ------------------------------------------ |
| VS Code 界面         | 点左下角"同步更改"按钮                     |
| 终端（第三章线路 B） | `git push -u origin feat/add-template-xxx` |

### 第 7 步：开 PR（在网页）

1. 打开 [你 fork 的仓库](https://github.com/你的用户名/MaaEnd)，页面顶部会有黄色提示条 → 点 "Compare & pull request"
2. 确认 base 分支是 `v2`（原仓库的主分支），head 分支是你刚 push 的分支
3. 标题写清楚：`feat(任务名): 添加了某某按钮的识别模板`
4. 没做完选 "Create draft pull request"
5. 点 "Create pull request"

### 然后呢

- Maintainer 会审核，可能在评论区提修改意见
- 你在本地改完 → commit → push，PR 自动更新
- 审核通过就合进去了 🎉

### 完整流程回顾

```text
Fork 仓库
    ↓
Clone 你自己的仓库
    ↓
创建分支（开一条自己的线）
    ↓
截图 + 框选识别区域（Maa Pipeline Support 插件）
    ↓
把图片放到 assets/resource/image/ 对应文件夹
    ↓
Commit（存档）
    ↓
Push（上传）
    ↓
开 PR 请求审核
    ↓
等审核 ✅
```

### 🎯 检查点

> - [ ] Fork 了 MaaEnd
> - [ ] Clone 到了本地
> - [ ] 创建了分支
> - [ ] 放了一张截图模板
> - [ ] Commit + Push 成功
> - [ ] 在 GitHub 上看到了自己的 PR
> - [ ] 🎉 恭喜！人生第一个开源贡献！

---

## 接下来看什么

完成这篇入门后，按顺序往下走：

| 顺序 | 文档                                         | 学什么                                     |
| ---- | -------------------------------------------- | ------------------------------------------ |
| 1    | [getting-started.md](./getting-started.md)   | 搭环境、跑起来、完成一个完整 Pipeline 任务 |
| 2    | [components-guide.md](./components-guide.md) | 项目架构、可复用节点                       |
| 3    | [tools-and-debug.md](./tools-and-debug.md)   | 调试工具、Maa Pipeline Support 用法        |
| 4    | [coding-standards.md](./coding-standards.md) | 编码规范，提交前必读                       |

> [!NOTE] > **外部资源**
>
> 以下链接指向 MaaEnd 以外的独立项目或第三方服务，供拓展参考。

- [MaaFramework 官网](https://maafw.com/)——MaaEnd 底层框架
- [MaaFramework Pipeline 协议](https://maafw.com/docs/3.1-PipelineProtocol/)——所有节点的详细语法
- [DeepWiki — MaaEnd](https://deepwiki.com/MaaEnd/MaaEnd)——AI 驱动的第三方在线文档浏览器

需要帮助？

遇到不懂的先别慌，也别急着到处问。试试这个顺序：

1. **搜**——把报错信息或关键词丢进搜索引擎、[DeepSeek](https://www.deepseek.com/)，八成能直接找到答案
2. **看**——打开 `assets/resource/pipeline/` 下别人写好的 JSON，看一行学一行；MaaEnd 几千个节点里大概率已经有人写过你要的东西
3. **拆**——把问题拆小。不要问"这个任务怎么写"，问"怎么识别这个按钮""识别到之后怎么点"——拆到最小，每个小问题都更容易搜到
4. **试**——改个数字、删个字段，跑起来看效果。Pipeline 改不坏，跑崩了再改回来就行
5. **问**——以上都试过了还是卡住，再去群里或 Issue 里提问。提问时带上你试过什么、报了什么错、截图，别只丢一句"不work"

> 站原地等别人喂答案，和下水扑腾自己摸——差距比你想的大得多。

---

> [!NOTE] > **最后几句**
>
> 你不需要从头学到尾才开始动手。最好的学习方式：
>
> 1. 打开别人写好的 JSON 看
> 2. 改一个数字试试
> 3. 跑起来看效果
> 4. 出错了再查文档
>
> 站着看永远学不会游泳。下水吧
