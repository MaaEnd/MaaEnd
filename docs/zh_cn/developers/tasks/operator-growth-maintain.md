# 开发手册 - 干员培养维护文档

本文说明 `OperatorGrowth` 的文件分布、培养流程和材料不足检查的实现。  
该文档更新于 2026 年 11 月 14 日。

干员培养是纯 Pipeline 任务，没有 Go 侧代码：进培养总览找目标干员，三个分页里查协议空间材料的拥有数量，发现红色不足就点击进入对应关卡刷取，刷完回来重新检查，直到三个分页都查不出不足为止。

## 文件路径

- 任务定义：`assets/tasks/OperatorGrowth.json`
- 主流程：`assets/resource/pipeline/OperatorGrowth/Growth.json`（`OperatorGrowthMaterialsSufficient` 节点也定义在这个文件里）
- ADB 覆盖：`assets/resource_adb/pipeline/OperatorGrowth/Growth.json`

## 两个选项

- `OperatorGrowthSchedule`：每周几跑，checkbox 类型，七个日选各自生成一个 `pipeline_override` 覆盖 `OperatorGrowthSchedule*` 节点。
- `OperatorGrowthOperator`：培养哪个干员，`select` 覆盖 34 个干员，每个 case 用 `pipeline_override` 把 `OperatorGrowthTargetAvatar` 的 `template` 换成头像截图、把 `OperatorGrowthTargetName` 的 `expected` 换成干员姓名文本。

新增干员需要一张头像截图（换 `OperatorGrowthTargetAvatar` 的 `template`），并把姓名加进 `OperatorGrowthTargetName` 的 `expected` 列表——要覆盖游戏支持的各语言（参考现有 case 的中/繁/英/日/韩），只填一种会让其他语言下 `OperatorGrowthSelected` 认不出目标。不改节点结构。

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

## 注意事项

下面是改这个模块时容易踩、但看代码一眼看不出来的地方。

### 跨模块依赖 ProtocolSpace

OperatorGrowth 大量改写 ProtocolSpace 的节点，而且是两套机制，别混：

- 任务启动时的 `pipeline_override`（写在 `tasks/OperatorGrowth.json` 的选项里）静态覆盖干员头像/姓名，以及 ProtocolSpace 的 `enabled`、`next`、`ProtocolSpaceLevelChoose.index`。
- 运行时的 `PipelineOverride` 动作（`Growth.json` 里 `OperatorGrowthConfigureRewardA` 和 `OperatorGrowthConfigureRewardB`）把 `ProtocolSpaceNormalPrepareChooseRewardABText` 的 `expected` 动态改成 `A` 或 `B`，只对当前任务这次运行有效。

要改 ProtocolSpace 相关行为，先确认你动的是哪一种。用 `PipelineOverride` 想顺手改某个节点的 `next` 时要注意：它默认 `allow_next: false`，会把 patch 里的 `next` 直接剥掉，得显式设 `allow_next: true` 才生效。

### 排程

`OperatorGrowthScheduleEnabled` 用 `ScheduleRecognition`，星期开关就在它自己的 `attach` 里（默认七天全 false），由 `OperatorGrowthSchedule` 选项的七个 case 各自覆盖。把星期写到别的节点、或者改了 `default_case` 漏掉某天，那天就静默不跑，不会有任何报错。

排程按游戏日的 04:00 算：本地时间 04:00 之前算作前一天。选了「周一」实际是周一 04:00 到周二 04:00，凌晨跑会被归到周日。

### 滚动计数

`OperatorGrowthResetScrollCount` 调 `ClearHitCount` 清掉回顶、下滑和三个分页下滑节点的 `max_hit`。这个动作 `strict` 默认 false：节点名写错或不存在时动作照样成功，但那个节点的计数没清掉，找干员可能卡在 `max_hit` 不再滑动，而且不报错。在 `nodes` 列表里加节点或拼错名字要逐一核对。

### 识别敏感点

- 九个节点用了 `box_index: 1`，取第二个匹配框（OpenOverview、SelectOperator、ClickInsufficientMaterial、OpenOptionalRewardSource、OpenDirectRewardSource、ClickOperatorLevel、ClickSkills、ClickWeaponLevel、FinishBattle）。界面同类元素顺序一旦变化，会点到错的那个。
- 三个选中节点（等级、技能、武器）靠 `ColorMatch` 蓝高亮 `lower [0,0,220] upper [180,35,255]` 认。游戏换主题或高亮色变了就认不到，先查这里而不是改模板。
- 奖励 A/B 名是五语言 `^...$` 正则白名单（`OperatorGrowthRewardAName` / `OperatorGrowthRewardBName`），和「目标库存」模式的映射绑死。协议空间新增或改名材料，`ConfigureRewardA/B` 会认不到，培养直接断在这一步。

### 加干员与改配置

加干员要三处一起改：任务选项里加 case、放 `OperatorGrowth/Operators/<Name>.png` 头像、在 `assets/locales/interface/*.json` 五个语言文件补 `operator.<Name>`。漏掉 locale 选项里会显示原始键名。有的干员不止一张头像，比如 Endministrator 有 Male 和 Female 两张。

材料不足点击的偏移两侧不一样：主文件 `[0,-65,0,0]`，ADB `[0,-81,0,0]`（差 16 像素）。改截图或分辨率相关参数，PC 和 ADB 都要看。

### 格式与适用范围

Pipeline 文件是 JSONC，允许注释和尾逗号，普通 JSON 校验器会报错。改完用 `tools/validate_schema.py` 过一遍。

任务 controller 覆盖 ADB、CloudADB、PlayCover、几个 Linux 后端和 Win32-Front，`group` 是 `sanity_sink`（自动校验组）。改流程要确认多端都能跑，不然 CI 的 sanity_sink 会挂。

唯一显式结束任务的节点是 `OperatorGrowthOverviewUnavailable`（找不到总览入口就结束）。其余看着像终点的节点只是被 `all_of` 引用的识别原语，没有 `next`。循环是 `FinishBattle` 认完战斗回到 `BattleCompletedInWorld`，再进 `OperatorGrowthEntry`，会对同一个干员一直培养下去，没有内置的「停」。
