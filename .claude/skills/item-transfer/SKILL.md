---
name: item-transfer
description: 仅当用户明确要求往「🐌库存转移 / ItemTransfer」任务里**新增可搬运物品**（即在 `assets/tasks/ItemTransfer.json` 的 `option.WhatToTransfer.cases` 里加 case，并同步 `assets/data/ItemTransfer/item_order.json` 与 5 个 locale 文件的 `item.*`）时使用。典型触发用语：「给库存转移加 xxx」「ItemTransfer 里新增 xxx」「WhatToTransfer 多一项 xxx」。**不要**在以下场景触发：修改 / 删除已有 ItemTransfer 物品、只改 pipeline 参数或模板图、其他任务的物品配置（AutoStockpile / AutoStockStaple / CreditShopping / SellProduct / DeliveryJobs / BatchUseDetector 等）、物品模型重训 / 新增 class ID、纯咨询"库存转移能搬什么"。
---

# ItemTransfer 新增物品指南

本指南用于在「🐌库存转移（ItemTransfer）」任务中新增一个可选择的物品。整体流程分为两个部分：**第一部分是用户要做的事**，**第二部分是交给 AI 执行的事**。

---

## 第一部分：用户操作步骤（Agent 跳过此部分）

> 这一部分是用户需要亲自在游戏里确认并在 JSON 中填好的内容，AI 不会替你判断。

### 1. 在游戏内确定物品在仓库中的排列顺序

1. 打开游戏内的仓库界面，切到物品对应的分页（矿物 / 植物 / 产物 / 可用道具）。
2. 排序方式与游戏默认一致（默认按升序看一遍，**从左到右、从上到下**记录下所有物品的顺序）。
3. 找到新增的物品在该分页里的位置，确认它**前后是哪一个物品**。

### 2. 把新增物品的中文名插入 `assets/data/ItemTransfer/item_order.json` 的 `category_order`

- **用户只需要改 `category_order`，不要动 `items` 字段。**
  `items` 里的数字 key 是识别模型的真实 class ID，由开发者在重训模型时统一维护，用户没法也不应该自己编一个。
- 在 `category_order.<category>`（`Ore` / `Plant` / `Product` / `Usable` 四选一）数组里，把新增物品的**中文名**按第 1 步游戏里观察到的位置**插入到正确的位置**。
    - 前一个是谁、后一个是谁必须和游戏里完全一致。
    - 中文名要和游戏里显示的完全一致，不要加空格或改字。

> ⚠️ `category_order` 里的顺序，是后续 OCR 滚动识别和 **locales / ItemTransfer.json 插入顺序**的唯一权威来源，必须和游戏里肉眼看到的顺序一致。

### 3. 告诉 AI 两件事

请在向 AI 下达任务时，明确告诉它以下两点，否则 AI 无法选对模板：

- **识别方式**：这个物品是「**可识别**」（有训练好的分类模型，用 `ItemTransferFindItemInRepo` 的 `expected` / `target_class` 匹配），还是「**纯 OCR**」（没有分类模型，只能用 `ItemTransferFindItemWithOCR` 按中文名识别）。
    - 一般新物品、模型没来得及训的物品用「纯 OCR」。
    - 老物品、有分类 ID 的用「可识别」。
- **排序方向**：这个物品在升序排列下靠前，就用「**升序**」；在升序下非常靠后、但在降序下靠前，就用「**降序**」。
    - 目的是让 OCR 滚动查找从近端开始，减少滚动距离。

把这两个信息给 AI 后，AI 会按第二部分的流程自动完成剩下的写入。

---

## 第二部分：AI 执行步骤（仅供 Agent 阅读）

> 以下是交给 AI 的执行流程，用户可忽略。Agent 在收到用户「新增某物品」的请求时，按步骤执行。

### 前置条件

在开始前，确保用户已经：

1. 在 `assets/data/ItemTransfer/item_order.json` 的 `category_order.<category>` 里把新增物品的**中文名**插到了正确位置。
2. 明确告知该物品是「可识别 / 纯 OCR」以及「升序 / 降序」。

