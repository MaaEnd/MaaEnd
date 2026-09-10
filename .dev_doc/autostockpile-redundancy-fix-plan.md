# AutoStockpile 冗余修复执行计划

- **依据**：[`autostockpile-redundancy-audit.md`](./autostockpile-redundancy-audit.md)（基线 `0f350e4c`，审计对象 `agent/go-service/autostockpile/`，23 文件 3254 行）
- **对象**：审计报告全部 **High（H1–H12）与 Medium（M1–M10）共 22 项**；**不含 Low（L1–L12）**
- **前提**：High 与 Medium 问题点已由人工确认为真实存在，本计划不再重复论证真伪，只给出可落地的修复方案
- **性质**：本文件是修复过程的唯一进度台账；每条修复完成后必须先回写本文件状态，再提交

---

## 1. 使用说明

### 1.1 状态定义

| 状态 | 含义 |
| --- | --- |
| `未进行` | 尚未开始 |
| `进行中` | 已开始编辑，但尚未通过验证命令 |
| `已完成` | 修复落地、验证命令通过、**修复提交已产生** |
| `失败` | 尝试后确认无法按本方案落地（放弃或阻塞），必须在备注列写明原因与后续处理 |

### 1.2 执行流程（每批次固定三步）

1. 把当前批次涉及的条目标为 `进行中`，然后按第 3 节的方案改代码 / 配置 / 文案。
2. 跑验证命令（见 1.4）。全部通过后，**提交一**：本批次全部产物（代码 + locale + 文档）。
3. 把本批次已完成条目回写为 `已完成`（`文件同步` 列填提交一的短 hash），**提交二**：仅提交本文件。提交信息格式：`docs(dev_doc): 同步 H1/H2 状态为已完成`。

即「修复与文件同步分为两个 commit」：**每一个修复批次恰好产生两个提交，第一个携带全部修复产物，第二个只携带本文件的状态变更**。合并为同一提交、或一个批次拆出多于两个提交，都视为违反本计划。

### 1.3 硬性约束

- **禁止自行推断设计问题**：遇到无法从代码推断的设计取舍时，必须停下来向用户提问并等待答复，禁止「先按自己的理解实现」。第 6 节列出已识别的断点；执行中新发现的同类问题同样适用。
- 状态只在「验证通过且提交一已完成」之后才可写 `已完成`；只写了代码未提交不得标记。
- 每批次只改本批次相关文件，不得顺手修 Low 项（L1–L12 明确排除在本次范围外）。
- 不修改 `agent/go-service/vendor/`、`go.mod`、`go.sum`（这是审计附录记录的独立前置问题，见 6.8）。

### 1.4 验证命令

```bash
# 编译（必须先修 -mod 问题见 6.8；勿改动 vendor/ 与 go.mod）
cd agent/go-service && go build -mod=mod ./... && go vet -mod=mod ./autostockpile/

# 格式（Go：每批次都跑）
pnpm format:go

# 本文件 / 文档格式（本计划在第 .dev_doc 且为 Markdown）
pnpm format:md:check && pnpm format:md

# 资源与用例（locale 与 assets 有改动时必跑）
pnpm check && pnpm test
```

仓库当前状态说明：`go build ./...`（默认 vendor 模式）会因 `vendor/modules.txt` 与 `go.mod` 版本不一致而失败（beta.14 vs beta.18），故用 `-mod=mod`；模块缓存已含 `v4.0.0-beta.18`。

---

## 2. 状态总览

状态：`未进行` / `进行中` / `已完成` / `失败`（初始全为 `未进行`）。

### 2.1 High（12 项）

| # | 修复主题 | 状态 | 批次 | 修复提交 | 文件同步提交 |
| --- | --- | --- | --- | --- | --- |
| H1 | 删除死的阈值 JSON 配置入口链 | 已完成 | B1 | `b616249c` | `（本次提交）` |
| H2 | 删除 `parsePriceLimitValue`（随 H1，无独立提交） | 已完成 | B1 | `b616249c` | `（本次提交）` |
| H3 | 删除测试价格注入（环境变量整链） | 已完成 | B2 | `b53c7b12` | `（本次提交）` |
| H4 | 删除只写不读的 `priceCandidate.text` | 已完成 | B2 | `b53c7b12` | `（本次提交）` |
| H5 | 去掉恒为真的 `if priceChanged` 包装 | 已完成 | B2 | `b53c7b12` | `（本次提交）` |
| H6 | 删除不可达的 Skip 数量模式整链 | 已完成 | B3 | `a111add6` | `（本次提交）` |
| H7 | `resolveQuantityDecision` 改写为二分支 | 已完成 | B3 | `a111add6` | `（本次提交）` |
| H8 | 删除 `len(roi) != 4` 恒假检查 | 已完成 | B4 | `de52d8e1` | `（本次提交）` |
| H9 | `filteredRecognitionResults` 直接返回字段 | 已完成 | B4 | `de52d8e1` | `（本次提交）` |
| H10 | 删除 `resolveDailyStoragePathFunc` 间接层 | 已完成 | B2 | `b53c7b12` | `（本次提交）` |
| H11 | 删除 `buildSelectionPipelineOverride` 的无用 ctx | 已完成 | B4 | `de52d8e1` | `（本次提交）` |
| H12 | 删除三处恒真的下标/边界守卫 | 已完成 | B4 | `de52d8e1` | `（本次提交）` |

