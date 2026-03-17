# Development Guide - QuantizedSliding Reference Document

`QuantizedSliding` is a go-service custom action implementation invoked through the `Custom` action type.
It is used for interfaces where you drag a slider to choose a quantity, but the target value is a discrete level rather than a continuous value.

It is suitable for scenarios like these:

- You first drag to an approximate position, then fine-tune the quantity with `+` / `-` buttons.
- The slider itself does not have stable fixed coordinates, but the slider handle template can be recognized.
- The current quantity can be read by OCR, and the maximum value on the screen changes with stock or other conditions.

The current implementation is located at:

- Go action package: `agent/go-service/quantizedsliding/`
- Package-local registration: `agent/go-service/quantizedsliding/register.go`
- go-service global registration entry: `agent/go-service/register.go`
- Shared Pipeline: `assets/resource/pipeline/QuantizedSliding/Main.json`
- Existing integration example: `assets/resource/pipeline/AutoStockpile/Task.json`

`agent/go-service/quantizedsliding/` is now split by responsibility:

| File           | Responsibility                                                         |
| -------------- | ---------------------------------------------------------------------- |
| `types.go`     | Parameter structs, action type, constants, and package-level variables |
| `handlers.go`  | `Run()` dispatch, per-stage handlers, and state reset                  |
| `overrides.go` | Pipeline override construction                                         |
| `ocr.go`       | OCR text extraction and recognition box parsing                        |
| `normalize.go` | Button parameter normalization and basic calculation helpers           |
| `register.go`  | Registers the `QuantizedSliding` action into go-service                |

## Execution modes

`QuantizedSliding` currently has two execution modes:

1. **External invocation mode**: when a business task calls it with `custom_action: "QuantizedSliding"`, the Go side automatically constructs the internal Pipeline override and starts running the full internal flow from `QuantizedSlidingMain` through its downstream nodes.
2. **Internal node mode**: when the current node itself is one of `QuantizedSlidingMain`, `QuantizedSlidingFindStart`, `QuantizedSlidingGetMaxQuantity`, `QuantizedSlidingFindEnd`, `QuantizedSlidingCheckQuantity`, or `QuantizedSlidingDone`, the Go side directly handles that specific stage.

In other words, the business-side caller usually only needs to pass `custom_action_param` once and does **not** need to manually chain the internal nodes.

## How it works

`QuantizedSliding` does not simply “swipe to a fixed percentage.” Instead, it uses a **detect, calculate, then fine-tune** flow.

The overall steps are:

1. Recognize the current slider handle position and record the drag start point.
2. Drag the slider to the maximum value.
3. Use OCR to recognize the current maximum selectable quantity.
4. Recognize the slider handle position again and record the drag end point.
5. Calculate the exact click position from `Target` and `maxQuantity`.
6. Click that position.
7. Use OCR again to read the current quantity. If it still does not equal the target value, fine-tune it through the increase/decrease buttons.
8. Finish after the quantity matches the target value.

For step 5, the current implementation computes the precise click position using linear interpolation:

```text
numerator = Target - 1
denominator = maxQuantity - 1
clickX = startX + (endX - startX) * numerator / denominator
clickY = startY + (endY - startY) * numerator / denominator
```

The computed `[clickX, clickY]` is then dynamically written into `QuantizedSlidingPreciseClick.action.param.target`.

The internal nodes are executed by `QuantizedSliding` itself through `QuantizedSlidingMain` and its downstream nodes. The caller does **not** need to manually chain those internal nodes.

## How to call it

In a business Pipeline, call it like a normal `Custom` action. The example below uses MaaFramework Pipeline protocol v2 syntax.

Pass `custom_action_param` as a JSON object directly.

```json
"SomeTaskAdjustQuantity": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "QuantizedSliding",
            "custom_action_param": {
                "Target": 1,
                "QuantityBox": [360, 490, 110, 70],
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "DecreaseButton": "AutoStockpile/DecreaseButton.png",
                "CenterPointOffset": [-10, 0]
            }
        }
    }
}
```

