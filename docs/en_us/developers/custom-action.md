<!-- markdownlint-disable MD060 -->

# Development Guide - Custom Action Reference

`Custom` is a generic node type in the Pipeline used to invoke **custom actions**.  
The concrete logic is registered on the project side via `MaaResourceRegisterCustomAction` (for example, implementations under `agent/go-service`), while the Pipeline is only responsible for **parameter passing and scheduling**.

Unlike normal click/recognition nodes, `Custom` does not limit what the action actually does—  
as long as it is registered during the resource loading stage, it can be called in any Pipeline in a unified way, for example:

- Take a screenshot once and save it locally.
- Execute multiple tasks in sequence like the `SubTask` action.
- Perform complex multi-step interactions (long-press, drag, combo keys, etc.).
- Do statistics, logging, or telemetry reporting.

---

<!-- markdownlint-enable MD060 -->

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
