# 开发手册 - 干员培养维护文档

本文说明 `Cultivation`（干员培养）的文件分布、读取流程与协议空间接入。  
该文档更新于 2026 年 10 月 21 日。

目前只在 CloudADB（云·终末地）官服简中上实机验证；PC 适配见[待补充：PC 适配](#待补充pc-适配)。

## 文件路径

| 路径 | 作用 |
| --- | --- |
| `assets/interface.json` | 任务挂载（`other_menu` 组） |
| `assets/tasks/Cultivation.json` | 六个任务入口与选项（生成） |
| `assets/resource/pipeline/Cultivation/Cultivation.json` | 进入培养总览、选干员、切换培养期望、四页签翻页（生成） |
| `assets/resource/image/Cultivation/Operators/` | PC 干员头像模板（生成） |
| `assets/resource_adb/image/Cultivation/Operators/` | ADB / CloudADB 干员头像模板（生成） |
| `agent/go-service/cultivation/` | 页面解析、需求合并、计划存储、协议空间派发 |
| `agent/go-service/cultivation/core/` | 不依赖 MaaFramework 的纯逻辑与单元测试 |
| `tools/Cultivation/build_resources.py` | 生成任务、Pipeline、五语文案与 Custom Schema |
| `assets/locales/go-service/` | 手工维护的五语运行提示 |
| `tools/Cultivation/build_operator_templates.py` | 从 CharIcon 原图生成两端头像模板 |
| `tools/Cultivation/calibrate_operator_scale.py` | 用新端截图校准头像缩放 |
| `tests/Cultivation/test_overview.json` | 培养总览入口与培养期望的节点测试 |

任务、Pipeline、界面文案和 Schema 由 `build_resources.py` 生成，不要手改生成文件；附加的 go-service 运行提示在五种语言文件中手工维护。改完生成器后运行：

```bash
python tools/Cultivation/build_resources.py
pnpm exec prettier --write assets/tasks/Cultivation.json assets/resource/pipeline/Cultivation/Cultivation.json
```

生成器会保留共享语言文件的键顺序；不要对整个 `assets/locales/interface/` 跑格式化，以免重排无关上游条目。

## 任务

| 任务 | 作用 |
| --- | --- |
| 读取培养需求 | 进入培养总览，选干员与培养期望，两遍读取四个页签，保存计划 |
| 培养任务进度 | 显示已有 / 需要 / 缺口、来源模块和基质状态 |
| 按培养计划刷协议空间 | 用计划目标运行上游协议空间（目标库存模式），**会消耗理智** |
| 生成模块接入预览 | 导出 `debug/record/CultivationModules.preview.json`，不执行 |
| 清空培养需求 | 删除计划，并让在途的旧扫描失效 |
| 我已在游戏中完成培养（手动确认） | 记录用户确认完成；新扫描会清除该确认 |

## 读取流程

1. `CultivationScan` 从任意界面经 `SceneEnterMenuOperator` 进入干员页，点右上角「培养总览」。
2. 选干员（选项 `CultivationOperator`，「当前已选」则跳过）：
    - 先自上而下用头像模板匹配，点击后 OCR 确认表头名字。
    - 头像未命中（皮肤、模板之后上线的干员）时兜底：`CultivationNextCard` 按卡片底部稀有度色条定位卡片，逐张点击并读表头，直到名字吻合或列表到底。兜底较慢（实测约 15 分钟）。
3. 切换培养期望（选项 `CultivationExpectation`，默认卓越）：表头已是所选期望则直接继续；否则点开面板、点所选等级、再次确认表头。无法确认时报错且不读取。切换会改游戏内该干员的培养期望设置，不消耗资源。
4. 依次读取「干员等级 / 技能与天赋 / 武器等级 / 武器基质」四个页签，整轮读两遍，两遍结果一致才保存计划；否则保留旧计划。

保存前校验本次明确选择的武器是否在新扫描列表内；越界时保留旧计划。「不培养武器」不会继承旧武器。扫描保存后如进度预览失败，会明确提示新计划已保存，需单独重试预览。更换有效武器选择会清除旧完成确认，相同选择保持确认。空计划也导出当前修订的预览，替换旧文件；没有材料行不代表所选武器的基质已满足。

### 读取约定

- 每页只读完整行：视口底边的行整行保留或丢弃；滚动后的页跳过顶部淡出行（该行已在上一页完整读取）。
- 每页与上一页对齐测出实际滚动距离（CloudADB 上为手指距离的 0.5–1.6 倍），按列表中的位置合并，重叠多少都不会漏行或重复；滚动超过约 280 px 的读取范围时报错。下翻滑动 160 px。
- 物品按 `IconRecognition` 稳定 ID 合并；同物品在多个区块的需求累加，库存只计一次。
- 「可替代」箱子跳过，不计入需求。
- 图标不在 `IconRecognition` 目录中的物品记为「未收录物品」并截图到 `debug/cultivation/`，不中断读取。
- 页签写着「已达上限」「已满级」时视为该页无需求；四页都如此则计划为空并提示无需培养。
- 武器页按武器名分区；一名干员只能装备一把，由选项 `CultivationWeapon` 选择计入哪一把。
- 基质页只读每把武器的无瑕（金色）基质数量。已注入且显示「完全适配」，或库存中有符合词条的无瑕基质，视为基质需求已满足；注入的基质词条不符不算满足。

## 协议空间接入

`CultivationFarm` 不复制协议空间流程，而是用 `ctx.RunTask` 运行上游 `ProtocolSpaceSchedule`：

1. 读取上游 `ProtocolSpace` 任务各选项的默认值（含 `AutoFightSetting` 等定义在其他任务文件中的共享选项），按客户端的方式深合并（例如星期复选框的 `attach` 累加）。
2. 强制 `ProtocolSpaceMode = TargetInventory`，并把计划需求渲染进 `SupplyPlanLimits` 的 16 项目标；未涉及的项目显式为 0。
3. `SupplyPlanAllTargetsMet` 换成正向库存比较，避免「没有匹配缺口」被当成达成。
4. 领取方式、理智不足时的处理由本任务的两个选项决定，默认单倍领取、结束任务（不用理智药）。

SupplyPlan 会先同步仓库，再选第一个缺口所在的关卡，直到目标全部达成或理智不足。任务结束后计划中的「已有」仍是扫描时的数字，需要再运行一次「读取培养需求」查看最新进度。

采集与基质暂未派发，只在预览中给出目标与阻断原因：上游采集、基质的目标库存模式当前不支持 CloudADB。

## 新增干员

干员选项与头像模板来自 `tools/zmdmap/data/sell_product.json`：

```bash
uv run python -m tools.Cultivation.build_operator_templates
python tools/Cultivation/build_resources.py
```

前者只补缺失的两端模板，后者为每名干员生成一个选项。目前尚未接入 `sync-zmdmap.yml`，同步 zmdmap 后需手动运行。头像匹配失败时兜底流程仍可找到干员，只是较慢。

## 待补充：PC 适配

作者没有 PC 测试条件，PC 端未做实机验证。已知需要处理的事项：

1. **头像模板**：PC 缩放 0.54 只用一张 PC 截图校准过。用 `calibrate_operator_scale.py` 对多张 PC 培养总览截图重新校准，写回 `RESOURCE_SCALES` 后重新生成。
2. **固定坐标**：以下坐标均按 CloudADB 实测，需要用 PC 截图核对：
    - 页签中心 `tabX`（`action.go`）。
    - 材料区 OCR `[420, 245, 820, 420]`（`recognition.go`、`action.go`）。
    - 表头 `[470, 75, 740, 85]`。
    - 无瑕基质数量 `[672, y-4, 40, 28]`。
    - 生成器中的 `OPERATOR_LIST`、`OPERATOR_NAME`、`EXPECTATION_HEADER`、`EXPECTATION_PANEL`。
3. **翻页距离**：PC 鼠标拖动与触控的滚动倍率不同，需要实测，保证相邻两页有重叠行。
4. **控制器限制**：Go 中的 `the cultivation scan supports CloudADB only` 检查，以及生成器中任务的 `controller: ['CloudADB']`。
5. **节点测试**：补 `Win32/Official_CN` 截图与测试用例。

## 已知限制

- 仅支持官服简中、单账号；计划一次只保存一名干员，新扫描替换旧计划。
- 一次完整读取约 6–9 分钟（视需求行数）；兜底选人另加时间。
- 「自动核验完成」未实现，完成状态只来自用户手动确认。
