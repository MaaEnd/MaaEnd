# Pipeline 更加完善的教程

> 面向零基础开发者，本教程将带你从零开始掌握 MaaEnd Pipeline（低代码自动化流程）的编写。
**本文档针对[零基础开发教程](./docs/zh_cn/developers/super-basic-introduction.md)进行补充的，结合菜鸟教程进行编写的，是结合版，如果想根据错误样例加深印象，这篇没有，去零基础开发教程里看**
---

## 第 1 章 · Pipeline 是什么

Pipeline 是一份 **JSON 文件**，通过声明式的节点（Node）描述「识别画面 → 执行操作 → 跳转到下一个节点」的自动化流程。

你可以把它理解为一张**流程图**，每个步骤就是一个节点，节点之间用箭头（`next`）连接：

```·
[识别商品] ──点击──→ [识别售卖按钮] ──点击──→ [识别确认弹窗] ──点击──→ ...
```

**为什么叫"低代码"？** 因为你只需要写 JSON 配置，不需要写 Go/C++ 代码来完成界面交互——除非遇到 JSON 做不了的复杂算法,简单说你只需要写流程

### 结构总览

```json
{
    "节点名称": {
        "action": "Click",          // 动作类型
        "recognition": "TemplateMatch", // 识别方式
        "template": "xxx.png",      // 识别用的模板图片
        "roi": [0, 0, 1280, 720],   // 识别区域
        "next": ["下一节点", "另一节点"]
    }
}
```

每个 Pipeline JSON 文件本质上就是一个「节点名 → 节点配置」的映射表。

---

## 第 2 章Pipeline本质

### 2.1 节点（Node）—— 流程的最小单元

任务由若干个节点组成。引擎会按顺序执行：

1. **识别**：在当前游戏画面上查找目标（模板图片、文字、颜色等）
2. **执行动作**：若识别命中，则执行相应动作（点击、滑动、按键等）
3. **跳转**：动作完成后，跳转到 `next` 中命中的下一个节点

### 2.2 识别（Recognition）—— "看到了什么"

Pipeline 支持多种识别方式：

| 识别类型 | 说明 | 示例字段 |
|----------|------|----------|

| `TemplateMatch` | 模板匹配：在画面中查找一张「截图模板」 | `template` |
| `OCR` | 文字识别：识别画面中的文字 | `expected`（正则） |
| `ColorMatch` | 颜色匹配：根据色块定位 | `roi` + 颜色范围 |
| `Custom` | 自定义识别：调用 Go/C++ 实现的复杂逻辑 | `custom_recognition` |

### 2.3 动作（Action）—— "做点什么"

| 动作类型 | 说明 |
|----------|------|

| `Click` | 点击识别命中的位置 |
| `Swipe` | 滑动（拖拽） |
| `Key` | 发送键盘按键 |
| `DoNothing` | 不做任何操作（仅用于识别占位） |
| `Custom` | 自定义动作（调用 Go/C++ 实现） |

### 2.4 next —— 流程的跳转规则

`next` 是节点的关键字段，表示「命中之后去哪个节点」。它有三种形态：

```json
// 1. 只有一个去向
"next": ["下一个节点"]

// 2. 多个去向（引擎会依次尝试，哪个命中就去哪个）
"next": ["节点A", "节点B", "节点C"]

// 3. 用 Custom 动态指定去向
"next": "Custom"
```

**铁律：尽可能扩充 `next` 列表。** 同一个画面可能同时出现多个预期元素（如弹窗 + 加载中 + 正常界面），把它们都放进 `next`，引擎可以在一次截图中全部命中——这就是「一次心跳，立即命中」。

---

## 第 3 章 · 第一个 Pipeline

以「自动售卖物品」的第一步为例。我们要做的是：在背包界面点击"售卖"按钮。

### 步骤 1：截图模板

游戏分辨率为 **1280×720**（720p）。首先截取售卖按钮的图片，保存到 `assets/resource/image/SellProduct/SellButton.png`。

### 步骤 2：确定识别区域（ROI）

`roi` 格式为 `[x, y, width, height]`，即左上角坐标和宽高。例如售卖按钮在大约 `[1100, 40, 170, 600]` 的右侧栏区域。

**提示：** 如果不确定 ROI，可以先设为 `[0, 0, 1280, 720]` 全屏搜索，等调通后再缩小范围以提升性能。

### 步骤 3：编写节点配置

在 `assets/resource/pipeline/SellProduct.json` 中新建节点：

