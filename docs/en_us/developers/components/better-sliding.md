# Development Manual - BetterSliding Reference Documentation

This CustomAction supports sliding a slider, allowing sliding to a specified value.

![BetterSliding Example](https://github.com/user-attachments/assets/27365f2c-b1a5-43cb-8ff6-d75d506716e2)

As shown in the image above, sliding can be performed using `SwipeButton`, and precise adjustments can be made using `DecreaseButton` and `IncreaseButton`.

> [!note]
> Some sliders hide when the number of slidable items is 1. Please handle this scenario appropriately.

## Swipe-Only Mode

Suitable for scenarios where you want to slide to the maximum/minimum. Parameters are as follows. For precise quantity control, please jump to the [Specified Quantity Mode](#specified-quantity-mode) section below.

### Parameter Description

| Field | Type | Required | Description |
| ---------------------- | -------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Direction` | `string` | Yes | Swipe direction. Supports `left` / `right` / `up` / `down`. |
| `SwipeButton` | `string` | No | Custom slider template path. Overrides the default template of the `BetterSlidingSwipeButton` node when provided. Default `""` (uses the shared default template `BetterSliding/SwipeButton.png`). |
| `ResetBeforeFindStart` | `bool` | No | When `true`, first swipes toward the minimum before matching the slider start position, then performs the swipe. Default `false`. |

> [!note]
> When matching the `SwipeButton` internally, the CustomAction sets `GreenMask` to `true`. For the green masking method, please refer to the default template.

### Example

```json
"SomeTaskSwipeToMax": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "SwipeButton": "BetterSliding/SwipeButton.png"
            }
        }
    }
}
```

## Specified Quantity Mode

> [!important]
> Before the CustomAction executes, ensure the slider is at its initial value, and that the initial value is 1. Otherwise, the position deviation of the slider between its minimum and maximum cannot be calculated, causing the quantity adjustment to fail. If the caller cannot guarantee that the slider starts at its initial value, set `ResetBeforeFindStart: true` so BetterSliding first swipes toward the minimum before matching the start position.

> [!note]
> When the resolved target quantity is strictly greater than 80% of the slider's max quantity, BetterSliding swipes toward the minimum once after recording the end position, before performing the proportional precise click, so values near the maximum end are set reliably from the minimum. When the target equals the max quantity, BetterSliding finishes directly without resetting. This behavior is enabled by default and requires no extra parameter.

### Parameter Description

#### Parameters that can be passed in `attach`

The following 5 fields are recommended to be passed via the calling node's `attach`. The `attach` priority is higher than the same-named fields in `custom_action_param`.

| Field | Type | Required | Description |
| ------------------------- | ------------------------ | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TargetQuantity` | `int` (positive integer) | Yes | Target quantity. The desired final slider value, which must be greater than 0. |
| `TargetQuantityType` | `string` | No | How to interpret `TargetQuantity`. `"Value"` (default): absolute count; `"Percentage"`: percentage of `availableQuantity` (1–100), rounded and clamped. |
| `ReverseTarget` | `bool` | No | When `true`, resolves the target from the available quantity: Value mode uses `availableQuantity - TargetQuantity`; Percentage mode uses the remaining percentage. Default `false`. |
| `FinishAfterPreciseClick` | `bool` | No | When `true`, returns success immediately after a precise click, without entering the quantity validation and fine-tuning process. Default `false`. |
| `ResetBeforeFindStart` | `bool` | No | When `true`, first swipes toward the minimum before matching the slider start position, so the recorded start position is the minimum value. Default `false`. |

> [!note]
> Combination calculation logic for `TargetQuantityType` and `ReverseTarget`:
>
> | TargetQuantityType | ReverseTarget | Effective target |
> | ------------------ | ------------- | ---------------------------------------------------------------------------------------------- |
> | `"Value"` | `false` | `TargetQuantity` |
> | `"Value"` | `true` | `availableQuantity - TargetQuantity` (not clamped, may be < 1) |
> | `"Percentage"` | `false` | `round(availableQuantity × TargetQuantity / 100)`, clamped to `[1, availableQuantity]` |
> | `"Percentage"` | `true` | `round(availableQuantity × (100 - TargetQuantity) / 100)`, clamped to `[1, availableQuantity]` |

