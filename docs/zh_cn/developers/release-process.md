# 发版流程

本文档说明 MaaEnd 的发布分支模型、PR 目标分支选择，以及维护者的发版操作流程。

---

## 普通开发者

以下内容所有贡献者都需要了解。主要是 **PR 往哪个分支提**。

### 分支模型

| 分支           | 用途                               | 允许合入的类型                                           |
| -------------- | ---------------------------------- | -------------------------------------------------------- |
| `v2`           | 主开发分支                         | `feat` `refactor` `perf` `fix` `docs` `chore` … 所有类型 |
| `release/vX.Y` | 当前发布分支（如 `release/v2.16`） | **仅 `fix`**                                             |

> 日常开发（新功能、重构、修 Bug）默认往 `v2` 分支提。`release/vX.Y` 只在特定场景下使用。

### 我该往哪个分支提 PR？

``` text
你的改动是什么？
  ├─ 新功能 / 重构 / 日常修复  → 往 v2 分支提
  └─ 修复影响正式版（Stable）的 Bug   → 往 release/vX.Y 分支提
```

**简单判断：这个 Bug 在最新正式版里也存在吗？**

- 不存在（只出现在公测版）→ `v2`
- 存在 → `release/vX.Y`

> 如果往 `release/vX.Y` 分支提了 fix PR，合入后 CI 会**自动**把修复 cherry-pick 回 `v2` 并发起 PR，**不需要你提两个 PR**。

### 分支命名

- 功能分支：`feat/简短描述`（如 `feat/auto-sell-items`）
- 修复分支：`fix/简短描述`

---

## 维护者

以下内容仅供维护者（有发版权限的人）参考。

### 发版周期

``` text
每天       Beta（vX.Y.Z-beta.N）→ 基于 v2（周四不发 Beta，发 RC）
    周四   RC（vX.Y.Z-rc.N）→ 基于 v2
           ├─ CI 自动创建 release/vX.Y 分支
           └─ 此后 release 分支只接受 fix PR
    周五   Stable（vX.Y.0）→ 基于 release/vX.Y
           ├─ CI 自动清理旧 release/v* 分支
           └─ 可按需发 Hotfix（vX.Y.1, vX.Y.2 …）
```

### 发版操作

1. **Beta / RC**：在 `v2` 上打 tag（如 `v2.17.0-beta.1`、`v2.17.0-rc.1`），推送即可触发 CI 构建和发布
2. **Stable**：在 `release/vX.Y` 分支上打 tag（如 `v2.17.0`），推送后触发正式版构建和发布
3. **Hotfix**：修复合入 `release/vX.Y` 后，在 release 分支上打新 tag（如 `v2.17.1`）

### 自动化

| 触发事件                 | 自动行为                                      | 工作流                      |
| ------------------------ | --------------------------------------------- | --------------------------- |
| RC 版本发布              | 创建 `release/vX.Y` 分支                      | `create-release-branch.yml` |
| Stable `.0` 发布         | 删除旧 `release/v*` 分支（保留有 open PR 的） | `create-release-branch.yml` |
| Fix PR 合入 release 分支 | Cherry-pick 到 `fix/pr-N` 并向 `v2` 发起 PR   | `cherry-pick-to-v2.yml`     |

### Hotfix 流程

1. 基于 `release/vX.Y` 创建 fix 分支，提交修复
2. 提 PR 到 `release/vX.Y`，合入
3. CI 自动 cherry-pick 到 `v2`，在 v2 上出现一条 `fix: cherry-pick #N from release/vX.Y to v2` 的 PR——**直接合入即可**
4. 在 release 分支上打新 tag（如 `v2.16.1`）触发发版

### Cherry-pick PR 有冲突怎么办

手动解决冲突，向 `fix/pr-N` 分支推送修复，然后合入。

### 旧 release 分支何时删除

新 Stable `.0` 发布时自动删除。删除前会检查是否有未合入的 open PR，有则跳过（但应尽快处理掉）。

### 注意事项

- Stable `.0` 的 tag **必须在 release 分支上打**，否则 changelog 不会包含 hotfix 修复
- Hotfix tag（`vX.Y.Z`，Z>0）不会触发清理，release 分支继续存活
- 自动 cherry-pick 的 commit 带有 `[skip changelog]`，不会在后续版本的 changelog 中重复出现
