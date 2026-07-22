# 开发手册 - Custom 动作与识别参考

`Custom` 用于在 Pipeline 中调用项目侧注册的自定义逻辑，分为两类：

- `Custom Action`：执行动作逻辑，如子任务调度、状态清理、复杂交互。
- `Custom Recognition`：执行识别逻辑，返回是否命中，以及可选的识别结果详情。

项目中的 Go 实现通常位于 `agent/go-service/` 下，并通过：

- `maa.AgentServerRegisterCustomAction(...)`
- `maa.AgentServerRegisterCustomRecognition(...)`

完成注册。

---

## Custom Action

Action 节点用于执行自定义动作。常见写法如下：

```json
{
    "action": "Custom",
    "custom_action": "SomeAction",
    "custom_action_param": {
        "foo": "bar"
    }
}
```

- `custom_action`：注册名。
- `custom_action_param`：任意 JSON 值，由框架序列化后传给实现侧。

### SubTask

`SubTask` 实现位于 `agent/go-service/subtask`，用于顺序执行一组子任务。

- 参数：
    - `sub: string[]`：子任务名列表，必填。
    - `continue?: bool`：某个子任务失败后是否继续执行后续子任务，默认 `false`。
    - `strict?: bool`：某个子任务失败时当前 Action 是否返回失败，默认 `true`。
    - `random_choice?: int`：若指定且大于 `0`，则先将 `sub` 列表随机打乱，再从中挑选不超过该数量的子任务执行；超过列表长度时按列表长度处理。默认不随机，按原顺序执行全部子任务。

    执行前会先剔除 `sub` 中无法解析或 `enabled` 为 `false` 的子任务节点（未显式声明 `enabled` 的节点视为启用），随后才进行随机挑选与执行。若过滤（及随机挑选）后没有可执行的子任务，当前 Action 不视为失败，仅记录一条 warn 日志并返回成功。

示例文件：[`SubTask.json`](../../../assets/resource/pipeline/Interface/Example/SubTask.json)

### FailureCollector

`FailureCollector` 实现位于 `agent/go-service/common/failurecollector`，用于在多个子任务中收集失败项，全部执行完毕后统一汇报失败。单个子任务失败不会中断后续子任务的执行。

提供三个 Custom Action，通过相同的 `key` 关联同一次收集：

- `FailureCollectorReset`：重置指定 `key` 的收集状态，必须在所有 RunTask 之前调用。
- `FailureCollectorRunTask`：执行 `task` 子任务。成功则继续；失败时按发生顺序记录 `failure_task` 并可选执行 `recovery_task`，Action 本身始终返回成功以保证 Pipeline 继续执行后续节点。
- `FailureCollectorFinish`：按失败发生顺序依次执行本轮记录的全部 `failure_task` 节点，随后清空状态。存在失败记录时返回失败，否则返回成功。

- 参数：
    - `FailureCollectorReset`：
        - `key: string`：收集标识，必填。同一流程中须保持一致且全局唯一。
    - `FailureCollectorRunTask`：
        - `key: string`：收集标识，必填。
        - `task: string`：要执行的子任务 Pipeline 节点名，必填。该节点被禁用（`Enabled = false`）时直接跳过，不视为失败。
        - `failure_task: string`：子任务失败时记录的 Pipeline 节点名，必填。该节点通常通过 `focus` 向 Agent 输出用户提示信息。
        - `recovery_task?: string`：子任务失败后执行的恢复任务节点名，可选。
    - `FailureCollectorFinish`：
        - `key: string`：收集标识，必填。

示例文件：[`AutoCollect.json`](../../../assets/resource/pipeline/AutoCollect.json)

### ClearHitCount

`ClearHitCount` 实现位于 `agent/go-service/clearhitcount`，用于清除指定节点的命中计数。

- 参数：
    - `nodes: string[]`：要清理的节点名列表，必填。
    - `strict?: bool`：任一节点清理失败时当前 Action 是否返回失败，默认 `false`。

示例文件：[`ClearHitCount.json`](../../../assets/resource/pipeline/Interface/Example/ClearHitCount.json)

### FalseAction

`FalseAction` 实现位于 `agent/go-service/common/falseaction`，始终返回失败。常用于 Pipeline 中需要强制让 Action 执行失败的占位场景。

- 参数：无。

### RepeatUntilFoundAction / RepeatUntilNotFoundAction

