# Development Manual - Custom Actions and Recognition Reference

`Custom` is used to invoke custom logic registered on the project side within a Pipeline. It is divided into two categories:

- `Custom Action`: Executes action logic, such as subtask scheduling, state cleanup, and complex interactions.
- `Custom Recognition`: Executes recognition logic, returns whether it matches, and optionally provides detailed recognition results.

Go implementations in the project are typically located under `agent/go-service/` and are registered via:

- `maa.AgentServerRegisterCustomAction(...)`
- `maa.AgentServerRegisterCustomRecognition(...)`

---

## Custom Action

Action nodes are used to execute custom actions. A common format is as follows:

```json
{
    "action": "Custom",
    "custom_action": "SomeAction",
    "custom_action_param": {
        "foo": "bar"
    }
}
```

- `custom_action`: The registration name.
- `custom_action_param`: An arbitrary JSON value, serialized by the framework and passed to the implementation side.

### SubTask

The `SubTask` implementation is located in `agent/go-service/subtask` and is used to execute a series of subtasks sequentially.

- Parameters:
    - `sub: string[]`: required list of subtask names.
    - `continue?: bool`: whether to continue after a subtask fails. Default is `false`.
    - `strict?: bool`: whether the current action should fail when a subtask fails. Default is `true`.
    - `random_choice?: int`: if set and greater than `0`, the `sub` list is shuffled and at most this many subtasks are picked to run; values exceeding the list length are clamped to it. Defaults to running every subtask in order.

    Before the random pick, subtask nodes in `sub` that cannot be resolved or whose `enabled` is `false` are filtered out (a node without an explicit `enabled` counts as enabled). If no runnable subtask remains after filtering (and the random pick), the action is not treated as a failure: it logs a single warning and returns success.

Example file: [`SubTask.json`](../../../assets/resource/pipeline/Interface/Example/SubTask.json)

### FailureCollector

`FailureCollector` is implemented in `agent/go-service/common/failurecollector`. It collects failures across multiple subtasks and reports overall failure only after all subtasks finish. An individual subtask failure does not interrupt subsequent subtasks.

Three Custom Actions are provided, linked into one collection by a shared `key`:

- `FailureCollectorReset`: Resets the collection state for the given `key`. Must be called before all RunTask invocations.
- `FailureCollectorRunTask`: Runs the subtask specified by `task`. On success, proceeds normally; on failure, records `failure_task` in occurrence order and optionally runs `recovery_task`. The Action itself always returns success so the Pipeline continues.
- `FailureCollectorFinish`: Runs every recorded `failure_task` node in failure order, then clears the state. Returns failure when any failures were recorded, success otherwise.

- Parameters:
    - `FailureCollectorReset`:
        - `key: string`: Collection identifier. Required. Must be consistent and globally unique within the same flow.
    - `FailureCollectorRunTask`:
        - `key: string`: Collection identifier. Required.
        - `task: string`: The subtask Pipeline node to run. Required. When the target node is disabled (`Enabled = false`), it is skipped and not treated as a failure.
        - `failure_task: string`: The Pipeline node recorded on failure. Required. This node typically uses `focus` to surface a user-visible message through the Agent.
        - `recovery_task?: string`: A recovery task node to run after a failure. Optional.
    - `FailureCollectorFinish`:
        - `key: string`: Collection identifier. Required.

Example file: [`AutoCollect.json`](../../../assets/resource/pipeline/AutoCollect.json)

### ClearHitCount

The `ClearHitCount` implementation is located in `agent/go-service/clearhitcount` and is used to clear the hit count of specified nodes.

- Parameters:
    - `nodes: string[]`: A list of node names to clear. Required.
    - `strict?: bool`: Whether the current Action returns a failure if clearing any node fails. Default is `false`.

Example file: [`ClearHitCount.json`](../../../assets/resource/pipeline/Interface/Example/ClearHitCount.json)

### FalseAction