若用户没有完整写明具体物品，使用 `git diff` 查看 `assets/data/ItemTransfer/item_order.json`，确认新增物品都有哪些。
如果任一信息缺失，**必须先向用户确认**，不要擅自猜测。

注意：`item_order.json` 里的 `items` 字段是识别模型的真实 class ID 表，由开发者维护，**Agent 不要新增或修改 `items` 条目**。

- 如果用户选「可识别」，说明该物品**已经**在 `items` 里有对应条目：Agent 需要用该中文 `name` 在 `items` 里反查出数字 key，作为下文模板里 `<Id>`（`expected` / `target_class`）的值。
- 如果在 `items` 里查不到该物品、但用户仍坚持「可识别」，**必须停下来**提示用户：该物品当前模型没有类别，只能用「纯 OCR」，请用户改口后再继续。
- 如果用户选「纯 OCR」，则 Agent 完全不需要读 `items`，也不需要任何数字 ID。

### 作用域约束

**Agent 只处理用户本次明确指定的新增物品**，不要横向扫描 `category_order` / `ItemTransfer.json` / locale 文件做"一致性比对"。

- `category_order` 里存在一些条目**本来就**不在 `items` 里、不在 `assets/tasks/ItemTransfer.json` 的 `option.WhatToTransfer.cases` 里。这是**有意为之**，不是遗漏。
- 发现这些"差异"时，**不要去汇报、不要去修复、不要追问用户**。只要专注完成本次用户点名的那几个物品的写入即可。

### 步骤 1：查找物品的英文字段名（label 的 i18n key）

1. 读取 `assets/locales/interface/en_us.json` 以及 `zh_cn.json`，搜索该物品所属类别下已有的命名风格（例如 `item.CupriumOre`、`item.AmberRice`、`item.AketinePowder`）。
2. 根据游戏内已发布的英文名 / 日文名 / 韩文名 / 繁中名，确定该物品的规范字段名 `item.<EnglishPascalCase>`。
    - 命名参考已有条目的风格（矿 = `*Ore`、瓶 = `*Bottle`、块 = 词根本身、粉末 = `*Powder`、种子 = `*Seed`、装备原件 = `*Component`、零件 = `*Part`、电池 = `LC/SC/HC` 容量前缀 + `*Battery` 等）。
    - 如果查不到官方翻译，优先使用语义合理的英文 PascalCase 名，并在回复里告诉用户「我这里用的名字是 xxx，如果官方译名不同请告诉我修改」。
3. 确定好字段名后（例如 `item.LCValleyBattery`），准备五种语言的翻译：
    - `zh_cn` — 中文简体（等于 `item_order.json` 里的 `name`）。
    - `zh_tw` — 中文繁体。
    - `en_us` — 英文。
    - `ja_jp` — 日文。
    - `ko_kr` — 韩文。
    - 如果某种语言的官方译名暂时查不到，则直接对中文名进行翻译，并提示用户确认。

### 步骤 2：按 `category_order` 的位置，写入 5 个 locale 文件

对 `assets/locales/interface/` 下的 **5 个文件**（`zh_cn.json`、`zh_tw.json`、`en_us.json`、`ja_jp.json`、`ko_kr.json`）执行相同的插入：

1. 读取 `assets/data/ItemTransfer/item_order.json` 的 `category_order.<category>`，找到新增物品在数组里的**前一项**与**后一项**。
2. 在 locale 文件中搜索前一项对应的 `item.*` key（例如前一项是「赤铜瓶」→ 搜 `"item.CupriumBottle":`）。
3. 在该行**下方**、后一项的 `item.*` key **上方**，插入新增物品的键值对：
    ```json
        "item.<EnglishPascalCase>": "<该语言的译名>",
    ```
4. 保持缩进、引号、逗号与相邻行严格一致，不要破坏 JSON 结构。

> 💡 locale 文件里 `item.*` 的顺序与 `category_order` 严格一致，插入时务必按这个顺序；不要随意追加到末尾。

### 步骤 3：按 `category_order` 的位置，写入 `assets/tasks/ItemTransfer.json`

#### 3.1 找到插入位置

