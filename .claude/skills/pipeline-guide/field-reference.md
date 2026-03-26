# Pipeline 字段速查表

基于 MaaFramework Pipeline Protocol，供快速查阅字段名、类型、默认值。完整说明见 [PipelineProtocol.md](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.1-PipelineProtocol.md)。

## 通用字段

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `recognition` | string \| object | `"DirectHit"` | 识别算法（v2 用 object `{type, param}`） |
| `action` | string \| object | `"DoNothing"` | 动作类型（v2 用 object `{type, param}`） |
| `next` | string \| NodeAttr \| list | `[]` | 后续节点列表 |
| `on_error` | string \| NodeAttr \| list | `[]` | 超时/失败后执行的节点 |
| `timeout` | int | `20000` | next 循环识别超时（ms），-1 永不超时 |
| `rate_limit` | uint | `1000` | 识别速率限制（ms） |
| `pre_delay` | uint | `200` | 识别到后、执行动作前的延迟（ms） |
| `post_delay` | uint | `200` | 执行动作后、识别 next 前的延迟（ms） |
| `pre_wait_freezes` | uint \| object | `0` | 动作前等待画面稳定（ms） |
| `post_wait_freezes` | uint \| object | `0` | 动作后等待画面稳定（ms） |
| `repeat` | uint | `1` | 动作重复次数 |
| `repeat_delay` | uint | `0` | 重复间延迟（ms） |
| `repeat_wait_freezes` | uint \| object | `0` | 重复间等待画面稳定（ms） |
| `inverse` | bool | `false` | 反转识别结果 |
| `enabled` | bool | `true` | 是否启用 |
| `max_hit` | uint | UINT_MAX | 最大命中次数 |
| `anchor` | string \| list \| object | `""` | 锚点名 |
| `focus` | object | `null` | 节点通知 |
| `attach` | object | `{}` | 附加配置（dict merge） |

## 节点生命周期

`pre_wait_freezes` → `pre_delay` → `action` → [`repeat_wait_freezes` → `repeat_delay` → `action`] × (repeat-1) → `post_wait_freezes` → `post_delay` → 截图 → 识别 next

## 识别算法字段

### DirectHit

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `roi` | array<int,4> \| string | `[0,0,0,0]`（全屏） |
| `roi_offset` | array<int,4> | `[0,0,0,0]` |

### TemplateMatch

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `template` | string \| list<string> | **必填** |
| `roi` / `roi_offset` | 同 DirectHit | |
| `threshold` | double \| list<double> | `0.7` |
| `order_by` | string | `"Horizontal"` |
| `index` | int | `0` |
| `method` | int | `5`（TM_CCOEFF_NORMED，推荐） |
| `green_mask` | bool | `false` |

### FeatureMatch

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `template` | string \| list<string> | **必填** |
| `roi` / `roi_offset` | 同 DirectHit | |
| `count` | uint | `4` |
| `detector` | string | `"SIFT"`（推荐） |
| `ratio` | double | `0.6` |
| `order_by` | string | `"Horizontal"` |
| `index` | int | `0` |
| `green_mask` | bool | `false` |

### ColorMatch

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `roi` / `roi_offset` | 同 DirectHit | |
| `method` | int | `4`（RGB） |
| `lower` | list<int> \| list<list<int>> | **必填** |
| `upper` | list<int> \| list<list<int>> | **必填** |
| `count` | uint | `1` |
| `connected` | bool | `false` |
| `order_by` | string | `"Horizontal"` |
| `index` | int | `0` |

### OCR

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `roi` / `roi_offset` | 同 DirectHit | |
| `expected` | string \| list<string> | 匹配全部 |
| `threshold` | double | `0.3` |
| `replace` | array<string,2> \| list | 无 |
| `only_rec` | bool | `false` |
| `model` | string | `""`（默认模型） |
| `color_filter` | string | `""`（ColorMatch 节点名） |
| `order_by` | string | `"Horizontal"` |
| `index` | int | `0` |

### NeuralNetworkClassify

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `roi` / `roi_offset` | 同 DirectHit | |
| `model` | string | **必填** |
| `labels` | list<string> | `["Unknown"]` |
| `expected` | int \| list<int> | 匹配全部 |
| `order_by` | string | `"Horizontal"` |
| `index` | int | `0` |

### NeuralNetworkDetect

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `roi` / `roi_offset` | 同 DirectHit | |
| `model` | string | **必填** |
| `labels` | list<string> | 自动读取 |
| `expected` | int \| list<int> | 匹配全部 |
| `threshold` | double \| list<double> | `0.3` |
| `order_by` | string | `"Horizontal"` |
| `index` | int | `0` |

### And

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `all_of` | list<string \| object> | **必填** |
| `box_index` | int | `0` |

### Or

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `any_of` | list<string \| object> | **必填** |

### Custom

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `custom_recognition` | string | **必填** |
| `custom_recognition_param` | any | `null` |
| `roi` / `roi_offset` | 同 DirectHit | |

## 动作字段

### Click / LongPress

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `target` | true \| string \| array<int,2> \| array<int,4> | `true` |
| `target_offset` | array<int,4> | `[0,0,0,0]` |
| `duration` | uint（仅 LongPress） | `1000` |
| `contact` | uint | `0` |

### Swipe

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `begin` / `begin_offset` | 同 Click target | `true` / `[0,0,0,0]` |
| `end` / `end_offset` | 同 Click target（支持 list） | `true` / `[0,0,0,0]` |
| `duration` | uint \| list<uint> | `200` |
| `end_hold` | uint \| list<uint> | `0` |
| `contact` | uint | `0` |

### Scroll

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `target` / `target_offset` | 同 Click | `true` / `[0,0,0,0]` |
| `dx` | int | `0` |
| `dy` | int | `0` |

### Custom Action

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `custom_action` | string | **必填** |
| `custom_action_param` | any | `null` |
| `target` / `target_offset` | 同 Click | `true` / `[0,0,0,0]` |

### 其他动作

| 动作 | 关键字段 |
|------|----------|
| `ClickKey` / `LongPressKey` | `key: int \| list<int>`, `duration`（仅 LongPressKey） |
| `KeyDown` / `KeyUp` | `key: int` |
| `InputText` | `input_text: string` |
| `StartApp` / `StopApp` | `package: string` |
| `Shell` | `cmd: string`, `shell_timeout: int`（默认 20000） |
| `Command` | `exec: string`, `args: list<string>`, `detach: bool` |
| `Screencap` | `filename: string`, `format: string`, `quality: int` |

## order_by 排序方式

| 值 | 说明 | 适用算法 |
|-----|------|----------|
| `Horizontal` | 左→右，同列上→下 | 全部 |
| `Vertical` | 上→下，同行左→右 | 全部 |
| `Score` | 分数降序 | TemplateMatch, FeatureMatch, NNClassify, NNDetect |
| `Area` | 面积降序 | FeatureMatch, ColorMatch, OCR, NNDetect |
| `Length` | 文本长度降序 | OCR |
| `Random` | 随机 | 全部 |
| `Expected` | 按 expected 顺序 | OCR, NNClassify, NNDetect |

## wait_freezes object 字段

| 字段 | 类型 | 默认值 |
|------|------|--------|
| `time` | uint | `1` |
| `target` / `target_offset` | 同 Click | `true` / `[0,0,0,0]` |
| `threshold` | double | `0.95` |
| `method` | int | `5` |
| `rate_limit` | uint | `1000` |
| `timeout` | int | `20000` |