## Parameter description

`custom_action_param` should be passed as a JSON object directly. The commonly used fields are:

| Field               | Type                    | Required | Description                                                                                               |
| ------------------- | ----------------------- | -------- | --------------------------------------------------------------------------------------------------------- |
| `Target`            | `int`                   | Yes      | The target quantity. The final discrete value you want to reach.                                          |
| `QuantityBox`       | `int[4]`                | Yes      | OCR region for the current quantity. The format must be `[x, y, w, h]`.                                   |
| `QuantityFilter`    | `object`                | No       | Optional color filtering for quantity OCR, useful when digit color is stable but the background is noisy. |
| `Direction`         | `string`                | Yes      | Drag direction. Supports `left` / `right` / `up` / `down`.                                                |
| `IncreaseButton`    | `string` or `int[2\|4]` | Yes      | The “increase quantity” button. Can be a template path or coordinates.                                    |
| `DecreaseButton`    | `string` or `int[2\|4]` | Yes      | The “decrease quantity” button. Can be a template path or coordinates.                                    |
| `CenterPointOffset` | `int[2]`                | No       | Click offset relative to the slider handle center, default `[-10, 0]`.                                    |

`CenterPointOffset` is used to fine-tune the final click position for `QuantizedSlidingPreciseClick`. Its format must be `[x, y]`:

- `x` is the horizontal offset. Negative moves left, positive moves right.
- `y` is the vertical offset. Negative moves up, positive moves down.
- If omitted, the default is `[-10, 0]`, which means clicking 10 pixels to the left of the slider center.

### `QuantityFilter`

`QuantityFilter` is an **optional enhancement**. If omitted, `QuantizedSliding` behaves exactly like the current version. If provided, OCR for `QuantizedSlidingGetQuantity` first applies color filtering and then reads the digits.

It is a good fit when:

- the digit color itself is stable;
- the background, outline, shadow, or nearby numbers introduce OCR noise;
- `QuantityBox` is already correct, but OCR needs one more preprocessing step.

Minimal example:

```json
"QuantityFilter": {
    "method": 4,
    "lower": [0, 0, 0],
    "upper": [255, 255, 255]
}
```

Constraints and limits:

- `lower` and `upper` must both be present and must have the same length;
- use the common `ColorMatch` methods already used in this repo: `4` (RGB), `40` (HSV), or `6` (GRAY);
- only a **single** color range is supported for now; `[[...], [...]]` multi-range input is not supported;
- `QuantityFilter` improves OCR preprocessing, but it is not a substitute for an inaccurate `QuantityBox`.

### `IncreaseButton` / `DecreaseButton` formats

These two fields support two forms:

#### 1. Pass a template path (recommended)

```json
"IncreaseButton": "AutoStockpile/IncreaseButton.png"
```

In this case, go-service dynamically rewrites the corresponding branch node to `TemplateMatch + Click`:

- The template threshold is fixed at `0.8`
- `green_mask` is fixed at `true`
- The click uses `target: true` and includes `target_offset: [5, 5, -5, -5]`

This is usually more stable than hardcoded coordinates, so it is the preferred option.

#### 2. Pass coordinates

Supported formats:

- `[x, y]`
- `[x, y, w, h]`

If `[x, y]` is passed, it will be automatically normalized to `[x, y, 1, 1]` internally.

Also note that after JSON deserialization on the Go side, these arrays may appear as `[]float64` or `[]any`. The current implementation normalizes them into integer arrays automatically. However, if the length is neither `2` nor `4`, the action fails immediately.

## Direction convention

`Direction` determines which direction is treated as “toward the maximum value.” The current implementation uses these hardcoded override end regions:

- `right` / `up`: `[1260, 10, 10, 10]`
- `left` / `down`: `[10, 700, 10, 10]`