### 2.2 Medium（10 项）

| # | 修复主题 | 状态 | 批次 | 修复提交 | 文件同步提交 |
| --- | --- | --- | --- | --- | --- |
| M1 | 改用 SDK `AsCustom()` 解包，删手写重复实现 | 已完成 | B3 | `a111add6` | `（本次提交）` |
| M2 | `recognitionParamROI` 类型 switch 收窄为 TemplateMatch | 已完成 | B3 | `a111add6` | `（本次提交）` |
| M3 | 删除不可达的 `threshold <= 0` 检查 | 已完成 | B3 | `a111add6` | `（本次提交）` |
| M4 | `validateItemMap` 由 3 次收敛为入口 1 次 | 已完成 | B4 | `de52d8e1` | `（本次提交）` |
| M5 | 统一 `result.Data` 判空语义（信不变式） | 失败（`de52d8e1` 引入 QuotaZeroSkip panic 回归，已回退重修） | B4 | 原修复 `de52d8e1` / 重修 `8c45ca46` | `5952a3c1` / 重修同步 `（本次提交）` |
| M6 | `resolveOverflow` 删除布尔返回值 | 已完成 | B4 | `de52d8e1` | `（本次提交）` |
| M7 | 删除 `reconcile` 的重复深拷贝 | 已完成 | B4 | `de52d8e1` | `（本次提交）` |
| M8 | `writeFileAtomic` 提取到公共包复用 | 已完成 | B4 | `de52d8e1` | `（本次提交）` |
| M9 | 收敛导出面（小写化 + 内联薄封装） | 已完成 | B4 | `de52d8e1` | `（本次提交）` |
| M10 | 删除 `screencapShelf` 无效的 `img == nil` 检查 | 已完成 | B4 | `de52d8e1` | `（本次提交）` |

批次与提交的对应关系（每个批次两个提交）：

| 批次 | 内容 | 提交一（修复） | 提交二（状态同步） |
| --- | --- | --- | --- |
| B1 | H1、H2 | `b616249c` | `（本次提交）` |
| B2 | H3、H4、H5、H10 | `b53c7b12` | `（本次提交）` |
| B3 | H6、H7、M1、M2、M3 | `a111add6` | `（本次提交）` |
| B4 | H8、H9、H11、H12、M4–M10 | `de52d8e1` | `（本次提交）` |

---

## 3. 逐项修复方案

### B1 —— 死配置入口（H1、H2）

#### H1. 删除死的阈值 JSON 配置入口链

- **现状**：`PriceLimitConfig.UnmarshalJSON` 唯一的潜在触发者不存在（`SelectionConfig` 只在 `strategy.go:68-77` 由公式构造，全仓库无 `json.Unmarshal` 目标为 `SelectionConfig`/`PriceLimitConfig`）。
- **方案**：
    - 删除 `types.go:101-124` 的 `UnmarshalJSON` 方法。
    - 删除 `thresholds.go:54-80` 的 `parsePositiveThresholdValue`、`thresholds.go:82-97` 的 `parsePriceLimitValue`（H2）。
    - `thresholds.go` 的 `encoding/json`、`strconv` 随两个函数一并判断是否仍被引用后移除。
    - **保留** `newThresholdConfigError` / `thresholdConfigError`（仍由 `resolveTierThreshold` 使用）与 `resolveTierThreshold` 的 `!ok` 分支。
    - **保留** `SelectionConfig.PriceLimits` 的 `json:"price_limits"` tag（不扩大改动面，见 6.3）。
    - 文档同步：`docs/zh_cn/developers/tasks/auto-stockpile-maintain.md`（201 行）在「地区与价格选项」章节补一句 —— 价格阈值只由 `strategy.go` 的「地区基准 + 档位基准 + 星期调整」公式产出，已无 JSON 配置入口，修改阈值只能改公式；避免后来者再照旧文档找配置入口。
- **验证**：`go build -mod=mod ./...` + `go vet -mod=mod ./autostockpile/` 通过；`grep -rn "price_limits" agent/ assets/ tools/ docs/` 仅剩 `thresholds.go` 的错误消息字段名（与 6.3 的保留项）。

#### H2. `parsePriceLimitValue` 字符串分支永远不可达

- **方案**：随 H1 一并删除（`thresholds.go:82-97`）。
- **提交**：不产生独立提交，与 H1 同属 B1 提交一；其状态行在 B1 的提交二中与 H1 一起改为 `已完成`，并在 `修复提交` 列写入与 H1 相同的短 hash。

### B2 —— 测试脚手架残留与平凡守卫（H3、H4、H5、H10）

#### H3. 删除测试价格注入

