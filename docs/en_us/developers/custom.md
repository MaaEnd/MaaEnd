# Development Guide - Custom Action and Recognition Reference

`Custom` is used in Pipeline to invoke project-registered custom logic. It has two forms:

- `Custom Action`: executes action logic such as subtask scheduling, state cleanup, or complex interactions.
- `Custom Recognition`: executes recognition logic and returns whether it matches, optionally with detail payload.

Go implementations in this project are usually located under `agent/go-service/` and registered via:

- `maa.AgentServerRegisterCustomAction(...)`
- `maa.AgentServerRegisterCustomRecognition(...)`

---

## Custom Action

An action node can invoke a custom action like this:

```json
{
    "action": "Custom",
    "custom_action": "SomeAction",
    "custom_action_param": {
        "foo": "bar"
    }
}
```

- `custom_action`: the registered action name.
- `custom_action_param`: any JSON value, serialized by the framework and passed to the implementation.

### SubTask

`SubTask` is implemented in `agent/go-service/subtask` and runs a list of subtasks in sequence.

- Parameters:
    - `sub: string[]`: required list of subtask names.
    - `continue?: bool`: whether to continue after a subtask fails. Default is `false`.
    - `strict?: bool`: whether the current action should fail when a subtask fails. Default is `true`.

Example file: [`SubTask.json`](../../../assets/resource/pipeline/Interface/Example/SubTask.json)

### ClearHitCount

`ClearHitCount` is implemented in `agent/go-service/clearhitcount` and clears hit counters of specific nodes.

- Parameters:
    - `nodes: string[]`: required list of node names to clear.
    - `strict?: bool`: whether the current action should fail when clearing any node fails. Default is `false`.

Example file: [`ClearHitCount.json`](../../../assets/resource/pipeline/Interface/Example/ClearHitCount.json)

### PipelineOverride

`PipelineOverride` is implemented in `agent/go-service/common/pipelineoverride`. It merges **partial per-node JSON** into the current pipeline at runtime (`ctx.OverridePipeline`). Use it to toggle nodes or tweak recognition params **without** rewriting the static transition graph when `allow_next` stays `false`.

- Parameters:
    - `patch: object`: required. Keys are **node names**; values are **partial** node objects. Semantics match MaaFramework `OverridePipeline` (merge same-named nodes, overwrite same-named properties).
    - `allow_next?: bool`: whether each partial node object may include a top-level `next`. Default `false`; when `false`, `next` is **removed** from every patch entry before applying, so runtime changes do not alter the preset topology.
    - `strict?: bool`: when `allow_next` is `false`, whether a patch that still contains `next` is an error. Default `false` (`next` is stripped and INFO-logged); when `true`, the action **fails** and nothing is applied—helps catch accidental `next` in `patch`. If `allow_next` is `true`, `strict` is ignored and normalized to `false`.

**Usage guidelines:**

- Prefer changing strategy at the **workflow entry**; if you must change mid-run, limit edits to fields like `enabled` or recognizer/action params, not the `next` graph.
- If you truly need to change `next` at runtime, set `allow_next: true` deliberately and assess debugging/regression cost; keep it off by default.
- Pair large overrides with logging/screenshot nodes when troubleshooting.
- Logs only record non-sensitive metadata such as node counts, node keys, and payload length. Do not rely on runtime logs to capture full `custom_action_param` or patch content, because those payloads may contain credentials or tokens.

Example file: [`PipelineOverride.json`](../../../assets/resource/pipeline/Interface/Example/PipelineOverride.json)

### AttachToExpectedRegexAction

`AttachToExpectedRegexAction` is implemented in `agent/go-service/common/attachregex`. It generically reads keywords from the target node's own `attach`, then writes the merged whitelist regex back into that target OCR node's `expected`.

- Parameters:
    - `target: string`: required target node name whose `expected` will be overridden.

Behavior:

- `attach` values support `string`, `string[]`, and `false`; string values are trimmed, deduplicated, and regex-escaped.
- When `attach.<key>` is `false`, that item key is **explicitly excluded** from the whitelist built for the current run and does not contribute any keywords to `expected`.
- `attach.<key> = true` currently does **not** mean "use default keywords"; it produces no whitelist keywords. Use an explicit string or string array instead.
- If the keyword list is empty, it generates `a^` (never matches).
- The final result is applied through `OverridePipeline` to the target node's `expected`.

A common pattern is: task options first write multiple item-name groups into one OCR node's `attach`; later in the run, once one item reaches its target amount, `PipelineOverride` can set that `attach.<key>` to `false` so future whitelist regeneration no longer matches that item.

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

Compatibility note:

- Credit shop has switched to direct use of `AttachToExpectedRegexAction`.
- If multiple targets need override, prefer multiple `Custom` nodes chained by `next` in Pipeline.
- If multiple nodes need the same whitelist, write the same `attach` content into each node in task configuration.
- Other tasks should also prefer this generic action name to avoid business coupling.

Example file: [`AttachToExpectedRegexAction.json`](../../../assets/resource/pipeline/Interface/Example/AttachToExpectedRegexAction.json)

---

## Custom Recognition

A recognition node can invoke a custom recognition like this:

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

- `custom_recognition`: the registered recognition name.
- `custom_recognition_param`: any JSON value, serialized by the framework and passed to the implementation.
- Returning `true` means matched; returning `false` means not matched.

### ExpressionRecognition

`ExpressionRecognition` is implemented in `agent/go-service/common/expressionrecognition` and evaluates boolean expressions composed of numeric recognition nodes.

Parameters:

- `expression: string`: required. The final result of the expression must be boolean.
- `box_node?: string`: optional. Which recognition node's result box should be returned when the expression matches; if that node is an `And`, it is executed first and the box is read directly from the child result selected by that node's native `box_index` in that run.

Placeholder rules:

- Use `{NodeName}` to reference another recognition node.
- Each referenced node is executed once against the current image `arg.Img`.
- If the referenced node is an `And`, the current implementation first executes that `And` node itself, then reads the child result selected by that node's native `box_index` directly from that run's returned combined result, and treats it as the final value source of the `And` node.
- The current implementation extracts numeric values from the referenced node's OCR result and supports common abbreviated formats such as `1.38万`, `13.8K`, and `22.01M`; these values are normalized to integers before expression evaluation. Formats such as `1.2W` are not supported.

Supported operators:

- Arithmetic: `+` `-` `*` `/` `%`
- Comparison: `<` `<=` `>` `>=` `==` `!=`
- Logic: `&&` `||` `!`
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

Other examples:

- `{CurrentCredit}<300`
- `{CurrentCredit}-{RefreshCost}<400`
- `({NodeA}+{NodeB})>=100 && {NodeC}==1`

Example file: [`ExpressionRecognition.json`](../../../assets/resource/pipeline/Interface/Example/ExpressionRecognition.json)

Notes:

- The final expression result must be boolean, otherwise the recognition fails.
- Referenced nodes must currently produce OCR results that can be parsed as numeric values, otherwise evaluation fails.
- For `And` nodes, the child result selected by `box_index` in that run must directly contain OCR results that can be parsed as numeric values.
- This recognizer is only responsible for expression evaluation. Business semantics should remain in Pipeline design.
