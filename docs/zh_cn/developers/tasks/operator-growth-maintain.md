# 开发手册 - 干员培养维护文档

本文说明 `OperatorGrowth` 的文件分布、培养流程和材料不足检查的实现。  
该文档更新于 2026 年 11 月 14 日。

干员培养是纯 Pipeline 任务，没有 Go 侧代码：进培养总览找目标干员，三个分页里查协议空间材料的拥有数量，发现红色不足就点击进入对应关卡刷取，刷完回来重新检查，直到三个分页都查不出不足为止。

## 文件路径

- 任务定义：`assets/tasks/OperatorGrowth.json`
- 主流程：`assets/resource/pipeline/OperatorGrowth/Growth.json`（另有 `OperatorGrowth.json` 收纳 `OperatorGrowthMaterialsSufficient`）
- ADB 覆盖：`assets/resource_adb/pipeline/OperatorGrowth/Growth.json`

注意 `tools/zmdmap/data/operator_growth.json`（干员培养的数值参考，等级经验与逐级消耗）不是任务运行的数据，它由 `datasets/operator_growth.py` 从 TableCfg 生成，目前没有被 pipeline 或用户文档引用。

## 两个选项

- `OperatorGrowthSchedule`：每周几跑，checkbox 类型，七个日选各自生成一个 `pipeline_override` 覆盖 `OperatorGrowthSchedule*` 节点。
- `OperatorGrowthOperator`：培养哪个干员，`select` 八个干员，每个 case 用 `pipeline_override` 把 `OperatorGrowthTargetAvatar` 和 `OperatorGrowthTargetName` 的 `template` 换成对应截图。

新增干员需要新截图两张（左侧头像、右侧姓名），不改节点结构。

## 主流程

入口 `OperatorGrowthSchedule` 按星期控制是否运行。进入后先 `OperatorGrowthCheckOverview` 确认在干员页，识别不到培养总览入口就走 `OperatorGrowthOverviewUnavailable` 结束。

`OperatorGrowthOpenOverview` 进总览前会跑一次 `OperatorGrowthResetScrollCount`，把回顶、下滑和三个分页下滑节点的 `max_hit` 全部清零，保证每次进入都有完整的滑动配额。

找干员是 `OperatorGrowthSearchOperator` 和 `OperatorGrowthScrollDown` 的循环：

- 每屏先试 `OperatorGrowthSelected`（右侧姓名一致）和 `OperatorGrowthSelectOperator`（头像模板命中），都没找到就下滑一屏继续找
- `OperatorGrowthScrollDown` 每次滑 240 像素，`max_hit: 10`；相邻页保留重叠区域，避免头像被切到跨页两头
- 找到后 `OperatorGrowthSelectOperator` 点头像，`OperatorGrowthSelected` 用右侧姓名确认选中的就是目标

## 材料不足的判定

三个分页——干员等级、技能、武器等级——结构完全一致：切页签 → 确认选中 → 查当前屏的红色 → 不足就点 → 下滑再看下一屏。

红色判定在 `OperatorGrowthRedQuantity`：`ColorMatch` 的 `roi` 直接写 `"OperatorGrowthMaterialTemplates"`，即以全部材料模板命中的框为基准，`roi_offset [0, 65, 0, -22]` 往下取拥有数量那一行；HSV 区间 `[[0,80,130],[5,255,255]]` 和 `[[175,80,130],[180,255,255]]` 框住红色，`count: 12` 要求至少十二个采样点，`order_by: Vertical` 按纵向排序。

材料不足时 `OperatorGrowthClickInsufficientMaterial` 点击该材料图片，按来源分两条路：

- 钱币、武器经验和深潜类走 `OperatorGrowthOpenDirectRewardSource`，直接入场
- 干员经验、干员进阶、技能提升、武器进阶四个来源的奖励是 A/B 二选一，走 `OperatorGrowthOpenOptionalRewardSource` 进材料弹窗，再由 `OperatorGrowthConfigureRewardA` / `ConfigureRewardB` 按目标材料切换五级奖励——这一段复用目标库存模式的奖励切换流程

打完一局 `OperatorGrowthFinishBattle` 认 `ProtocolSpaceRewardClaimSuccess` 加白色取消按钮点掉奖励窗，`OperatorGrowthBattleCompletedInWorld` 确认回到大世界，然后重新进总览从头检查。

三个分页全部查完、`OperatorGrowthMaterialsSufficient`（挂在武器分页选中之后）命中，才算培养完成。

## ADB 覆盖

ADB 覆盖了 14 个节点，全是识别侧：总览入口与「全部」标题的 OCR、目标头像和姓名的模板、材料模板与红色判定的 roi 和颜色区间、三个分页的页签与选中确认。动作和流程结构不动。

改截图或分辨率相关参数时，PC 和 ADB 两侧都要看，别只改一边。

## 调试

按 `docs/zh_cn/developers/node-testing.md` 单独跑节点。常见问题先看这三处：

- 找干员卡住：看 `OperatorGrowthScrollDown` 的命中次数是不是被前几次进总览用掉了，`OperatorGrowthResetScrollCount` 有没有跑
- 红色判定漏：`OperatorGrowthMaterialTemplates` 的模板框和 `roi_offset` 是一对，改模板图高度就要动 offset
- 刷完不回来：`OperatorGrowthBattleCompletedInWorld` 靠大世界画面确认，如果这一步失败，后续的重新进总览也不会发生