- **现状**：`applyTestPricesIfEnabled` 在写真价格前改写商品价格（`recognition.go:120`），改写值同时进入选品决策与 `debug/record/ElasticGoodsPrices.json` 落盘；环境变量全仓库零引用且本包无测试。
- **方案**：
    - 删除 `goods_scan.go:350-420` 的 `applyTestPricesIfEnabled`。
    - 删除 `goods_scan.go:19` 的 `testPricesEnvVar` 常量。
    - 删除 `recognition.go:120` 的调用。
    - 移除因此失去引用的 `os`、`math/rand` 导入（已核实该文件仅此处使用）。
- **不做的事**：不为其补测试、不保留「调试开关」注释块。
- **验证**：`go build -mod=mod ./...` 通过；`grep -rn "MAAEND_AUTOSTOCKPILE_RECOGNITION_TEST_PRICES" .`（排除 `.git`/`vendor`/`node_modules`）零命中。

#### H4. `priceCandidate.text` 只写不读

- **方案**：删除 `goods_scan.go:27-31` 的 `text` 字段；删除 `goods_scan.go:305` 的赋值 `text: priceText`。保留 `priceText` 局部变量（`seenPrice` 去重 key 仍需要）。
- **验证**：`go build -mod=mod ./...` 通过；`grep -rn "\.text" agent/go-service/autostockpile/` 零命中。

#### H5. `reconcile.go` 的 `priceChanged` 二次判断恒为真

- **方案**：`reconcile.go:139-141` 去掉 `if priceChanged` 包装，直接 `maafocus.Print(ctx, i18n.T("autostockpile.reconcile_price_corrected", oldPrice, price))`；上一行的前置 `return true` 保证此处必然经过。
- **不改**：`oldPrice`、`price` 局部变量保留（表达式仍需要）。

#### H10. `resolveDailyStoragePathFunc` 无人改写的间接层

- **方案**：删除 `daily_storage.go:17` 的变量；`daily_storage.go:64` 改为直接调用 `resolveDailyStoragePath()`。
- **验证**：`grep -rn "resolveDailyStoragePathFunc" agent/` 零命中。

### B3 —— 不可达业务分支与 SDK 能力复用（H6、H7、M1、M2、M3）

#### H6. 删除不可达的 Skip 数量模式整链

- **现状**：进入 `computeDecision` 时 `Quota.Current >= 1`（`Current == 0` 时识别层已返回 `QuotaZeroSkip` 并在 `selector.go:74-76` 短路），而 Skip 分支要求 `min(Overflow, Current) <= 0` 即 `Overflow == 0`，与进入条件 `Overflow > 0` 矛盾。
- **方案**（按审计建议选「删除」而非「标注保留」，理由：Skip 只能由该分支产出，删除后无行为变化）：
    - `quantity.go:8` 删除 `quantityModeSkip` 常量。
    - `quantity.go:43-48` 删除 `overflowTarget <= 0` 的 Skip 返回块（保留 `min(Overflow, Current)` 截断逻辑）。
    - `selector.go:178-197` 删除整个 Skip 短路块（含 `maafocus.Print`、`overrideSkipBranch` 调用）。
    - i18n：`assets/locales/go-service/{zh_cn,zh_tw,en_us,ja_jp,ko_kr}.json` 删除 `autostockpile.hit_but_skip`（`selector.go:187` 是其唯一引用）；`autostockpile.qty_overflow_invalid` 同理删除（`quantity.go:46` 唯一引用）。已核实两键未被任何 HTML 模板引用。
    - locale 删除后复核：5 个文件键集合保持一致；`assets/locales/go-service/` 不受 `tools/i18n/` 同步脚本管理（脚本只管 OCR expected），删除不会被打回。
    - 保留并复核：`overrideSkipBranch` 仍被 `selector.go:167`、`routeSkipWithAbortReason` 使用；`i18n`、`maafocus` 在该文件仍被引用。
- **验证**：`grep -rn "quantityModeSkip\|hit_but_skip\|qty_overflow_invalid" agent/ assets/` 零命中；`pnpm check`、`pnpm test` 通过。

#### H7. `resolveQuantityDecision` 的 `default` 分支不可达

- **方案**：`quantity.go:19-28` 改写为：

```go
func resolveQuantityDecision(selection SelectionResult, data RecognitionData) quantityDecision {
    if selection.CurrentPrice < selection.Threshold {
        return resolveThresholdQuantityDecision()
    }
    return resolveOverflowQuantityDecision(data.Quota)
}
```

- 保留 `selection` 形参并继续以价格为第一判据：`bypass == false` 时 `SelectBestProduct` 只接受 `score > 0`（必然 `price < threshold`）；`price >= threshold` 只可能来自溢出放行路径（`bypass == true ⇒ Overflow > 0`）。因此 `default` 不可达，删除后与原 `switch` 行为完全一致。
- **修正记录**：本节初稿曾给出 `if data.Quota.Overflow > 0` 优先的写法。该写法在「`Overflow > 0` 且 `CurrentPrice < Threshold`」时会把数量模式从 `SwipeMax`（低价买满）改为按防溢出数量购买，**并非等价重构**（审计原等价性论证只覆盖了 `bypass == false` 一侧）。经用户确认改用上面保留价格优先判据的二分支实现，详见 §6.9 追加记录。
- **验证**：`go build -mod=mod ./...` 通过；`grep -rn "resolveQuantityDecision" agent/` 仅 `decision.go` 一处调用。

