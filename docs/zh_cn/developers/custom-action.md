<!-- markdownlint-disable MD060 -->

# 开发手册 - Custom 自定义动作参考

`Custom` 是 Pipeline 中用于调用 **自定义动作** 的通用节点类型。  
具体逻辑由项目侧通过 `MaaResourceRegisterCustomAction` 注册（如 `agent/go-service` 中的实现），Pipeline 仅负责 **传参与调度**。

与普通点击、识别节点不同，`Custom` 不限定具体行为——  
只要在资源加载阶段完成注册，就可以在任意 Pipeline 中以统一的方式调用，例如：

- 执行一次截图并保存到本地。
- 按顺序执行多个任务如 `SubTask` 动作。
- 修改节点状态如 `ClearHitCount` 动作。
- 进行复杂的多步交互（长按、拖拽、组合键等）。
- 做一些统计、日志或埋点上报。

---

<!-- markdownlint-enable MD060 -->

## SubTask 动作

`SubTask` 是一个通过 `Custom` 调用的子任务执行动作，实现位于 `agent/go-service/subtask`  
按顺序执行 `custom_action_param` 中 `sub` 字段指定的任务名。

- **参数（`custom_action_param`）**

    - 需要传入一个 JSON 对象，由框架序列化为字符串后传给 Go。
    - 字段说明：
        - `sub: string[]`：要顺序执行的任务名列表（必填）。例如 `["TaskA", "TaskB"]` 会先执行 TaskA，完成后执行 TaskB。
        - `continue?: bool`：任一子任务失败时是否继续执行后续子任务（可选，默认 `false`）。设置为 `true` 时，即使某个子任务失败也会继续执行列表中的剩余任务。
        - `strict?: bool`：任一子任务失败时当前 action 是否视为失败（可选，默认 `true`）。设置为 `false` 时，即使子任务失败，action 也会返回成功。

- **使用示例**

    完整示例请参考：[`SubTask.json`](../../../assets/resource/pipeline/Interface/Example/SubTask.json)

- **注意事项**
    - 子任务按 `sub` 数组顺序依次执行，前一个子任务完成后才会开始下一个。
    - 子任务可以是任何已加载的任务，包括其他 Pipeline 文件中定义的任务。
    - 当 `strict: true` 且任一子任务失败时，整个 SubTask 动作会返回失败。

---

## ClearHitCount 动作

`ClearHitCount` 是一个通过 `Custom` 调用的清除节点命中计数动作，实现位于 `agent/go-service/clearhitcount`  
清除 `custom_action_param` 中 `nodes` 字段指定节点的命中计数。

- **参数（`custom_action_param`）**

    - 需要传入一个 JSON 对象，由框架序列化为字符串后传给 Go。
    - 字段说明：
        - `nodes: string[]`：要清除命中计数的节点名称列表（必填）。例如 `["NodeA", "NodeB"]` 会清除 NodeA 和 NodeB 的命中计数。
        - `strict?: bool`：是否严格模式，任一节点清除失败时当前 action 是否视为失败（可选，默认 `false`）。设置为 `false` 时，即使部分节点清除失败，action 也会返回成功。设置为 `true` 时，任一节点清除失败都会导致 action 返回失败。

- **使用示例**

    完整示例请参考：[`ClearHitCount.json`](../../../assets/resource/pipeline/Interface/Example/ClearHitCount.json)

- **注意事项**
    - 节点按 `nodes` 数组顺序依次清除计数，某个节点清除失败不影响其他节点的清除。
    - 节点名称必须与 Pipeline 中定义的节点名称完全一致。
    - 节点不存在或从未被执行过时，清除操作会失败。
    - 当 `strict: false` 时，即使部分节点清除失败，action 也会返回成功，适用于清理可能不存在的可选节点。
    - 当 `strict: true` 时，任一节点清除失败都会导致 action 返回失败，适用于关键节点的计数清理。

---

## RecoDetailFocusAction 动作

`RecoDetailFocusAction` 是一个通过 `Custom` 调用的识别结果展示动作，实现位于 `agent/go-service/recodetailfocus`。  
它会直接读取当前 action 节点的识别结果（`RecognitionDetail`），提取其中 OCR 文本并组合后通过 Focus 事件显示。

- **参数（`custom_action_param`）**

    - 可传入一个 JSON 对象，由框架序列化为字符串后传给 Go。
    - 字段说明：
        - `text?: string`：展示模板（可选）。未传时使用默认模板：`roi={roi}, text={text}`。

- **支持的替换变量**

    - `{text}`：从当前识别结果中提取并去重后拼接的 OCR 文本（使用 ` | ` 连接）。
    - `{node}`：当前节点名（`CurrentTaskName`）。
    - `{hit}`：当前识别是否命中（`true/false`）。
    - `{roi}`：当前实现固定为 `N/A`（仅保留模板兼容）。

- **使用示例**

```json
{
    "action": "Custom",
    "custom_action": "RecoDetailFocusAction",
    "custom_action_param": {
        "text": "节点={node}, 命中={hit}, 识别={text}"
    }
}
```

- **注意事项**
    - 变量名区分大小写，推荐按文档中的写法使用。
    - 当某项信息不存在时，会替换为 `N/A`。
    - 本动作不会主动执行 OCR；它依赖当前节点已有的 `RecognitionDetail`。
