# Development Guide - Custom Action Reference

`Custom` is a generic node type in the Pipeline used to invoke **custom actions**.  
The concrete logic is registered on the project side via `MaaResourceRegisterCustomAction` (for example, implementations under `agent/go-service`), while the Pipeline is only responsible for **parameter passing and scheduling**.

Unlike normal click/recognition nodes, `Custom` does not limit what the action actually does—  
as long as it is registered during the resource loading stage, it can be called in any Pipeline in a unified way, for example:

- Take a screenshot once and save it locally.
- Execute multiple tasks in sequence like the `SubTask` action.
- Modify node states like the `ClearHitCount` action.
- Perform complex multi-step interactions (long-press, drag, combo keys, etc.).
- Do statistics, logging, or telemetry reporting.

---

## SubTask Action

`SubTask` is a subtask execution action invoked through `Custom`, implemented in `agent/go-service/subtask`.  
It executes the task names specified in the `sub` field of `custom_action_param` in sequence.

- **Parameters (`custom_action_param`)**

    - A JSON object is required, which is serialized to a string by the framework and passed to Go.
    - Field descriptions:
        - `sub: string[]`: List of task names to execute in sequence (required). For example, `["TaskA", "TaskB"]` will execute TaskA first, then TaskB after completion.
        - `continue?: bool`: Whether to continue executing subsequent subtasks if any subtask fails (optional, default `false`). When set to `true`, even if a subtask fails, it will continue executing the remaining tasks in the list.
        - `strict?: bool`: Whether the current action is considered failed if any subtask fails (optional, default `true`). When set to `false`, the action will return success even if a subtask fails.

- **Usage Example**

    See [`SubTask.json`](../../../assets/resource/pipeline/Interface/Example/SubTask.json) for a complete example.

- **Notes**
    - Subtasks are executed in the order of the `sub` array, starting the next subtask only after the previous one is completed.
    - Subtasks can be any loaded task, including tasks defined in other Pipeline files.
    - When `strict: true` and any subtask fails, the entire SubTask action will return failure.

---

## ClearHitCount Action

`ClearHitCount` is a hit count clearing action invoked through `Custom`, implemented in `agent/go-service/clearhitcount`.  
It clears the hit counts of the nodes specified in the `nodes` field of `custom_action_param`.

- **Parameters (`custom_action_param`)**

    - A JSON object is required, which is serialized to a string by the framework and passed to Go.
    - Field descriptions:
        - `nodes: string[]`: List of node names to clear hit counts for (required). For example, `["NodeA", "NodeB"]` will clear the hit counts of NodeA and NodeB.
        - `strict?: bool`: Whether to use strict mode, where the current action is considered failed if any node's hit count clearing fails (optional, default `false`). When set to `false`, the action will return success even if some nodes' hit counts fail to clear. When set to `true`, any failure to clear a node will cause the action to return failure.

- **Usage Example**

    See [`ClearHitCount.json`](../../../assets/resource/pipeline/Interface/Example/ClearHitCount.json) for a complete example.

- **Notes**
    - Nodes' hit counts are cleared in the order of the `nodes` array; failure to clear one node does not affect the clearing of other nodes.
    - Node names must match exactly with the node names defined in the Pipeline.
    - Clearing a node will fail if the node does not exist or has never been executed.
    - When `strict: false`, the action will return success even if some nodes fail to clear, suitable for cleaning up optional nodes that may not exist.
    - When `strict: true`, any failure to clear a node will cause the action to return failure, suitable for clearing hit counts of critical nodes.