#### M1. 改用 SDK `AsCustom()` 解包，删手写重复实现

- **现状**：`extractCustomRecognitionDetailJSON` 每次 action 都对整串 `DetailJson` 再 `json.Unmarshal`，与 SDK `detailRawToString`（`vendor/.../recognition_result.go:153-164`）逐行等价。
- **方案**：
    - 删除 `recognition_results.go:20-35`、`recognition_results.go:126-138`。
    - `selector.go:35-41` 改为：从 `arg.RecognitionDetail.Results.Best.AsCustom().Detail` 取串；`AsCustom()` 返回 `false` 或 `Detail == ""` 时记 error 日志并 `return false`（保持原有失败语义）。
    - 保留 `selector.go:43-50` 的 `json.Unmarshal`（Custom 识别 Detail 自身就是一段 JSON，这一步不可省）。
    - 同文件 `bestTemplateHit`、`filteredOCRCandidates`、`ocrTextCandidates` 仍用 `maa`，不产生未用导入。
- **等价性依据**：`RecognitionDetail.DetailJson` 与 `Results` 由同一个 detail 解析而来（`vendor/.../tasker.go:234/300`；`recognition_result.go:362` 的 `DetailJson: string(item.Detail)`），故 `Best.AsCustom().Detail` 等于原实现取到的 `best.detail` 字符串。
- **验证**：`go build -mod=mod ./...` 通过；`grep -rn "extractCustomRecognitionDetailJSON\|rawJSONToString" agent/go-service/autostockpile/` 零命中。

#### M2. `recognitionParamROI` 类型 switch 收窄为 TemplateMatch

- **现状**：目标节点 `AutoStockpileSelectedGoodsClick` 在 `DecisionLoop.json:83-102` 固定为 `TemplateMatch`（`roi: [50,180,1200,500]`），override 也只写 `template`/`roi`；另外 6 个 case 永不可达。
- **方案**：

```go
param, ok := node.Recognition.Param.(*maa.TemplateMatchParam)
if !ok || param == nil {
    return nil, fmt.Errorf("node %s has unsupported recognition param type %T", selectedGoodsClickNodeName, node.Recognition.Param)
}
rect, err := param.ROI.AsRect()
if err != nil {
    return nil, fmt.Errorf("node %s roi: %w", selectedGoodsClickNodeName, err)
}
return []int{rect[0], rect[1], rect[2], rect[3]}, nil
```

- `maa.Target`/`AsRect()` 仍被使用，导入不变。
- **行为差异**：目标节点若被改成其它识别类型，将从「静默支持」变为**报错**（审计主张更安全）。这属于错误处理策略，按 6.5 向用户确认。
- **验证**：`go build -mod=mod ./...`；构造性验证 `overrideSelectedGoodsClickROIY` 的调用链（`goods_scan.go:245/251/254`）仍能取到 ROI。

#### M3. 删除不可达的 `threshold <= 0` 检查

- **现状**：H1 之后 JSON 路径不存在，阈值只来自公式（最小 `0 + 600 - 250 = 350 > 0`），`threshold <= 0` 恒假；真正可达的是 `!ok`。
- **方案**：删除 `thresholds.go:21-23`。**与 6.3 的「不恢复配置入口」一并生效；若 6.3 结论是恢复入口，则本项作废并改为保留检查（两者互斥）。**
- **验证**：`go build -mod=mod ./...`；确认 `!ok` 分支（`AbortReasonThresholdConfigInvalidFatal` 来源）逻辑不变。
- **备注**：本项落地后 `AbortReasonThresholdConfigInvalidFatal` 与 i18n `autostockpile.abort.ThresholdConfigInvalidFatal` 在 AutoStockpile 内不再可达。是否连带清理按 6.4 询问用户，本计划默认保留。

### B4 —— 结构性收敛（H8、H9、H11、H12、M4–M10）

#### H8. 删除 `len(roi) != 4` 恒假检查

- **方案**：删除 `overrides.go:125-127` 的 3 行判断。
- **依据**：`recognitionParamROI` 返回 `[]int`，其元素来自 `rect.Rect = [4]int`，长度恒为 4。

#### H9. `filteredRecognitionResults` 直接返回字段

- **方案**：`recognition_results.go:37-45` 收缩为 `return detail.Results.Filtered`（保留前置 nil 判断）。
- **等价性依据**：所有调用方（`recognition_results.go:48/71`、`quota.go:28/66`）只判断 `len(x) == 0`，nil 与空切片行为一致。

#### H11. 删除 `buildSelectionPipelineOverride` 的无用 ctx 参数

- **方案**：删除 `overrides.go:10` 的 `_ *maa.Context` 形参；更新调用点 `selector.go:216`、`reconcile.go:208`。
- **验证**：`go build -mod=mod ./...`；`grep -rn "buildSelectionPipelineOverride" agent/` 仅剩 3 处且签名一致。

#### H12. 删除三处恒真的下标/边界守卫

