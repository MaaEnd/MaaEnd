# 开发手册 - 存放与取回背包维护文档

本文说明 `StashBackpack`、`RetrieveBackpack` 与内嵌存放能力的状态生命周期和维护边界。
该文档更新于 2026 年 9 月 8 日。

## 支持范围

- 存放和取回已合并为同一任务的选项，当前仍仅支持 `Win32-Front`；内嵌存放也保留 Win32 门控。
- 物品存放和取回统一调用 `InventoryTransferStackAction`，桌面端为 `Shift + Click`。公共动作已提供 ADB 手势，但这不代表背包任务的导航、识别和滚动已完成 ADB 适配。接口与清理约束见 [Inventory 文档](../../../../agent/go-service/common/inventory/README.md)。
- Pipeline 负责业务流程、界面导航、分类切换和物品移动；Go Service 封装完整快照扫描、维护差集队列和页级验证状态。

## 文件分布

| 路径 | 作用 |
| --------------------------------------------------------------------- | ------------------------------------ |
| `assets/tasks/StashBackpack.json` | 合并后的存放、取回选项 |
| `assets/resource/pipeline/StashBackpack.json` | 存放主流程与内嵌入口 |
| `assets/resource/pipeline/StashBackpack/Snapshot.json` | 背包真实快照 |
| `assets/resource/pipeline/StashBackpack/Search.json` | 背包、仓库分页搜索 |
| `assets/resource/pipeline/StashBackpack/Category.json` | 手动存放分类门控 |
| `assets/resource/pipeline/StashBackpack/Retrieve.json` | 取回流程与分类门控 |
| `agent/go-service/stashbackpack/` | 快照、差集、目标队列和批次状态 |
| `tools/schema/components/stash_backpack.schema.json` | Custom 组件参数约束 |
| `assets/locales/interface/*.json` | 任务、仓库和分类文案 |

## 快照生命周期

快照的合并列表保存 `item_id`、`category_type` 和重新编号的逻辑 `row` / `column`，同时保留各页识别结果供取回计数建立基线。逻辑行列只用于保持稳定顺序，禁止据此推算空格或点击坐标；操作目标来自当前页识别框。计数指同一物品占据的格子数，不是单格堆叠数量。需要背包真实状态时扫描生成快照；取回验证则复用初始快照，并在成功后更新内存计数。

| 方案名 | 实现名称 | 含义 |
| ------ | ------------------ | -------------------------------- |
| `S0` | `s0` | 一键存放后、手动存放前的背包；一键存放物品不纳入取回范围 |
| 中间态 | `working` | 基础存放完成、补充可用道具前的背包 |
| `S1` | `s1` | 存放任务全部完成后的背包 |
| `T` | `temporary` | 宿主任务或取回任务当前背包的临时真实快照 |
| `R1` | `retrieve_current` | 存放新增物品后的背包；无变更时复用 `T` |

完整存放任务只有在 `s0`、`s1` 均扫描完成并执行 `complete_full` 后才发布可用状态。中断留下的半成品不得被取回或宿主任务使用。同一任务队列重复执行完整存放任务时会输出红色警告并成功跳过，避免覆盖后续任务依赖的快照。

取回顺序固定为：

1. 生成临时快照 `T`。
2. 可选存放 `T - S1`，即存放任务结束后新增的物品。
3. 背包发生变更时重新扫描生成 `R1`，否则复制 `T`。
4. 从仓库取回 `S0 - R1`，并按用户勾选的分类门控。

差集是保留 `S0` 网格顺序的多重集合差集，同一 `item_id` 出现多次时不能先去重。

## 存放新获得物品

`StoreNewItemsWithStashBackpackSubTask` 供 AutoCollect、AutoEcoFarm 和 GiftOperator 在获得物品后调用：

1. 先确认当前控制器是 Win32，且本批次已有完整 `S0/S1`；不满足时输出红色警告并成功跳过，不影响宿主任务。
2. 进入同批次选定的仓库，按原设置先执行可选的一键存放，再生成临时快照 `T` 并准备 `T - S1`；不覆盖 `S0/S1`。
3. 批量存放前回顶一次，识别当前页全部剩余目标 ID，按行列顺序转移；当前页缓存处理完后重新识别，以对应物品的格子数减少确认成功。未减少的目标总共尝试 3 次（含首次），仍失败则跳过；队列耗尽立即结束，否则继续向下翻页。
4. MXU 在一批顶层任务全部结束后停止 Agent 进程，因此 Go 进程内状态天然限制在一个任务批次内；同批顶层任务共享完整快照。

完整存放任务会同时记录用户选择的仓库；同批次的取回和内嵌存放自动复用该仓库，不再要求重复选择。

取回时仓库源格可能继续存在，不能沿用存放判据。开始取回前仅将背包回顶一次，并由 `R1` 建立每页每个物品 ID 的格子数基线。每次移动后识别当前页指定 ID，数量超过基线才判定成功，并更新内存基线，防止后续同 ID 物品被重复计为成功。当前页未验证成功则向下翻页；后续物品从已找到的页面继续验证，不按行列连续性寻找空格，也不在每次移动前重扫快照。

## 识别与搜索约束

- 快照扫描使用 `IconRecognition` 的 `item_filters: ["Normal:*"]`。
- 背包批量识别传入剩余目标 ID，反查使用 `item_recheck_filters: ["Normal:*"]`，保留同 ID 的多个格子。
- 仓库反查使用当前 `item_id` 与具体 `Normal:<Category>`。
- 批量存放和取回验证均在开始前回顶一次，之后沿页面顺序向下推进；不为每个物品反复上下扫描背包。
- 存放以当前页目标格子数减少验证，取回以当前页指定 ID 格子数增加验证。当前取回实现的候选与反查过滤均为该目标的 `Normal:<Category>`；不使用单格 ROI 验证。
- 每次扫描通过最大“已有后缀 / 新页前缀”重叠合并分页，既消除相邻页重叠，又保留背包内真实重复项。

## 扩展分类时

新增或调整分类必须同步检查：

1. `assets/tasks/StashBackpack.json` 的手动存放与取回 checkbox。
2. `Category.json` 与 `Retrieve.json` 的分类门控和仓库分类切换节点。
3. `stash_backpack.schema.json` 的 `Category` 枚举。
4. `assets/locales/interface/*.json` 的五语言分类文案。
5. `IconRecognition` 数据中的 `categoryType` 与仓库 `Normal:<Category>` 是否一致。

完成修改后运行 `pnpm format`、`pnpm format:go`、`pnpm check` 和 `pnpm test`。
