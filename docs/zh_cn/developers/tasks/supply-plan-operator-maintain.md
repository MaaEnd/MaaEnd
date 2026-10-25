# 开发手册 - 协议空间按干员规划维护文档

本文说明协议空间目标库存模式下“按干员规划”的数据来源、数据流与维护方式。  
该文档更新于 2026 年 10 月 25 日。

## 用途

在 `ProtocolSpace` 的目标库存模式中勾选一个或多个干员，按 zmdmap `operator_growth` 数据算出“从零养满”（1→90 级、全部突破与装备节点、四个技能到 12 级、全部天赋）各路线总量并相加，覆盖 `SupplyPlanLimits` 的目标。库存扫描与按完成率刷取沿用目标库存模式，开跑时提示每条路线的总量、库存与还差。

## 数据流

- zmdmap `operator_growth` 经 `tools/pipeline-generate/SupplyPlanOperator/generate.mjs` 生成 `assets/resource/pipeline/SupplyPlan/SupplyPlanOperator.json` 的 `targets`，同时生成 `assets/tasks/ProtocolSpace.json` 的 `SupplyPlanOperators` 选项。
- 勾选项把 `{"<干员Name>": true}` 写入 `SupplyPlanOperatorPlan` 的 `attach`，并把 `SupplyPlanMain.next` 改为先 `SyncItemData` 再进入 `SupplyPlanOperatorPlan`。
- Go 动作 `SupplyPlanOperatorPlan`（`agent/go-service/supplyplan/`）读取 `attach` 与 `targets`，把所选干员的总量相加，用与任务选项相同的公式重建 32 个 `SupplyPlanInsufficient_*` 与 `SupplyPlanRewardItemMet_*` 表达式，再进入 `SupplyPlanDispatch`。
- Go 测试逐字比对任务选项中的表达式，上游公式变化时测试失败。

## 规则与限制

- 经验按等级 60 分界：1–59 级计作战记录经验，60–89 级计认知载体经验。依据是经验曲线在 60 级断开，以及上游两项经验的默认目标正好分别是一名干员 1–59 级（747110）与 60–89 级（1045180）经验总量的两倍。
- 植物与“存续的痕迹”（`item_char_skill_crown`）协议空间刷不到，不计入。
- 武器材料不在数据集中，勾选干员后武器相关路线目标为 0。
- 计划是静态的“从零养满”上限，不读取干员当前练度；练到一半的干员会按上限刷，干员练成后请取消勾选。
- 未勾选任何干员时，行为与原目标库存模式完全一致。

## 维护

- 数据更新运行 `pnpm generate:SupplyPlanOperator`。
- 新增干员需先有 `operator.<Name>` 文案，否则生成器报错。
- `SupplyPlanLimits` 增删路线时，同步 `agent/go-service/supplyplan/expression.go` 的路线表与生成器的物品到路线表。
