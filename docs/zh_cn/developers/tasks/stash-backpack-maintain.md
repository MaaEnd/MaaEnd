# 开发手册 - 存放背包维护文档

本文说明 `StashBackpack` 的文件分布与两条存放路线。  
该文档更新于 2026 年 11 月 14 日。

存放背包有两个模式：一键存放，和只存放数量不超过 4 的可用道具。后者要在整理之后逐个快移，节点链更长，踩过的坑也都在那边。

## 文件路径

- 任务定义：`assets/tasks/StashBackpack.json`
- 主流程：`assets/resource/pipeline/StashBackpack.json`
- ADB 覆盖：`assets/resource_adb/pipeline/StashBackpack.json`

## 主流程

`StashBackpackSubTask` 是给别的任务当子任务用的入口，用它把 `StashBackpackMain` 拉起来。
`StashBackpackMain` 先认 `StashBackpackEnterSuccess`，认不到就 `[JumpBack]SceneEnterMenuBackpackWithDepot` 进背包。选了 `ValleyIV` 或 `Wuling` 地区时，这个回退节点会被换成 `SceneEnterMenuBackpackWithDepotValleyIV` / `SceneEnterMenuBackpackWithDepotWuling`，排查导航问题时要按实际选项看。

进入背包后 `StashBackpackEnterSuccess` 用 SubTask 依次拉起两个模式，最后跑 `StashBackpackResetUsableItemsLimitedHitCount` 把拖动次数清零，保证下次运行还有完整的十次。

两个模式各有一个包装节点，内容节点默认 `enabled: false`，靠选项打开：

- 一键存放：`StashBackpackStashQuickSubTask` 拉起 `StashBackpackStashQuick`
- 只存 ≤4：`StashBackpackUsableItemsLimitedSubTask` 拉起 `StashBackpackUsableItemsLimited`

一键存放本身很简单：在 `[871, 472, 162, 125]` 里找 `StashBackpack/QuickStash.png`，按分数排序点击，点完等 200 毫秒画面静止。

## 只存 ≤4 这一条

`StashBackpackUsableItemsLimited` 先执行 SubTask 调 `StashBackpackSort` 点一次整理，然后按顺序试下面三个：

1. `StashBackpackUsableItemsStoreFull` —— OCR 认仓库满横幅
2. `[JumpBack]StashBackpackUsableItemsLimitedDrag` —— 找数量 0 到 4 的物品并按住 Shift
3. `StashBackpackSort` —— 再整理一次兜底

`StashBackpackUsableItemsStoreFull` 没有 `next`，认到就地结束。这个节点原意是「认出仓库满就收工」，它认不出来，丢的不只是一句提示，而是整条出口。

`StashBackpackUsableItemsLimitedDrag` 是 `And` 识别：先用模板在背包区找可用道具图标，再在图标下方偏移 `[20, 44, 4, -2]` 的位置 OCR 一位数字，`box_index: 1` 取的是数字那个框。`max_hit: 10`，一次运行最多拖十件。

拖完接 `QuickMoveClick`，点击位置取拖动框再向上偏 24 像素（`target_offset: [0, -24, 0, 0]`），再 `QuickMoveKeyUp` 放开 Shift 并等画面静止。

## 两个坑

### 整理回流比静止判定慢

`StashBackpackSort` 的 `post_wait_freezes` 是 200 毫秒，区域 `[741, 116, 385, 416]`。这是「等画面不动」，不是硬等。

问题在于整理引起的回流在有些机器上要一秒多才开始。200 毫秒时画面还是旧的，静止判定提前放行，紧接着的拖放就会认到整理前的格子，把别的物品当成目标点下去。

7280 那次把等待区域扩大了，时长没动，这个窗口还在。

### 仓库满的横幅认不出来

`StashBackpackUsableItemsStoreFull` 用 OCR 认「目标空间已满，部分物品未能完全移动」，roi 是 `[319, 0, 601, 122]`，屏幕顶部一条。

7284 的日志里，这个节点十一次心跳全部失败，读到的只有 `M`、`T`、`CN`、`4` 这类碎片。认不出来，流程就继续拖，`max_hit` 会被同一件存不进的物品耗光，最后落到兜底的整理节点，任务报成功但还剩一堆没搬。

## ADB 的差异

ADB 那份覆盖动了四个地方，和 PC 不是一个实现：

- `StashBackpackEnterSuccess` 不用 `InBackpack`，改成用 `StashBackpack/QuickStash.png` 模板确认已经在背包里
- `StashBackpackUsableItemsLimitedDrag` 不按 Shift，改用 `TouchDown` 触点 4，`auto_up` 让框架自己抬手
- `StashBackpackUsableItemsLimitedQuickMoveClick` 完全不点格子，改成找「转移到仓库」图标，用 `RepeatUntilNotFoundAction` 反复点，最多五次
- `StashBackpackUsableItemsLimitedQuickMoveKeyUp` 跟着换成 `TouchUp` 释放触点 4，不再是释放 Shift

第三个节点要注意上限：`repeat_count` 是 5、`interval_ms` 是 500。图标点五次还在，动作直接判失败，不会继续试。排查「物品没搬过去」时先数这条命中了几次。

所以调参数前先确认 ADB 有没有自己那份，别只改一边。

## 调试

按 `docs/zh_cn/developers/node-testing.md` 单独跑节点。重点看这几处：

- 拖放认出的是哪个格子，看 `Recognizer` 打出来的 `box`
- `StashBackpackUsableItemsStoreFull` 有没有出现过 `Recognition.Succeeded`，一次都没有就说明这条出口是空的
- 每轮心跳的 `next` 列表里它是不是一直排在第一个、一直失败
- `StashBackpackUsableItemsLimitedDrag` 的命中次数有没有被提前耗光，日志里数 `Node.Action.Starting` 出现几次