- **方案**：
    - `goods_scan.go:445-447`、`goods_scan.go:485-487`：删除 `if bestIdx < len(used) { … }` 包装，直接 `used[bestIdx] = true`。
    - `goods_scan.go:509`：保留唯一一处 `i < len(used)` 断言（`findBestPriceCandidate` 内），因为它才是「`prices` 与 `used` 长度不一致」的唯一显式守卫，并补一行注释说明该不变式。
- **依据**：`used := make([]bool, len(prices))`（`goods_scan.go:71`），`bestIdx` 必落在 `[0, len(prices))`。

#### M4. `validateItemMap` 由 3 次收敛为入口 1 次

- **方案**：
    - 删除 `recognition.go:79-89` 的校验及其 `itemMapCounts` 日志块（保留 `itemMap := GetItemMap()`）。
    - 删除 `goods_scan.go:53-55` 的校验。
    - **保留** `params.go:69-71` 的入口校验：`resolveGoodsRegionFromCustomActionParam` 同时做 `itemMapHasRegion` 校验（`params.go:72`），是两条调用链（识别路径 `resolveGoodsRegionFromTaskNode`、动作路径 `resolveGoodsRegionFromActionArg`）共同经过的唯一关卡，`assets` 侧 `items.json` 未定义参数 schema，这是唯一防线。
- **附带**：删除后 `itemmap.go:164` 的 `itemMapCounts` 失去唯一调用者。它不属于 H/M 列表，本次**保留**；在提交信息里注明「`itemMapCounts` 暂无调用者，待 Low 批次或后续独立提交处理」，是否连带删除按 6.4 询问。
- **验证**：`go vet -mod=mod ./autostockpile/` 通过；`grep -rn "validateItemMap" agent/go-service/autostockpile/` 仅剩定义与 `params.go:69`。

#### M5. 统一 `result.Data` 判空语义 —— **失败，已回退重修**

- **原方案**：采用「信任不变式」：删除 `selector.go:59-62` 的 `if result.Data != nil` 兜底，`goodsCount` 直接由 `len(result.Data.Goods)` 计算，与 `selector.go:78` 的 `data := result.Data` 保持一致。
- **原依据（有缺陷）**：`types.go:127-144` 的 `Validate()` 已保证 `AbortReason == None ⇔ Data != nil`，且 `selector.go:51-57` 已调用它。
- **失败原因（2026-09-10 事后核验发现）**：该依据的 `⇔` 不成立，`Validate()` 只强制单向蕴含 `Data != nil ⇒ AbortReason == None`；反向「非 None ⇒ 一定被路由接住」并不由 `Validate()` 保证，而依赖 `shouldStopTask`/`shouldRouteSkip` 的**字符串后缀约定**。更关键的是原方案**只检查了到达 `selector.go:78` 时 `Data` 非 nil，从未检查 `selector.go:59`（`goodsCount := len(result.Data.Goods)`）的解引用位置早于 :71/:74 的两条路由判断**。
    - 实测：`QuotaZeroSkip` + `Data == nil` 通过 `Validate()`（`shouldStopTask=false`、`shouldRouteSkip=true`，路由本身正确），但解引用在路由之前执行 ⇒ **必然空指针 panic**。
    - `QuotaZeroSkip` 由 `quota.go:95-98` 的 `current == 0` 产生，即玩家当日额度用尽后重跑任务——**日常高频路径，非边界情况**，条件满足即 100% 复现。
    - 基线行为对比：基线 `selector.go:60` 的 `if result.Data != nil` 对该原因恰好判假 ⇒ 安全跳过、`goodsCount=0`、被 `:74` 路由接住并正常 `return true`。**故该「冗余兜底」对 `QuotaZeroSkip` 严格可达，原判定「删之无行为变化」错误。**
- **重修方案（已落地，`agent/go-service/autostockpile/selector.go`）**：**不恢复判空**，改为把解引用放到不变式已被路由确立之后——将 `data := result.Data` 与 `goodsCount := len(data.Goods)` 整体移到 `shouldStopTask` / `shouldRouteSkip` 两条路由**之后**，并在该处加注释说明「解引用必须留在两条路由之后，否则 `QuotaZeroSkip` 会 panic」。
    - 保留 M5 的原始意图（不重新引入两处语义分叉）；abort 分支的 `goods_count=0` 日志本就无信息量（`abort_reason` 已由 `routeSkipWithAbortReason`/`stopTaskWithFocus` 输出）。
    - **重修提交**：`8c45ca46`（`fix(autostockpile): 修复 QuotaZeroSkip 路径的 nil 解引用 panic`）。
- **验证**：`go build -mod=mod ./autostockpile/`、`go vet -mod=mod ./autostockpile/` 通过；临时测试确认 `QuotaZeroSkip` + nil `Data` 被 `shouldRouteSkip` 接住且未触碰 `result.Data`，`None` 路径 `goodsCount` 正常（按仓库约定，验证用临时测试已删除）。
- **未覆盖的残留风险（属 L2，本计划范围外）**：新增既不以 `Fatal` 也不以 `Warn`/`Skip` 结尾的 `AbortReason` 时仍会穿透两条路由。建议后续把后缀判定改为显式分类表，并让 `Validate()` 校验分类存在，使该约定从「命名约定」升级为受保护的不变式。

