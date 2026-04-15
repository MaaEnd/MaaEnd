# Development Guide - BetterSliding Reference Document

`BetterSliding` is a go-service custom action invoked through the `Custom` action type. It handles interfaces where you drag a slider to choose a quantity, but the target value is a discrete level rather than a continuous value.

It is suitable for scenarios like these:

- You first drag to an approximate position, then fine-tune the quantity with `+` / `-` buttons.
- The slider itself does not have stable fixed coordinates, but the slider handle template can be recognized.
- The current quantity can be read by OCR, and the maximum value on the screen changes with stock or other conditions.

The current implementation is located at:

- Go action package: `agent/go-service/bettersliding/`
- Package-local registration: `agent/go-service/bettersliding/register.go`
- go-service global registration entry: `agent/go-service/register.go`
- Shared Pipeline: `assets/resource/pipeline/BetterSliding/Main.json` and `Helper.json`
- Test Pipeline: `assets/resource/pipeline/BetterSliding/Test.json`
- Existing integration example: `AutoStockpileSwipeSpecificQuantity` in `assets/resource/pipeline/AutoStockpile/Purchase.json`

`agent/go-service/bettersliding/` is now split by responsibility:

| File           | Responsibility                                                         |
| -------------- | ---------------------------------------------------------------------- |
| `types.go`     | Parameter structs, action type, runtime state, and package-level types |
| `params.go`    | Parameter parsing and normalization                                    |
| `nodes.go`     | Shared action name, internal node names, and override key constants    |
| `handlers.go`  | `Run()` dispatch, per-stage handlers, and state reset                  |
| `overrides.go` | Pipeline override construction                                         |
| `ocr.go`       | Typed-first recognition helpers for hit box and quantity reads         |
| `normalize.go` | Button parameter normalization and basic calculation helpers           |
| `register.go`  | Registers the `BetterSliding` action into go-service                   |

## Test Pipeline: `Test.json`

`assets/resource/pipeline/BetterSliding/Test.json` is the manual regression test suite for `BetterSliding`. After changing `agent/go-service/bettersliding/`, `BetterSliding/Main.json`, `Helper.json`, or related parameter-parsing logic, you should run this test task at least once manually.

Its current entry node is `BetterSlidingTest`. The file itself says to run it in the **Outpost management** screen, with the currently tradable quantity roughly in the **1k to 3k** range. That range is large enough to exercise normal targets, percentage-based targets, and out-of-range fallback behavior.

Its high-level structure is:

- `BetterSlidingTest`: test entry;
- `__BS-T-2`: reset step that drags the slider back to the left, so each case starts from a more consistent state;
- `__BS-T-1`: dispatcher that fans out from the reset state into each test case;
- `__BS-T-3` / `__BS-T-4` / `__BS-T-5`: result markers for exit, expected out-of-range routing success, and unexpected routing failure.

The built-in test scenarios currently cover:

| Node     | Purpose                                                                                                                    |
| -------- | -------------------------------------------------------------------------------------------------------------------------- |
| `__BS-1` | Normal Value-mode target with `Target = 325`                                                                               |
| `__BS-2` | `TargetReverse: true`, validating reverse-style targets such as “keep 325”                                                 |
| `__BS-3` | `TargetType: "Percentage"`, validating a `10%` target                                                                      |
| `__BS-4` | `Percentage + TargetReverse`, validating “keep 10%”                                                                        |
| `__BS-5` | `FinishAfterPreciseClick: true`, validating the path that ends right after the precise click                               |
| `__BS-6` | Oversized target `10000` + `ExceedingOverrideEnable`, validating fallback routing when the resolved target is out of range |

In practice, this `Test.json` checks that the following behaviors still work together:

- the base flow of reset, swipe-to-max, and end-position recognition;
- the three target interpretation paths: Value, Percentage, and Reverse;
- both completion paths: fine-tune after the precise click, or finish immediately;
- expected branch routing through `ExceedingOverrideEnable` when the target exceeds the reachable range.

If you add new parameter semantics or new branching behavior to `BetterSliding`, add a matching case to `Test.json` and extend this section as well, so the documentation and regression coverage stay aligned.

