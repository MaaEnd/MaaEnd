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