二者实现均位于 `agent/go-service/common/repeataction`，用于反复执行一次内置或自定义动作，每次执行后等待再识别；条件满足即成功，耗尽次数仍不满足则失败。

- `RepeatUntilFoundAction`：`wait_nodes` 中**任一命中**即成功。
- `RepeatUntilNotFoundAction`：`wait_node` **未命中**即成功。

- 公共参数：
    - `action: string`：内置动作类型（如 `Click`），与 `custom_action` 二选一。
    - `custom_action?: string`：已注册的自定义动作名（如 `AutoAltClickAction`），与 `action` 二选一。
    - `custom_action_param?: object`：透传给内层自定义动作的参数。
    - `repeat_count?: int`：最大尝试次数；省略或 `<= 0` 时默认 `3`。
    - `interval_ms?: int`：每次尝试后、识别前的等待（毫秒）；省略或 `0` 时默认 `1000`；负值非法。
- `RepeatUntilFoundAction` 额外参数：
    - `wait_nodes: string[]`：等待出现的 Pipeline 节点名列表，必填。
- `RepeatUntilNotFoundAction` 额外参数：
    - `wait_node: string`：等待消失的 Pipeline 节点名，必填；一次只支持一个节点。

目标位置固定使用触发本 Action 的识别框 `box`（可由外层 `target` / `target_offset` 调整）。循环在任务停止信号（`Stopping`）时会立即中止并返回失败。

示例文件：[`RepeatUntilFoundAction.json`](../../../assets/resource/pipeline/Interface/Example/RepeatUntilFoundAction.json)

### PipelineOverride

`PipelineOverride` 实现位于 `agent/go-service/common/pipelineoverride`，用于在运行时把**按节点组织的局部 JSON** 合并到当前 Pipeline 中（`ctx.OverridePipeline`）。适合在**不改静态流转拓扑**的前提下，动态切换节点开关或调整识别/动作参数。

- 参数：
    - `patch: object`：必填。键为**节点名**，值为该节点的**局部覆盖对象**。语义与 MaaFramework `OverridePipeline` 一致：同名节点合并、同名字段覆盖。
    - `allow_next?: bool`：是否允许局部节点对象包含顶层 `next`。默认 `false`；为 `false` 时，会在应用前移除每个 patch 项里的 `next`，避免运行时改写预设拓扑。
    - `strict?: bool`：当 `allow_next` 为 `false` 时，若 patch 中仍带有 `next`，是否直接报错失败。默认 `false`（会移除 `next` 并记录日志）；为 `true` 时当前 Action 直接失败且不会应用任何覆盖。若 `allow_next` 为 `true`，则 `strict` 会被忽略并按 `false` 处理。

使用建议：

- 优先在**流程入口**决定策略；若必须中途调整，尽量只改 `enabled`、识别器参数、动作参数等字段，不要随意改 `next` 图结构。
- 如果确实需要在运行时修改 `next`，请显式设置 `allow_next: true`，并自行评估调试与回归成本；默认应保持关闭。
- 排障时可结合额外日志节点或截图节点一起使用。
- 运行时日志只记录节点数量、节点名、参数长度等非敏感元数据，不会输出完整 `custom_action_param` 或 patch 内容；这些负载里可能包含凭证、token 等敏感信息。

示例文件：[`PipelineOverride.json`](../../../assets/resource/pipeline/Interface/Example/PipelineOverride.json)

### AttachToExpectedRegexAction

`AttachToExpectedRegexAction` 实现位于 `agent/go-service/common/attachregex`，用于通用地读取目标节点自身 `attach` 中的关键词，并把合并后的白名单正则写回该目标 OCR 节点的 `expected`。

- 参数：
    - `target: string`：目标节点名（将被覆盖 `expected`），必填。

处理规则：

- `attach` 内支持 `string` 或 `string[]` 两种值类型；会自动去空白、去重和正则转义。
- 当关键词列表为空时，生成 `a^`（等价于“永不匹配”）。
- 最终通过 `OverridePipeline` 覆盖目标节点的 `expected`。

示例：

```json
{
    "action": "Custom",
    "custom_action": "AttachToExpectedRegexAction",
    "custom_action_param": {
        "target": "Priority2OCR"
    }
}
```

兼容性说明：

- 信用点商店已切换为直接使用 `AttachToExpectedRegexAction`。
- 若需要覆盖多个目标节点，建议在 Pipeline 中拆成多个 `Custom` 节点并通过 `next` 串联。
- 若多个节点需要相同白名单，应在任务配置中分别把同一份 `attach` 写入各自节点。
- 其他任务也建议优先使用通用名，避免与具体业务耦合。