## Execution modes

`BetterSliding` currently has two execution modes:

1. **External invocation mode**: when a business task calls it with `custom_action: "BetterSliding"`, the Go side automatically constructs the internal Pipeline override and starts running the full internal flow from `BetterSlidingMain` through its downstream nodes.
2. **Internal node mode**: when the current node itself is one of `BetterSlidingMain`, `BetterSlidingFindStart`, `BetterSlidingGetMaxQuantity`, `BetterSlidingFindEnd`, `BetterSlidingCheckQuantity`, or `BetterSlidingDone`, the Go side directly handles that specific stage.

The business-side caller usually only needs to pass `custom_action_param` once and does **not** need to manually chain the internal nodes.

## How it works

`BetterSliding` does not simply "swipe to a fixed percentage." Instead, it uses a **detect, calculate, then fine-tune** flow.

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

The computed `[clickX, clickY]` is then dynamically written into `BetterSlidingPreciseClick.action.param.target`.

## How to call it

In a business Pipeline, call it like a normal `Custom` action. The example below uses MaaFramework Pipeline protocol v2 syntax.

```json
"SomeTaskAdjustQuantity": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "GreenMask": false,
                "Target": 1,
                "Quantity": {
                    "Box": [360, 490, 110, 70],
                    "OnlyRec": false
                },
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "DecreaseButton": "AutoStockpile/DecreaseButton.png",
                "CenterPointOffset": [-10, 0]
            }
        }
    }
}
```

## Swipe-Only Mode

If you only need to drag the slider to its maximum position without reading any quantity or fine-tuning, you can use **swipe-only mode**.

Swipe-only mode is activated automatically when `custom_action_param` contains **only `Direction` (required)** and an optional `SwipeButton`, with **no other parameters** present.

In this mode, `BetterSliding` performs the `SwipeToMax` drag and returns success immediately, skipping OCR, proportional clicking, and fine-tuning entirely. `Direction` is required and specifies which side corresponds to the maximum value, while `SwipeButton` is still respected — you can supply a custom slider template path even in swipe-only mode.

> **Note:** In external call mode, `attach.Target`, `attach.TargetType`, `attach.TargetReverse`, and `attach.FinishAfterPreciseClick` are merged into `custom_action_param` **before** swipe-only mode is evaluated. If `attach` supplies `Target`, `TargetType`, or `TargetReverse`, the action will not enter swipe-only mode even when `custom_action_param` itself contains only `Direction` and optional `SwipeButton`. `FinishAfterPreciseClick` is also merged, but it does **not** affect swipe-only mode detection.

Minimal example:

```json
"SomeTaskSwipeToMax": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right"
            }
        }
    }
}
```

With a custom slider template:

```json
"SomeTaskSwipeToMax": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "SwipeButton": "MyFeature/MySlider.png"
            }
        }
    }
}
```

## Parameter description

`BetterSliding` parameters can be divided into two groups:

1. **Parameters that can be passed through the caller node's `attach`**: useful when one shared `custom_action_param` needs per-node runtime overrides.
2. **Parameters that are only read from `custom_action_param`**: these are part of the action's own configuration and are not read from `attach`.

### Parameters that can be passed through `attach`

The following 4 fields can be written either in `custom_action_param` or overridden by the caller node's `attach`; in external invocation mode, `attach` has higher priority.

