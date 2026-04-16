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

示例文件：[`SubTask.json`](../../../assets/resource/pipeline/Interface/Example/SubTask.json)

### ClearHitCount

`ClearHitCount` 实现位于 `agent/go-service/clearhitcount`，用于清除指定节点的命中计数。

- 参数：
    - `nodes: string[]`：要清理的节点名列表，必填。
    - `strict?: bool`：任一节点清理失败时当前 Action 是否返回失败，默认 `false`。

示例文件：[`ClearHitCount.json`](../../../assets/resource/pipeline/Interface/Example/ClearHitCount.json)

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
- 该识别器只负责表达式求值，不负责业务语义本身，业务侧应在 Pipeline 中自行组织节点与阈值。