### PostStop

`PostStop` 实现位于 `agent/go-service/common/poststop`，调用 `Tasker.PostStop()` 异步停止当前任务。适合在 Pipeline 中某个条件满足后主动终止整个任务的场景。

- 参数：无。

### AutoAltClickAction

`AutoAltClickAction` 实现位于 `agent/go-service/common/autoalt`，用于在指定位置执行 Alt + 点击操作。先按下 Alt 键，再点击目标位置，最后松开 Alt 键。

- 参数：
    - `target_offset?: [int, int, int, int]`：可选。形如 `[dx, dy, dw, dh]`，叠加到 `box` 后再取中心点击，语义与内置 `Click` 动作的 `target_offset` 一致；省略时直接点击 `box` 中心。

默认目标位置由 Pipeline 节点的 `box` 决定。

### AutoAltSwipeAction

`AutoAltSwipeAction` 实现位于 `agent/go-service/common/autoalt`，用于执行 Alt + 滑动操作。先按下 Alt 键，再执行滑动，最后松开 Alt 键。

- 参数（均可选，透传给子节点 `__AutoAltSwipeMouseSwipeAction` 的 Swipe 动作）：
    - `begin?: [int, int] | [int, int, int, int]`：滑动起点；省略时默认 `arg.Box`。
    - `end?: [int, int] | [int, int, int, int]`：滑动终点；省略时默认 `arg.Box`。
    - `begin_offset?: [int, int, int, int]`：在默认起点（`arg.Box`）上叠加 `[dx, dy, dw, dh]`。
    - `end_offset?: [int, int, int, int]`：在默认终点（`arg.Box`）上叠加 `[dx, dy, dw, dh]`。
    - `duration?: int`：滑动持续时间（毫秒）。
    - `end_hold?: int`：滑动结束后按住时长（毫秒）。
    - `only_hover?: bool`：是否仅悬停滑动。

---

## Custom Recognition

Recognition 节点用于执行自定义识别。常见写法如下：

```json
{
    "recognition": {
        "type": "Custom",
        "param": {
            "custom_recognition": "SomeRecognition",
            "custom_recognition_param": {
                "foo": "bar"
            }
        }
    }
}
```

- `custom_recognition`：注册名。
- `custom_recognition_param`：任意 JSON 值，由框架序列化后传给实现侧。
- 返回 `true` 表示命中；返回 `false` 表示未命中。

### ExpressionRecognition

`ExpressionRecognition` 实现位于 `agent/go-service/common/expressionrecognition`，用于计算由数字识别节点组成的布尔表达式。

参数：

- `expression: string`：必填。表达式最终必须计算为布尔值。
- `box_node?: string`：可选。命中后返回哪个识别节点的结果框；若该节点是 `And`，则会先执行该节点，再按其原生 `box_index` 从本次识别返回结果中直接读取对应子识别结果的框。

占位规则：

- 使用 `{节点名}` 引用其他识别节点。
- 被引用节点会以当前图片 `arg.Img` 执行一次识别。
- 若被引用节点是 `And`，当前实现会先执行该 `And` 节点本身，再按该节点原生 `box_index` 从本次识别返回结果中直接读取对应子识别结果，并将其视为该节点的最终取值来源。
- 当前实现会从被引用节点的 OCR 结果中提取数值参与计算，并支持常见缩写格式，例如 `1.38万`、`13.8K`、`22.01M`；这类值会先换算为整数再参与表达式计算。

支持的运算：

- 算术：`+` `-` `*` `/` `%`
- 比较：`<` `<=` `>` `>=` `==` `!=`
- 逻辑：`&&` `||` `!`
- 分组：`(...)`

示例：

```json
{
    "recognition": {
        "type": "Custom",
        "param": {
            "custom_recognition": "ExpressionRecognition",
            "custom_recognition_param": {
                "expression": "{CreditShoppingReserveCreditOCRInternal}<{ReserveCreditThreshold}",
                "box_node": "CreditShoppingReserveCreditOCRInternal"
            }
        }
    }
}
```

再例如：

- `{CurrentCredit}<300`
- `{CurrentCredit}-{RefreshCost}<400`
- `({NodeA}+{NodeB})>=100 && {NodeC}==1`

注意事项：

