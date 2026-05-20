# 开发手册 - ExprCoord 坐标表达式版识别/动作

## 简介

带表达式的坐标版自定义识别/动作，让非 16:9 链路的 ROI/target 可用表达式书写。

### 边界说明

ExprCoord 只解决“坐标层”的表达方式问题，不改变底层识别算法本身；它仅适用于使用 `Mp` 前缀的自定义节点。

> [!TIP]
>
> 当你已经知道识别算法仍然是 TemplateMatch / OCR，只是 ROI 或点击坐标需要随画面宽高变化时，ExprCoord 就是最轻量的接入方式。

## 节点说明

### custom_recognition: MpExprTemplateMatch

使用表达式版 ROI 运行一次 TemplateMatch。

#### 节点参数

**必填参数 (`custom_recognition_param`)**：

- `roi`: 长度为 4 的数组 `[x, y, w, h]`。每个元素都可以是数字，或使用 `WIDTH` / `HEIGHT` 的表达式字符串。
- `template`: 模板图路径数组，用法与原生 `TemplateMatch` 一致。

**可选参数 (`custom_recognition_param`)**：

- `threshold`: 阈值数组，用法与原生 `TemplateMatch` 一致。
- `green_mask`: 是否启用绿幕遮罩。
- `order_by`: 结果排序方式。
- `index`: 取第几个结果。
- `method`: TemplateMatch 算法编号。

#### ROI 表达式写法

`roi` 的 4 个元素都支持：

- 纯数字：如 `200`
- 字符串表达式：如 `"WIDTH/2-100"`

> [!WARNING]
>
> ExprCoord 不会自动纠正错误表达式；例如 `"WIDTH/"`、`"FOO"`、`"1+"` 都会直接导致该节点识别失败。

### custom_recognition: MpExprOCR

使用表达式版 ROI 运行一次 OCR。

#### 节点参数

**必填参数 (`custom_recognition_param`)**：

- `roi`: 长度为 4 的数组 `[x, y, w, h]`。每个元素都可以是数字，或使用 `WIDTH` / `HEIGHT` 的表达式字符串。
- `expected`: 期望文本数组，用法与原生 `OCR` 一致。

**可选参数 (`custom_recognition_param`)**：

- `threshold`: OCR 阈值。
- `replace`: OCR 替换规则。
- `order_by`: 结果排序方式。
- `index`: 取第几个结果。
- `only_rec`: 是否只做识别不做检测。
- `model`: OCR 模型名。

#### ROI 表达式写法

语法与 `MpExprTemplateMatch` 的 `roi` 完全一致。

### custom_action: MpExprClick

使用表达式版 target 运行一次 Click。

#### 节点参数

**必填参数 (`custom_action_param`)**：

- `target`: 支持两种形式：
    - 长度为 2 的点数组 `[x, y]`
    - 长度为 4 的矩形数组 `[x, y, w, h]`

其中每个元素都可以是数字，或使用 `WIDTH` / `HEIGHT` 的表达式字符串。

**可选参数 (`custom_action_param`)**：

- `target_offset`: 长度为 4 的数组 `[x, y, w, h]`。每个元素都可以是数字，或使用 `WIDTH` / `HEIGHT` 的表达式字符串；语义与原生 `Click.target_offset` 一致。
- `contact`: 触点编号；ADB 下对应手指编号，Win32 下对应鼠标按键编号。

#### target 表达式写法

- 点坐标示例：`["WIDTH/2", "HEIGHT/2"]`
- 矩形坐标示例：`["WIDTH-200", "0", "200", "200"]`
- 偏移示例：`["5", "5", "-10", "-10"]`

### custom_action: MpExprSwipe

使用表达式版 begin / end 运行一次 Swipe。

#### 节点参数

**必填参数 (`custom_action_param`)**：

- `begin`: 长度为 2 的点数组 `[x, y]`，或长度为 4 的矩形数组 `[x, y, w, h]`。
- `end`: 长度为 2 的点数组 `[x, y]`，或长度为 4 的矩形数组 `[x, y, w, h]`。

**可选参数 (`custom_action_param`)**：

- `duration`: 滑动持续时间，单位毫秒。
- `end_hold`: 滑动终点按住时间，单位毫秒。
- `contact`: 触点编号；ADB 下对应手指编号，Win32 下对应鼠标按键编号。

