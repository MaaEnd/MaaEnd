# 开发手册 - 存放与取回背包维护文档

本文说明 `StashBackpack`、`RetrieveBackpack` 与内嵌存放能力的状态生命周期和维护边界。
该文档更新于 2026 年 8 月 31 日。

## 支持范围

- 独立的存放、取回任务当前仅支持 `Win32-Front`。
- 物品存放和取回统一使用 `Shift + Click`。ADB 资源保留 `AutoShiftClickAction` 覆盖入口，并通过 `FalseAction` 显式失败，防止错误退化成普通点击。
- Pipeline 负责界面导航、分类切换、滚动和物品移动；Go Service 只维护有序快照、差集队列和当前目标。

## 文件分布

| 路径 | 作用 |
| --------------------------------------------------------------------- | ------------------------------------ |
| `assets/tasks/StashBackpack.json` | 两个独立任务及其选项 |
| `assets/resource/pipeline/StashBackpack.json` | 存放主流程与内嵌入口 |
| `assets/resource/pipeline/StashBackpack/Snapshot.json` | 背包真实快照 |
| `assets/resource/pipeline/StashBackpack/Search.json` | 背包、仓库分页搜索 |
| `assets/resource/pipeline/StashBackpack/Category.json` | 手动存放分类门控 |
| `assets/resource/pipeline/StashBackpack/Retrieve.json` | 取回流程与分类门控 |
| `agent/go-service/stashbackpack/` | 快照、差集、目标队列和批次状态 |
| `tools/schema/components/stash_backpack.schema.json` | Custom 组件参数约束 |
| `assets/locales/interface/*.json` | 任务、仓库和分类文案 |

## 快照生命周期

快照只保存按背包网格顺序排列的 `item_id`、`category_type` 和分页合并后重新编号的逻辑 `row` / `column`，不保存数量或屏幕坐标。逻辑行列只用于保持稳定顺序，禁止用于推算拖放坐标。每批背包变更后必须重新滚动并生成真实快照，不能用移动结果推导新列表。

| 方案名 | 实现名称 | 含义 |
| ------ | ------------------ | -------------------------------- |
| `S0` | `s0` | 存放任务开始前的背包 |
| 中间态 | `working` | 基础存放完成、补充可用道具前的背包 |
| `S1` | `s1` | 存放任务全部完成后的背包 |
| `T` | `temporary` | 宿主任务或取回任务当前背包的临时真实快照 |
| `R1` | `retrieve_current` | 存放新增物品后重新扫描的背包 |

完整存放任务只有在 `s0`、`s1` 均扫描完成并执行 `complete_full` 后才发布可用状态。中断留下的半成品不得被取回或宿主任务使用。同一任务队列重复执行完整存放任务时会输出红色警告并成功跳过，避免覆盖后续任务依赖的快照。

取回顺序固定为：

1. 生成临时快照 `T`。
2. 可选存放 `T - S1`，即存放任务结束后新增的物品。
3. 重新真实扫描生成 `R1`。
4. 从仓库取回 `S0 - R1`，并按用户勾选的分类门控。

差集是保留 `S0` 网格顺序的多重集合差集，同一 `item_id` 出现多次时不能先去重。

## 存放新获得物品

`StoreNewItemsWithStashBackpackSubTask` 供 AutoCollect、AutoEcoFarm 和 GiftOperator 在获得物品后调用：

1. 先确认当前控制器是 Win32，且本批次已有完整 `S0/S1`；不满足时输出红色警告并成功跳过，不影响宿主任务。
2. 进入仓库后覆盖临时快照 `T`，始终准备 `T - S1`，不建立基线，也不覆盖 `S0/S1`。
3. 对每个目标执行 `Shift + Click`，只复核点击前记录的背包源格；该格不再是目标物品后才消费目标，禁止用全背包查无触发往返滚动。
4. MXU 在一批顶层任务全部结束后停止 Agent 进程，因此 Go 进程内状态天然限制在一个任务批次内；同批顶层任务共享完整快照。

完整存放任务会同时记录用户选择的仓库；同批次的取回和内嵌存放自动复用该仓库，不再要求重复选择。

取回同样使用 `Shift + Click`，但仓库源格可能因剩余数量不为零而继续存在，不能沿用存放判据。取回前通过当前背包页物品的 `row` / `column` 连续性定位首个空格；仅当当前页 4×5 网格全满时才继续向下翻页。动作后只在该空格内识别当前目标物品，命中后才消费目标。

## 识别与搜索约束

- 快照扫描使用 `IconRecognition` 的 `item_filters: ["Normal:*"]`。
- 背包反查使用当前 `item_id` 与 `item_recheck_filters: ["Normal:*"]`。
- 仓库反查使用当前 `item_id` 与具体 `Normal:<Category>`。
- 连续找物品时先从当前位置向后搜索；到底仍未找到时回顶完整扫描。不要把每个物品都直接回顶，避免重复滚动。
- 存放后的验证只检查点击源格；取回后的验证只检查预先定位的目标空格，两者都不得调用全列表查无流程。
- 每次扫描通过最大“已有后缀 / 新页前缀”重叠合并分页，既消除相邻页重叠，又保留背包内真实重复项。

## 扩展分类时

新增或调整分类必须同步检查：

1. `assets/tasks/StashBackpack.json` 的手动存放与取回 checkbox。
2. `Category.json` 与 `Retrieve.json` 的分类门控和仓库分类切换节点。
3. `stash_backpack.schema.json` 的 `Category` 枚举。
4. `assets/locales/interface/*.json` 的五语言分类文案。
5. `IconRecognition` 数据中的 `categoryType` 与仓库 `Normal:<Category>` 是否一致。

完成修改后运行 `pnpm format`、`pnpm format:go`、`pnpm check` 和 `pnpm test`。