This does not mean the slider handle literally moves along the screen diagonal. The current implementation simply overrides the `Swipe` node with a sufficiently distant end region to force the slider toward the corresponding endpoint.

Because of that, when `Direction` is set incorrectly, the most common result is not “slightly off,” but rather:

- The maximum value is recognized incorrectly;
- The slider is not pushed to the real endpoint;
- All subsequent proportional clicks are shifted.

## Shared nodes it depends on

Internally, `QuantizedSliding` depends on shared nodes in `assets/resource/pipeline/QuantizedSliding/Main.json`, mainly including:

- `QuantizedSlidingSwipeButton`: recognizes the slider template `QuantizedSliding/SwipeButton.png`
- `QuantizedSlidingSwipeToMax`: drags to the maximum value
- `QuantizedSlidingGetQuantity`: OCR for the current quantity
- `QuantizedSlidingCheckQuantity`: determines whether fine-tuning is needed
- `QuantizedSlidingIncreaseQuantity` / `QuantizedSlidingDecreaseQuantity`: clicks the increase/decrease buttons
- `QuantizedSlidingDone`: successful exit

Two points are the most critical:

1. The slider template `QuantizedSliding/SwipeButton.png` must be recognized reliably.
2. The OCR for `QuantityBox` must be able to read numbers reliably.

If either of these prerequisites is not met, more accurate proportional calculations will not help.

## Integration steps

It is recommended to integrate it in the following order.

### 1. Confirm that the scenario is a good fit

Suitable when:

- The target quantity is a discrete value;
- The current value can be read;
- Dragging to the maximum reveals the upper bound;
- Clickable increase/decrease buttons exist to compensate for errors.

Not suitable when:

- There is no readable number;
- There are no increase/decrease buttons as a fallback;
- The slider is not linearly quantized, or click position does not have a monotonic relationship with quantity.

### 2. Prepare the slider template

By default, `QuantizedSliding` uses the shared template node `QuantizedSlidingSwipeButton`, whose template path is:

```text
assets/resource/image/QuantizedSliding/SwipeButton.png
```

If the target screen uses a different slider-handle style, add a matching template resource or adjust the shared node first.

### 3. Calibrate the quantity OCR region

Fill `QuantityBox` with the region where the current quantity is displayed.

Note:

- You must use **1280×720** as the baseline resolution;
- The OCR node currently uses `expected: "\\d+"`, meaning it expects digits only;
- On the Go side, **all digit characters** are extracted from the OCR text and then converted to an integer.

That means:

- `Qty 12` is usually parsed as `12`;
- `12/99` is parsed as `1299`, not `12`;
- If OCR frequently misreads digits as letters, the whole action will fail.

So `QuantityBox` must not only “read digits,” but should also avoid including unrelated numeric groups whenever possible.

### 4. Choose how to locate the buttons

The recommended priority is:

1. **Template path**: most stable;
2. `[x, y, w, h]`: second best;
3. `[x, y]`: only use this when the button position is extremely stable.

### 5. Call it in the business task

See the actual usage currently in this repository.
The human-readable strings below are kept exactly as they appear in the current repo:

```json
"AutoStockpileSwipeSpecificQuantity": {
    "desc": "滑动到指定数值",
    "enabled": false,
    "pre_delay": 0,
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "QuantizedSliding",
            "custom_action_param": {
                "DecreaseButton": "AutoStockpile/DecreaseButton.png",
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "QuantityBox": [360, 490, 110, 70],
                "CenterPointOffset": [-10, 0],
                "Target": 1
            }
        }
    },
    "post_delay": 0,
    "rate_limit": 0,
    "next": ["AutoStockpileRelayNode"],
    "focus": {
        "Node.Action.Failed": "定量滑动失败，取消购买"
    }
}
```

File location: `assets/resource/pipeline/AutoStockpile/Task.json`

## Success and failure conditions

### Success conditions