```json
{
    "BackpackSellButton": {
        "next": ["BackpackSellButton"],
        "recognition": "TemplateMatch",
        "action": "Click",
        "template": "SellButton.png",
        "roi": [1100, 40, 170, 600]
    }
}
```

这个节点解释如下：

- `BackpackSellButton`：节点名称，使用 **PascalCase**（大驼峰）命名
- `recognition: TemplateMatch`：在画面中查找 `SellButton.png` 模板
- `action: Click`：找到后点击
- `next: ["BackpackSellButton"]`：点击后回到自己——形成**循环**，等待下一个可售卖物品

### 步骤 4：注册到任务

在 `assets/tasks/SellProduct.json` 中定义任务入口：

```json
{
    "SellProduct": {
        "type": "SellProduct",
        "name": "自动售卖",
        "entry": "SellProductMain",
        "pipeline_override": {},
        "option": []
    }
}
```

然后在 `assets/interface.json` 中添加该任务到任务列表。

---

## 第 4 章 · 常用模式

### 4.1 循环模式

最常见的形式：节点操作后回到自身，反复执行直到画面变化。

```json
"SellItemLoop": {
    "next": ["SellItemLoop"],
    "recognition": "TemplateMatch",
    "template": "Item.png",
    "action": "Click"
}
```

**退出条件**：当某个节点的 `next` 列表中的「退出节点」命中时，流程自然跳出循环。例如：

```json
"SellItemLoop": {
    "next": ["SellItemLoop", "BackpackEmpty"],
    "recognition": "TemplateMatch",
    "template": "Item.png",
    "action": "Click"
},
"BackpackEmpty": {
    "next": ["TaskEnd"],
    "recognition": "TemplateMatch",
    "template": "EmptyIcon.png",
    "action": "DoNothing"
}
```

### 4.2 分支模式

利用 `next` 的优先级实现分支——排在数组前面的节点优先被尝试。

```json
"CheckPopup": {
    "next": ["ClosePopup", "ContinueWork", "ErrorScreen"],
    "recognition": "ColorMatch",
    "roi": [0, 0, 1280, 720]
}
```

引擎依次尝试匹配 `ClosePopup` → `ContinueWork` → `ErrorScreen` 的识别条件，哪个先命中就走哪个。

### 4.3 空闲冻结（Freezes）

当你需要等待某个操作完成（例如加载动画消失），不要用 `pre_delay` 硬等，而应该用 `pre_wait_freezes` / `post_wait_freezes`：

```json
"WaitLoading": {
    "next": ["AfterLoad"],
    "recognition": "TemplateMatch",
    "template": "LoadingIcon.png",
    "action": "DoNothing",
    "pre_wait_freezes": 2000
}
```

这表示：任务会在该节点命中后等待 2 秒，期间若画面一直匹配（「冻结」），则超时后自动跳到 `next`；若画面提前变化（不再匹配），立即跳转

### 4.4 子任务（SubTask）

通过 `SubTask` 调用另一个 Pipeline 文件作为子流程：

```json
"MySubTask": {
    "action": "Custom",
    "custom_action": "SubTask",
    "custom_action_param": {
        "subtask": "AnotherPipeline"
    },
    "next": ["ContinueHere"]
}
```

这样有助于代码的简洁与规范，建议实际编码参考一下

---

## 第 5 章 · 完整示例：自动售卖

以下从 MaaEnd 实际代码中简化出的完整售卖流程：

### 主入口

```json
"SellProductMain": {
    "next": ["SellProductLoop"]
}
```

### 售卖循环

```json
"SellProductLoop": {
    "next": [
        "SellItem",
        "SellConfirmDialog",
        "SellProductDone"
    ]
}
```

- `SellItem`：点击可售卖物品
- `SellConfirmDialog`：处理确认弹窗（点"确认"）
- `SellProductDone`：背包已空，结束任务

### 售卖单件物品

```json
"SellItem": {
    "next": ["SellAdjustQuantity"],
    "recognition": "OCR",
    "expected": "出售",
    "action": "Click",
    "roi": [200, 100, 400, 500]
}
```

使用 OCR 识别"出售"按钮并点击。

### 调节数量

```json
"SellAdjustQuantity": {
    "next": ["SellAdjustQuantity", "SellProductLoop"],
    "recognition": "Custom",
    "custom_action": "BetterSliding",
    "custom_action_param": {
        "target": 99,
        "target_type": "Value"
    }
}
```

使用 `BetterSliding` 组件将数量滑到最大值。成功后回到 `SellProductLoop`。

---

## 第 6 章 · 进阶技巧