#### Parameters that can only be passed via `custom_action_param`

In addition to the 5 fields above, all other parameters can only be read from `custom_action_param`:

| Field | Type | Required | Description |
| ------------------------------- | ----------------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Direction` | `string` | Yes | Swipe direction. Specifies "the direction of the maximum value", supports `left` / `right` / `up` / `down`. |
| `SliderQuantity.Box` | `int[4]` | Yes | OCR region for the current slider quantity, format `[x, y, w, h]`. |
| `IncreaseButton` | `string` or `int[2\|4]` | Yes | "Increase quantity" button. Template path is recommended (threshold fixed at `0.8`), or coordinates `[x, y]` or `[x, y, w, h]`. |
| `DecreaseButton` | `string` or `int[2\|4]` | Yes | "Decrease quantity" button. Format same as `IncreaseButton`. |
| `AvailableQuantity.Box` | `int[4]` | No | OCR region for reading the total available quantity. When missing, the slider endpoint value is used as the calculation reference. |
| `SliderQuantity.Filter` | `object` | No | Color filter parameters for the current slider quantity OCR. |
| `AvailableQuantity.Filter` | `object` | No | Color filter parameters for available-quantity OCR. Used only when `AvailableQuantity` is explicitly provided. |
| `SliderQuantity.OnlyRec` | `bool` | No | Whether to enable `only_rec` for slider-quantity OCR. Default `false`. |
| `AvailableQuantity.OnlyRec` | `bool` | No | Whether to enable `only_rec` for `BetterSlidingGetAvailableQuantity`. |
| `GreenMask` | `bool` | No | When locating buttons using a template path, whether to enable green mask filtering for template matching. Default `false`. Applies to `IncreaseButton` and `DecreaseButton`. |
| `CenterPointOffset` | `int[2]` | No | Click offset relative to the center point of the slider's recognition box `[x, y]`, negative values left/up, positive right/down. Default `[-10, 0]`. |
| `ClampTargetToSliderMax` | `bool` | No | When `true`, a target above `sliderMaxQuantity` is clamped to the maximum selectable slider quantity. Default `false`. |
| `SwipeButton` | `string` | No | Custom slider template path, overrides the default template of the `BetterSlidingSwipeButton` node. Default `""` (uses the shared default template). |
| `OutOfRangeOverrideEnable` | `string` | No | When the resolved target is outside the slidable range, enables the specified Pipeline node and returns success. Default `""`. |
| `TargetReachableOverrideEnable` | `string` | No | When the resolved target needs no clamping and falls within `[1, sliderMaxQuantity]`, enables the specified Pipeline node. Default `""`. |

### Outcome Node Contract

`OutOfRangeOverrideEnable` and `TargetReachableOverrideEnable` report the current BetterSliding outcome to the caller. They must reference different nodes, and each outcome node should default to `enabled: false`.

| Resolved target | `OutOfRangeOverrideEnable` | `TargetReachableOverrideEnable` | BetterSliding behavior |
| -------------------------------------------------------------------------------- | -------------------------- | ------------------------------- | -------------------------------------------------------------------- |
| Below 1, zero `sliderMaxQuantity`, or above `sliderMaxQuantity` without clamping | `true` | `false` | Returns success without adjustment; the caller handles the outcome |
| Within `[1, sliderMaxQuantity]` | `false` | `true` | Adjusts to the target quantity |
| Above `sliderMaxQuantity` with clamping enabled | `false` | `false` | Adjusts to `sliderMaxQuantity`; original target is not yet reachable |

`sliderMaxQuantity == 0` only means that no positive target is currently selectable. BetterSliding does not infer business causes such as insufficient balance, insufficient stock, or a disabled control. Callers that need to distinguish those states should recognize the corresponding UI in Pipeline.

> [!important]
> `TargetReachableOverrideEnable` only means that the caller's next operation can reach the target; it does not mean that operation has succeeded. Selling, purchasing, and similar flows must still confirm the outer transaction in Pipeline before recording the business target as completed.

### Example

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
                "SliderQuantity": {
                    "Box": [340, 430, 200, 140]
                }
            }
        }
    },
    "attach": {
        "TargetQuantity": 50,
        "TargetQuantityType": "Percentage",
        "ReverseTarget": false
    }
}
```
