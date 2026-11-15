# Developer Guide - Inventory Transfer Component

Inventory provides reusable item transfer actions for the depot and backpack screen. Each action performs one transfer; the caller must open the correct screen, recognize the item, and verify the result.

## Public Entries

| Action | Purpose |
| --- | --- |
| `InventoryTransferAll` | Transfer all items of the same type as the item in the recognition box |
| `InventoryTransferStack` | Transfer one stack of the item in the recognition box |
| `InventoryTransferHalf` | Transfer half of the item in the recognition box |

## Calling Transfer Actions

Before calling, open the screen where the depot and backpack appear side by side. Use the transfer action directly on the node that recognizes the source item; the action transfers the item in that node's recognition box. `ExampleVerifyInventoryTransfer` is the caller's transfer verification node:

```json
{
    "ExampleFindInventoryItem": {
        "recognition": "TemplateMatch",
        "template": "Example/Item.png",
        "action": "Custom",
        "custom_action": "InventoryTransferAll",
        "next": [
            "ExampleVerifyInventoryTransfer"
        ]
    }
}
```

The transfer actions are provided by go-service; start the project's Agent before using them.

## Platforms and Failure Handling

- Win32 holds Ctrl, Shift, or Alt and clicks the item; macOS uses the corresponding platform key mappings.
- ADB holds the item with the first contact and, once the transfer buttons appear, taps one with a second contact while the first contact stays pressed; buttons on either side of the item can be recognized.
- On failure, the action attempts to release pressed keys or contacts and returns failure, so the caller node enters `on_error`.
- A successful action does not prove that the item reached its destination; the caller should recognize the screen again and verify the result.