The `FalseAction` implementation is located in `agent/go-service/common/falseaction` and always returns a failure. It is commonly used as a placeholder in Pipelines where an Action needs to be forced to fail.

- Parameters: None.

### PipelineOverride

The `PipelineOverride` implementation is located in `agent/go-service/common/pipelineoverride` and is used at runtime to merge **node-organized partial JSON** into the current Pipeline (`ctx.OverridePipeline`). It is suitable for dynamically toggling node switches or adjusting recognition/action parameters **without changing the static flow topology**.

- Parameters:
    - `patch: object`: Required. Keys are **node names**, and values are the **partial override objects** for those nodes. Semantics are consistent with MaaFramework's `OverridePipeline`: same-named nodes are merged, same-named fields are overwritten.
    - `allow_next?: bool`: Whether to allow partial node objects to contain top-level `next`. Default is `false`; when `false`, `next` will be removed from each patch item before application to avoid runtime modification of the preset topology.
    - `strict?: bool`: When `allow_next` is `false`, if a patch still contains `next`, whether to immediately report an error and fail. Default is `false` (will remove `next` and log it); if `true`, the current Action fails immediately and no overrides are applied. If `allow_next` is `true`, `strict` is ignored and treated as `false`.

Usage Recommendations:

- Prioritize deciding the strategy at the **process entry point**; if adjustments are necessary midway, try to only modify fields like `enabled`, recognizer parameters, and action parameters. Avoid arbitrarily changing the `next` graph structure.
- If runtime modification of `next` is genuinely required, explicitly set `allow_next: true` and self-assess the debugging and regression costs; it should be kept off by default.
- For troubleshooting, use in conjunction with additional log or screenshot nodes.
- Runtime logs only record non-sensitive metadata such as node count, node names, and parameter length; they do not output the complete `custom_action_param` or patch content, which may contain sensitive information like credentials and tokens.

Example file: [`PipelineOverride.json`](../../../assets/resource/pipeline/Interface/Example/PipelineOverride.json)

### AttachToExpectedRegexAction

The `AttachToExpectedRegexAction` implementation is located in `agent/go-service/common/attachregex`. It is used to generically read keywords from the target node's own `attach` and write the merged allowlist regex back to the target OCR node's `expected`.

- Parameters:
    - `target: string`: The target node name (which will have its `expected` overwritten). Required.

Processing Rules:

- `attach` supports both `string` and `string[]` value types; it automatically trims whitespace, deduplicates, and applies regex escaping.
- When the keyword list is empty, `a^` (equivalent to "never match") is generated.
- The final merged regex overrides the target node's `expected` via `OverridePipeline`.

Example:

```json
{
    "action": "Custom",
    "custom_action": "AttachToExpectedRegexAction",
    "custom_action_param": {
        "target": "Priority2OCR"
    }
}
```

Compatibility Notes:

- The Credit Shop has been switched to directly use `AttachToExpectedRegexAction`.
- If multiple target nodes need to be overridden, it is recommended to split them into multiple `Custom` nodes in the Pipeline and link them via `next`.
- If multiple nodes require the same allowlist, the same `attach` should be written into their respective nodes in the task configuration.
- Other tasks are also recommended to use the generic name to avoid coupling with specific business logic.

### PostStop

The `PostStop` implementation is located in `agent/go-service/common/poststop`. It calls `Tasker.PostStop()` to asynchronously stop the current task. It is suitable for scenarios where a condition in the Pipeline requires actively terminating the entire task.

- Parameters: None.

### AutoAltClickAction

The `AutoAltClickAction` implementation is located in `agent/go-service/common/autoalt`. It performs an Alt + Click operation at a specified position. It first presses the Alt key, clicks the target position, and then releases the Alt key.

- Parameters:
    - `target_offset?: [int, int, int, int]`: Optional. Format like `[dx, dy, dw, dh]`, overlaid onto `box` before clicking the center; semantics are consistent with the `target_offset` of the built-in `Click` action. If omitted, it directly clicks the center of `box`.

The default target position is determined by the `box` of the Pipeline node.

