# MaaEndBot 分析报告 - Issue #1290

## 问题理解

您希望为「自动倒卖」任务新增一个**「只购买不出售」**选项。当勾选该选项时：
- ✅ 自动购买当日降价幅度最高（即利润最高）的物品
- ❌ 不执行进入好友飞船售卖的流程

## 当前实现分析

### 现有选项逻辑

当前 `AutoResell` 任务通过 `AutoBuyAndSell` 选项控制行为：

| 选项值 | 行为 |
|--------|------|
| **Yes** | 自动购买 + 自动售卖（需配置最低利润 `ImportMinimumProfit`） |
| **No** | 仅识别价格并给出购买建议，**不实际购买和售卖** |

因此目前缺少「购买但不售卖」的中间态。

### 相关代码位置

1. **任务配置**：`assets/tasks/AutoResell.json`
   - `AutoBuyAndSell` 为二值开关（Yes/No）
   - Yes 时使用 `ImportMinimumProfit` 控制购买阈值
   - No 时通过 `pipeline_override` 将 `MinimumProfit` 设为 999999，使 `ResellDecideAction` 不触发购买

2. **购买后流程**：`assets/resource/pipeline/Resell/SelectProduct.json`
   - 购买成功后：`ResellReturnToStore` → `ResellScrollToTop` → `ResellGotoSell`
   - `ResellGotoSell` 负责进入库存、选择好友、进入飞船进行售卖

3. **决策逻辑**：`agent/go-service/resell/decide.go`
   - 根据扫描结果选择**利润最高**的商品（`Profit = 好友出售价 - 成本价`）
   - 利润 ≥ `MinimumProfit` 时执行购买

## 实现建议

### 方案一：将 AutoBuyAndSell 改为三档开关（推荐）

将 `AutoBuyAndSell` 从二值改为三档，新增「只购买」选项：

| 选项值 | 行为 |
|--------|------|
| **BuyAndSell** | 购买 + 售卖（原 Yes） |
| **OnlyBuy** | 仅购买，不售卖（新增） |
| **OnlySuggest** | 仅建议，不购买不售卖（原 No） |

**修改要点：**

1. **`assets/tasks/AutoResell.json`**：扩展 `AutoBuyAndSell` 的 `cases`：

```json
"cases": [
    {
        "name": "BuyAndSell",
        "option": ["ImportMinimumProfit"]
    },
    {
        "name": "OnlyBuy",
        "option": ["ImportMinimumProfit"],
        "pipeline_override": {
            "ResellReturnToStore": {
                "next": ["ResellMain"]
            }
        }
    },
    {
        "name": "OnlySuggest",
        "pipeline_override": {
            "ResellStart": {
                "action": {
                    "param": {
                        "custom_action_param": {
                            "MinimumProfit": 999999
                        }
                    }
                }
            }
        }
    }
]
```

2. **`assets/misc/locales/`**：更新各语言文件，将 `AutoBuyAndSell` 改为三档选项的文案，并新增 `OnlyBuy` 的 label 和 description。

3. **`assets/tasks/preset/AutoTrading.json`**：若预设使用 `AutoBuyAndSell: "Yes"`，需改为 `"BuyAndSell"` 以保持兼容。

### 方案二：新增独立选项

保留现有 `AutoBuyAndSell`，新增 `OnlyBuy` 开关。两者互斥时，由 `OnlyBuy` 优先控制「只购买不出售」行为。实现上需处理选项组合逻辑，复杂度更高，不推荐。

## 关于「当日降价幅度最高」

当前实现按**利润**（好友出售价 - 成本价）选择商品，与「当日降价幅度最高」在逻辑上等价：成本价可视为当日市场价，利润高即降价幅度大。若游戏内另有「降价幅度」的专门定义，可再在 `ResellDecideAction` 或扫描逻辑中补充对应计算。

## 注意事项

1. **协议合规**：`pipeline_override` 的 `next` 覆盖需符合 [MaaFramework Pipeline 协议](https://github.com/MaaXYZ/MaaFramework/raw/refs/heads/main/docs/en_us/3.1-PipelineProtocol.md)。
2. **国际化**：新增选项需在 `zh_cn`、`zh_tw`、`en_us`、`ja_jp`、`ko_kr` 中同步文案。
3. **格式化**：修改 JSON 后请执行 Prettier，符合 `.prettierrc` 规范。

---

如需进一步协助实现或测试，欢迎继续补充需求或环境信息。