1. 在 `option.WhatToTransfer.cases` 里，搜索前一项的 `"name": "<前一项中文名>"`，定位到它的 case 对象。
2. 在该 case 对象的 **闭合 `}`** 后、后一项 case 的 **`{`** 前，插入新增物品的 case 对象。
3. 如果新增物品是类别里的第一项或最后一项，按 `category_order` 自然就能推导出插入点（类别的所有项在 `cases` 里是连续成段的）。

#### 3.2 选择模板

根据用户告知的「识别方式 + 排序方向」组合，选对应的模板。四个模板如下。占位符含义：

- `<Name>` — 物品中文名（和 `item_order.json` 的 `name`、以及 `category_order` 里的字符串完全一致）。
- `<LabelKey>` — 步骤 1 确定的英文字段名（例如 `CupriumOre`），`label` 写成 `"$item.<LabelKey>"`。
- `<Category>` — `Ore` / `Plant` / `Product` / `Usable`，首字母大写，对应 `template` 的文件名 `ItemTransfer/<Category>.png`。
- `<Id>` — `item_order.json` 里该物品的数字 ID。**只有「可识别」模板才需要 `<Id>`，「纯 OCR」模板用不到。**

---

##### 模板 A：可识别 + 升序（参考：`蓝铁矿`）

```json
{
    "name": "<Name>",
    "label": "$item.<LabelKey>",
    "pipeline_override": {
        "ItemTransferClickItemCategory": {
            "template": "ItemTransfer/<Category>.png"
        },
        "ItemTransferFindItemInRepo": {
            "expected": <Id>
        },
        "ItemTransferFindItemInBag": {
            "expected": <Id>
        },
        "ItemTransferFindItemInBagReturn": {
            "expected": <Id>
        },
        "ItemTransferFindItemFallback": {
            "custom_action_param": {
                "target_class": <Id>
            }
        },
        "ItemTransferFindItemFallbackBag": {
            "custom_action_param": {
                "target_class": <Id>
            }
        },
        "ItemTransferFindItemFallbackBagReturn": {
            "custom_action_param": {
                "target_class": <Id>
            }
        }
    }
}
```

##### 模板 B：可识别 + 降序（参考：`源矿`）

```json
{
    "name": "<Name>",
    "label": "$item.<LabelKey>",
    "pipeline_override": {
        "ItemTransferClickItemCategory": {
            "template": "ItemTransfer/<Category>.png"
        },
        "ItemTransferFindItemInRepo": {
            "expected": <Id>
        },
        "ItemTransferFindItemInBag": {
            "expected": <Id>
        },
        "ItemTransferFindItemInBagReturn": {
            "expected": <Id>
        },
        "ItemTransferRepoToBag": {
            "next": [
                "ItemTransferClickSortDescending",
                "ItemTransferClickItemCategory",
                "ItemTransferFindItemInRepo",
                "ItemTransferScrollDownwardRepo"
            ]
        },
        "ItemTransferFindItemFallback": {
            "custom_action_param": {
                "target_class": <Id>,
                "descending": true
            }
        },
        "ItemTransferFindItemFallbackBag": {
            "custom_action_param": {
                "target_class": <Id>
            }
        },
        "ItemTransferFindItemFallbackBagReturn": {
            "custom_action_param": {
                "target_class": <Id>
            }
        }
    }
}
```

##### 模板 C：纯 OCR + 升序（参考：`赤铜瓶`）

```json
{
    "name": "<Name>",
    "label": "$item.<LabelKey>",
    "pipeline_override": {
        "ItemTransferClickItemCategory": {
            "template": "ItemTransfer/<Category>.png",
            "next": [
                "ItemTransferFindItemWithOCR"
            ]
        },
        "ItemTransferRepoToBag": {
            "next": [
                "ItemTransferClickSortAscending",
                "ItemTransferClickItemCategory",
                "ItemTransferFindItemWithOCR"
            ]
        },
        "ItemTransferBagToRepo": {
            "next": [
                "ItemTransferFindItemWithOCRBag"
            ]
        },
        "ItemTransferBagToOriginRepo": {
            "next": [
                "ItemTransferFindItemWithOCRBagReturn"
            ]
        },
        "ItemTransferFindItemWithOCR": {
            "custom_action_param": {
                "item_name": "<Name>"
            }
        },
        "ItemTransferFindItemWithOCRBag": {
            "custom_action_param": {
                "item_name": "<Name>"
            }
        },
        "ItemTransferFindItemWithOCRBagReturn": {
            "custom_action_param": {
                "item_name": "<Name>"
            }
        }
    }
}
```

