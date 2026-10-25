# Development Manual - Protocol Space Operator Plan Maintenance Documentation

This document explains the data source, data flow, and maintenance of the "plan by operator" feature in the target inventory mode of Protocol Space.  
This documentation was last updated on October 25, 2026.

## Purpose

In the target inventory mode of `ProtocolSpace`, check one or more operators. The totals of every route needed to fully grow them from scratch (level 1→90, all breakthrough and equipment nodes, four skills to level 12, all talents) are computed from the zmdmap `operator_growth` data, summed, and used to override the `SupplyPlanLimits` targets. Inventory scanning and completion-ratio farming reuse the target inventory mode, and the total, owned, and missing amount of each route is shown at the start of the run.

## Data Flow

- The zmdmap `operator_growth` data is turned by `tools/pipeline-generate/SupplyPlanOperator/generate.mjs` into `targets` in `assets/resource/pipeline/SupplyPlan/SupplyPlanOperator.json`, and into the `SupplyPlanOperators` option in `assets/tasks/ProtocolSpace.json`.
- Each checked item writes `{"<OperatorName>": true}` into the `attach` of `SupplyPlanOperatorPlan` and changes `SupplyPlanMain.next` to run `SyncItemData` first and then `SupplyPlanOperatorPlan`.
- The Go action `SupplyPlanOperatorPlan` (`agent/go-service/supplyplan/`) reads `attach` and `targets`, sums the totals of the selected operators, rebuilds the 32 `SupplyPlanInsufficient_*` and `SupplyPlanRewardItemMet_*` expressions with the same formula as the task option, and then enters `SupplyPlanDispatch`.
- A Go test compares the expressions in the task option verbatim, so it fails when the upstream formula changes.

## Rules and Limitations

- Experience is split at level 60: levels 1–59 count as combat record EXP, and levels 60–89 count as cognitive carrier EXP. The basis is that the experience curve breaks at level 60, and that the two upstream EXP default targets are exactly twice one operator's EXP total for levels 1–59 (747110) and levels 60–89 (1045180) respectively.
- Plants and "Traces of Survival" (`item_char_skill_crown`) cannot be farmed in Protocol Space and are not counted.
- Weapon materials are not in the dataset: the weapon EXP, heavy cast die and cast die routes keep the manual `SupplyPlanLimits` targets (Go reads them from the matching `SupplyPlanRewardItemMet_*` expressions); other routes the checked operators do not use get target 0.
- The plan is a static "fully grown from scratch" upper bound and does not read the operator's current progress; a half-grown operator is farmed up to the upper bound, so uncheck an operator once fully grown.
- With no operator checked, behavior is identical to the original target inventory mode.

## Daily Red-Label Check (Optional)

- Switch `SupplyPlanRedCheck` (off by default, CloudADB only). When on, `SupplyPlanMain.next` enters `SupplyPlanRedCheckNext` first, which reads the red labels on the first two tabs of each checked operator's Cultivation Overview (expectation: Excellent), then syncs inventory and plans.
- `SupplyPlanRedCheckNext` only picks the next operator to read, overrides the portrait and name nodes of the reader in `Cultivation.json`, and points `SupplyPlanRedCheckRoute.next` to `CultivationScan`; the reader's exit nodes return to `SupplyPlanRedCheckNext`. Portraits and Chinese names are written into its param by the generator; templates come from `tools/Cultivation/build_operator_templates.py`.
- Results are cached per game day (04:00 boundary) in `debug/record/CultivationRed.json` and are not re-read on the same day. When planning, an operator read successfully keeps only routes still shown in red. If any red label icon is not identified (merged EXP card labels, for example), or the overview header shows another operator, that operator uses fresh-to-max totals.
- If the portrait, operator or expectation is not found, or the controller is not CloudADB, that operator falls back to fresh-to-max totals without stopping farming; recognition or scrolling errors during a read still fail the task.
- With several operators sharing one inventory, each red label compares one operator against the whole inventory, so a shortage that only appears in the combined demand is missed.
- When maintaining the reader, run `python tools/Cultivation/build_resources.py` to regenerate `Cultivation.json`.

## Maintenance

- To update the data, run `pnpm generate:SupplyPlanOperator`.
- A new operator needs an `operator.<Name>` locale text first, otherwise the generator fails.
- When routes are added to or removed from `SupplyPlanLimits`, update the route table in `agent/go-service/supplyplan/expression.go` and the item-to-route table in the generator together.