### 6.1 ROI 选取

ROI 越小，识别越快。好的做法是：

1. 先用全屏 `[0, 0, 1280, 720]` 调试
2. 确定目标位置后缩小到 `[目标x-50, 目标y-20, 150, 80]` 左右的容错范围
3. 不要卡得太死——游戏 UI 可能略微偏移，可使用截图工具进行ROI偏移
**一定要使用截图工具（这里指VS Code 插件MaaFramee work Support,如果没有，请下载）进行！！！！**

### 6.2 模板图片要点

- 分辨率严格 **1280×720（当然这点截图工具已经做了，但是还是要注意的）**
- 选择**独一无二**的特征点，避免和界面其他元素相似
- 文件名使用 `UpperCamelCase`，如 `SellButton.png`、`ConfirmDialog.png`
- 放到与节点名称对应的子目录，如 `SellProduct/SellButton.png`

### 6.3 命名规范

| 规则 | 正确 | 错误 |
|------|------|------|

| 节点名：PascalCase | `SellItem` | `sell_item`、`sellItem` |
| 模板文件：PascalCase | `SellButton.png` | `sell-button.png` |
| Pipeline 文件名：PascalCase | `SellProduct.json` | `sellproduct.json` |

### 6.4 禁止硬延迟

**严禁**使用 `pre_delay` / `post_delay` 等待某个操作完成。正确的做法是：

- 增加中间识别节点（识别加载图标、过渡动画等）
- 使用 `pre_wait_freezes` / `post_wait_freezes`

**为什么？** 硬延迟在不同设备上表现不一致——高端机 1 秒加载完了要空等，低端机 1 秒还没加载完会漏识别。

---

## 第 7 章 · 调试与测试

### 7.1 单节点测试

不跑完整任务，单独测试某个节点：

参考 `docs/zh_cn/developers/node-testing.md`，使用 MaaFramework 的调试工具对单个节点截图验证识别精度。

### 7.2 常见错误排查

| 症状 | 可能原因 | 解决方法 |
|------|----------|----------|

| 节点永远不命中 | 模板图片不对 / ROI 太小 | 检查模板是否与当前画面一致，扩大 ROI |
| 点击位置偏离 | 模板匹配偏移 / ROI 位置错误 | 检查 ROI 坐标，确认分辨率 720p |
| 流程卡住不跳转 | `next` 列表不完整 | 补充所有可能出现的界面到 `next` |
| 无限循环退不出 | 退出条件节点不命中 | 检查退出节点的识别条件，补充 `pre_wait_freezes` |

## 第 8 章 · 下一步

### 8.1 阅读可复用组件文档

许多复杂交互已有现成组件，直接调用即可：

- **通用按钮**：`WhiteConfirmButton`、`YellowConfirmButton`、`CancelButton`、`CloseButton` 等 → `docs/zh_cn/developers/common-buttons.md`
- **滑条调节**：`BetterSliding` → `docs/zh_cn/developers/components/better-sliding.md`
- **战斗操作**：`AutoFight` → `docs/zh_cn/developers/components/auto-fight.md`
- **场景切换**：`InScene` / `SceneManager` → `docs/zh_cn/developers/in-scene.md` / `scene-manager.md`

### 8.2 阅读 Custom 参考

当 JSON 配置不够用时，Custom Action / Custom Recognition 提供了扩展能力 → `docs/zh_cn/developers/custom.md`

### 8.3 参考现有任务

打开 `assets/resource/pipeline/` 下的现有 Pipeline JSON 文件，它们是**最好的学习资料**：

| 文件 | 涉及内容 |
|------|----------|

| `SellProduct.json` | 基础循环、OCR 识别、BetterSliding |
| `ItemTransfer.json` 对应的 Pipeline | 可选参数装配、分支覆盖 |
| `DijiangRewards.json` | 多阶段场景切换 |
| `CreditShopping.json` | 复杂层级识别链 |

### 8.4 相关协议文档

- [MaaFramework Pipeline 协议规范](https://github.com/MaaXYZ/MaaFramework/raw/refs/heads/main/docs/en_us/3.1-PipelineProtocol.md)
- [MaaFramework 项目接口 V2](https://github.com/MaaXYZ/MaaFramework/raw/refs/heads/main/docs/en_us/3.3-ProjectInterfaceV2.md)

---

> **记住 Pipeline 的流程：识别 → 操作 → 再识别。** 每一步都基于明确的识别结果，严禁假设上一个操作一定成功了，下水吧，去编写你的Pipeline吧