- 表达式结果必须是布尔值，否则识别失败。
- 被引用节点当前应能返回可解析的 OCR 数值结果，否则表达式求值失败。
- 对 `And` 节点，`box_index` 指向的本次子识别结果当前需要直接包含可解析的 OCR 数值结果。
- 表达式中的整数字面量，以及 OCR 换算后的数值，若超出当前平台 `int` 可表示范围，会自动钳制到 `int` 最大值或最小值（正溢出取最大值，负溢出取最小值），并输出警告日志；表达式会继续求值，而不是直接失败。
- 该识别器只负责表达式求值，不负责业务语义本身，业务侧应在 Pipeline 中自行组织节点与阈值。

### ListCompleteRecognition

`ListCompleteRecognition` 实现位于 `agent/go-service/common/listcomplete`，用于通过 OCR 指纹是否变化判断列表是否仍在更新（常见于滑动列表到底检测）。

参数：

- `node: string`：必填。OCR 节点名，或 `And` 节点名（其 `box_index` 指向的子项必须是 OCR）。
- `retry: int`：可选，默认 `0`。指纹与 `attach.last_text` 相等后，仍返回命中的次数；当连续相等次数 `unchanged_count > retry` 时才视为到底并返回未命中。`0` 表示不重试（第一次相等即到底）。

行为：

1. 执行 `node` 识别；未命中或无法提取 OCR 文字时返回未命中。
2. 从目标 OCR 结果收集命中（优先 `Filtered`，否则 `All`），按纵向（再按横向）排序后只取首尾两条用换行拼接为指纹（仅一条时用该条）；返回框取最上方一条。比只用 `Best` 更能发现「顶不变、底已滚」；比整屏 join 更耐中间 OCR 抖动。
3. 读取当前自定义识别节点自身的 `attach.last_text` / `attach.unchanged_count`。
4. 若 `last_text` 为空（首次成功）：返回命中，写入当前指纹，并将 `unchanged_count` 置 `0`。
5. 若当前指纹与 `last_text` 一致：`unchanged_count += 1`；若 `unchanged_count > retry` 返回未命中（到底），否则仍返回命中（确认重试）。
6. 若当前指纹与 `last_text` 不一致：更新 `attach.last_text`、将 `unchanged_count` 置 `0` 并返回命中。

对 `And` 节点，目标解析与 `ExpressionRecognition` 共用 `pkg/recogtarget`：先执行该 `And` 节点本身，再按其原生 `box_index`（默认 `0`）从本次 `CombinedResult` 中选取对应子识别结果，并从该子结果提取 OCR。节点定义阶段也会校验 `box_index` 目标含 OCR。

示例文件：[`ListCompleteRecognition.json`](../../../assets/resource/pipeline/Interface/Example/ListCompleteRecognition.json)

```json
{
    "recognition": {
        "type": "Custom",
        "param": {
            "custom_recognition": "ListCompleteRecognition",
            "custom_recognition_param": {
                "node": "SomeListAnchorOCR",
                "retry": 1
            }
        }
    }
}
```

注意事项：

- 状态保存在**当前 Custom 识别节点**的 `attach.last_text` / `attach.unchanged_count`，不是 `node` 指向的 OCR/`And` 节点。
- 需要重新开始一轮列表扫描时，应清空该 Custom 节点的 `attach.last_text`（建议同时将 `unchanged_count` 置 `0`，例如通过 `PipelineOverride`）。
- 该识别器只负责“OCR 首尾指纹是否变化”，滑动、点击等流程仍由 Pipeline 组织。

### ExpendableRecognition

`ExpendableRecognition` 实现位于 `agent/go-service/common/expendable`，用于「点过一次就不再点同一目标」的列表消费（如活动中心未读入口、好友列表逐个拜访）。

参数：

- `candidate: string`：必填。候选节点：`OCR`，或 `And`（`box_index` 指向文案 OCR）。只覆盖该命名 OCR；命中框即返回给 `Click` 的框。
- `visited_node: string`：可选。黑名单读写该节点的 `attach.visited`；省略则用当前 Custom 节点。多个消费节点可指向同一 `visited_node` 共享黑名单（例如备注优先节点 + 普通节点）。
- `key_regex: string`：可选。从 OCR 原文提取写入 `attach.visited` 的 key（有捕获组取第 1 组，否则取整段匹配；未命中则用原文）。未配置时与旧行为一致：原文精确入库、精确排除。配置后黑名单会额外容忍 key 后的非数字 OCR 尾噪。业务文案规则（如备注截到第一个 `)`、普通名截到 `#UID`）由 Pipeline 声明，不写进通用模块。

