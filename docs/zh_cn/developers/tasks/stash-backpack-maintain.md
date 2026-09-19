# 开发手册 - 存放与取回背包维护文档

本文说明 `StashBackpack`、`RetrieveBackpack` 与内嵌存放能力的状态生命周期和维护边界。
该文档更新于 2026 年 9 月 13 日。

## 支持范围

- 存放和取回是同一任务的选项，开放 `Win32-Front`、`ADB` 和 `CloudADB`；内嵌存放共用同一套存放能力，平台判断同步允许 Win32 / Adb。
- 物品存放和取回统一调用 `InventoryTransferStackAction`。ADB 已配置物品及滚动条 ROI、一键存放区域和识别前等待，导航与分类复用现有 SceneManager 适配。四个公共滚动节点集中维护仓库、背包的上下翻页；补充已有道具时先在源格长按，再拖到背包中的同物品格。接口见 [Inventory 文档](../../../../agent/go-service/common/inventory/README.md)。
- 补充可用道具时先搜索背包；格内数量 OCR 命中 `50` 则跳过该目标，否则再搜索仓库并拖动补充。
- ADB 当前用于测试，尚未完成实机验收。需验证滚动惯性与页重叠、长按补充、菜单关闭后的识别、连续存取和中断清理；CloudADB 还需验证多指触控。静态截图通过不等于整个流程稳定。
- Pipeline 负责业务流程、界面导航、分类切换和物品移动；Go Service 封装完整快照扫描、存放记录、派生快照和目标队列状态。

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

快照的合并列表保存 `item_id`、`category_type` 和重新编号的逻辑 `row` / `column`，同时保留各页识别结果。逻辑行列只用于保持稳定顺序，禁止据此推算空格或点击坐标；操作目标来自当前页识别框。计数指同一物品占据的格子数，不是单格堆叠数量。需要背包真实状态时扫描生成快照；手动存放通过在确认成功时同步扣减 `working` 快照完成记账，存放完成后没有任何二次扫描或派生步骤。

| 方案名 | 实现名称 | 含义 |
| ------ | ------------------ | -------------------------------- |
| `S0` | `s0` | 一键存放后、手动存放前的背包；一键存放物品不纳入取回范围 |
| 中间态 | `working` | `S0` 落盘时同步建立的副本，随每次存放确认实时扣减；补充可用道具前的背包 |
| `S1` | `s1` | 存放任务全部完成后的背包，由 `working` 复制（补充只改数量不改格子） |
| `T` | `temporary` | 宿主任务或取回任务当前背包的临时真实快照 |

完整存放任务只有在 `working`、`s1` 均生成并执行 `complete_full` 后才发布可用状态。中断留下的半成品不得被取回或宿主任务使用。同一任务队列重复执行完整存放任务时会输出红色警告并成功跳过，避免覆盖后续任务依赖的快照与存放记录。

取回基于**存放记录**而非快照差集：

1. 手动存放的每次确认成功（按 `manual_item_stored` reason 识别）都会记入存放记录；一键存放、存放新物品与补充不进入记录。
2. 取回任务开始时，若未开启"存放新物品"且记录为空，直接结束，不进入仓库、不扫描背包。
3. 开启"存放新物品"时才扫描临时快照 `T`，并可选存放 `T - S1`（新增物品存入仓库后留在仓库，不参与取回）。
4. 将存放记录转为取回目标队列，从仓库按顺序逐个取回，并按用户勾选的分类门控。

存放记录的生命周期与存取对一致：重复存放会被入口守卫拒绝，取回中止时剩余目标写回记录；记录只在本批次内有效，批次结束随 Agent 状态失效。取回入口按"有记录即取回"放行：即使上次存放中断未生成完整快照，已确认存入的物品仍可取回；完整快照只是"存放新物品"差集的前置条件，缺失时跳过存放新物品并提示。

## 存放新获得物品

`StoreNewItemsWithStashBackpackSubTask` 供 AutoCollect、AutoEcoFarm 和 GiftOperator 在获得物品后调用：

1. 先确认当前控制器是 Win32 或 Adb，且本批次已有完整 `S0/S1`；不满足时输出红色警告并成功跳过，不影响宿主任务。
2. 进入同批次选定的仓库，按原设置先执行可选的一键存放，再生成临时快照 `T` 并准备 `T - S1`；不覆盖 `S0/S1`。
3. 批量存放前回顶一次，识别当前页全部剩余目标 ID，按行列顺序转移；当前页缓存处理完后重新识别，以对应物品的格子数减少确认成功。未减少的目标总共尝试 3 次（含首次），仍失败则跳过；队列耗尽立即结束，否则继续向下翻页。
4. 存放新物品确认的物品不写入取回记录，按设计留在仓库。

完整存放任务会同时记录用户选择的仓库；同批次的取回和内嵌存放自动复用该仓库，不再要求重复选择。

取回验证采用背包侧计数复核：取回前只扫描一次背包建立 `retrieve_current` 分页基线，然后回顶，从当前页开始向下查找目标物品。当前页同名格子数较基线增加才判定取回成功，成功后更新该页基线，避免同名目标被重复确认；未找到时继续翻页，滚动条到末页则停止并保留剩余记录。转移动作本身失败也进入同一中止路径。

## 识别与搜索约束

- 快照扫描使用 `IconRecognition` 的 `item_filters: ["Normal:*"]`。
- 背包批量识别传入剩余目标 ID，反查使用 `item_recheck_filters: ["Normal:*"]`，保留同 ID 的多个格子。
- 仓库反查使用当前 `item_id` 与具体 `Normal:<Category>`。
- 批量存放开始前回顶一次，之后沿页面顺序向下推进；不为每个物品反复上下扫描背包。
- 存放以当前页目标格子数减少验证；取回以取回前背包快照为基线，使用背包当前页同名格子数增加验证。
- 每次扫描通过最大“已有后缀 / 新页前缀”重叠合并分页，既消除相邻页重叠，又保留背包内真实重复项。

## 扩展分类时

新增或调整分类必须同步检查：

1. `assets/tasks/StashBackpack.json` 的手动存放与取回 checkbox。
2. `Category.json` 与 `Retrieve.json` 的分类门控和仓库分类切换节点。
3. `stash_backpack.schema.json` 的 `Category` 枚举。
4. `assets/locales/interface/*.json` 的五语言分类文案。
5. `IconRecognition` 数据中的 `categoryType` 与仓库 `Normal:<Category>` 是否一致。

完成修改后运行 `pnpm format`、`pnpm format:go`、`pnpm check` 和 `pnpm test`。
