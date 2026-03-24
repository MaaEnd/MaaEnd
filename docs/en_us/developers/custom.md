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

`ExpressionRecognition` is implemented in `agent/go-service/expressionrecognition` and evaluates boolean expressions composed of numeric recognition nodes.

Parameters:

- `expression: string`: required. The final result of the expression must be boolean.

Placeholder rules:

- Use `{NodeName}` to reference another recognition node.
- Each referenced node is executed once against the current image `arg.Img`.
- The current implementation extracts digits from the referenced node's OCR result and uses them in the expression.

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
                "expression": "{CreditShoppingReserveCreditOCRInternal}<{ReserveCreditThreshold}"
            }
        }
    }
}
```

Other examples:

- `{CurrentCredit}<300`
- `{CurrentCredit}-{RefreshCost}<400`
- `({NodeA}+{NodeB})>=100 && {NodeC}==1`

Notes:

- The final expression result must be boolean, otherwise the recognition fails.
- Referenced nodes must currently produce OCR results containing digits, otherwise evaluation fails.
- This recognizer is only responsible for expression evaluation. Business semantics should remain in Pipeline design.