#### M6. `resolveOverflow` 删除布尔返回值

- **方案**：`quota.go:91-94` 改为只返回 `overflowAmount`；`recognition.go:42-57` 的 `overflowDetected` 变量删除，日志改用 `Bool("overflow_detected", overflowAmount > 0)`。
- **验证**：`grep -rn "resolveOverflow\b" agent/` 仅 `quota.go` 定义与 `recognition.go` 调用各一处。

#### M7. 删除 `reconcile` 的重复深拷贝

- **方案**：`reconcile.go:93` 的 `updatedData := copyRecognitionData(state.RawRecognitionData)` 改为直接使用 `state.RawRecognitionData`，后续 `updatedData.Goods[i].Price = price` 等引用统一改名（或保留 `updatedData` 别名指向同一值）。
- **依据**：`getDecisionState()`（`state.go:51-60`）返回的已是深拷贝；`setDecisionState` 入参会再拷贝一次，因此就地修改不会污染全局状态。

#### M8. `writeFileAtomic` 提取到公共包复用

- **现状**：`autostockpile/daily_storage.go:108-143` 与 `creditshopping/storage.go:137` 起逐行相同（仅差空行）；审计提到的 `sellproduct/operator/cache.go:395`、`ims/storage.go:71` 在当前分支**不存在**（无 `sellproduct` 目录，`ims/storage.go` 无该函数），提交信息中一并说明。
- **方案**：
    - 新增 `agent/go-service/pkg/fsutil/atomic.go`：`package fsutil`，导出 `func WriteFileAtomic(path string, content []byte, perm os.FileMode) error`，实现照搬现有 36 行（`CreateTemp` → `Write` → `Chmod` → `Sync` → `Close` → `Rename`，`cleanup` 标记 + `defer os.Remove`）。
    - `agent/go-service/pkg/` 下已有 `jsonclean`、`resource`、`i18n` 等同类工具包，位置符合现有约定；不涉及注册机制。
    - `autostockpile/daily_storage.go`、`creditshopping/storage.go` 删除本地实现，改调 `fsutil.WriteFileAtomic`，清理失去引用的 `os`/`filepath` 导入（按实际引用判断）。
- **验证**：`go build -mod=mod ./...`；`grep -rn "func writeFileAtomic" agent/go-service` 零命中；两处落盘路径（`debug/record/ElasticGoodsPrices.json`、信用购物快照）行为不变。

#### M9. 收敛导出面（小写化 + 内联薄封装）

- **方案**（已核实这些符号在包外零引用，排除 vendor）：
    - `itemmap.go`：`LoadItemMap`→`loadItemMap`、`GetItemMap`→`getItemMap`、`MatchGoodsName`→`matchGoodsName`、`ParseTierFromID`→`parseTierFromID`、`BuildTemplatePath`→`buildTemplatePath`；**保留 `InitItemMap` 导出**（`register.go` 唯一外部入口）。
    - `selector.go:269`：`SelectBestProduct`→`selectBestProduct`。
    - `minbuy.go:41`：`SelectCheapestProduct`→`selectCheapestProduct`。
    - `abort_reason.go`：`ValidateAbortReason`→`validateAbortReason` 并内联进 `LookupAbortReason`（唯一调用者）；`LookupAbortReason` 保留导出。
    - 同步本包内全部调用点与相关注释。
- **注意**：审计称注册点需要 `GetItemMap`/`LoadItemMap` 导出，实际注册点只用 `InitItemMap`（`register.go`），故上述小写化成立。
- **验证**：`go build -mod=mod ./...` 通过（编译即证明无遗漏调用点）；`grep -rn "GetItemMap\|LoadItemMap\|MatchGoodsName\|ParseTierFromID\|BuildTemplatePath\|SelectBestProduct\|SelectCheapestProduct\|ValidateAbortReason" agent/go-service --include='*.go' | grep -v vendor` 只命中包内小写符号。

#### M10. 删除 `screencapShelf` 无效的 `img == nil` 检查

- **方案**：删除 `shelf_swipe.go:49-51`；`err != nil` 分支保留（`shelf_swipe.go:46-48`）。
- **依据**：`CacheImage()` 失败返回 `(nil, err)`、成功返回 `*image.RGBA`（`vendor/.../controller.go:440-462`），错误已在上一行拦截；`image.Image` 接口比较对 typed-nil 也不为真。

---

## 4. 影响面与非目标

### 4.1 公共 API / schema / 数据流影响

- **Go 导出 API**：仅 M9 收敛为包内小写；包外唯一依赖面（注册名 `AutoStockpile.SelectItem` / `AutoStockpile.ReconcileDecision` / `AutoStockpile.Recognition`、`InitItemMap`、`Register`）不变。
- **Pipeline / Interface JSON**：不改任何节点、参数、`interface.json`、`assets/tasks/AutoStockpile.json`。
- **Custom Schema**：`tools/schema/custom.*.schema.json` 不改（`AutoStockpile.SelectItem` 本就没有参数规则，`params.go` 校验保留）。
- **i18n**：删除 `autostockpile.hit_but_skip`、`autostockpile.qty_overflow_invalid` 共 5 个语言文件各 2 键；其余键不动。
- **落盘数据**：`debug/record/ElasticGoodsPrices.json` 结构不变；H3 只移除「环境变量改写价格」这条非常规写入源。
- **用户可见行为**：H3/H4/H5/H7/H8/H9/H10/H11/H12/M1/M4/M6/M7/M8/M10 为等价重构；H6 删除不可达分支；M2/M3/M5 把「不可达的容忍」换成「显式失败」，需 6.1/6.5 确认。

