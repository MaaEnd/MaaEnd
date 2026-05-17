# Git 常用操作总结、速查

参与 MaaEnd 开发时最常用的 Git 命令。

> **提示**：这是针对[超基础开发文档](super-basic-introduction.md)的命令补充。每条命令对应下一行的文字解释。
> **提示**：如果对 Git 完全陌生，先玩 [Learn Git Branching](https://learngitbranching.js.org/)。

---

## 远程仓库

```bash
git remote -v
```

查看配置是否正确，确认 `origin`（你的 fork）和 `upstream`（上游仓库）是否已设置。

```bash
git remote add upstream https://github.com/MaaEnd/MaaEnd.git
```

添加上游仓库（如果还没有）。

---

## 同步上游

```bash
git fetch upstream
```

根据上游仓库的情况更新本地，适合大部分情况。

```bash
git merge upstream/v2
```

更新本地主 v2 分支，让你的 fork 与上游仓库一致。

```bash
git pull myfork v2
```

根据你 fork 的仓库内容更新，_需要 myfork 仓库与上游仓库一致_。

---

## 克隆与子模块

```bash
git clone --recursive 仓库链接
```

克隆仓库到本地，\*`--recursive` 会一并拉取子模块，**必不可少\***。

```bash
git submodule status
```

检查子模块的状况。

```bash
git submodule update --init --recursive
```

下载所有子模块。\*遇到模型缺失或"幽灵修改"时，**先执行这条命令\***。

---

## 分支

```bash
git checkout -f 分支名
```

**创建并切到分支**。_`-f` 会舍弃所有未提交的改动。_

```bash
git checkout 分支名
```

切换到已有分支。

---

## 重置本地内容

```bash
git reset --hard HEAD
```

\*⚠️ 舍弃所有本地的改动，恢复到上次拉取或更新的状态。**不可撤销！\***

## 正常开发的最后一个流程

## Commi存档

```bash
git add .                                                # 暂存所有改动
git commit -m "feat(任务名): 做了什么"                     # 存档 + 写备注
```

commit 消息格式见下方。如果只想存档某几个文件，把 `git add .` 换成 `git add 文件路径`。

## 上传到 GitHub

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

## 开 PR

push 完之后，打开浏览器访问 `https://github.com/你的用户名/MaaEnd`，页面顶部会有黄色提示条 "xxx had recent pushes"，点 **Compare & pull request**。标题写清楚，没做完勾上 **Create draft pull request**，点 **Create pull request**。

---

## Commit 消息格式（两条线通用）

本项目遵循 [约定式提交（Conventional Commits）](https://www.conventionalcommits.org/zh-hans/v1.0.0/)，详见 [getting-started.md § 0. 提交规范](./getting-started.md)。下面是常用前缀速查：

| 前缀     | 什么时候用                            |
| -------- | ------------------------------------- |
| `feat:`  | 新增功能（Pipeline 节点、识别模板等） |
| `fix:`   | 修复 Bug                              |
| `docs:`  | 仅文档更改                            |
| `style:` | 格式/空白调整（不影响代码含义）       |
| `chore:` | 构建、依赖等杂项                      |

示例：`feat(SellProduct): 添加售货按钮识别模板`、`fix: 修复启动崩溃`。