#### swipe 表达式写法

- 右侧区域向上滑动示例：`begin=["WIDTH-230", "540"]`，`end=["WIDTH-230", "365"]`

## 表达式语法

| 项目       | 说明         | 示例            |
| ---------- | ------------ | --------------- |
| `+`        | 加法         | `WIDTH/2+10`    |
| `-`        | 减法         | `WIDTH-200`     |
| `*`        | 乘法         | `WIDTH*0.5`     |
| `/`        | 除法         | `HEIGHT/2`      |
| `WIDTH`    | 当前截图宽度 | `WIDTH/2`       |
| `HEIGHT`   | 当前截图高度 | `HEIGHT-100`    |
| `()`       | 括号分组     | `(WIDTH-200)/2` |
| 负号       | 一元负号     | `-50`           |
| 整数与小数 | 数字字面量   | `1280`、`0.5`   |

> [!TIP]
>
> 表达式只支持四则运算、括号、负号、`WIDTH`、`HEIGHT`。不支持函数调用，也不支持其他变量名。

## 与 MaaXYZ/MaaFramework#1336 的关系

当上游合入相关支持并配合 `display_expand: [1280, 720]` 使用时，表达式里的 `WIDTH` / `HEIGHT` 会始终等于 `1280` / `720`；这时 ExprCoord 仍然可用，但它表达的是“扩展后坐标系”而不是原始窗口像素尺寸。

## 何时不该用

如果目标只是利用 MaaFramework 原生支持的负坐标/越界坐标能力，那就不该优先引入 ExprCoord。例如右侧贴边 ROI 本来就可以直接写成：`[-200, 0, 200, 200]`。

> [!WARNING]
>
> ExprCoord 不是“坐标万能适配器”。如果原生节点已经能稳定表达需求，优先保持原生写法，避免增加额外心智负担。

## 示例

下面是 `assets/resource/pipeline/Interface/Example/MpExpr.json` 的完整示例：

```json
{
    "MpExprTemplateMatchExample": {
        "desc": "MpExprTemplateMatch 示例：屏幕中心识别 WorldMenu 模板",
        "recognition": "Custom",
        "custom_recognition": "MpExprTemplateMatch",
        "custom_recognition_param": {
            "roi": [
                "WIDTH-200",
                "0",
                "200",
                "200"
            ],
            "template": ["SceneManager/WorldMenu.png"],
            "threshold": [0.7],
            "green_mask": true
        },
        "action": "DoNothing"
    },
    "MpExprOCRExample": {
        "desc": "MpExprOCR 示例：屏幕顶部居中找'设置'",
        "recognition": "Custom",
        "custom_recognition": "MpExprOCR",
        "custom_recognition_param": {
            "roi": [
                "WIDTH/2-100",
                "0",
                "200",
                "60"
            ],
            "expected": [
                "设置",
                "Settings"
            ]
        },
        "action": "DoNothing"
    },
    "MpExprClickExample": {
        "desc": "MpExprClick 示例：屏幕中心点击 1x1",
        "action": "Custom",
        "custom_action": "MpExprClick",
        "custom_action_param": {
            "target": [
                "WIDTH/2",
                "HEIGHT/2"
            ],
            "target_offset": [
                "5",
                "5",
                "-10",
                "-10"
            ]
        }
    },
    "MpExprSwipeExample": {
        "desc": "MpExprSwipe 示例：在右侧下拉框区域向上滑动",
        "action": "Custom",
        "custom_action": "MpExprSwipe",
        "custom_action_param": {
            "begin": [
                "WIDTH-230",
                "540"
            ],
            "end": [
                "WIDTH-230",
                "365"
            ],
            "duration": 350,
            "end_hold": 100
        }
    }
}
```

这个示例分别展示了四件事：

1. `MpExprTemplateMatch` 可以把右上角 200×200 区域写成依赖 `WIDTH` 的表达式。
2. `MpExprOCR` 可以把顶部居中 ROI 写成围绕 `WIDTH/2` 展开的表达式。
3. `MpExprClick` 可以直接点击屏幕中心，也可以叠加 `target_offset` 复用原生点击偏移语义。
4. `MpExprSwipe` 可以在随 `WIDTH` 移动的区域内执行滑动。