### 4.2 非目标

- Low 项（L1–L12）不在本次范围。
- 不修复 `vendor/modules.txt` 与 `go.mod` 版本不一致（审计附录的独立问题，见 6.8）。
- 不为 `autostockpile` 新增测试文件（审计未要求；补测试会改变包规模与提交性质）。
- 不改 `docs/zh_cn` 之外的语言文档（除 H1 的 2 行中文说明，如需英文镜像同步按 6.x 询问时一并确认）。

---

## 5. 验收标准

1. 状态总览中 H1–H12、M1–M10 全部为 `已完成`（或经用户确认后为 `失败` 并附原因），无残留 `未进行`/`进行中`。
2. 每个批次恰好两个提交：`修复提交`（代码 + locale + 文档）与 `文件同步提交`（仅本文件），且总览表的 hash 列与 `git log` 一致。
3. `cd agent/go-service && go build -mod=mod ./... && go vet -mod=mod ./autostockpile/` 通过。
4. `pnpm format:go` 无残留改动（执行后 `git status` 干净）。
5. `pnpm check`、`pnpm test` 通过（locale 相关改动后必跑）。
6. 关键 grep 断言全部为「零命中」：`price_limits`（除保留项）、`MAAEND_AUTOSTOCKPILE_RECOGNITION_TEST_PRICES`、`quantityModeSkip`、`hit_but_skip`、`qty_overflow_invalid`、`resolveDailyStoragePathFunc`、`extractCustomRecognitionDetailJSON`、`rawJSONToString`、`func writeFileAtomic`。
7. `pnpm format:md:check` 通过（本文件符合 markdownlint）。
8. 第 6 节的断点问题均已获得用户答复并落实，无任何「未询问即自行推断」的改动。

---

## 6. 必须向用户确认的设计问题（禁止自行推断）

以下问题无法从代码推断出唯一正确取舍，执行到相关批次前必须逐条提问并等待答复；得到答复后把结论补写进本文件对应条目再继续。

| # | 触发批次 | 问题 | 选项 | 默认（未获答复时**不得继续**，仅作推荐） |
| --- | --- | --- | --- | --- |
| 6.1 | B4 / M5 | 删掉 `result.Data != nil` 兜底后，契约被破坏会 panic 而非返回 `false`。取哪种一致性？ | (a) 信任不变式，两处都不判；(b) 两处都判并返回 `false` | (a) 信任 `Validate()`，与 `selector.go:78` 对齐 |
| 6.2 | B3 / H6 | `AbortReasonQuotaZeroSkip` 与 quota 语义：确认 Skip 分支是永久废弃，而非为未来 quota 变化预留？ | (a) 按计划删除；(b) 保留并加「当前不可达，为 quota 语义变化预留」注释 | (a) 删除（删除后无行为变化） |
| 6.3 | B1 / H1 | 是否确认**永久放弃**阈值 JSON 配置入口（只保留公式）？这决定 M3 是删检查还是保留。 | (a) 永久放弃 → 删 `UnmarshalJSON` 链 + M3 检查；(b) 计划恢复入口 → 本计划 H1/M3 作废，改为补文档与入口 | (a) 永久放弃 |
| 6.4 | B3/B4 | 因删除而死掉、但本身有用的 API 如何处置：`AbortReasonThresholdConfigInvalidFatal` + i18n 文案（M3 之后）、`itemMapCounts`（M4 之后）。 | (a) 一律保留（只删不可达检查，不删枚举与文案）；(b) 一并删除 | (a) 保留，避免牵连用户可见文案 |
| 6.5 | B3 / M2 | `recognitionParamROI` 收窄后，若节点被改成非 `TemplateMatch` 是否接受直接报错？ | (a) 报错（fail fast，审计主张）；(b) 保留多类型兼容 | (a) 报错 |
| 6.6 | B4 / M8 | 新增 `agent/go-service/pkg/fsutil` 是否可接受？审计建议先「提出方案」，当前仓库只有 2 处使用方（审计提到的另 2 处在本分支不存在）。 | (a) 新建公共包并复用；(b) 暂不动 | (a) 新建 |
| 6.7 | B1 之前 | `.dev_doc/` 当前未被 git 跟踪。审计报告是否随本计划一并纳入提交？ | (a) 首次提交一并纳入 `.dev_doc/autostockpile-redundancy-audit.md` + 本计划；(b) 只提交本计划，审计报告留在工作区 | (a) 一并纳入 |
| 6.8 | 每个批次 | 工作区 `go build ./...` 默认 vendor 模式失败（`vendor/modules.txt` beta.14 vs `go.mod` beta.18）。 | (a) 不改，验证统一用 `-mod=mod`（需联网/模块缓存）；(b) 允许先跑 `go mod vendor` 同步 vendor 目录（会产生大量 vendor 改动） | (a) 不改 |