| Field                     | Type             | Required | Description                                                                                                                                                                                                                                                                                  |
| ------------------------- | ---------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Target`                  | `int` (positive) | Yes\*    | The target quantity. The final discrete value you want to reach. Must be greater than 0 in normal mode. Ignored in swipe-only mode. If the target needs to vary by caller node, prefer passing it through `attach.Target`.                                                                   |
| `TargetType`              | `string`         | No       | How to interpret `Target`. `"Value"` (default): absolute discrete count. `"Percentage"`: percentage (1–100) of `maxQuantity`, rounded and clamped to `[1, maxQuantity]`. If one shared node setup needs different target semantics by caller, prefer passing it through `attach.TargetType`. |
| `TargetReverse`           | `bool`           | No       | When `true`, reverses the target: `maxQuantity - target` (Value mode) or `round(maxQuantity * (100 - target) / 100)` (Percentage mode). Default: `false`. If reverse behavior depends on the caller, prefer passing it through `attach.TargetReverse`.                                       |
| `FinishAfterPreciseClick` | `bool`           | No       | When `true`, returns success immediately after the precise click, skipping quantity verification and fine-tuning. Default: `false`. If whether to skip fine-tuning depends on the caller, prefer passing it through `attach.FinishAfterPreciseClick`.                                        |

### Parameters that can only be passed through `custom_action_param`

Other than the 4 fields above, all remaining parameters are currently read only from `custom_action_param`:

| Field                     | Type                    | Required | Description                                                                                                                                                                                                                                                                             |
| ------------------------- | ----------------------- | -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GreenMask`               | `bool`                  | No       | Whether to enable green mask filtering for template matching when locating buttons via template paths. Default: `false`.                                                                                                                                                                |
| `Quantity.Box`            | `int[4]`                | Yes\*    | OCR region for the current quantity. The format must be `[x, y, w, h]`. Ignored in swipe-only mode.                                                                                                                                                                                     |
| `QuantityFilter`          | `object`                | No       | Optional color filtering for quantity OCR, useful when digit color is stable but the background is noisy.                                                                                                                                                                               |
| `Quantity.OnlyRec`        | `bool`                  | No       | Whether to enable `only_rec` for the quantity OCR node. The current default is `false`; if provided explicitly, the passed value takes precedence. The Go side still reads quantity text only from `Results.Best.AsOCR().Text`.                                                         |
| `Direction`               | `string`                | Yes      | Drag direction. Supports `left` / `right` / `up` / `down`. The Go side trims surrounding whitespace and lowercases it before validation.                                                                                                                                                |
| `IncreaseButton`          | `string` or `int[2\|4]` | Yes\*    | The "increase quantity" button. Can be a template path or coordinates. Ignored in swipe-only mode.                                                                                                                                                                                      |
| `DecreaseButton`          | `string` or `int[2\|4]` | Yes\*    | The "decrease quantity" button. Can be a template path or coordinates. Ignored in swipe-only mode.                                                                                                                                                                                      |
| `CenterPointOffset`       | `int[2]`                | No       | Click offset relative to the slider handle center, default `[-10, 0]`.                                                                                                                                                                                                                  |
| `ClampTargetToMax`        | `bool`                  | No       | If `true`, when the target exceeds the recognized `maxQuantity`, the target is clamped to `maxQuantity` and the action continues instead of failing. Default: `false` (fail immediately).                                                                                               |
| `SwipeButton`             | `string`                | No       | Custom slider template path. When provided, overrides the `BetterSlidingSwipeButton` node's default template. Path is relative to the `resource/image/` directory. Default: `""` (use the shared default template).                                                                     |
| `ExceedingOverrideEnable` | `string`                | No       | When the resolved target is out of the slidable range, sets the `enabled` field of the named Pipeline node to `true`, then returns success. Useful for triggering a fallback branch when the target cannot be reached. Default: `""` (disabled — the action fails immediately instead). |

\* Required in normal mode; ignored in swipe-only mode.

`CenterPointOffset` is used to fine-tune the final click position for `BetterSlidingPreciseClick`. Its format must be `[x, y]`:

- `x` is the horizontal offset. Negative moves left, positive moves right.
- `y` is the vertical offset. Negative moves up, positive moves down.
- If omitted, the default is `[-10, 0]`, which means clicking 10 pixels to the left of the slider center.

### `QuantityFilter`

`QuantityFilter` is an **optional enhancement**. If omitted, it only means color-filter preprocessing is disabled for `BetterSlidingGetQuantity`; if provided, that OCR result is color-filtered before reading digits.

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
- The channel count must match the `method`: `4` (RGB) and `40` (HSV) require 3 channels, `6` (GRAY) requires 1 channel;
- Only a **single** color range is supported for now; `[[...], [...]]` multi-range input is not supported;
- You can treat it as an approximate color-based binarization step for the quantity area before OCR;
- If the interfering digits use exactly the same color as the target digits, `QuantityFilter` cannot fundamentally separate them, so tightening `Quantity.Box` is still the first choice;
- `QuantityFilter` improves OCR preprocessing, but it is not a substitute for an inaccurate `Quantity.Box`.

