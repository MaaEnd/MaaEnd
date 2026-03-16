# 开发手册 - QuantizedSliding 参考文档

`QuantizedSliding` 是一个通过 `Custom` 节点调用的 go-service 自定义动作，
用于处理“拖动滑条选择数量，但目标值是离散档位”的界面。

它适合下面这类场景：

- 先拖到大致位置，再通过 `+` / `-` 按钮微调数量；
- 拖条本身没有稳定的固定坐标，但滑块模板可以识别；
- 当前数量可以通过 OCR 读出，且界面最大值会随库存或条件变化。

当前实现位于：

- Go 动作：`agent/go-service/quantizedsliding/action.go`
- 公共 Pipeline：`assets/resource/pipeline/QuantizedSliding/Main.json`
- 现有接入示例：`assets/resource/pipeline/AutoStockpile/Task.json`

## 它是怎么工作的

`QuantizedSliding` 不是“按固定百分比滑到某个位置”，而是一个**先探测、再计算、再微调**的流程。

整体步骤如下：

1. 识别滑块当前位置，记录滑动起点。
2. 将滑块拖到最大值。
3. OCR 识别当前最大可选数量。
4. 再次识别滑块位置，记录滑动终点。
5. 根据 `Target` 与 `maxQuantity` 计算精确点击位置。
6. 点击该位置。
7. OCR 再次识别当前数量；若仍不等于目标值，则通过加减按钮微调。
8. 数量与目标一致后结束。

对应的内部节点由 `QuantizedSliding` 自己调用 `QuantizedSlidingMain` 子流程完成，调用方**不需要**手动串这些内部节点。

## 调用方式

在业务 Pipeline 中，像普通 `Custom` 动作一样调用即可：

```json
"SomeTaskAdjustQuantity": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "QuantizedSliding",
            "custom_action_param": {
                "Target": 1,
                "QuantityBox": [360, 490, 110, 70],
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "DecreaseButton": "AutoStockpile/DecreaseButton.png"
            }
        }
    }
}
```

## 参数说明

`custom_action_param` 需要传入一个 JSON 对象，字段如下：

| 字段             | 类型                    | 必填 | 说明                                              |
| ---------------- | ----------------------- | ---- | ------------------------------------------------- |
| `Target`         | `int`                   | 是   | 目标数量。最终希望调到的档位值。                  |
| `QuantityBox`    | `int[4]`                | 是   | 当前数量 OCR 区域，格式固定为 `[x, y, w, h]`。    |
| `Direction`      | `string`                | 是   | 拖动方向，支持 `left` / `right` / `up` / `down`。 |
| `IncreaseButton` | `string` 或 `int[2\|4]` | 是   | “增加数量”按钮。可传模板路径，也可传坐标。        |
| `DecreaseButton` | `string` 或 `int[2\|4]` | 是   | “减少数量”按钮。可传模板路径，也可传坐标。        |

### `IncreaseButton` / `DecreaseButton` 的写法

这两个字段支持两种形式：

#### 1. 传模板路径（推荐）

```json
"IncreaseButton": "AutoStockpile/IncreaseButton.png"
```

此时 go-service 会动态把对应分支节点改成 `TemplateMatch + Click`：

- 模板阈值固定为 `0.8`
- `green_mask` 固定为 `true`
- 点击时使用 `target: true`，并附带 `target_offset: [5, 5, -5, -5]`

这种方式通常比硬编码坐标更稳，推荐优先使用。

#### 2. 传坐标

支持：

- `[x, y]`
- `[x, y, w, h]`

如果传入 `[x, y]`，内部会自动补成 `[x, y, 1, 1]`。

## 方向约定

`Direction` 决定“滑到最大值”时的目标方向：

- `right` / `up`：会把滑动终点覆盖到屏幕右上区域附近；
- `left` / `down`：会把滑动终点覆盖到屏幕左下区域附近。

这不是说滑块一定沿着屏幕对角线运动，而是当前实现通过给 `Swipe` 节点动态覆盖一个足够远的终点区域，强制把滑块推向对应端点。

因此，`Direction` 填错时，最常见的结果不是“差一点”，而是：

- 最大值识别错误；
- 滑块没有被推到真实端点；
- 后续比例点击整体偏移。

## 依赖的公共节点

`QuantizedSliding` 内部依赖 `assets/resource/pipeline/QuantizedSliding/Main.json` 中的公共节点，主要包括：

- `QuantizedSlidingSwipeButton`：识别滑块模板 `QuantizedSliding/SwipeButton.png`
- `QuantizedSlidingSwipeToMax`：拖到最大值
- `QuantizedSlidingGetQuantity`：OCR 当前数量
- `QuantizedSlidingCheckQuantity`：判断是否需要微调
- `QuantizedSlidingIncreaseQuantity` / `QuantizedSlidingDecreaseQuantity`：执行加减按钮点击
- `QuantizedSlidingDone`：成功结束

其中有两点最关键：

1. 必须能稳定识别滑块模板 `QuantizedSliding/SwipeButton.png`；
2. `QuantityBox` 对应的 OCR 必须能稳定读出数字。

只要这两个前提不成立，后面的比例计算再准确也没有意义。

## 接入步骤