### 6.9 确认记录

**2026-09-10：用户逐条确认第 6.1–6.8 全部采用推荐取向（即上表「默认」列），无一项改为备选。** 结论：

| # | 已确认结论 | 对执行的影响 |
| --- | --- | --- |
| 6.1 | 信任 `Validate()` 不变式，`selector.go` 两处都不判空 | M5 按「删兜底」实现 |
| 6.2 | Skip 分支永久废弃 | H6 按删除实现 |
| 6.3 | 永久放弃阈值 JSON 配置入口，只保留公式 | H1 删链；M3 删检查；`docs/zh_cn/.../auto-stockpile-maintain.md` 补说明 |
| 6.4 | 因删除而死掉的枚举、i18n 文案、函数一律保留 | 仅删不可达检查，不动 `AbortReasonThresholdConfigInvalidFatal`、其 5 语言文案与 `itemMapCounts` |
| 6.5 | ROI 类型收窄为 `TemplateMatch`，其他类型直接报错 | M2 按 fail fast 实现 |
| 6.6 | 允许新建 `agent/go-service/pkg/fsutil` | M8 按新建公共包实现 |
| 6.7 | 审计报告与本计划一并纳入基线提交 | 见下表「基线文档提交」 |
| 6.8 | 不动 `vendor/`、`go.mod`、`go.sum`，验证统一用 `-mod=mod` | 每批次验证命令按 1.4 执行 |

因此第 6 节不再构成断点；执行中出现**新的**设计取舍时仍按 1.3 硬性约束停下来提问，并把新结论追加到本小节。

**2026-09-10（B3 执行中追加）：H7 方案初稿与现有行为不等价，经用户确认改为真正等价的二分支。** 冲突点：初稿 `if data.Quota.Overflow > 0` 优先，而原 `switch` 以 `selection.CurrentPrice < selection.Threshold` 为第一判据；当 `Overflow > 0` 且选中商品低于阈值时，原实现返回 `SwipeMax`（低价买满），初稿返回「按防溢出数量购买」。用户选择「改成真正等价的二分支（价格优先）」，即仅删除不可达的 `default`、保留价格判据（§3 H7 已同步修正）；H7 仍属等价重构，§4.1 表述继续成立。

**2026-09-10（B4 之后追加）：6.1 的前提不成立，M5 判定为失败并已回退重修。** 用户复核时指出 `QuotaZeroSkip` + `Data == nil` 同样能通过 `Validate()`，经复查确认：

- 6.1 的两个选项 (a)/(b) 都建立在「删兜底后仅在**契约被破坏**时才会 panic」这一前提上，但该前提错误 —— `QuotaZeroSkip` 是**正常运行产物**（`quota.go:96-98`，额度用尽的日常路径），其 `Data == nil` 是设计使然，而 `selector.go:59` 的解引用早于两条 abort 路由，故 panic 在日常路径上必然发生。
- 根因不是「判空该不该删」，而是**解引用位置早于路由**。审计与 6.1 均未检查该顺序。
- **用户裁决（本次）**：执行「2 + 3」——(2) 只修代码，**不**新增包内回归测试（维持 `autostockpile` 零测试现状）；(3) 回写本文件与 `autostockpile-redundancy-audit.md` 两份文档。
- **实现取舍**：仍不恢复判空（维持 6.1 原本认可的「不引入两处语义分叉」），改为把解引用移到路由之后，使不变式在解引用点已由路由确立。
- **重修提交**：`8c45ca46`（`fix(autostockpile)`）；本文档状态回写为同步提交 `（本次提交）`。
- 6.1 表格保持原文以留痕，其结论作废，以本条为准。

### 6.10 已产生的文档提交

| 提交 | 内容 | 短 hash |
| --- | --- | --- |
| 基线文档提交 | 本计划 + `autostockpile-redundancy-audit.md`（6.7 确认纳入版本控制） | `a2652c0c` |
| 状态同步提交 | 本文件回填上述 hash 与 §6.9 确认记录 | `（本次提交）` |

**本次会话范围**：按用户指令，仅写入并提交文档，**未对任何生产代码做改动**（`git diff --stat` 为空）。B1–B4 的代码修复尚未开始，状态总览保持 `未进行`。

---

## 7. 失败与回退处理

- 某批次验证不通过且无法快速定位：把该批次仍为 `进行中` 的条目标为 `失败`，在备注列写明失败命令、报错与已尝试的修复方向，**提交二照常产生**（只含状态降级），并停下向用户报告。
- 某条目经用户确认不再需要修复：状态标 `失败`，备注写「用户确认不修复」，不产生该条目的代码改动。
- 已提交的修复若事后发现等价性判断有误：新建回退提交并修改状态为 `失败` 或重新 `进行中`，不得改写已推送的历史。
- 每批次开始前确认上一批次的提交一、提交二都已产生（`git log --oneline -4`），防止状态与提交错位。