### `IncreaseButton` / `DecreaseButton` formats

These two fields support two forms:

#### 1. Pass a template path (recommended)

```json
"IncreaseButton": "AutoStockpile/IncreaseButton.png"
```

In this case, go-service dynamically rewrites the corresponding branch node to `TemplateMatch + Click`:

- The template threshold is fixed at `0.8`
- The top-level parameter `GreenMask` defaults to `false`, and is mapped to the TemplateMatch protocol field `green_mask`
- The click uses `target: true` and includes `target_offset: [5, 5, -10, -10]`

This is usually more stable than hardcoded coordinates, so it is the preferred option.

#### 2. Pass coordinates

Supported formats:

- `[x, y]`
- `[x, y, w, h]`

If `[x, y]` is passed, it will be automatically normalized to `[x, y, 1, 1]` internally.

Also note that after JSON deserialization on the Go side, these arrays may appear as `[]float64` or `[]any`. The current implementation normalizes them into integer arrays automatically. However, if the length is neither `2` nor `4`, the action fails immediately.

## Attach Parameters

`BetterSliding` currently reads these 4 fields from the caller node's `attach` block: `Target`, `TargetType`, `TargetReverse`, and `FinishAfterPreciseClick`.

For these 4 fields, **prefer passing them through `attach`** instead of hard-coding them directly in `custom_action_param`. This is useful because:

- one shared `BetterSliding` parameter set can be reused by multiple caller nodes, with only the target-related fields changed in `attach`;
- changing the target value, target interpretation, or reverse behavior does not require duplicating the entire `custom_action_param` block;
- it matches the current runtime override behavior, so maintenance is clearer: you can tell what each caller is actually trying to reach just by reading its `attach` block.

The current implementation applies these values in this order:

1. `runInternalPipeline` reads the caller node's `attach` block;
2. if `attach` contains `Target`, `TargetType`, `TargetReverse`, or `FinishAfterPreciseClick`, those values overwrite the corresponding keys in `custom_action_param`;
3. the merged result is then parsed as the final `BetterSliding` parameter set.

In other words, in **external call mode** (that is, when a business node calls `custom_action: "BetterSliding"`), these 4 fields in `attach` take priority over fields with the same names in `custom_action_param`.

Notes:

- only these 4 keys are read; any other `attach` fields are ignored;
- this applies only to external call mode; if the current node is already `BetterSlidingMain` or another internal BetterSliding node, the Go side does not merge `attach` again;
- if `attach` is missing, the node JSON cannot be read, or one of these fields has an invalid type, that field falls back to the original value from `custom_action_param`.

### `TargetType`

| Value          | Behavior                                                                                                                 |
| -------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `"Value"`      | (default) `Target` is an absolute discrete count.                                                                        |
| `"Percentage"` | `Target` is a percentage (1–100). The Go side computes `round(max * t/100)` and clamps the result to `[1, maxQuantity]`. |

### `TargetReverse`

When `true`, the target is computed from the **far end** of the range:

- `Value` mode: effective target = `maxQuantity - Target`
- `Percentage` mode: effective target = `round(maxQuantity * (100 - Target) / 100)`, clamped to `[1, maxQuantity]`

For `Value + TargetReverse`, the computed result is **not** clamped — it may be less than `1`. In that case the action fails unless `ExceedingOverrideEnable` is set (see below).

### `FinishAfterPreciseClick`

When `true`, `BetterSliding` returns success immediately after the precise click, skipping `BetterSlidingCheckQuantity` verification and `BetterSlidingIncreaseQuantity` / `BetterSlidingDecreaseQuantity` fine-tuning entirely.

**Difference from `SwipeOnlyMode`:**