行为：

1. 从 `visited_node`（或当前节点）读取 `attach.visited`。
2. 解析 `candidate` 的 key OCR（And 用 `box_index`），读取其 `expected`，按 `visited` 拼负向黑名单并只覆盖 `expected`（`order_by` 等其它字段保持原样）。
3. 执行 `candidate`；未命中则失败。
4. 从命中结果取 OCR 文案；若配置了 `key_regex` 则先提取 key，再写入上述节点的 `attach.visited`，返回命中框。

候选结构、点击目标、备注优先（多项 `expected` + `order_by: Expected`，或拆成两个消费节点 + 共享 `visited_node`），以及 OCR 文案如何收成 key，均由 Pipeline 配置。

示例文件：[`ExpendableRecognition.json`](../../../assets/resource/pipeline/Interface/Example/ExpendableRecognition.json)

```json
{
    "recognition": {
        "type": "Custom",
        "param": {
            "custom_recognition": "ExpendableRecognition",
            "custom_recognition_param": {
                "candidate": "SomeCandidateAnd",
                "key_regex": ".*?[)）]"
            }
        }
    },
    "attach": {
        "visited": []
    }
}
```

注意事项：

- 状态保存在 `visited_node`（默认当前 Custom 节点）的 `attach.visited`。
- 新一轮扫描前应清空该节点的 `attach.visited`（例如任务重入或 `PipelineOverride`）。
- 发现到的 key OCR 上的 `expected` 会被整表覆盖为「基模板 + visited 黑名单」；基模板来自覆盖前节点（会剥掉上一轮注入的负向前缀）。
- key OCR 必须是**命名节点引用**（`And.box_index` 不能指向内联 OCR）。
- `key_regex` 只执行调用方声明的截取规则；通用模块不内置具体游戏文案。
### ScheduleRecognition

`ScheduleRecognition` 实现位于 `agent/go-service/common/schedule`，用于按星期几判断当前任务是否应继续执行。它只返回识别是否命中，不在 Go 中直接运行子任务；后续流程应通过 Pipeline 的 `next` 组织。

- 参数：无。
- `attach` 字段（写在当前识别节点中，可以在任务配置中合并）：
    - `monday: bool` — 周一是否执行。
    - `tuesday: bool` — 周二是否执行。
    - `wednesday: bool` — 周三是否执行。
    - `thursday: bool` — 周四是否执行。
    - `friday: bool` — 周五是否执行。
    - `saturday: bool` — 周六是否执行。
    - `sunday: bool` — 周日是否执行。

省略某个工作日标志时，默认视为 `false`（当天不执行）。若当天不在调度范围内，该 Recognition 会发出一条“今日跳过”的本地化提示并返回未命中。

## 小结

写 Pipeline 时，内置的 `TemplateMatch` / `OCR` / `Click` / `Swipe` 能解决绝大多数需求。遇到它们搞不定的——比如要比较两个 OCR 数值、运行时动态调参数、批量跑子任务——再来查这篇，看有没有现成的 Custom 能用。

| 场景                          | 用什么                        |
| ----------------------------- | ----------------------------- |
| 按顺序跑一组子任务            | `SubTask`                     |
| 清零某节点的命中计数          | `ClearHitCount`               |
| 强制让 Action 失败            | `FalseAction`                 |
| 重复动作直到节点出现          | `RepeatUntilFoundAction`      |
| 重复动作直到节点消失          | `RepeatUntilNotFoundAction`   |
| 主动停止当前任务              | `PostStop`                    |
| 运行时改节点参数              | `PipelineOverride`            |
| 把关键词拼成正则写回 OCR 节点 | `AttachToExpectedRegexAction` |
| 计算 OCR 数值表达式           | `ExpressionRecognition`       |
| 判断列表 OCR 文本是否变化     | `ListCompleteRecognition`     |
| 消费性点选（visited 排除）    | `ExpendableRecognition`       |
| 按星期几门控后续节点          | `ScheduleRecognition`         |
| 在指定位置 Alt + 点击         | `AutoAltClickAction`          |
| Alt + 滑动                    | `AutoAltSwipeAction`          |

所有 Custom 的 Go 代码实现在 `agent/go-service/` 下，Pipeline 作者不需要关心，照文档参数写 JSON 就行。