- The slider start point can be recognized;
- It can be dragged to the maximum value successfully;
- OCR can read both the maximum value and the current value;
- The target value `Target` is not greater than the maximum value;
- After the proportional click and fine-tuning, the current value finally equals `Target`.

### Common failure conditions

- `QuantityBox` is not a 4-tuple `[x, y, w, h]`;
- `Direction` is not one of `left/right/up/down`;
- OCR does not read any digits;
- The maximum value `maxQuantity` is smaller than `Target`;
- The maximum value is less than or equal to `1`, so the ratio cannot be calculated;
- The increase/decrease buttons cannot be recognized or clicked;
- Too many fine-tuning attempts still do not converge.

The current implementation clamps a single fine-tuning branch to the range `0 ~ 30`, and `QuantizedSlidingCheckQuantity` has `max_hit = 4`. If those limits are exhausted and the target value is still not reached, the flow fails and enters `QuantizedSlidingFail`.

## Why fine-tuning buttons are still needed

At first glance, once the exact click position has been calculated from the start point, end point, and maximum value, it may seem like `+` / `-` buttons are unnecessary.

But in real interfaces, these error sources are common:

- The recognized slider template box is not the exact geometric center;
- The touchable area does not perfectly overlap with the visual position;
- Some quantity levels are not mapped uniformly;
- OCR or transition animations introduce slight bias after the click.

So the current implementation uses this approach:

> First use a proportional click to get close to the target, then finish with the increase/decrease buttons.

This is much faster than relying only on repeated button clicks, and much more stable than using only a single proportional click.

## Common pitfalls

- **Treating it like a single `Swipe` action**: it is essentially a complete internal flow, not just one `Swipe` step.
- **Setting `Direction` backwards**: this breaks the “swipe to max” step itself.
- **Including multiple number groups in `QuantityBox`**: for example, `12/99` is parsed as `1299`, not automatically treated as the first number only.
- **Making `QuantityBox` too tight**: OCR easily fails when digits move or outlines change.
- **Using only button coordinates without a recognition fallback**: small UI shifts can make clicks miss.
- **Assuming the slider template is universally reusable**: the shared template may fail if different screens use different slider styles.
- **Using a target value above the limit**: `Target > maxQuantity` fails immediately and does not automatically fall back to the maximum value.
- **Adding extra hard waits without thinking**: this shared flow already uses `post_wait_freezes`, so business integration should not stack many more hard delays on top.

## Self-checklist

After integration, check at least the following:

1. Whether the slider template `QuantizedSliding/SwipeButton.png` can be matched reliably.
2. Whether `QuantityBox` is based on **1280×720**, and OCR can read digits reliably.
3. Whether `Direction` matches the direction where the maximum value lies.
4. Whether `IncreaseButton` / `DecreaseButton` use template paths whenever possible.
5. Whether `Target` can exceed the maximum value allowed by the current scenario.
6. Whether the failure branch has a clear handling strategy, such as a prompt, skip, or canceling the current task.

## Code references

If you need to follow the implementation further, review in this order:

1. `agent/go-service/quantizedsliding/register.go`: confirm the registered action name.
2. `agent/go-service/quantizedsliding/handlers.go`: see how `Run()` distinguishes external invocation mode from internal node mode.
3. `agent/go-service/quantizedsliding/overrides.go`: see how internal Pipeline overrides, direction end regions, and button branches are generated.
4. `agent/go-service/quantizedsliding/ocr.go`: see OCR text and recognition box extraction logic.
5. `agent/go-service/quantizedsliding/normalize.go`: see button parameter normalization, click-repeat clamping, and center-point calculation.
6. `assets/resource/pipeline/QuantizedSliding/Main.json`: see default shared-node configuration such as `max_hit`, `post_wait_freezes`, and default `next` relationships.

## Related documents

- [Custom Action Reference Document](./custom-action.md): Learn the general calling convention of `Custom` actions.
- [Development Guide](./development.md): Learn the overall development conventions for Pipeline and Go Service.