建议按下面的顺序接入。

### 1. 先确认场景适合用它

适合：

- 目标数量是离散值；
- 可以读出当前值；
- 拖到最大后可以得到上限；
- 存在可点击的加减按钮用于补偿误差。

不适合：

- 没有可读的数字；
- 没有加减按钮兜底；
- 拖条不是线性档位，或者点击位置与数量不是单调关系。

### 2. 准备滑块模板

`QuantizedSliding` 默认使用公共模板节点 `QuantizedSlidingSwipeButton`，其模板路径是：

```text
assets/resource/image/QuantizedSliding/SwipeButton.png
```

如果目标界面的滑块样式与现有模板不一致，需要先补模板资源或调整公共节点。

### 3. 标定数量 OCR 区域

将当前数量显示区域填写到 `QuantityBox`。

注意：

- 必须使用 **1280×720** 为基准；
- OCR 节点当前使用的 `expected` 是 `"\\d+"`，也就是只期望数字；
- Go 侧最终会从 OCR 文本中提取所有数字字符后再转为整数。

这意味着像 `12/99`、`数量 12` 这类文本，只要 OCR 能稳定读到数字，通常也能被解析；
但如果 OCR 容易把数字识别成字母，整个动作就会失败。

### 4. 选择按钮定位方式

优先顺序建议如下：

1. **模板路径**：最稳；
2. `[x, y, w, h]`：次稳；
3. `[x, y]`：仅在按钮特别稳定时使用。

### 5. 在业务任务中调用

参考当前仓库的实际用法：

```json
"AutoStockpileSwipeSpecificQuantity": {
    "desc": "滑动到指定数值",
    "enabled": false,
    "pre_delay": 0,
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "QuantizedSliding",
            "custom_action_param": {
                "DecreaseButton": "AutoStockpile/DecreaseButton.png",
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "QuantityBox": [360, 490, 110, 70],
                "Target": 1
            }
        }
    },
    "post_delay": 0,
    "rate_limit": 0,
    "next": ["AutoStockpileRelayNode"],
    "focus": {
        "Node.Action.Failed": "定量滑动失败，取消购买"
    }
}
```

文件位置：`assets/resource/pipeline/AutoStockpile/Task.json`

## 成功与失败条件

### 成功条件

- 能识别到滑块起点；
- 能成功拖到最大值；
- 能 OCR 出最大值与当前值；
- 目标值 `Target` 不大于最大值；
- 经过精确点击与微调后，当前值最终等于 `Target`。

### 常见失败条件

- `QuantityBox` 不是 `[x, y, w, h]` 四元组；
- `Direction` 不是 `left/right/up/down` 之一；
- OCR 没有读到数字；
- 最大值 `maxQuantity` 小于 `Target`；
- 最大值小于等于 `1`，无法计算比例；
- 加减按钮无法识别或无法点击；
- 微调次数过多仍未收敛。

当前实现会把单次微调点击次数限制在 `30` 以内，`QuantizedSlidingCheckQuantity` 的 `max_hit` 为 `4`。如果走满后仍未到目标值，就会失败并进入 `QuantizedSlidingFail`。

## 为什么还需要微调按钮

表面上看，既然已经根据起点、终点和最大值算出了精确点击位置，似乎不需要再点 `+` / `-`。

但实际界面里往往会有这些误差来源：

- 滑块模板识别框不是严格的几何中心；
- 触控区域和视觉位置不完全重合；
- 某些档位的实际映射并非完全均匀；
- OCR 或动画过渡导致点击后落点有轻微偏差。

所以当前实现采用的是：

> 先用比例点击快速靠近目标，再用加减按钮收尾。

这比“全靠加减按钮硬点”快得多，也比“只点一次比例位置”稳得多。

## 常见坑

- **把它当成普通滑动节点**：它本质上是一个完整子流程，不只是一次 `Swipe`。
- **`Direction` 填反**：会导致“滑到最大值”这一步本身就不成立。
- **`QuantityBox` 截得太紧**：数字跳动或描边变化时 OCR 容易失败。
- **只给按钮坐标，不做识别兜底**：界面轻微偏移后就可能点歪。
- **滑块模板不通用**：不同界面滑块样式不一致时，公共模板可能失效。
- **目标值超过上限**：`Target > maxQuantity` 会直接失败，不会自动回退到最大值。
- **没有考虑冻结等待**：该公共流程内部已经使用了 `post_wait_freezes`，业务接入时不要再额外叠很多硬延迟。

## 自检清单

接入后，至少检查下面这些点：

1. 滑块模板 `QuantizedSliding/SwipeButton.png` 是否能稳定命中。
2. `QuantityBox` 是否基于 **1280×720**，且 OCR 能稳定读出数字。
3. `Direction` 是否与“最大值所在方向”一致。
4. `IncreaseButton` / `DecreaseButton` 是否优先使用模板路径。
5. `Target` 是否有可能大于当前场景允许的最大值。
6. 失败分支是否有明确处理，例如提示、跳过或取消当前任务。

## 相关文档

- [Custom 自定义动作参考文档](./custom-action.md)：了解 `Custom` 节点的通用调用方式。
- [开发手册](./development.md)：了解 Pipeline / Go Service 的整体开发规范。
