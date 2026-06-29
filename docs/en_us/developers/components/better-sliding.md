# Development Manual - BetterSliding Reference Documentation

This CustomAction supports sliding a slider, allowing sliding to a specified value.

![BetterSliding Example](https://github.com/user-attachments/assets/27365f2c-b1a5-43cb-8ff6-d75d506716e2)

As shown in the image above, sliding can be performed using `SwipeButton`, and precise adjustments can be made using `DecreaseButton` and `IncreaseButton`.

> [!note]
> Some sliders hide when the number of slidable items is 1. Please handle this scenario appropriately.

## Swipe-Only Mode

Suitable for scenarios where you want to slide to the maximum/minimum. Parameters are as follows. For precise quantity control, please jump to the [Specified Quantity Mode](#specified-quantity-mode) section below.

### Parameter Description

| Field         | Type     | Required | Description                                                                                                                                                                                        |
| ------------- | -------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Direction`   | `string` | Yes      | Swipe direction. Supports `left` / `right` / `up` / `down`.                                                                                                                                        |
| `SwipeButton` | `string` | No       | Custom slider template path. Overrides the default template of the `BetterSlidingSwipeButton` node when provided. Default `""` (uses the shared default template `BetterSliding/SwipeButton.png`). |

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
> Before the CustomAction executes, ensure the slider is at its initial value, and that the initial value is 1. Otherwise, the position deviation of the slider between its minimum and maximum cannot be calculated, causing the quantity adjustment to fail.

### Parameter Description

#### Parameters that can be passed in `attach`

The following 4 fields are recommended to be passed via the calling node's `attach`. The `attach` priority is higher than the same-named fields in `custom_action_param`.

| Field                     | Type                     | Required | Description                                                                                                                                                                                                                       |
| ------------------------- | ------------------------ | -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Target`                  | `int` (positive integer) | Yes      | Target quantity. The desired final gear value to slide to, must be greater than 0.                                                                                                                                                |
| `TargetType`              | `string`                 | No       | How to interpret `Target`. `"Value"` (default): absolute discrete count; `"Percentage"`: percentage of `maxQuantity` (1–100), rounded and clamped to `[1, maxQuantity]`.                                                          |
| `TargetReverse`           | `bool`                   | No       | When `true`, calculates the target from the far end of the range: Value mode becomes `maxQuantity - Target`; Percentage mode becomes `round(maxQuantity * (100 - Target) / 100)`, clamped to `[1, maxQuantity]`. Default `false`. |
| `FinishAfterPreciseClick` | `bool`                   | No       | When `true`, returns success immediately after a precise click, without entering the quantity validation and fine-tuning process. Default `false`.                                                                                |

> [!note]
> Combination calculation logic for `TargetType` and `TargetReverse`:
>
> | TargetType     | TargetReverse | Effective Target                                                           |
> | -------------- | ------------- | -------------------------------------------------------------------------- |
> | `"Value"`      | `false`       | `Target` (original value)                                                  |
> | `"Value"`      | `true`        | `maxQuantity - Target` (not clamped, may be < 1)                           |
> | `"Percentage"` | `false`       | `round(maxQuantity × Target / 100)`, clamped to `[1, maxQuantity]`         |
> | `"Percentage"` | `true`        | `round(maxQuantity × (100 - Target) / 100)`, clamped to `[1, maxQuantity]` |

#### Parameters that can only be passed via `custom_action_param`

In addition to the 4 fields above, all other parameters can only be read from `custom_action_param`:

| Field                     | Type                    | Required | Description                                                                                                                                                                                 |
| ------------------------- | ----------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Direction`               | `string`                | Yes      | Swipe direction. Specifies "the direction of the maximum value", supports `left` / `right` / `up` / `down`.                                                                                 |
| `Quantity.Box`            | `int[4]`                | Yes      | OCR region for the current quantity, format `[x, y, w, h]`.                                                                                                                                 |
| `IncreaseButton`          | `string` or `int[2\|4]` | Yes      | "Increase quantity" button. Template path is recommended (threshold fixed at `0.8`), or coordinates `[x, y]` or `[x, y, w, h]`.                                                             |
| `DecreaseButton`          | `string` or `int[2\|4]` | Yes      | "Decrease quantity" button. Format same as `IncreaseButton`.                                                                                                                                |
| `MaxTarget.Box`           | `int[4]`                | No       | OCR region for reading the item's maximum available quantity (e.g., purchasable/sellable quantity), format `[x, y, w, h]`. When missing, the slider's endpoint value is used as a fallback. |
| `Quantity.Filter`         | `object`                | No       | Color filter parameters for the current quantity OCR, suitable for scenarios where the digit color is stable but background interference is high.                                           |
| `MaxTarget.Filter`        | `object`                | No       | Color filter parameters for the maximum target quantity OCR. Used only when `MaxTarget` is explicitly provided.                                                                             |
| `Quantity.OnlyRec`        | `bool`                  | No       | Whether to enable `only_rec` for the quantity OCR node. Default `false`.                                                                                                                    |
| `MaxTarget.OnlyRec`       | `bool`                  | No       | Whether to enable `only_rec` for the `BetterSlidingGetMaxTarget` OCR node. Used only when `MaxTarget` is explicitly provided.                                                               |
| `GreenMask`               | `bool`                  | No       | When locating buttons using a template path, whether to enable green mask filtering for template matching. Default `false`. Applies to `IncreaseButton` and `DecreaseButton`.               |
| `CenterPointOffset`       | `int[2]`                | No       | Click offset relative to the center point of the slider's recognition box `[x, y]`, negative values left/up, positive right/down. Default `[-10, 0]`.                                       |
| `ClampTargetToMax`        | `bool`                  | No       | When `true`, if the target exceeds `maxQuantity`, it is automatically clamped to `maxQuantity` and execution continues, instead of failing directly. Default `false`.                       |
| `SwipeButton`             | `string`                | No       | Custom slider template path, overrides the default template of the `BetterSlidingSwipeButton` node. Default `""` (uses the shared default template).                                        |
| `ExceedingOverrideEnable` | `string`                | No       | When the resolved target exceeds the slidable range, sets the specified Pipeline node's `enabled` to `true`, then returns success. Default `""` (disabled, the action fails directly).      |

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
                "Quantity": {
                    "Box": [340, 430, 200, 140]
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
