# 开发手册 - BetterSliding 参考文档

该CustomAction支持对滑块进行滑动，支持滑动到指定数值

![BetterSliding示例](https://github.com/user-attachments/assets/cad74409-911e-43aa-81ba-3d540e2bf6d9)

如上图所示，可通过`SwipeButton`实现滑动，并通过`DecreaseButton`与`IncreaseButton`进行精确操作

> [!note]
> 部分滑条在可滑动数量为1时会隐藏，请注意处理该种情况。

## 仅滑动模式

适合滑动到最大/最小的情景，仅能传入下述参数。仅滑动模式由参数推断：一旦传入指定数量模式字段，即按指定数量模式校验。如需精确控制数量，请跳转下文[指定数量模式](#指定数量模式)。

### 参数说明

| 字段 | 类型 | 必填 | 说明 |
| ---------------------- | -------- | ---- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `Direction` | `string` | 是 | 滑动方向。支持 `left` / `right` / `up` / `down`。 |
| `SwipeButton` | `string` 或 `object` | 否 | 滑块模板识别参数。提供时覆盖 `BetterSlidingSwipeButton` 节点的识别参数。默认不配置（使用共享默认模板 `BetterSliding/SwipeButton.png`）。写法见[识别参数：String 与 Object](#识别参数string-与-object)。 |
| `ResetBeforeFindStart` | `bool` | 否 | 为 `true` 时，先向最小方向滑动复位，再匹配滑块起始位置并执行滑动。默认 `false`。 |

> [!note]
> `SwipeButton` 使用模板匹配时默认开启绿色掩码（`green_mask: true`）。可在补丁中显式写 `"green_mask": false` 覆盖该默认值。请按默认模板的涂绿方式处理模板图片（不参与匹配的部分涂绿 RGB: (0, 255, 0)）。

### 示例

```json
"SomeTaskSwipeToMax": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "SwipeButton": { "template": "BetterSliding/SwipeButton.png" }
            }
        }
    }
}
```

## 指定数量模式

> [!important]
> 在CustomAction执行前，请确保滑块位于初始值，且初始值为1。否则将无法计算滑块在最小与最大的位置偏差，导致数量调整失效。若调用方无法保证滑块位于初始值，可设置 `ResetBeforeFindStart: true`，BetterSliding 会在匹配起始位置前先向最小方向滑动复位。

> [!note]
> 当解析后的目标数量严格大于滑条最大数量的 80% 时，BetterSliding 会在记录终点位置后、执行精确点击前，先向最小方向滑动复位一次，再从最小值起按比例精确点击，保证靠近最大端的档位也能稳定命中。目标数量等于最大数量时仍直接完成，不执行复位。该行为默认开启，无需额外参数。

### 参数说明

#### 可在 `attach` 中传入的参数

以下 6 个字段推荐通过调用节点的 `attach` 传入，`attach` 优先级高于 `custom_action_param` 中的同名字段。

| 字段 | 类型 | 必填 | 说明 |
| ------------------------- | --------------- | ---- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TargetQuantity` | `int`（正整数） | 是 | 目标数量。最终希望滑到的档位值，必须大于 0。 |
| `TargetQuantityType` | `string` | 否 | 如何解释 `TargetQuantity`。`"Value"`（默认）：绝对离散计数；`"Percentage"`：`availableQuantity` 的百分比（1–100），四舍五入后钳制到 `[1, availableQuantity]`。 |
| `ReverseTarget` | `bool` | 否 | 为 `true` 时从可用总量反向计算目标：Value 模式为 `availableQuantity - TargetQuantity`；Percentage 模式按剩余百分比计算。默认 `false`。 |
| `FineTuneQuantity` | `bool` 或 `int` | 否 | 精确点击后是否继续用 Increase/Decrease 微调。`true`（默认）：始终微调；`false`：一律不微调；整数 `N`（须 `>= 1`）：仅当 `abs(当前数量 − 目标数量) <= N` 时微调。 |
| `FineTuneFallback` | `string` | 否 | 仅在本次判定为「不微调」时生效，控制此时的行为。`"none"`（默认）：不做补偿，直接收尾；`"more"` / `"less"`：朝增大/减小数量的方向补偿并复查，详见[不微调语义](#不微调语义)。 |
| `ResetBeforeFindStart` | `bool` | 否 | 为 `true` 时，在匹配滑条起始位置前先向最小方向滑动复位，保证后续记录到的起始位置为最小值。默认 `false`。 |

> [!note]
> `TargetQuantityType` 与 `ReverseTarget` 的组合计算逻辑：
>
> | TargetQuantityType | ReverseTarget | 有效目标 |
> | ------------------ | ------------- | ------------------------------------------------------------------------------------------ |
> | `"Value"` | `false` | `TargetQuantity`（原值） |
> | `"Value"` | `true` | `availableQuantity - TargetQuantity`（不钳制，可能 < 1） |
> | `"Percentage"` | `false` | `round(availableQuantity × TargetQuantity / 100)`，钳制到 `[1, availableQuantity]` |
> | `"Percentage"` | `true` | `round(availableQuantity × (100 - TargetQuantity) / 100)`，钳制到 `[1, availableQuantity]` |

#### 仅能通过 `custom_action_param` 传入的参数

除上述 6 个字段外，其余参数都只能从 `custom_action_param` 读取：

| 字段 | 类型 | 必填 | 说明 |
| ------------------------------- | ---------------------------- | ---- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `Direction` | `string` | 是 | 滑动方向。指定"最大值所在方向"，支持 `left` / `right` / `up` / `down`。 |
| `IncreaseButton` | `int[2\|4]` 或 `string` 或 `object` | 是 | "增加数量"按钮。传坐标 `[x, y]` / `[x, y, w, h]` 时直接点击该区域；传 String / Object 时按[识别参数](#识别参数string-与-object)做模板匹配，默认阈值 `0.8` 与 `green_mask: true`，命中后点击识别框。 |
| `DecreaseButton` | `int[2\|4]` 或 `string` 或 `object` | 是 | "减少数量"按钮。格式同 `IncreaseButton`。 |
| `SwipeButton` | `string` 或 `object` | 否 | 滑块识别参数，覆盖 `BetterSlidingSwipeButton` 节点。默认不配置（使用共享默认模板）。 |
| `SliderQuantity` | `string` 或 `object` | 是 | 当前滑条数量 OCR 识别参数，覆盖 `BetterSlidingGetSliderQuantity` 节点；例如 `{"roi": [x, y, w, h], "only_rec": true}`。 |
| `SliderQuantityFilter` | `string` 或 `object` | 否 | 当前滑条数量 OCR 预处理使用的颜色过滤（ColorMatch）识别参数，覆盖 `BetterSlidingSliderQuantityFilter` 节点，并通过 `color_filter` 链接到滑条数量节点。默认不配置。 |
| `AvailableQuantity` | `string` 或 `object` | 否 | 可用总量 OCR 识别参数，覆盖并启用 `BetterSlidingGetAvailableQuantity`。仅当完全不提供 `AvailableQuantity`（或写 `null`）时，才使用滑条终点值作为计算基准。 |
| `AvailableQuantityFilter` | `string` 或 `object` | 否 | 可用总量 OCR 预处理使用的颜色过滤（ColorMatch）识别参数，覆盖 `BetterSlidingAvailableQuantityFilter` 节点。默认不配置。 |
| `CenterPointOffset` | `int[2]` | 否 | 相对滑块识别框中心点的点击偏移 `[x, y]`，负数向左/上，正数向右/下。默认 `[-10, 0]`。 |
| `ClampTargetToSliderMax` | `bool` | 否 | 为 `true` 时，若目标超过 `sliderMaxQuantity`，则钳制为滑条最大可选数量继续执行。默认 `false`。 |
| `OutOfRangeOverrideEnable` | `string` | 否 | 当解析后的目标超出可滑动范围时，将指定 Pipeline 节点的 `enabled` 设为 `true` 并返回成功；未配置该字段（默认 `""`）时，本次动作直接返回失败。 |
| `TargetReachableOverrideEnable` | `string` | 否 | 当解析后的目标无需钳制且位于 `[1, sliderMaxQuantity]` 时，将指定 Pipeline 节点的 `enabled` 设为 `true`。默认 `""`。 |

### 识别参数：String 与 Object

`SwipeButton`、`SliderQuantity`、`SliderQuantityFilter`、`AvailableQuantity`、`AvailableQuantityFilter` 以及模板形态的 `IncreaseButton` / `DecreaseButton` 统一接受两种写法：

| 写法 | 语义 |
| -------- | --------------------------------------------------------------------------------------------------------- |
| `string` | **节点引用**。读取该节点的 `recognition.param` 作为识别参数补丁（不做后缀判断，任何字符串都按节点名解析）。 |
| `object` | **识别参数补丁**。对象内容即目标节点的 `recognition.param` 键值。 |

示例：

```jsonc
"custom_action_param": {
    "Direction": "right",
    // String：引用一个已有的 OCR / 模板节点，复用其识别参数
    "SliderQuantity": "SomeExistingQuantityOCRNode",
    // Object：直接给出识别参数补丁
    "AvailableQuantity": { "roi": [1073, 327, 119, 25], "only_rec": true },
    "SliderQuantityFilter": { "method": 4, "lower": [75, 75, 75], "upper": [255, 255, 255] },
    "IncreaseButton": { "template": "AutoStockpile/IncreaseButton.png" },
    "DecreaseButton": [965, 570, 20, 10]
}
```

约束与行为：

- **禁止替换识别类型**：补丁只写入 `recognition.param`，不写 `type`，因此会保留目标节点原本的识别算法与未提及字段。Object 内出现 `recognition` / `type` / `action` 键会直接报错并返回失败，不会被静默忽略。
- **按钮模板形态**：`IncreaseButton` / `DecreaseButton` 传 String / Object 时，模板参数写入 `BetterSlidingIncreaseButton` / `BetterSlidingDecreaseButton` 节点，数量节点以 `And all_of` 引用它并点击命中框。补丁默认补充 `green_mask: true`，可在补丁中显式覆盖。
- **数组仅按钮可用**：除 `IncreaseButton` / `DecreaseButton` 外的参数写数组会直接报错；按钮数组是坐标而非识别补丁。
- **Filter 与 `color_filter` 的优先级**：`SliderQuantityFilter` / `AvailableQuantityFilter` 会写入对应内建 Filter 节点，并把节点名填进 Quantity 补丁的 `color_filter`；若 Quantity 补丁自身已声明 `color_filter`，以补丁为准。未配置 Filter 时不写 `color_filter`。
- **Filter 的 String 引用应指向 ColorMatch 节点**：`color_filter` 在识别阶段按节点名查找并要求其识别类型为 ColorMatch，指向其他类型会识别失败。参数层面不做校验，请自行保证引用类型正确。
- **`AvailableQuantityFilter` 可独立配置**：未同时提供 `AvailableQuantity` 时，Filter 参数仍会写入内建节点，但 `BetterSlidingGetAvailableQuantity` 保持 `enabled: false`（不告警）。
- **定量模式判据**：任意一个 Filter 参数存在即视为定量模式（退出仅滑动模式）。

### 最小值短路

当 `TargetQuantityType` 为 `"Value"`（大小写不敏感）、`TargetQuantity` 为 `1` 且 `ReverseTarget` 为 `false` 时，目标即滑条最小值，BetterSliding 会走短路路径，跳过滑条最大数量 OCR、终点识别与精确点击：

| `ResetBeforeFindStart` | 行为 |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| `true` | 只执行一次向最小方向的复位滑动（`BetterSlidingFindSwipeForReset` → `BetterSlidingReset`），复位完成后直接结束。 |
| `false`（默认） | 不执行任何识别、滑动与点击，直接成功返回。**调用方必须保证滑条当前已位于初始值 1**。 |

> [!note]
> `Percentage` 模式与 `ReverseTarget: true` 不参与短路：它们的有效目标取决于运行时读到的 `availableQuantity`，无法在进入流程前判定，因此仍走完整流程。

### 不微调语义

`FineTuneFallback` 只在**本次判定为不微调**时生效：`FineTuneQuantity` 为 `false` 时始终不微调；为整数阈值 `N` 时，仅当当前数量与目标数量的差值不大于 `N` 才微调，否则不微调。此时 BetterSliding 不再逐步逼近目标，而是按 `FineTuneFallback` 决定如何收尾：

| 取值 | 行为 |
| --- | --- |
| `"none"`（默认） | 不做任何补偿，本次调整到此结束。 |
| `"more"` | 当前数量小于目标数量时，朝**增大数量**的方向补偿并复查；当前数量不小于目标数量时不做补偿，直接结束。 |
| `"less"` | 当前数量大于目标数量时，朝**减小数量**的方向补偿并复查；当前数量不大于目标数量时不做补偿，直接结束。 |

### 结果节点契约

`OutOfRangeOverrideEnable` 与 `TargetReachableOverrideEnable` 用于把本次 BetterSliding 的判定传回调用方：每次判定至多启用其中一个节点（另一个若已配置会被设为 `enabled: false`）。两个参数必须引用不同节点，且结果节点建议默认设置 `enabled: false`。

| 解析后的目标 | override node | BetterSliding 行为 |
| --- | --- | --- |
| 小于 1、`sliderMaxQuantity` 为 0，或未钳制时大于滑条最大数量 | `OutOfRangeOverrideEnable` | 不调整数量并返回成功；未配置该字段时本次动作直接失败 |
| 位于 `[1, sliderMaxQuantity]` | `TargetReachableOverrideEnable` | 调整到目标数量 |
| 大于 `sliderMaxQuantity` 且启用钳制 | 无 | 调整到 `sliderMaxQuantity`，尚不能达到原始目标 |

`sliderMaxQuantity == 0` 只表示当前没有可选的正数目标，BetterSliding 不推断余额不足、库存不足或控件不可用等业务原因。调用方如需区分具体状态，应在 Pipeline 中识别对应界面。

> [!note]
> 最小值短路命中时，BetterSliding 未读取 `sliderMaxQuantity`，但仍把 `TargetReachableOverrideEnable` 指向的节点置为 `true`（目标即滑条最小值，必然可达），`OutOfRangeOverrideEnable` 保持 `false`。

> [!important]
> `TargetReachableOverrideEnable` 只表示**解析后的目标可达**，与最终调整结果无关：该判定在读取目标数量与滑条上限时即已确定，之后无论微调、偏移复查是否命中目标，都不会改变它。它只表示调用方的下一步操作可以达到目标，不表示该操作已经成功。例如售卖、购买等流程仍须在外层 Pipeline 确认交易成功后，才能记录业务目标已完成。

### 示例

```json
"SomeTaskAdjustQuantity": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "IncreaseButton": { "template": "AutoStockpile/IncreaseButton.png" },
                "DecreaseButton": { "template": "AutoStockpile/DecreaseButton.png" },
                "SliderQuantity": {
                    "roi": [340, 430, 200, 140]
                }
            }
        }
    },
    "attach": {
        "TargetQuantity": 50,
        "TargetQuantityType": "Percentage",
        "ReverseTarget": false
    }
}
```
