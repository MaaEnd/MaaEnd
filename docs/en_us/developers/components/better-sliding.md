# Development Manual - BetterSliding Reference Documentation

This CustomAction supports sliding a slider, allowing sliding to a specified value.

![BetterSliding Example](https://github.com/user-attachments/assets/cad74409-911e-43aa-81ba-3d540e2bf6d9)

As shown in the image above, sliding can be performed using `SwipeButton`, and precise adjustments can be made using `DecreaseButton` and `IncreaseButton`.

> [!note]
> Some sliders hide when the number of slidable items is 1. Please handle this scenario appropriately.

## Swipe-Only Mode

Suitable for scenarios where you want to slide to the maximum/minimum. Only the parameters below can be passed. Swipe-only mode is inferred from the parameters: once any specified-quantity-mode field is passed, the call is validated as specified-quantity mode. For precise quantity control, please jump to the [Specified Quantity Mode](#specified-quantity-mode) section below.

### Parameter Description

| Field | Type | Required | Description |
| ---------------------- | -------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Direction` | `string` | Yes | Swipe direction. Supports `left` / `right` / `up` / `down`. |
| `SwipeButton` | `string` or `object` | No | Slider template recognition parameters. Overrides the recognition parameters of the `BetterSlidingSwipeButton` node when provided. Default: not configured (uses the shared default template `BetterSliding/SwipeButton.png`). See [Recognition Parameters: String vs Object](#recognition-parameters-string-vs-object). |
| `ResetBeforeFindStart` | `bool` | No | When `true`, first swipes toward the minimum before matching the slider start position, then performs the swipe. Default `false`. |

> [!note]
> When `SwipeButton` uses template matching, the green mask is enabled by default (`green_mask: true`). You can override it by writing `"green_mask": false` in the patch. Prepare your template images following the default template's green masking method (paint non-matching regions green, RGB: (0, 255, 0)).

### Example

```json
"SomeTaskSwipeToMax": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "SwipeButton": { "template": "BetterSliding/SwipeButton.png" }
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

The following 6 fields are recommended to be passed via the calling node's `attach`. The `attach` priority is higher than the same-named fields in `custom_action_param`.

| Field | Type | Required | Description |
| ------------------------- | ------------------------ | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TargetQuantity` | `int` (positive integer) | Yes | Target quantity. The desired final slider value, which must be greater than 0. |
| `TargetQuantityType` | `string` | No | How to interpret `TargetQuantity`. `"Value"` (default): absolute count; `"Percentage"`: percentage of `availableQuantity` (1–100), rounded and clamped to `[1, availableQuantity]`. |
| `ReverseTarget` | `bool` | No | When `true`, resolves the target from the available quantity: Value mode uses `availableQuantity - TargetQuantity`; Percentage mode uses the remaining percentage. Default `false`. |
| `FineTuneQuantity` | `bool` or `int` | No | Whether to keep fine-tuning via Increase/Decrease after the precise click. `true` (default): always fine-tune; `false`: never fine-tune; integer `N` (must be `>= 1`): fine-tune only when `abs(current - target) <= N`. |
| `FineTuneFallback` | `string` | No | Takes effect only when this run decides not to fine-tune. `"none"` (default): no compensation, finish directly; `"more"` / `"less"`: compensate toward increasing/decreasing the quantity and re-check. See [No-Fine-Tune Semantics](#no-fine-tune-semantics). |
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

In addition to the 6 fields above, all other parameters can only be read from `custom_action_param`:

| Field | Type | Required | Description |
| ------------------------------- | ----------------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Direction` | `string` | Yes | Swipe direction. Specifies "the direction of the maximum value", supports `left` / `right` / `up` / `down`. |
| `IncreaseButton` | `int[2\|4]` or `string` or `object` | Yes | "Increase quantity" button. Coordinates `[x, y]` / `[x, y, w, h]` are clicked directly; a String / Object is used as [recognition parameters](#recognition-parameters-string-vs-object) for template matching (default threshold `0.8` and `green_mask: true`), and the matched box is clicked. |
| `DecreaseButton` | `int[2\|4]` or `string` or `object` | Yes | "Decrease quantity" button. Format same as `IncreaseButton`. |
| `SwipeButton` | `string` or `object` | No | Slider recognition parameters, overriding the `BetterSlidingSwipeButton` node. Default: not configured (uses the shared default template). |
| `SliderQuantity` | `string` or `object` | Yes | OCR recognition parameters for the current slider quantity, overriding the `BetterSlidingGetSliderQuantity` node; for example `{"roi": [x, y, w, h], "only_rec": true}`. |
| `SliderQuantityFilter` | `string` or `object` | No | ColorMatch recognition parameters used to preprocess the slider-quantity OCR, overriding the `BetterSlidingSliderQuantityFilter` node and linked to the quantity node via `color_filter`. Default: not configured. |
| `AvailableQuantity` | `string` or `object` | No | OCR recognition parameters for the total available quantity, overriding and enabling `BetterSlidingGetAvailableQuantity`. The slider endpoint value is used as the calculation reference only when `AvailableQuantity` is not provided at all (or is `null`). |
| `AvailableQuantityFilter` | `string` or `object` | No | ColorMatch recognition parameters used to preprocess the available-quantity OCR, overriding the `BetterSlidingAvailableQuantityFilter` node. Default: not configured. |
| `CenterPointOffset` | `int[2]` | No | Click offset relative to the center point of the slider's recognition box `[x, y]`, negative values left/up, positive right/down. Default `[-10, 0]`. |
| `ClampTargetToSliderMax` | `bool` | No | When `true`, a target above `sliderMaxQuantity` is clamped to the maximum selectable slider quantity. Default `false`. |
| `OutOfRangeOverrideEnable` | `string` | No | When the resolved target is outside the slidable range, enables the specified Pipeline node and returns success; when the field is unset (default `""`), the action fails directly. |
| `TargetReachableOverrideEnable` | `string` | No | When the resolved target needs no clamping and falls within `[1, sliderMaxQuantity]`, enables the specified Pipeline node. Default `""`. |

### Recognition Parameters: String vs Object

`SwipeButton`, `SliderQuantity`, `SliderQuantityFilter`, `AvailableQuantity`, `AvailableQuantityFilter`, and the template form of `IncreaseButton` / `DecreaseButton` all accept two forms:

| Form | Meaning |
| -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `string` | **Node reference.** Reads that node's `recognition.param` as the recognition patch (no suffix detection; every string is resolved as a node name). |
| `object` | **Recognition patch.** The object content is the target node's `recognition.param` key/value set. |

Example:

```jsonc
"custom_action_param": {
    "Direction": "right",
    // String: reference an existing OCR / template node to reuse its recognition parameters
    "SliderQuantity": "SomeExistingQuantityOCRNode",
    // Object: provide the recognition patch directly
    "AvailableQuantity": { "roi": [1073, 327, 119, 25], "only_rec": true },
    "SliderQuantityFilter": { "method": 4, "lower": [75, 75, 75], "upper": [255, 255, 255] },
    "IncreaseButton": { "template": "AutoStockpile/IncreaseButton.png" },
    "DecreaseButton": [965, 570, 20, 10]
}
```

Constraints and behavior:

- **Recognition type cannot be replaced**: a patch only writes `recognition.param` and never writes `type`, so the target node keeps its original recognition algorithm and any unmentioned fields. If an Object contains `recognition` / `type` / `action` keys, the action fails immediately with an error instead of silently ignoring them.
- **Template-form buttons**: when `IncreaseButton` / `DecreaseButton` receive a String / Object, the template parameters are written to the `BetterSlidingIncreaseButton` / `BetterSlidingDecreaseButton` nodes, and the quantity node references those with `And all_of` and clicks the matched box. The patch defaults to `green_mask: true`, which can be overridden explicitly.
- **Arrays are button-only**: passing an array for any parameter other than `IncreaseButton` / `DecreaseButton` fails immediately; a button array is coordinates, not a recognition patch.
- **Filter and `color_filter` precedence**: `SliderQuantityFilter` / `AvailableQuantityFilter` are written to the corresponding built-in filter node and their node name is put into the quantity patch's `color_filter`; if the quantity patch already declares `color_filter`, the patch wins. No `color_filter` is written when the filter is not configured.
- **A Filter's String reference should point to a ColorMatch node**: `color_filter` is resolved during recognition by node name and requires the node's recognition type to be ColorMatch; pointing at another type makes recognition fail. This is not validated at the parameter level, so make sure the referenced type is correct.
- **`AvailableQuantityFilter` works standalone**: when `AvailableQuantity` is not provided at the same time, the filter parameters are still written to the built-in node, but `BetterSlidingGetAvailableQuantity` stays `enabled: false` (no warning).
- **Quantity-mode criteria**: the presence of any filter parameter makes this a quantity-mode run (leaving swipe-only mode).

### Minimum-Value Short Circuit

When `TargetQuantityType` is `"Value"` (case-insensitive), `TargetQuantity` is `1`, and `ReverseTarget` is `false`, the target is the slider's minimum value and BetterSliding takes a short-circuit path that skips the slider-maximum OCR, end-point recognition, and precise click:

| `ResetBeforeFindStart` | Behavior |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `true` | Performs a single swipe toward the minimum (`BetterSlidingFindSwipeForReset` → `BetterSlidingReset`) and finishes right after the reset. |
| `false` (default) | Performs no recognition, swipe, or click and returns success directly. **The caller must ensure the slider is already at its initial value 1.** |

> [!note]
> `Percentage` mode and `ReverseTarget: true` do not short-circuit: their effective target depends on the `availableQuantity` read at runtime, so it cannot be decided before entering the flow.

### No-Fine-Tune Semantics

`FineTuneFallback` only takes effect when **this run decides not to fine-tune**: with `FineTuneQuantity: false` it is always "no fine-tune"; with an integer threshold `N`, fine-tuning is used only when the difference between the current quantity and the target quantity is not greater than `N`, otherwise the run is "no fine-tune". In that case BetterSliding no longer approaches the target step by step; instead `FineTuneFallback` decides how to finish:

| Value | Behavior |
| --- | --- |
| `"none"` (default) | No compensation at all; this adjustment ends here. |
| `"more"` | When the current quantity is less than the target quantity, compensates toward **increasing** the quantity and re-checks; when the current quantity is not less than the target quantity, no compensation and it finishes directly. |
| `"less"` | When the current quantity is greater than the target quantity, compensates toward **decreasing** the quantity and re-checks; when the current quantity is not greater than the target quantity, no compensation and it finishes directly. |

### Outcome Node Contract

`OutOfRangeOverrideEnable` and `TargetReachableOverrideEnable` report the current BetterSliding outcome to the caller: at most one node is enabled per outcome (the other, when configured, is set to `enabled: false`). They must reference different nodes, and each outcome node should default to `enabled: false`.

| Resolved target | Override node | BetterSliding behavior |
| --- | --- | --- |
| Below 1, zero `sliderMaxQuantity`, or above `sliderMaxQuantity` without clamping | `OutOfRangeOverrideEnable` | No adjustment and returns success; fails directly when the field is unset |
| Within `[1, sliderMaxQuantity]` | `TargetReachableOverrideEnable` | Adjusts to the target quantity |
| Above `sliderMaxQuantity` with clamping enabled | None | Adjusts to `sliderMaxQuantity`; original target is not yet reachable |

`sliderMaxQuantity == 0` only means that no positive target is currently selectable. BetterSliding does not infer business causes such as insufficient balance, insufficient stock, or a disabled control. Callers that need to distinguish those states should recognize the corresponding UI in Pipeline.

> [!note]
> On a minimum-value short circuit, BetterSliding does not read `sliderMaxQuantity`, but it still enables the node referenced by `TargetReachableOverrideEnable` (the target is the slider minimum and therefore always reachable), while `OutOfRangeOverrideEnable` stays `false`.

> [!important]
> `TargetReachableOverrideEnable` only means that the **resolved target is reachable**; it is unrelated to the final adjustment result. The decision is made when the target quantity and slider maximum are read, and is not changed afterwards regardless of whether fine-tuning or nudging actually hits the target. It only means that the caller's next operation can reach the target; it does not mean that operation has succeeded. Selling, purchasing, and similar flows must still confirm the outer transaction in Pipeline before recording the business target as completed.

### Example

```json
"SomeTaskAdjustQuantity": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "IncreaseButton": { "template": "AutoStockpile/IncreaseButton.png" },
                "DecreaseButton": { "template": "AutoStockpile/DecreaseButton.png" },
                "SliderQuantity": {
                    "roi": [340, 430, 200, 140]
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
