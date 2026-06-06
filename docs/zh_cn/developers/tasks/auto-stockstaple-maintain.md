# 开发手册 - 自动购买稳定需求物资维护文档

本文说明 `AutoStockStaple` 的文件分布与执行流程。  
以 **四号谷地** 为主线；**武陵** Pipeline 结构完全对称，仅地区后缀与场景识别不同。  
该文档更新于 2026 年 6 月 6 日。

## 文件路径

| 路径 | 作用 |
| --- | --- |
| `assets/interface.json` | 任务挂载（`regional_development` 组） |
| `assets/tasks/AutoStockStaple.json` | 任务入口、地区开关、物品勾选、上限与折扣选项 |
| `assets/resource/pipeline/AutoStockStaple/Main.json` | 执行周期、入口初始化、地区子任务调度 |
| `assets/resource/pipeline/AutoStockStaple/ValleyIV.json` | 四号谷地列表扫描循环 |
| `assets/resource/pipeline/AutoStockStaple/Wuling.json` | 武陵列表扫描循环 |
| `assets/resource/pipeline/AutoStockStaple/General/Item.json` | 商品锚点、名称/折扣识别、BetterSliding、确认购买 |
| `assets/resource/pipeline/AutoStockStaple/General/Goods.json` | 购买弹窗内物品 OCR |
| `assets/resource/pipeline/AutoStockStaple/General/GoodsCountValidate.json` | 弹窗右上角持有数量 OCR |
| `assets/resource/pipeline/AutoStockStaple/General/QuantityControl.json` | 弹窗分支调度、排除物品、确认购买 |
| `assets/resource/pipeline/AutoStockStaple/General/Template.json` | 售罄、调度券、确认购买等通用模板 |
| `assets/resource/pipeline/Interface/InScene/StockStaple.json` | 地区与稳定物资界面场景识别 |
| `assets/resource_adb/pipeline/AutoStockStaple/` | ADB ROI 偏移镜像（需与 Win32 同步检查） |
| `agent/go-service/autostockstaple/action.go` | 计算购买数量并驱动 BetterSliding |
| `agent/go-service/common/attachregex/action.go` | attach 关键词合并为 OCR 白名单正则 |
| `tools/pipeline-generate/AutoStockStaple/General/` | 批量生成 Goods / CountValidate / QuantityControl |
| `assets/locales/interface/*.json` | 任务、选项与 focus 文案 |

## 执行流程