##### 模板 D：纯 OCR + 降序（参考：`赤铜块`）

```json
{
    "name": "<Name>",
    "label": "$item.<LabelKey>",
    "pipeline_override": {
        "ItemTransferClickItemCategory": {
            "template": "ItemTransfer/<Category>.png",
            "next": [
                "ItemTransferFindItemWithOCR"
            ]
        },
        "ItemTransferClickSortDescending": {
            "next": [
                "ItemTransferClickItemCategory",
                "ItemTransferFindItemWithOCR"
            ]
        },
        "ItemTransferRepoToBag": {
            "next": [
                "ItemTransferClickSortDescending",
                "ItemTransferClickItemCategory",
                "ItemTransferFindItemWithOCR"
            ]
        },
        "ItemTransferBagToRepo": {
            "next": [
                "ItemTransferFindItemWithOCRBag"
            ]
        },
        "ItemTransferBagToOriginRepo": {
            "next": [
                "ItemTransferFindItemWithOCRBagReturn"
            ]
        },
        "ItemTransferFindItemWithOCR": {
            "custom_action_param": {
                "item_name": "<Name>",
                "descending": true
            }
        },
        "ItemTransferFindItemWithOCRBag": {
            "custom_action_param": {
                "item_name": "<Name>"
            }
        },
        "ItemTransferFindItemWithOCRBagReturn": {
            "custom_action_param": {
                "item_name": "<Name>"
            }
        }
    }
}
```

> 注意：**"升序 / 降序"只适用于仓库查找这一个环节**，模板里对应 `ItemTransferFindItemWithOCR`（纯 OCR）或 `ItemTransferFindItemFallback`（可识别）。只有它需要写 `"descending": true`。
> 其他 `*Bag` / `*BagReturn` 分支（背包、背包回放）**没有升降序的概念**，模板里就是不写 `descending` 字段，按模板原样照抄即可。不要把这种"只有一个地方带 descending"视为不一致或漏写，这是现有所有条目的统一写法。

#### 3.3 插入 case 对象

1. 把选好的模板填入实际的 `<Name>` / `<LabelKey>` / `<Category>` / `<Id>`。
2. 将填好的 case 对象插入到 3.1 定位到的位置。
3. 确保相邻 case 之间有且仅有一个逗号分隔，末尾最后一个 case 后无多余逗号。
4. 插入完成后，用 JSON 解析器检查 `assets/tasks/ItemTransfer.json` 没有语法错误（所有大括号/中括号/逗号匹配）。

### 步骤 4：自检清单

完成后，Agent 必须逐项确认：

- [ ] `assets/data/ItemTransfer/item_order.json` 的 `category_order.<category>` 已包含新物品，且位置和游戏内肉眼观察顺序一致。
- [ ] **没有**擅自修改 `item_order.json` 的 `items` 字段（那是模型类别表，只由开发者维护）。
- [ ] 如果用了「可识别」模板，`<Id>` 是从现有 `items` 里按中文 `name` 反查到的真实 class ID，不是自己编的。
- [ ] 5 个 locale 文件（`zh_cn` / `zh_tw` / `en_us` / `ja_jp` / `ko_kr`）里都新增了 `item.<LabelKey>` 条目，且顺序和 `category_order` 一致。
- [ ] `assets/tasks/ItemTransfer.json` 的 `option.WhatToTransfer.cases` 里新增了该物品的 case，位置正确、模板选对（识别方式 × 排序方向 = 4 选 1）。
- [ ] `<Category>` 对应的 `template` 文件 `ItemTransfer/<Category>.png` 写法与同类别其他条目一致。
- [ ] 所有 JSON 文件可以被正常解析，逗号 / 引号没写错。

若任何一项没通过，先修复再回复用户。回复用户时，简要说明改了哪几个文件、新增物品落在哪个位置、用了哪个模板，让用户二次确认游戏内顺序是否和代码里一致。