- `SwipeOnlyMode`: skips proportional clicking and fine-tuning entirely, only dragging to the maximum position. Suitable for scenarios where you only need to "swipe to the end."
- `FinishAfterPreciseClick`: still performs the proportional click, but skips subsequent OCR verification and fine-tuning. Suitable for scenarios where "positional deviation is acceptable; just click to the approximate position."

**Notes:**

- When `FinishAfterPreciseClick` is enabled, the final actual quantity is **not guaranteed** to match `Target` exactly.
- `ExceedingOverrideEnable` and `ClampTargetToMax` are still evaluated before the precise click and are unaffected by this parameter.

### Attach example

```json
"SomeTaskAdjustQuantity": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "DecreaseButton": "AutoStockpile/DecreaseButton.png",
                "Quantity": {
                    "Box": [340, 430, 200, 140],
                    "OnlyRec": true
                },
                "QuantityFilter": {
                    "lower": [20, 150, 150],
                    "upper": [35, 255, 255],
                    "method": 40
                }
            }
        }
    },
    "attach": {
        "Target": 50,
        "TargetType": "Percentage",
        "TargetReverse": false
    }
}
```

In the example above, `Target` is read from `attach` and injected into the top-level `Target` field before parsing, so the slider targets 50% of the current maximum.

Here is an example with `FinishAfterPreciseClick` passed through `attach`:

```json
"SomeTaskClickAndLeave": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "DecreaseButton": "AutoStockpile/DecreaseButton.png",
                "Quantity": {
                    "Box": [340, 430, 200, 140],
                    "OnlyRec": true
                }
            }
        }
    },
    "attach": {
        "Target": 799,
        "FinishAfterPreciseClick": true
    }
}
```

In this example, `FinishAfterPreciseClick` is read from `attach` and injected, so the slider clicks to make the target quantity close to 799 and then returns success immediately, without verifying the actual quantity or performing fine-tuning.

If a business node always uses one fixed target value, target type, and reverse setting, keeping them directly in `custom_action_param` is also fine. But as soon as those values need to vary by caller while reusing the same node setup, `attach` should be the preferred approach.

## `ExceedingOverrideEnable`

When the resolved target is out of the slidable range, this parameter determines what happens instead of failing.

### Out-of-range conditions

- `resolved target > maxQuantity` — always out of range.
- `TargetType = "Value"` and `TargetReverse = true` and `maxQuantity - target < 1` — the computed value is negative or zero, treated as out of range.

### Priority over `ClampTargetToMax`

`ExceedingOverrideEnable` is evaluated **before** `ClampTargetToMax`. When `ExceedingOverrideEnable` is set:

1. If out of range: the named Pipeline node's `enabled` is set to `true`, the flow branches directly to `BetterSlidingDone`, and `BetterSliding` returns **success**.
2. If in range: the named Pipeline node's `enabled` is set to `false`, and the normal flow continues (including any `ClampTargetToMax` logic).

When `ExceedingOverrideEnable` is **not** set and the target is out of range (including `Value + TargetReverse` yielding `< 1`), the action returns **false** immediately and `ClampTargetToMax` is not applied.

### Example

```json
"SomeTaskAdjustWithFallback": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "ExceedingOverrideEnable": "SomeFallbackNode",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "Target": 1,
                "Quantity": {
                    "Box": [340, 430, 200, 140],
                    "OnlyRec": true
                },
                "QuantityFilter": {
                    "lower": [20, 150, 150],
                    "upper": [35, 255, 255],
                    "method": 40
                }
            }
        }
    },
    "post_delay": 0,
    "rate_limit": 0,
    "next": ["AutoStockpileRelayNodeSwipe"],
    "focus": {
        "Node.Action.Failed": "定量滑动失败，取消购买"
    }
}
```

File location: `assets/resource/pipeline/AutoStockpile/Purchase.json` (node: `AutoStockpileSwipeSpecificQuantity`)

## Success and failure conditions

### Success conditions

