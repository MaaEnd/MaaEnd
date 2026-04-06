# 开发手册 - 基建任务维护文档

本文用于说明 `DijiangRewards`（基建任务）的整体结构、四个阶段任务的职责，以及 `assets/tasks/DijiangRewards.json` 中各个 `interface` 选项对 Pipeline 的覆盖逻辑和设计原因，便于后续维护与扩展。  
该文档最近更新于 2026 年 4 月 7 日，已同步 [fix(GrowthChamber): 修复“再次种植”按钮可能会被忽略 (#2003)](https://github.com/MaaEnd/MaaEnd/pull/2003)

## 文件概览

当前实现主要分布在以下文件中：

| 模块           | 路径                                                                 | 作用                                                      |
| -------------- | -------------------------------------------------------------------- | --------------------------------------------------------- |
| 项目接口挂载   | `assets/interface.json`                                              | 将 `tasks/DijiangRewards.json` 挂到 `daily` 任务组        |
| 任务与选项定义 | `assets/tasks/DijiangRewards.json`                                   | 定义任务入口、界面选项、子选项和 `pipeline_override`      |
| 任务入口       | `assets/resource/pipeline/DijiangRewards/Entry.json`                 | 从任务入口进入帝江号总控中枢                              |
| 主流程分发     | `assets/resource/pipeline/DijiangRewards/MainFlow.json`              | 从总控中枢依次分发到四个子阶段                            |
| 恢复心情       | `assets/resource/pipeline/DijiangRewards/RecoveryEmotion.json`       | 处理总控中枢的好友助力恢复心情                            |
| 会客室         | `assets/resource/pipeline/DijiangRewards/ReceptionRoom.json`         | 处理线索收集、接收、放置、赠予、线索交流                  |
| 制造舱         | `assets/resource/pipeline/DijiangRewards/Manufacturing.json`         | 处理收菜、补货、助力                                      |
| 培养舱         | `assets/resource/pipeline/DijiangRewards/GrowthChamber.json`         | 处理领取、领奖后的再次种植分支、普通培养，以及提取基核    |
| 公共状态模板   | `assets/resource/pipeline/DijiangRewards/Template/Location.json`     | 维护各舱室界面定位节点                                    |
| 公共文本模板   | `assets/resource/pipeline/DijiangRewards/Template/TextTemplate.json` | 维护按钮/状态文本 OCR 模板                                |
| 补充状态模板   | `assets/resource/pipeline/DijiangRewards/Template/Status.json`       | 维护红点、数量、培养库存等辅助识别                        |

## 总体执行逻辑

任务入口是 `Entry.json` 中的 `DijiangRewards`：

1. 先通过 `SceneEnterMenuDijiangControlNexus` 进入帝江号总控中枢。
2. 命中 `MainFlow.json` 中的 `ControlNexus` 后，按 `next` 顺序依次尝试四个阶段：
    1. `[JumpBack]RecoveryEmotionMain`
    2. `[JumpBack]ReceptionRoomMain`
    3. `[JumpBack]MFGCabinMain`
    4. `[JumpBack]GrowthChamberMain`
3. 每个阶段完成后都回到 `InDijiangControlNexus`，再继续检查下一个阶段。
4. 当前面四个阶段都不再命中时，进入 `FinishDijiangRewards` 结束。

这里的设计重点是“总控中枢分发 + 子阶段回跳”：

- 四个阶段彼此独立，便于单独启用、禁用和维护。
- 每个阶段只关心“如何进入本舱室、完成本舱室逻辑、返回总控中枢”。
- `interface` 选项只需要覆盖各阶段入口节点或分支节点，不需要改主流程骨架。

## 四个阶段的职责

### 1. 恢复心情

`RecoveryEmotionMain` 在总控中枢识别“需要助力”红点后进入助力界面：

- 先点击“使用助力”。
- 再在“选择待恢复心情的干员”界面中寻找心情条有空缺的干员。
- 遇到“干员心情已满”或“没有更多的心情激励点数”时做收尾处理。
- 最终返回总控中枢。

这部分本质上是一个“在总控中枢消费助力点”的阶段任务。

### 2. 会客室

`ReceptionRoomMain` 进入会客室后，核心顺序是：

1. 先兜底处理“情报交流结束”弹窗。
2. 在 `ReceptionRoomViewIn` 中依次尝试：
    1. 收集线索
    2. 接收线索
    3. 放置/替换线索
    4. 开始线索交流
    5. 退出会客室

其中“赠予线索”并不是一个顶层阶段，而是线索溢出后的处理分支：

- 当线索库存已满时，进入 `ReceptionRoomSendCluesEntry`。
- 通过 `ClueItem` + `ClueItemCount` 找出满足阈值的线索。
- 再结合好友缺失颜色或直接发送按钮完成赠予。

### 3. 制造舱

`MFGCabinMain` 进入制造舱后，在 `MFGCabinViewIn` 中依次尝试：

1. 领取产出
2. 补货
3. 使用助力
4. 退出制造舱

这部分没有复杂选项覆盖，维护成本主要在按钮识别和收尾稳定性。

### 4. 培养舱

进入培养舱后，任务会先确认自己已经处在培养舱详情界面（`GrowthChamberMain` -> `GrowthChamberViewIn`），然后依次尝试：

1. 领取培养奖励
2. 如当前配置开启 `GrowAgain`，则在领奖关闭后尝试“再次种植”
3. 否则进入普通培养，或直接退出培养舱

培养舱是整个任务里最依赖 `interface` 覆盖的阶段。它的基础骨架没有很复杂，但很多行为都不是直接写死在 `GrowthChamber.json` 里，而是由 `assets/tasks/DijiangRewards.json` 在运行前改写。

如果不考虑任何界面选项，只看默认 Pipeline，培养舱的默认行为其实是“领奖 + 普通培养 + 退出”；“再次种植”分支默认关闭（`GrowthChamberGrowAgain` 默认 `enabled=false`），只有被 `interface` 显式开启后才会在领奖关闭后插入一次尝试。基于这个前提，默认流程可以拆成下面几步：

1. 先确认当前已经回到培养舱详情页（`GrowthChamberViewIn`）。
2. 如果画面上出现“全部收取”，就先领取作物（`GrowthChamberClaimReward`）。
3. 领奖完成后，会先关闭奖励界面（`GrowthChamberClaimRewardClose`）；只有在 `GrowAgain` 模式下，这里才会继续跳到 `GrowthChamberGrowAgain` 去尝试“再次种植”按钮。
4. 如果当前配置允许普通培养，并且能识别到“培养”按钮，就进入选材界面（`GrowthChamberGrow`）。这里不是批量培养，而是在 9 个培养目标中挑一个继续处理。
5. 进入选材界面后，会按这个顺序循环尝试（`GrowthChamberGrowViewIn`）：
    1. 如有需要，先调整排序方式或排序方向（`GrowthChamberSortBy`、`GrowthChamberSortOrder`）
    2. 在当前列表里寻找符合条件的目标（`GrowthChamberFindTarget`）
    3. 当前屏没找到就向下滚一屏继续找（`GrowthChamberTargetNotFound`）
    4. 如果已经没有可做动作，就返回培养舱详情页（`GrowthChamberReturn`）
6. 真正执行点击前，任务会先确认这一行同时满足两个条件：名称符合当前配置的目标范围，并且这一行的“作物数量”或“基核数量”至少有一个大于 0（`GrowthChamberSelectTarget` + `GrowthChamberCheckTargetNotEmpty`；后者默认等价于 `GrowthChamberCheckSeedNotEmpty` 或 `GrowthChamberCheckPlantNotEmpty`）。
7. 点击目标后，会根据后续画面进入三个互斥结果之一：
    1. 可以直接开始培养，就点确认开始培养（`GrowthChamberGrowConfirm`）
    2. 需要先补基核，就进入“前往提取基核”分支（`GrowthChamberSeedExtract`）
    3. 当前配置不允许提取基核，就直接退回列表（`GrowthChamberGrowExit`）
8. 一次处理完成后，会回到培养舱详情页或选材页，再继续判断是否还有后续动作（回到 `GrowthChamberViewIn` 或 `GrowthChamberGrowViewIn`）。

真正需要重点维护的是第 4 到第 6 步，因为这里的“排序谁来开、找什么目标、什么情况下允许点进去、点进去后要不要提取基核”，几乎都由 `interface` 选项决定。

可以把培养舱的 `interface` 选项理解成“2 个主决策 + 2 个补充决策”：

1. `SelectToGrow`：到底要不要培养，要培养谁。
2. `AutoExtractSeed`：目标缺基核时，是否允许走提取分支。
3. `SortBy`：仅在“任意材料”模式下，补充决定候选列表按什么规则排序。
4. `SortOrder`：仅在“任意材料”模式下，补充决定排序方向。

下面按界面选项对照说明实际动作。

#### `SelectToGrow` 决定“培养大方向”

这是培养舱的主开关。它先决定“进入培养舱以后，到底是只领奖、走领奖后的再次种植，还是进入普通选材培养”。

##### `SelectToGrow=DoNothing`

实际动作：

- 通过 `pipeline_override` 关闭“培养”入口（`GrowthChamberGrow.enabled=false`）。
- 结果是任务只会处理成熟奖励，不再进入选材界面。
- 奖励领完以后，培养舱阶段剩下的可执行动作基本就只剩退出。

也就是说，这个选项对应的用户行为其实是“只收成熟奖励，不做任何新的培养动作”。

##### `SelectToGrow=GrowAgain`

实际动作：

- 关闭“进入选材界面”的普通培养入口（`GrowthChamberGrow.enabled=false`）。
- 打开“再次种植”入口（`GrowthChamberGrowAgain.enabled=true`）。

对应到实际行为，就是在培养舱详情页不再进入“挑材料”的那条链，而是改成在领奖关闭后优先尝试：

1. 命中“再次种植”按钮
2. 点击后进入确认再次种植
3. 点击确认后直接回培养舱主界面

这里有一个本次更新后很容易漏掉的细节：

- `GrowthChamberGrowAgain` 不再直接挂在 `GrowthChamberViewIn.next` 里，而是由 `GrowthChamberClaimRewardClose.next` 触发。
- 这意味着 `GrowAgain` 模式优先处理的是“领奖之后立刻再次种植”的场景，而不是在培养舱详情页无条件直接点“再次种植”。

这个模式完全绕开选材列表，因此：

- 不会用到“目标名称筛选”（`GrowthChamberSelectTarget`）
- 不会用到 `AutoExtractSeed`
- 不会用到 `SortBy` 和 `SortOrder`

##### `SelectToGrow=Any`

这是默认模式，也是逻辑最复杂的一种。

实际动作：

- 把“可命中的目标名称”覆盖成全部可培养材料的多语言列表（`GrowthChamberSelectTarget.expected`）。
- 保留“进入选材界面”的普通培养分支（`GrowthChamberGrow`）。
- 继续向用户展开三个子选项：`AutoExtractSeed`、`SortBy`、`SortOrder`。

这意味着在 `GrowthChamberGrowViewIn` 中：

1. 可以先调整列表顺序。
2. 然后在“全量候选名单”里找当前屏最合适的目标。
3. 找到后再检查这行是否真的还有可用库存。

这里的“任意”不是随机点击，而是：

- 先用 `SortBy` / `SortOrder` 改变列表顺序。
- 再点击当前排序下最先命中的可用目标（`GrowthChamberFindTarget`）。

所以在维护时要记住，`Any` 模式的最终行为不只取决于候选名单，还取决于排序配置。

##### `SelectToGrow=具体材料`

例如 `Wulingstone`、`Igneosite`、`FalseAggela` 等 case，动作模式是一致的：

1. 把“目标名称筛选”收窄成这个材料自己的多语言名称集合（`GrowthChamberSelectTarget.expected`）。
2. 把“该行是否可继续处理”的判断改成更贴近当前目标行的识别方式（覆盖 `GrowthChamberCheckSeedNotEmpty.recognition`）。
3. 只向用户展开 `AutoExtractSeed`，不再显示排序项。

对应到实际动作就是：

- 进入选材界面后，不再“从所有候选里挑一个合适的”，而是“只找这一个名字”。
- 一旦找到目标行，就立即围绕这一个目标判断是否还能继续培养。

这里不展示 `SortBy`、`SortOrder` 的原因不是遗漏，而是设计上不需要：

- 固定材料模式的语义是“找到这个材料就处理它”。
- 排序只影响它出现在列表中的先后位置，不改变最终目标是谁。
- 因此排序不是业务语义的一部分，没必要暴露给用户。

#### `AutoExtractSeed` 决定“能不能接受缺基核目标”

这个选项只在 `SelectToGrow=Any` 或某个具体材料时出现，因为这两种模式都会真正进入选材流程。

##### `AutoExtractSeed=Yes`

实际动作：

- 打开“前往提取基核”这条分支（`GrowthChamberSeedExtract.enabled=true`）。
- 关闭“看到提取入口就直接返回列表”这条分支（`GrowthChamberGrowExit.enabled=false`）。
- 上面的识别条件完全相同,不同的点只在于命中后的结果流向

在默认识别逻辑下，“这行目标是否可处理”的判断（`GrowthChamberCheckTargetNotEmpty`）是：

- 这一行已经有可用基核
- 或这一行还有可提取成基核的作物本体（对应 `GrowthChamberCheckSeedNotEmpty` / `GrowthChamberCheckPlantNotEmpty`）

可以直接理解成下面两类例子：

- 例 1：某一行显示这个材料的基核数量是 `3`，作物本体数量是 `0`。这种情况下它已经具备直接培养的材料，会被视为“可处理目标”。
- 例 2：某一行显示这个材料的基核数量是 `0`，但作物本体数量是 `5`。这种情况下它暂时还不能直接培养，但因为还能先去提取基核，所以在 `AutoExtractSeed=Yes` 时也会被视为“可处理目标”。

换句话说，默认逻辑不是只找“已经能直接培养”的那一行，而是同时接受两种目标：

- 已经有基核，可以直接培养
- 只有本体库存，但还能先提取基核再培养

因此当 `AutoExtractSeed=Yes` 时，点击候选后的真实动作是：

1. 如果这行已经有可用基核，就直接确认开始培养（`GrowthChamberGrowConfirm`）
2. 如果只有本体、需要补基核，就进入提取基核（`GrowthChamberSeedExtract`）
3. 提取完成后，关闭奖励页并返回选材界面（`GrowthChamberSeedExtractClose`、`GrowthChamberGrowBack`）
4. 再继续下一次查找或培养

这相当于允许“本体转基核”的补料行为。

##### `AutoExtractSeed=No`

实际动作：

- 把“这行目标是否可处理”的判断收紧成“必须已经有基核”（`GrowthChamberCheckTargetNotEmpty` 只依赖 `GrowthChamberCheckSeedNotEmpty`）。
- 关闭提取基核分支（`GrowthChamberSeedExtract.enabled=false`）。
- 打开“看到提取入口就退回列表”分支（`GrowthChamberGrowExit.enabled=true`）。

这三个覆盖必须一起看，才能理解真正动作：

1. 先在查找阶段就排除“只有本体、没有基核”的目标。
   例如某一行显示“基核=0，本体=5”，在 `AutoExtractSeed=Yes` 时它还能算可处理目标；但在 `AutoExtractSeed=No` 时，这一行在查找阶段就会被过滤掉。
2. 即使点击后出现提取入口，也不允许走提取分支。
   例如某个目标因为识别波动或界面状态变化，还是被点进了“前往提取基核”的画面，此时任务不会继续点“提取”，而是把这条路当成不可执行分支。
3. 如果还是进入了提取入口，就直接返回列表，不再继续处理（`GrowthChamberGrowExit`）。
   例如本来想找“已经有基核”的目标，但实际点进某一行后弹出了提取入口，任务会立刻退回选材列表，继续找下一个目标，而不是在当前目标上继续消耗时间。
   这一个任务是fallback任务,部分情况下连续点击种植会直接返回培养舱主界面,需要这个任务引导回来

所以这个选项不是“找到目标后不点提取”这么简单，而是从筛选条件开始就把“需要提取基核的目标”尽量排除掉。

#### `SortBy` 决定“Any 模式下按什么顺序挑目标”

这个选项只在 `SelectToGrow=Any` 时出现，属于补充配置，不参与培养舱主流程分支判断。

它的作用很单纯：

- 任务在进入选材界面后，如有需要会先切换排序方式（`GrowthChamberSortBy`、`GrowthChamberSortByChoose`）。
- 排序方式只会影响“任意材料”模式下候选材料的前后顺序。
- 它不会改变任务是“领奖 / 普通培养 / 再次培养 / 退出”哪一条主分支，只会影响最终更容易先点到哪一个候选。

#### `SortOrder` 决定“排序方向”

`SortOrder` 同样只在 `SelectToGrow=Any` 时出现，也是排序的补充配置。

它的作用是：

- 在排序方式确定后，再决定列表按升序还是降序排列（`GrowthChamberSortOrder`）。
- 和 `SortBy` 一样，它只影响候选顺序，不改变主流程结构。
- 维护时只要记住：任务会在当前方向不符合目标时点一次切换按钮，把方向翻到用户配置的值。

#### 把这四个选项连起来看

如果把培养舱真正执行的动作按 `interface` 配置串起来，可以得到下面这套心智模型：

1. `SelectToGrow` 先决定是：
    1. 不培养
    2. 再次培养
    3. 普通培养
2. 如果进入普通培养流程，再细分为：
    1. 任意目标培养
    2. 固定材料培养
3. 如果进入普通培养流程：
    1. `Any` 模式下，先用 `SortBy` + `SortOrder` 决定列表顺序
    2. 再用“目标名称匹配规则”决定“找谁”（`GrowthChamberSelectTarget`）
    3. 再用 `AutoExtractSeed` 决定“只有基核可用算可处理”，还是“作物本体也能接受、后续可提取基核”
4. 找到目标后：
    1. 能直接培养就确认培养
    2. 需要提取基核且允许提取，就走提取分支
    3. 需要提取基核但不允许提取，就返回列表

所以维护者在看培养舱问题时，最好优先先问 3 个问题：

1. 当前 `SelectToGrow` 属于哪一类模式？
2. 当前模式下有没有排序覆盖？
3. 当前模式下 `AutoExtractSeed` 是否改变了候选筛选条件？

只要这三件事想清楚，培养舱大多数“为什么会点这个材料”“为什么没去提取”“为什么没有进入培养”之类的问题，基本都能顺着节点关系定位出来。

## interface 选项结构

`DijiangRewards` 在任务层只直接暴露 4 个顶层选项：

- `AutoStartExchange`
- `StageTaskSetting`
- `ClueSetting`
- `SelectToGrow`

其中后 3 个还是“父选项”：

- `StageTaskSetting=Yes` 时，继续显示 4 个阶段开关。
- `ClueSetting=Yes` 时，继续显示线索赠送次数和库存阈值。
- `SelectToGrow=Any` 时，继续显示提取基核和排序相关选项。
- `SelectToGrow=具体材料` 时，只继续显示 `AutoExtractSeed`。

这意味着当前任务的 `interface` 设计并不是把所有配置平铺给用户，而是先暴露高频决策，再按需要展开高级项。

## 选项覆盖逻辑与原因

下面按“选项做了什么”和“为什么这样做”两个角度说明。

### AutoStartExchange

| 配置  | 覆盖节点                     | 覆盖内容        | 原因                                                 |
| ----- | ---------------------------- | --------------- | ---------------------------------------------------- |
| `Yes` | `ReceptionRoomStartExchange` | `enabled=true`  | 允许基建任务在会客室内直接开启线索交流               |
| `No`  | `ReceptionRoomStartExchange` | `enabled=false` | 默认不主动开启线索交流，把开启时机留给信用点联动任务 |

设计原因：

- 线索交流会消耗会客室状态，和信用点获取链路耦合较强。
- 默认值设为 `No`，是为了把“开始交流”留给更高价值的信用点联动场景。
- 这个选项只改 `enabled`，因为它控制的是“做不做”，不是“怎么识别”。

### StageTaskSetting 及四个阶段开关

| 选项                   | 覆盖节点              | 覆盖内容             | 原因                                               |
| ---------------------- | --------------------- | -------------------- | -------------------------------------------------- |
| `StageTaskSetting=Yes` | 无直接 Pipeline 覆盖  | 只展开子选项         | 把“高级阶段控制”折叠起来，避免普通用户面对过多开关 |
| `RecoveryEmotionStage` | `RecoveryEmotionMain` | `enabled=true/false` | 控制是否执行恢复心情阶段                           |
| `ReceptionRoomStage`   | `ReceptionRoomMain`   | `enabled=true/false` | 控制是否执行会客室阶段                             |
| `ManufacturingStage`   | `MFGCabinMain`        | `enabled=true/false` | 控制是否执行制造舱阶段                             |
| `GrowthChamberStage`   | `GrowthChamberMain`   | `enabled=true/false` | 控制是否执行培养舱阶段                             |

设计原因：

- `ControlNexus` 的主流程已经天然按阶段分发，所以最稳妥的做法就是直接开关各阶段入口节点。
- 子阶段节点被关掉后，主流程仍保持不变，维护时不需要改 `MainFlow.json`。
- 默认 `StageTaskSetting=No`，让常规用户使用推荐全流程；维护者或高阶用户再按需细分。

### ClueSetting、ClueSend、ClueStockLimit

| 选项                 | 覆盖节点                            | 覆盖内容                          | 原因                                                     |
| -------------------- | ----------------------------------- | --------------------------------- | -------------------------------------------------------- |
| `ClueSetting=Yes`    | 无直接 Pipeline 覆盖                | 展开 `ClueSend`、`ClueStockLimit` | 允许用户自定义赠送策略                                   |
| `ClueSetting=No`     | `ReceptionRoomSendCluesSelectClues` | `max_hit=3`                       | 默认最多赠送 3 次，限制单次任务的赠送规模                |
| `ClueSetting=No`     | `ClueItemCount`                     | `expected=^(?:[3-9]\|[1-9]\\d+)$` | 默认单种线索库存大于等于 3 才赠送，也就是“每种保留 2 个” |
| `ClueSend`           | `ReceptionRoomSendCluesSelectClues` | `max_hit={MaxClueSend}`           | 把“最多赠送几次”直接映射到节点命中次数                   |
| `ClueStockLimit=1/2` | `ClueItemCount`                     | 改 OCR 正则阈值                   | 把“库存上限”落实到赠送目标筛选条件                       |

设计原因：

- 会客室赠送逻辑的核心筛选点就在 `ReceptionRoomSendCluesSelectClues`，因此次数限制直接改 `max_hit` 最自然。
- 库存阈值本质上是“哪些线索算溢出”，所以直接改 `ClueItemCount.expected` 的正则，而不是额外引入新的判断节点。
- `ClueSetting=No` 时仍显式写默认覆盖，是为了把“隐藏高级项”和“使用默认策略”绑定在一起，避免子选项不显示时行为不透明。

默认策略可以理解为：

- 每种线索至少保留 2 个。
- 一次基建任务最多赠送 3 次。

### SelectToGrow

这是培养舱维护里最关键的选项。它不只是“选目标”，还决定后续哪些子选项需要出现。

#### 1. `DoNothing`

| 覆盖节点            | 覆盖内容        | 原因                         |
| ------------------- | --------------- | ---------------------------- |
| `GrowthChamberGrow` | `enabled=false` | 只领培养奖励，不进入培养流程 |

这是最直接的“关闭培养行为”模式。

#### 2. `GrowAgain`

| 覆盖节点                 | 覆盖内容        | 原因                             |
| ------------------------ | --------------- | -------------------------------- |
| `GrowthChamberGrow`      | `enabled=false` | 避免与普通培养入口冲突           |
| `GrowthChamberGrowAgain` | `enabled=true`  | 在领奖关闭后启用“再次种植”分支   |

设计原因：

- `GrowthChamberViewIn` 现在只直接挂 `GrowthChamberClaimReward`、`GrowthChamberGrow` 和 `GrowthChamberExit`；`GrowthChamberGrowAgain` 改为由 `GrowthChamberClaimRewardClose.next` 触发。
- `GrowAgain` 仍然必须显式关闭普通培养，避免领奖关闭后或后续回到详情页时，又命中“培养”按钮而走回普通培养链。

#### 3. `Any`

| 覆盖节点                    | 覆盖内容                                 | 原因                               |
| --------------------------- | ---------------------------------------- | ---------------------------------- |
| `GrowthChamberSelectTarget` | 把 `expected` 改成全材料多语言列表       | 允许在候选列表里匹配任意可培养目标 |
| 展开子选项                  | `AutoExtractSeed`、`SortBy`、`SortOrder` | 任意模式下需要额外决定选材顺序     |

设计原因：

- “任意”模式不关心具体材料名，而关心“从列表中稳定挑出一个可培养目标”。
- 因为候选很多，排序方式和排序顺序会直接影响最终挑中的目标，所以只在 `Any` 模式下暴露 `SortBy`、`SortOrder`。
- 具体材料模式已经通过名称精确锁定目标，排序不会改变最终语义，因此没必要把排序选项继续暴露给用户。

#### 4. `具体材料`

每个具体材料 case 都做两件事：

1. 把 `GrowthChamberSelectTarget.expected` 缩小到该材料的多语言名称集合。
2. 覆盖 `GrowthChamberCheckSeedNotEmpty` 的识别定义。

第一点很好理解，第二点是维护时容易忽略的重点。

设计原因：

- 固定材料模式下，最重要的是把点击框稳定绑定到指定材料那一行。
- 这类 case 会把 `GrowthChamberCheckSeedNotEmpty` 改成基于 `GrowthChamberSelectTarget` 的整行识别，降低对小数字 OCR 的依赖，避免因为数量识别抖动导致找不到目标行。
- 同时只展开 `AutoExtractSeed`，因为固定目标时不需要再通过排序影响选择结果。

如果后续新增材料 case，必须同步维护：

- `SelectToGrow.cases.*.pipeline_override.GrowthChamberSelectTarget.expected`
- 对应的多语言名称
- 是否仍需要沿用当前的 `GrowthChamberCheckSeedNotEmpty` 覆盖策略

### AutoExtractSeed

| 配置  | 覆盖节点                           | 覆盖内容                                    | 原因                                 |
| ----- | ---------------------------------- | ------------------------------------------- | ------------------------------------ |
| `Yes` | `GrowthChamberSeedExtract`         | `enabled=true`                              | 允许在无基核时进入提取基核流程       |
| `Yes` | `GrowthChamberGrowExit`            | `enabled=false`                             | 避免命中“不提取直接返回”分支         |
| `No`  | `GrowthChamberCheckTargetNotEmpty` | 改为只要求 `GrowthChamberCheckSeedNotEmpty` | 不提取基核时，只应选择已有基核的目标 |
| `No`  | `GrowthChamberSeedExtract`         | `enabled=false`                             | 禁止进入提取基核分支                 |
| `No`  | `GrowthChamberGrowExit`            | `enabled=true`                              | 在“前往提取基核”界面直接返回列表     |

设计原因：

- 默认的 `GrowthChamberCheckTargetNotEmpty` 是“基核够 or 本体够”，因为允许通过本体去提取基核。
- 一旦关闭 `AutoExtractSeed`，这个判断就必须收紧成“基核够才行”，否则会点进无法继续培养的目标。
- 所以 `AutoExtractSeed=No` 不只是关掉 `GrowthChamberSeedExtract`，还要同步改筛选条件。

### SortBy 与 SortOrder

这两个选项只在 `SelectToGrow=Any` 时出现。

#### SortBy

| case                                                    | 覆盖节点                    | 覆盖内容                                   | 原因                                         |
| ------------------------------------------------------- | --------------------------- | ------------------------------------------ | -------------------------------------------- |
| `Default` / `AmountOwned` / `AmountOfSeeds` / `Quality` | `GrowthChamberSortBy`       | 修改当前排序按钮应识别到的“非目标选项”文本 | 只有在当前排序不是目标值时才点击展开排序面板 |
| 同上                                                    | `GrowthChamberSortByChoose` | 修改排序面板里要点击的目标文案             | 指向真正要切换成的排序方式                   |
| `Default`                                               | `GrowthChamberSortBySwipe`  | `enabled=false`                            | 默认项通常无需额外滚动                       |

设计原因：

- `GrowthChamberSortBy` 不是“无脑点一下”，而是先判断当前是否已经是目标排序。
- 所以 case 覆盖的不是单个文案，而是一组“当前状态允许匹配”的文案，确保只有在需要切换时才执行点击。
- `GrowthChamberSortByChoose` 再负责命中具体目标项，实现“先判断，再切换”。

#### SortOrder

| case   | 覆盖节点                          | 覆盖内容                    | 原因                                   |
| ------ | --------------------------------- | --------------------------- | -------------------------------------- |
| `ASC`  | `GrowthChamberSortOrder.expected` | 识别当前按钮上是“降序/DESC” | 因为想要升序时，只在当前是降序时才点击 |
| `DESC` | `GrowthChamberSortOrder.expected` | 识别当前按钮上是“升序/ASC”  | 因为想要降序时，只在当前是升序时才点击 |

设计原因：

- 这里识别的是“当前状态”，不是“目标状态”。
- 只有识别到当前顺序与目标不一致时才点击，能避免来回切换。

## 维护时最容易漏掉的同步点

### 1. 改了阶段入口，别只改子文件

如果新增或重构阶段任务，除了对应的 Pipeline 文件，还要同步检查：

- `MainFlow.json` 中 `ControlNexus.next`
- `assets/tasks/DijiangRewards.json` 中是否需要新增阶段开关
- `assets/locales/interface/*.json` 中的文案

### 2. 改了线索默认策略，记得同时改“隐藏高级项”分支

当前默认策略并不是写在 Pipeline 原始节点里，而是写在：

- `ClueSetting=No` 的 `pipeline_override`

如果只改 `ClueSend` 或 `ClueStockLimit` 的子选项而不改 `ClueSetting=No`，会导致：

- 高级选项展开时是一套行为
- 高级选项隐藏时又是另一套行为

### 3. 培养舱选项之间是联动的，不要只关单个节点

培养舱至少有三类联动：

- `SelectToGrow` 决定走普通培养、再次培养、任意培养还是固定材料培养。
- `AutoExtractSeed` 决定能否接受“只有本体没有基核”的目标。
- `SortBy` / `SortOrder` 决定任意模式下的挑选顺序。

如果改其中一项，最好顺手检查：

- `GrowthChamberViewIn.next`
- `GrowthChamberClaimRewardClose.next`
- `GrowthChamberFindTarget`
- `GrowthChamberCheckTargetNotEmpty`
- `GrowthChamberSeedExtract`
- `GrowthChamberGrowExit`

### 4. 多语言文案要和 OCR 列表一起维护

本任务大量依赖 OCR：

- 舱室标题
- 按钮文本
- 线索名称
- 培养目标名称
- 排序项名称

只改 `assets/locales/interface/*.json` 的显示文案并不够。若游戏文案或翻译更新，还要同步检查：

- `Template/Location.json`
- `Template/TextTemplate.json`
- `GrowthChamber.json` 中各 case 覆盖的 `expected`

## 推荐的理解方式

维护 `DijiangRewards` 时，建议把它分成三层：

1. **主流程层**：`Entry.json` + `MainFlow.json`，负责“去哪一个舱室”。
2. **阶段业务层**：`RecoveryEmotion.json`、`ReceptionRoom.json`、`Manufacturing.json`、`GrowthChamber.json`，负责“这个舱室要做什么”。
3. **界面配置层**：`assets/tasks/DijiangRewards.json`，负责“哪些阶段启用、哪些分支启用、哪些识别条件改写”。

这样看会比较容易定位问题：

- 进不去基建，看主流程层。
- 进了舱室做错事，看阶段业务层。
- 同一任务在不同选项下行为不一致，看界面配置层。

## 自检清单

修改后至少检查以下几项：

1. `assets/interface.json` 是否仍然导入了 `tasks/DijiangRewards.json`。
2. `ControlNexus.next` 是否与阶段入口节点保持一致。
3. 新增或修改的选项是否在 `assets/locales/interface/*.json` 中补齐文案。
4. 若修改线索赠送规则，`ClueSetting=No` 的默认覆盖是否同步更新。
5. 若修改培养目标逻辑，`SelectToGrow`、`AutoExtractSeed`、`SortBy`、`SortOrder` 是否仍然语义一致。
6. 若新增固定材料，是否补齐多语言名称并确认目标行绑定仍然稳定。