### AutoAltLongPressAction

The `AutoAltLongPressAction` implementation is located in `agent/go-service/common/autoalt`. It performs an Alt + Long Press operation at a specified position.

- Parameters:
    - `duration: int`: Long press duration in milliseconds. Required.

### AutoAltSwipeAction

The `AutoAltSwipeAction` implementation is located in `agent/go-service/common/autoalt`. It performs an Alt + Swipe operation. It first presses the Alt key, executes the swipe, and then releases the Alt key.

- Parameters (all optional, passed through to the Swipe action of the child node `__AutoAltSwipeMouseSwipeAction`):
    - `begin?: [int, int] | [int, int, int, int]`: Swipe start point; defaults to `arg.Box` if omitted.
    - `end?: [int, int] | [int, int, int, int]`: Swipe end point; defaults to `arg.Box` if omitted.
    - `begin_offset?: [int, int, int, int]`: Overlays `[dx, dy, dw, dh]` onto the default start point (`arg.Box`).
    - `end_offset?: [int, int, int, int]`: Overlays `[dx, dy, dw, dh]` onto the default end point (`arg.Box`).
    - `duration?: int`: Swipe duration in milliseconds.
    - `end_hold?: int`: Hold duration after the swipe ends in milliseconds.
    - `only_hover?: bool`: Whether to only hover swipe.

---

## Custom Recognition

Recognition nodes are used to execute custom recognition. A common format is as follows:

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

- `custom_recognition`: The registration name.
- `custom_recognition_param`: An arbitrary JSON value, serialized by the framework and passed to the implementation side.
- Returns `true` to indicate a match; returns `false` to indicate no match.

### ExpressionRecognition

The `ExpressionRecognition` implementation is located in `agent/go-service/common/expressionrecognition`. It is used to evaluate boolean expressions composed of numerical recognition nodes.

Parameters:

- `expression: string`: Required. The expression must ultimately evaluate to a boolean value.
- `box_node?: string`: Optional. Which recognition node's result box to return upon a match; if the node is `And`, it will first execute that node, then read the corresponding sub-recognition result's box directly from the current recognition results based on its native `box_index`.

Placeholder Rules:

- Use `{NodeName}` to reference other recognition nodes.
- Referenced nodes are executed once with the current image `arg.Img`.
- If a referenced node is `And`, the current implementation first executes the `And` node itself, then reads the corresponding sub-recognition result directly from the current recognition results based on that node's native `box_index`, and treats it as the final source for that node's value.
- The current implementation extracts numerical values from the referenced node's OCR results to participate in the calculation and supports common abbreviation formats, such as `1.38万`, `13.8K`, `22.01M`; these values are converted to integers before participating in the expression calculation.

Supported Operations:

- Arithmetic: `+` `-` `*` `/` `%`
- Comparison: `<` `<=` `>` `>=` `==` `!=`
- Logical: `&&` `||` `!`
- Grouping: `(...)`

Example:

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

Another example:

- `{CurrentCredit}<300`
- `{CurrentCredit}-{RefreshCost}<400`
- `({NodeA}+{NodeB})>=100 && {NodeC}==1`

Important Notes:

- The expression result must be a boolean value; otherwise, recognition fails.
- Referenced nodes should currently return a parseable OCR numerical result; otherwise, expression evaluation fails.
- For `And` nodes, the sub-recognition result pointed to by `box_index` currently needs to directly contain a parseable OCR numerical result.
- Integer literals in expressions, and values converted from OCR, if they exceed the range representable by the platform's `int`, are automatically clamped to the `int` maximum or minimum (positive overflow takes the maximum, negative overflow takes the minimum), and a warning log is output; expression evaluation continues rather than failing immediately.
- This recognizer is only responsible for expression evaluation, not for the business semantics itself; the business side should organize nodes and thresholds within the Pipeline.

### ListCompleteRecognition

The `ListCompleteRecognition` implementation is located in `agent/go-service/common/listcomplete`. It detects whether a list is still updating by checking whether OCR text has changed (commonly used to detect when a scrollable list has reached the end).