- The slider start point can be recognized;
- It can be dragged to the maximum value successfully;
- OCR can read both the maximum value and the current value;
- The target value `Target` is not greater than the maximum value, or `ClampTargetToMax` is `true` (in which case the target value is clamped to `maxQuantity`);
- If the recognized `maxQuantity` is `1` and the final target is also `1` (including the case after `ClampTargetToMax`), the flow branches directly to success without proportional clicking;
- If `FinishAfterPreciseClick` is `true`, the proportional click is performed and success is returned immediately, without verifying whether the actual quantity matches the target;
- If `FinishAfterPreciseClick` is `false` (default), after the proportional click and fine-tuning, the current value finally equals `Target`.

### Common failure conditions

- `Quantity.Box` is not a 4-tuple `[x, y, w, h]`;
- `Direction` is not one of `left/right/up/down`;
- OCR does not read any digits;
- The maximum value `maxQuantity` is smaller than `Target`, and `ClampTargetToMax` is `false` (default); this also covers the case where the maximum is only `1` but the target still remains greater than `1`;
- The increase/decrease buttons cannot be recognized or clicked;
- Too many fine-tuning attempts still do not converge.

The current implementation clamps a single fine-tuning branch to the range `0 ~ 30`, and `BetterSlidingCheckQuantity` has `max_hit = 4`. If those limits are exhausted and the target value is still not reached, the flow fails and enters `BetterSlidingFail`.

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
- **Setting `Direction` backwards**: this breaks the "swipe to max" step itself.
- **Including multiple number groups in `Quantity.Box`**: for example, `12/99` is parsed as `1299`, not automatically treated as the first number only.
- **Making `Quantity.Box` too tight**: OCR easily fails when digits move or outlines change.
- **Using only button coordinates without a recognition fallback**: small UI shifts can make clicks miss.
- **Assuming the slider template is universally reusable**: the shared template may fail if different screens use different slider styles.
- **Using a target value above the limit**: `Target > maxQuantity` fails immediately by default. Set `ClampTargetToMax: true` to automatically clamp to the maximum value instead of failing, but note that the actual final quantity will be `maxQuantity`, not the original `Target`.
- **Adding redundant hard waits**: avoid stacking many hard delays on top of the internal flow; rely on the built-in rate limiting and max_hit mechanisms instead.

## Self-checklist

After integration, check at least the following:

1. Whether the slider template `BetterSliding/SwipeButton.png` can be matched reliably.
2. Whether `Quantity.Box` is based on **1280×720**, and OCR can read digits reliably.
3. Whether `Direction` matches the direction where the maximum value lies.
4. Whether `IncreaseButton` / `DecreaseButton` use template paths whenever possible.
5. Whether `Target` can exceed the maximum value allowed by the current scenario.
6. If `ClampTargetToMax` is enabled, whether the caller can handle the case where the actual final quantity may be less than the original `Target`.
7. Whether the failure branch has a clear handling strategy, such as a prompt, skip, or canceling the current task.

## Code references

If you need to follow the implementation further, review in this order:

1. `agent/go-service/bettersliding/register.go`: confirm the registered action name.
2. `agent/go-service/bettersliding/handlers.go`: see how `Run()` distinguishes external invocation mode from internal node mode.
3. `agent/go-service/bettersliding/nodes.go`: see the shared action name, internal node names, and override keys.
4. `agent/go-service/bettersliding/params.go`: see parameter parsing and normalization.
5. `agent/go-service/bettersliding/overrides.go`: see how internal Pipeline overrides, direction end regions, and button branches are generated.
6. `agent/go-service/bettersliding/ocr.go`: see typed-first quantity and hit box extraction logic.
7. `agent/go-service/bettersliding/normalize.go`: see button parameter normalization, click-repeat clamping, and center-point calculation.
8. `assets/resource/pipeline/BetterSliding/Main.json`: see default shared-node configuration such as `max_hit`, `pre_delay`, `post_delay`, `rate_limit`, and default `next` relationships.
9. `assets/resource/pipeline/BetterSliding/Helper.json`: see basic recognition node configuration.

## Related documents

- [Custom Action and Recognition Reference](../custom.md): Learn the general calling convention of `Custom` actions and recognitions.
- [Coding standards](../coding-standards.md): Pipeline, Go, and resource conventions.
- [Developer documentation index](../README.md): Reading order and links to tools, testing, and task docs.
