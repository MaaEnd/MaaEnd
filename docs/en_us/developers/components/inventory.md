# Developer Guide - Inventory Transfer and Scroll Component

Inventory provides reusable transfer and scroll nodes for the depot and backpack screen. Each entry performs one operation; the caller must open the correct screen, recognize the item, and verify the result.

## Public Entries

| Node | Purpose |
| --- | --- |
| `InventoryTransferAll` | Transfer all items of the same type as the source item |
| `InventoryTransferStack` | Transfer one stack of the source item |
| `InventoryTransferHalf` | Transfer half of the source item |
| `InventoryBagScrollUpward` / `InventoryBagScrollDownward` | Scroll the backpack up / down once |
| `InventoryRepoScrollUpward` / `InventoryRepoScrollDownward` | Scroll the depot up / down once |

## Calling Transfer Nodes

Before calling an entry, open the screen where the depot and backpack appear side by side and recognize the source item. If the recognition result box is suitable for clicking, bind the `InventoryTransferSource` anchor directly to the recognition node and call the desired transfer entry from a subsequent node.

The following fragment omits the existing recognition configuration of `ExampleFindInventoryItem`; `ExampleVerifyInventoryTransfer` is the caller's transfer verification node:

```json
{
    "ExampleFindInventoryItem": {
        "anchor": {
            "InventoryTransferSource": "ExampleFindInventoryItem"
        },
        "next": [
            "ExampleRunInventoryTransfer"
        ]
    },
    "ExampleRunInventoryTransfer": {
        "pre_delay": 0,
        "action": "Custom",
        "custom_action": "SubTask",
        "custom_action_param": {
            "sub": [
                "InventoryTransferAll"
            ]
        },
        "post_delay": 0,
        "rate_limit": 0,
        "next": [
            "ExampleVerifyInventoryTransfer"
        ]
    }
}
```

If the recognition box needs adjustment with `roi_offset`, add a `DirectHit` node after recognition, point its `roi` to the recognition node, and bind the anchor to it; no extra node is needed when the box is already suitable. Anchors are updated after the node's action, so the transfer entry cannot be called in the same action that sets the anchor. A caller may also use `JumpBack`, but must control repeated entry and verify the result after it returns.

The `SubTask` in the example and the component's `FalseAction` failure path are provided by go-service; start the project's Agent before using them.

## Platforms and Failure Handling

- Win32 uses Ctrl, Shift, or Alt with a click; macOS uses the corresponding platform key mappings.
- ADB holds the source item and uses a second contact to click the corresponding transfer menu button; buttons on either side of the item can be recognized.
- On failure, the component attempts to release pressed keys or contacts and returns failure. `auto_up` also releases unmatched input when supported by the framework.
- A successful action does not prove that the item reached its destination; the caller should recognize the screen again and verify the result.

## Calling Scroll Nodes

Scroll nodes target the screen where the depot and backpack appear side by side, using 1280×720 reference coordinates. Desktop controllers use the mouse wheel, and ADB uses swipe gestures. The caller must wait for the screen to stabilize, check the boundaries, and decide whether to continue scrolling.