1. 检查今日是否在[执行周期](#执行周期)内；未命中则直接结束。
2. 读取用户勾选的购买物品，[合并为商品名 OCR 白名单](#attach-与白名单初始化)（实现见 `Main.json` + `attachregex/action.go`）。
3. 按选项依次进入已启用的地区（四号谷地 / 武陵），跳转到该地区稳定物资界面。
4. 在列表页循环扫描，每轮按顺序判断：
    - 剩余调度券是否[低于保留阈值](#调度券保留阈值) → 停止本地区扫描。
    - 是否[识别到可买目标商品](#商品识别链) → 点击进入购买弹窗。
    - 是否已售罄 → 停止本地区扫描。
    - 否则向下滑动列表继续找（单地区最多滑 25 次）。
5. 购买弹窗内按[数量控制三分支](#数量控制三分支)处理：券不足退出 / 未达上限则购买 / 已达上限则排除。
6. 所有启用地区完成后结束。

> 列表里「识别到商品并点击」不等于「购买成功」；只有数量控制走到确认购买才算下单。

## 特殊处理

### 执行周期

实现位于 `Main.json`。用户勾选的星期几会写入周期节点的 `attach`，由 `ScheduleRecognition` 判断今天是否执行；未勾选的日子任务直接结束，不进入购买流程。

### attach 与白名单初始化

本任务不靠运行时拼用户输入字符串，而是：

1. 用户在界面勾选物品 → `assets/tasks/AutoStockStaple.json` 把各语言商品名写入 `attach.{slug}`。
2. 任务入口执行 `AttachToExpectedRegexAction`，读取所有 attach 关键词，合并为 `^(别名1|别名2|...)$` 正则，覆盖到列表页商品名 OCR 节点。
3. `attach` 为 `false` 的键会被排除，不再进入白名单。

Exclude 分支（物品已达标或券不足被剔除）也会触发重新初始化，保证后续列表扫描不会再点已排除的物品。  
排除动作通过 `PipelineOverrideAction` 将对应 `attach.{slug}` 设为 `false`。

### 调度券保留阈值

实现位于 `ValleyIV.json` / `Wuling.json`（武陵节点名后缀为 `Wuling`）。

扫描循环的**第一项**判断：右上角调度券 OCR 读数，与用户配置的保留阈值做表达式比较。  
若「保留阈值 > 当前剩余券」，表示券已不够继续买，结束本地区扫描；否则继续找商品。

列表扫描阶段**不**判断单价是否买得起；买得起与否留到购买弹窗内处理。

### 商品识别链

实现位于 `General/Item.json` + 地区 JSON 中的折扣节点。思路与[信用点商店](./credit-shopping-maintain.md)类似：**先找锚点，再偏移识别后续字段**，但锚点是商品卡片左上角的**剩余刷新时间框**（青绿色 ColorMatch），不是信用点图标。

```text
剩余时间锚点 → 商品名（颜色 + OCR 白名单） → 折扣（OCR 或 ColorMatch）
```

1. **锚点**：定位列表中每个商品卡片的时间区域，作为后续偏移基准。
2. **商品名**：锚点 → 名称标签色 → 文字底色 → OCR；仅命中用户勾选的白名单商品。
3. **折扣**：从名称区域偏移到折扣位；默认 OCR 识别具体折扣数值，也可被选项改为「任意折扣」（有折扣色块即通过）或指定最低折扣档。

三者同时命中才点击商品，进入数量控制。

### 数量控制三分支

实现分布在 `General/QuantityControl.json`、`Goods.json`、`GoodsCountValidate.json` 与 `autostockstaple/action.go`。  
弹窗打开后，依次尝试各物品的专用分支，每个物品固定三路：

#### 分支 1：调度券不足

弹窗内识别到底部红色「调度券不足」提示时：

1. 将该物品从 attach 白名单排除。
2. 重新合并正则白名单。
3. 关闭弹窗，回到列表——避免反复点击买不起的商品。

#### 分支 2：持有量未达上限，执行购买

读取弹窗右上角当前持有数量 OCR，与用户配置的上限做表达式比较（`上限 > 当前持有量` 时命中）：

1. Go 动作计算 `需购数量 = 上限 − 当前持有量`。
2. 将结果写入 BetterSliding 的 `Target`，平滑调节购买数量滑条。
3. 点击确认购买，关闭奖励弹窗，回到列表继续扫描。

#### 分支 3：持有量已达上限，排除

`上限 <= 当前持有量` 时命中：

1. 从 attach 白名单排除该物品。
2. 重新合并正则白名单。
3. 关闭弹窗，不购买，继续扫描其他物品。

> **举例**：谷地刻写券上限 50、当前持有 48，分支 2 会算出再买 2 个；若已持有 50，走分支 3 直接剔除，本轮不再点击该商品。

### 运行时 Override 一览

| 时机 | 动作 | 作用 |
| --- | --- | --- |
| 任务入口 | `AttachToExpectedRegexAction` | 合并 attach → 商品名 OCR 正则 |
| 物品被排除后 | `PipelineOverrideAction` + 再次 `AttachToExpectedRegexAction` | 剔除 attach 键并刷新白名单 |
| 确认购买前 | `AutoStockStapleQuantityControlAction` | 算差值并 override BetterSliding 目标数量 |

## 新增物品时需改的路径

1. `tools/pipeline-generate/AutoStockStaple/General/data.mjs`（`id`、`slug`、多语言 `expected`）
2. 重新生成（仓库根目录）：

```bash
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/goods-config.json
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/goods-count-validate-config.json
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/quantity-control-config.json
```

1. `assets/tasks/AutoStockStaple.json`（勾选 case + 上限 override）
2. `assets/locales/interface/*.json`（选项与 `quantity_control.buy.*` focus 文案）

生成规则见 [`tools/pipeline-generate/AutoStockStaple/General/README.md`](../../../../tools/pipeline-generate/AutoStockStaple/General/README.md)。

## 新增地区时需改的路径

对照四号谷地复制一套即可（武陵即为现成镜像）：

1. `assets/resource/pipeline/AutoStockStaple/{Region}.json`（扫描四分支 + 折扣节点）
2. `Main.json`（子任务入口与 SceneManager 跳转）
3. `StockStaple.json` 或地区 InScene（场景 OCR）
4. `assets/tasks/AutoStockStaple.json`（地区 switch 与选项组）

## 与 AutoStockpile 的区别

| 项目 | AutoStockStaple（稳定需求） | AutoStockpile（弹性囤货） |
| --- | --- | --- |
| 决策主体 | Pipeline + 少量 Go | Go Service 主导 |
| 商品定位 | 列表时间锚点 + OCR 偏移链 | 模板匹配 + OCR 映射 |
| 数量控制 | 弹窗 BetterSliding + 表达式 | Go 解析详情页调节 |

两者界面相似但逻辑独立；日志分析见 `.claude/skills/autostockstaple-log-analysis/SKILL.md`。