Parameters:

- `node: string`: Required. An OCR node name, or an `And` node name whose `box_index` target must be OCR.

Behavior:

1. Run recognition on `node`; return no match if it misses or no OCR text can be extracted.
2. Read `attach.last_text` from the current custom recognition node itself.
3. If `last_text` is empty (first success): return a match, with the box set to the OCR text position, and write the current text into `attach.last_text`.
4. If the current text equals `last_text`: return no match (treat as list complete / unchanged).
5. If the current text differs from `last_text`: update `attach.last_text` and return a match.

For `And` nodes, target resolution is shared with `ExpressionRecognition` via `pkg/recogtarget`: first run the `And` node itself, then read the corresponding sub-recognition result from this run's `CombinedResult` using that node's native `box_index` (default `0`), and extract OCR text/box from that selected child. Node definition validation also requires the `box_index` target to contain OCR.

Example file: [`ListCompleteRecognition.json`](../../../assets/resource/pipeline/Interface/Example/ListCompleteRecognition.json)

```json
{
    "recognition": {
        "type": "Custom",
        "param": {
            "custom_recognition": "ListCompleteRecognition",
            "custom_recognition_param": {
                "node": "SomeListAnchorOCR"
            }
        }
    }
}
```

Notes:

- State is stored in `attach.last_text` on the **current Custom recognition node**, not on the OCR/`And` node referenced by `node`.
- To restart a list scan, clear that Custom node's `attach.last_text` (for example via `PipelineOverride`).
- This recognizer only answers "did the text change"; scrolling/clicking still belong in Pipeline.

### ScheduleRecognition

The `ScheduleRecognition` implementation is located in `agent/go-service/common/schedule`. It is used to determine whether the current task should continue executing based on the day of the week. It only returns whether recognition matches; it does not directly run subtasks in Go; subsequent flows should be organized via the Pipeline's `next`.

- Parameters: None.
- `attach` field (written in the current recognition node, can be merged in the task configuration):
    - `monday: bool` — Whether to execute on Monday.
    - `tuesday: bool` — Whether to execute on Tuesday.
    - `wednesday: bool` — Whether to execute on Wednesday.
    - `thursday: bool` — Whether to execute on Thursday.
    - `friday: bool` — Whether to execute on Friday.
    - `saturday: bool` — Whether to execute on Saturday.
    - `sunday: bool` — Whether to execute on Sunday.

When a weekday flag is omitted, it defaults to `false` (do not execute that day). If the current day is not within the scheduling range, this Recognition emits a localized prompt "Skipping today" and returns no match.

## Summary

When writing a Pipeline, the built-in `TemplateMatch` / `OCR` / `Click` / `Swipe` can handle most needs. When they fall short—for example, comparing two OCR values, dynamically adjusting parameters at runtime, or batch running subtasks—then refer to this document to see if there's an existing Custom action or recognition to use.

| Scenario                                 | Use                           |
| ---------------------------------------- | ----------------------------- |
| Run a series of subtasks in order        | `SubTask`                     |
| Clear hit count of a node                | `ClearHitCount`               |
| Force an Action to fail                  | `FalseAction`                 |
| Actively stop the current task           | `PostStop`                    |
| Change node parameters at runtime        | `PipelineOverride`            |
| Write keywords as regex back to OCR node | `AttachToExpectedRegexAction` |
| Evaluate OCR numerical expressions       | `ExpressionRecognition`       |
| Detect whether list OCR text changed     | `ListCompleteRecognition`     |
| Gate subsequent nodes by day of week     | `ScheduleRecognition`         |
| Alt + Click at specified position        | `AutoAltClickAction`          |
| Alt + Long Press at specified position   | `AutoAltLongPressAction`      |
| Alt + Swipe                              | `AutoAltSwipeAction`          |

All Custom Go code implementations are located under `agent/go-service/`. Pipeline authors do not need to concern themselves with this; just write the JSON according to the documentation parameters.
