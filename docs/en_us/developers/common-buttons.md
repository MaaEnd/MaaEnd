<!-- markdownlint-disable MD060 -->

# Development Guide - Common Button Nodes Reference

> **Tip**: For the buttons involved in the document (whether with text or not), the **text content does not affect the recognition result**. Recognition only relies on the button's background color, icon shape and position-buttons of the same type can be reused for the corresponding node even if the copy is different.
> **Tip**: **Nodes without ROI** perform full-screen searches and will find and click matching buttons anywhere on the screen; nodes with ROI only recognize within the specified area.

---

## WhiteConfirmButtonType1

**Description**: General confirmation button with text, white background, and **circular ring** icon.

<!-- Screenshot: docs/developers/images/common-buttons/WhiteConfirmButtonType1.png (Reference Resource:Common/Button/WhiteConfirmButtonType1.png) -->

![WhiteConfirmButtonType1](https://github.com/user-attachments/assets/556fcf7b-968c-4f06-87c9-8f16610c6b63)

| Item                   | Description                                                                                                                                                                                                        |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Recognition Method** | First locate the white button background via color matching (`WhiteButtonBackground`), then perform template matching (circular ring icon) within the background ROI, supporting normal and Hover state templates. |
| **Search Range**       | **Full-screen search**, no fixed ROI-buttons of this type appearing anywhere on the screen will be recognized and clicked.                                                                                         |
| **Action**             | Single click.                                                                                                                                                                                                      |
| **Usage Condition**    | Use when a confirmation button with **white background + circular ring icon** appears on the interface. If the button has a checkmark icon, use `WhiteConfirmButtonType2`.                                         |

---

## WhiteConfirmButtonType2

**Description**: General confirmation button with text, white background, and **checkmark** icon.

<!-- Screenshot: docs/developers/images/common-buttons/WhiteConfirmButtonType2.png -->

![WhiteConfirmButtonType2](https://github.com/user-attachments/assets/9285eff0-fca2-4039-a9a3-74e1b95a0eb5)

| Item                   | Description                                                                                                                                                        |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Recognition Method** | Same as Type1, relying on `WhiteButtonBackground` + template matching (checkmark icon, supporting normal and Hover state templates).                               |
| **Search Range**       | **Full-screen search**, no fixed ROI-buttons of this type appearing anywhere on the screen will be recognized and clicked.                                         |
| **Action**             | Single click.                                                                                                                                                      |
| **Usage Condition**    | Use when a confirmation button with **white background + checkmark icon** appears on the interface. If it has a circular ring icon, use `WhiteConfirmButtonType1`. |

---

## YellowConfirmButtonType1

**Description**: General confirmation button with text, **yellow background**, and **circular ring** icon.

<!-- Screenshot: docs/developers/images/common-buttons/YellowConfirmButtonType1.png -->

![YellowConfirmButtonType1](https://github.com/user-attachments/assets/ed927343-ff53-433a-87ef-4079bfca8b2f)

| Item                   | Description                                                                                                                                                                                                          |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | First locate the yellow button background via color matching (`YellowButtonBackground`), then perform template matching (circular ring icon) within the background ROI, supporting normal and Hover state templates. |
| **Search Range**       | **Full-screen search**, no fixed ROI-buttons of this type appearing anywhere on the screen will be recognized and clicked.                                                                                           |
| **Action**             | Single click.                                                                                                                                                                                                        |
| **Usage Condition**    | Use when a confirmation button with **yellow background + circular ring icon** appears on the interface. If the button has a checkmark icon, use `YellowConfirmButtonType2`.                                         |

---

## YellowConfirmButtonType2

**Description**: General confirmation button with text, **yellow background**, and **checkmark** icon.

<!-- Screenshot: docs/developers/images/common-buttons/YellowConfirmButtonType2.png -->

![YellowConfirmButtonType2](https://github.com/user-attachments/assets/4c14686a-2cfa-4fbd-abd6-e46b7d7e60b8)

| Item                   | Description                                                                                                                                                          |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | Same as Type1, relying on `YellowButtonBackground` + template matching (checkmark icon, only normal state template is supported).                                    |
| **Search Range**       | **Full-screen search**, no fixed ROI-buttons of this type appearing anywhere on the screen will be recognized and clicked.                                           |
| **Action**             | Single click.                                                                                                                                                        |
| **Usage Condition**    | Use when a confirmation button with **yellow background + checkmark icon** appears on the interface. If it has a circular ring icon, use `YellowConfirmButtonType1`. |

---

## CancelButton

**Description**: General cancel button with text, white background, and **X-shaped** icon.

<!-- Screenshot: docs/developers/images/common-buttons/CancelButton.png -->

![CancelButton](https://github.com/user-attachments/assets/849665ba-661e-4838-b30b-faedb3cba652)

| Item                   | Description                                                                                                                                                                                            |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Recognition Method** | First locate the white button background via color matching (`WhiteButtonBackground`), then perform template matching (X icon) within the background ROI, supporting normal and Hover state templates. |
| **Search Range**       | **Full-screen search**, no fixed ROI-buttons of this type appearing anywhere on the screen will be recognized and clicked.                                                                             |
| **Action**             | Single click.                                                                                                                                                                                          |
| **Usage Condition**    | Use when a cancel button with **white background + X icon** appears on the interface. Distinguish from confirmation buttons (circular ring/checkmark) to avoid accidental clicks on confirmation.      |

---

## TeleportButton

**Description**: Teleport button, fixed in the lower right area of the screen.

<!-- Screenshot: docs/developers/images/common-buttons/TeleportButton.png -->

![TeleportButton](https://github.com/user-attachments/assets/f1b0e309-e587-4bbd-89c8-7828e1d861db)

| Item                   | Description                                                                                                                                                                                                                                                               |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | Perform template matching within the fixed ROI `[1181, 611, 94, 102]` (720p, blue area), supporting normal and Hover states.                                                                                                                                              |
| **Search Range**       | **Restricted area** ROI `[1181, 611, 94, 102]` (720p)-only recognizes within the blue area, and identical buttons appearing in other positions will not be clicked.                                                                                                       |
| **Action**             | Single click.                                                                                                                                                                                                                                                             |
| **Usage Condition**    | Use only when the **teleport button appears at this fixed position** (approximate area starting at 1181,611 in the lower right corner, 94x102). If the interface layout or button position is different, do not reference this node-define ROI and templates by yourself. |

---

## CloseRewardsButton

**Description**: Button to close the reward interface, located slightly below the middle of the interface, no text, with a checkmark sign.

<!-- Screenshot: docs/developers/images/common-buttons/CloseRewardsButton.png -->

![CloseRewardsButton](https://github.com/user-attachments/assets/bf8672db-b861-4283-b2ef-7ffd17544a5e)

| Item                   | Description                                                                                                                                                                                                                                      |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Recognition Method** | Template matching within ROI `[571, 594, 139, 126]` (720p, blue area) with a threshold of 0.9, supporting normal and Hover states.                                                                                                               |
| **Search Range**       | **Restricted area** ROI `[571, 594, 139, 126]` (720p)-only recognizes within the blue area, and identical buttons appearing in other positions will not be clicked.                                                                              |
| **Action**             | Single click.                                                                                                                                                                                                                                    |
| **Usage Condition**    | Use when a close button with **no text + checkmark icon** appears slightly below the middle in the **reward/claim result interface**. If the reward interface layout or button style is different, create a separate template or adjust the ROI. |

---

## CloseButtonType1

**Description**: General close/exit button, the **X in the upper right corner** of the interface. **Cannot close the ESC menu** (only matches the X button, does not recognize the ESC menu interface—if the current interface is the ESC menu, recognition will fail and cause a freeze).

<!-- Screenshot: docs/developers/images/common-buttons/CloseButtonType1.png -->

![CloseButtonType1](https://github.com/user-attachments/assets/46fc3d2d-d673-4d31-942c-ff7fef8412c9)

| Item                   | Description                                                                                                                                                                                                                                                         |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | Perform template matching within ROI `[882, 0, 398, 335]` (720p, blue area), supporting multiple X styles (Type1~Type4).                                                                                                                                            |
| **Search Range**       | **Restricted area** ROI `[882, 0, 398, 335]` (720p)-only recognizes within the blue area, and identical buttons appearing in other positions will not be clicked.                                                                                                   |
| **Action**             | Single click.                                                                                                                                                                                                                                                       |
| **Usage Condition**    | Use when you need to close the **current interface** and confirm that the **ESC menu will not appear**. If the ESC menu may pop up in the same scenario, you must use `CloseButtonType2`-otherwise, failure to close the ESC menu will cause the process to freeze. |

---

## CloseButtonType2

**Description**: General close/exit button, also the X in the upper right corner, but **compatible with the ESC menu** (clicks when either the X or ESC menu is recognized-can close the interface or the ESC menu, avoiding freezes caused by inability to close ESC).

<!-- Screenshot: docs/developers/images/common-buttons/CloseButtonType2.png -->

![CloseButtonType2](https://github.com/user-attachments/assets/54f54907-4597-442e-b6b1-0760597cef9e)

| Item                   | Description                                                                                                                                                                                                                                                                                                      |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | `Or`: Hits either `CloseButtonType1` (upper right blue area) or `ESCMenu` (exploration level text in the lower left blue area).                                                                                                                                                                                  |
| **Search Range**       | **Restricted area**, determined by the ROIs of child nodes `CloseButtonType1` and `ESCMenu`-only recognizes within the specified areas of each child node.                                                                                                                                                       |
| **Action**             | Right-click (`contact: 1`), or Back key in ADB mode.                                                                                                                                                                                                                                                             |
| **Usage Condition**    | Use when you need to **unify handling of "closing the interface" and "closing the ESC menu"**. For example: clicking close may either close a pop-up or open the ESC menu-clicking again in the next frame closes the ESC menu. If the ESC menu never appears in the scenario, `CloseButtonType1` is sufficient. |

---

## Summary

| Node Name                  | Typical Usage Scenarios                                                                                  |
| -------------------------- | -------------------------------------------------------------------------------------------------------- |
| `WhiteConfirmButtonType1`  | Confirm button with white background + circular ring icon                                                |
| `WhiteConfirmButtonType2`  | Confirm button with white background + checkmark icon                                                    |
| `YellowConfirmButtonType1` | Confirm button with yellow background + circular ring icon                                               |
| `YellowConfirmButtonType2` | Confirm button with yellow background + checkmark icon                                                   |
| `CancelButton`             | Cancel button with white background + X icon                                                             |
| `TeleportButton`           | Teleport button at the fixed position in the lower right corner                                          |
| `CloseRewardsButton`       | Checkmark close button in the lower middle of the reward interface                                       |
| `CloseButtonType1`         | Upper right X that only closes the interface (freezes under ESC)                                         |
| `CloseButtonType2`         | Universal upper right X for closing the interface or ESC (use this to avoid freezes when ESC may appear) |

<!-- markdownlint-enable MD060 -->
