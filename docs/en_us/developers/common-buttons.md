# Development Guide - Common Button Nodes Reference

> **Tip**: For the buttons covered in this document (whether they contain text or not), the **text content does not affect the recognition result**. Recognition only relies on the button's background color, icon shape, and position. The corresponding node can be reused for buttons of the same type even if the copy is different.
>
> **Tip**: **Nodes without ROI** perform full-screen searches and will find and click matching buttons anywhere on the screen; nodes with ROI only recognize within the specified area.

---

## WhiteConfirmButtonType1

**Description**: General confirmation button with text, white background, and **circular ring** icon.

<!-- Screenshot: docs/developers/images/common-buttons/WhiteConfirmButtonType1.png (Reference Resource: Common/Button/WhiteConfirmButtonType1.png) -->

![WhiteConfirmButtonType1](https://github.com/user-attachments/assets/556fcf7b-968c-4f06-87c9-8f16610c6b63)

| Item | Description |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Recognition Method** | `Or`: first perform template matching (circular ring icon) directly within the common area ROI `[672, 416, 608, 304]` (720p); on a miss, fall back to an `And` combined recognition: locate the white button background via color matching (`WhiteButtonBackground`), then perform template matching (circular ring icon) within the background box expanded by `roi_offset: [-20, -20, 60, 40]`. Both steps support normal and Hover state templates. |
| **Search Range** | **Full-screen search**: the common-area ROI is tried first, then a full-screen background color search, so buttons of this type anywhere on the screen will be recognized and clicked. |
| **Action** | Single click, followed by an 80ms freeze wait. |
| **Usage Condition** | Use when a confirmation button with **white background + circular ring icon** appears on the interface. If the button has a checkmark icon, use `WhiteConfirmButtonType2`. |

---

## WhiteConfirmButtonType2

**Description**: General confirmation button with text, white background, and **checkmark** icon.

<!-- Screenshot: docs/developers/images/common-buttons/WhiteConfirmButtonType2.png -->

![WhiteConfirmButtonType2](https://github.com/user-attachments/assets/9285eff0-fca2-4039-a9a3-74e1b95a0eb5)

| Item | Description |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Recognition Method** | Same as Type1: first template matching directly within the common area ROI `[672, 416, 608, 304]` (720p), then on a miss a fallback to `WhiteButtonBackground` + template matching (checkmark icon, supporting normal and Hover state templates). |
| **Search Range** | **Full-screen search**: the common-area ROI is tried first, then a full-screen background color search, so buttons of this type anywhere on the screen will be recognized and clicked. |
| **Action** | Single click, followed by an 80ms freeze wait. |
| **Usage Condition** | Use when a confirmation button with **white background + checkmark icon** appears on the interface. If it has a circular ring icon, use `WhiteConfirmButtonType1`. |

---

## YellowConfirmButtonType1

**Description**: General confirmation button with text, **yellow background**, and **circular ring** icon.

<!-- Screenshot: docs/developers/images/common-buttons/YellowConfirmButtonType1.png -->

![YellowConfirmButtonType1](https://github.com/user-attachments/assets/ed927343-ff53-433a-87ef-4079bfca8b2f)

| Item | Description |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | First locate the yellow button background via color matching (`YellowButtonBackground`), then perform template matching (circular ring icon) within the background ROI, supporting normal and Hover state templates. |
| **Search Range** | **Full-screen search**, with no fixed ROI. Buttons of this type appearing anywhere on the screen will be recognized and clicked. |
| **Action** | Single click, followed by an 80ms freeze wait. |
| **Usage Condition** | Use when a confirmation button with **yellow background + circular ring icon** appears on the interface. If the button has a checkmark icon, use `YellowConfirmButtonType2`. |

---

## YellowConfirmButtonType2

**Description**: General confirmation button with text, **yellow background**, and **checkmark** icon.

<!-- Screenshot: docs/developers/images/common-buttons/YellowConfirmButtonType2.png -->

![YellowConfirmButtonType2](https://github.com/user-attachments/assets/4c14686a-2cfa-4fbd-abd6-e46b7d7e60b8)

| Item | Description |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | Same as Type1, relying on `YellowButtonBackground` + template matching (checkmark icon, supporting normal and Hover state templates). |
| **Search Range** | **Full-screen search**, with no fixed ROI. Buttons of this type appearing anywhere on the screen will be recognized and clicked. |
| **Action** | Single click, followed by an 80ms freeze wait. |
| **Usage Condition** | Use when a confirmation button with **yellow background + checkmark icon** appears on the interface. If it has a circular ring icon, use `YellowConfirmButtonType1`. |

---

## WhiteCancelButton

**Description**: General cancel button with text, white background, and **X-shaped** icon.

<!-- Screenshot: docs/developers/images/common-buttons/WhiteCancelButton.png -->

![WhiteCancelButton](https://github.com/user-attachments/assets/849665ba-661e-4838-b30b-faedb3cba652)

| Item | Description |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Recognition Method** | First locate the white button background via color matching (`WhiteButtonBackground`), then perform template matching (X icon) within the background box expanded by `roi_offset: [-20, -20, 60, 40]`, trying Normal, Hover, thin-line Normal and thin-line Hover templates in order. |
| **Search Range** | **Full-screen search**, with no fixed ROI. Buttons of this type appearing anywhere on the screen will be recognized and clicked. |
| **Action** | Single click, followed by an 80ms freeze wait. |
| **Usage Condition** | Use when a cancel button with **white background + X icon** appears on the interface. Distinguish from confirmation buttons (circular ring/checkmark) to avoid accidental clicks on confirmation. |

---

## GrayCancelButton

**Description**: General cancel button with text, **gray background**, and **X-shaped** icon.

<!-- Screenshot: tests/MaaEndTestset/Win32/Official_CN/四号谷地_地区建设_仓储节点_送货任务_确认转交.png -->

![GrayCancelButton](https://raw.githubusercontent.com/MaaEnd/MaaEndTestset/main/Win32/Official_CN/%E5%9B%9B%E5%8F%B7%E8%B0%B7%E5%9C%B0_%E5%9C%B0%E5%8C%BA%E5%BB%BA%E8%AE%BE_%E4%BB%93%E5%82%A8%E8%8A%82%E7%82%B9_%E9%80%81%E8%B4%A7%E4%BB%BB%E5%8A%A1_%E7%A1%AE%E8%AE%A4%E8%BD%AC%E4%BA%A4.png)

| Item | Description |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | `And`: first locate the gray button background via color matching (`GrayButtonBackground`), then perform template matching (X icon) within the background box expanded by `roi_offset: [-20, -20, 60, 40]`, matching normal → Hover state in short-circuit order, and finally use the X icon box as the click target (`box_index: 1` selects the icon box). |
| **Search Range** | **Full-screen search**, with no fixed ROI. Buttons of this type appearing anywhere on the screen will be recognized and clicked. |
| **Action** | Single click, followed by an 80ms freeze wait. |
| **Usage Condition** | Use when a cancel button with **gray background + X icon** appears on the interface. For example, the gray cancel button beside the yellow confirm button in a "Confirm?" dialog; using this node avoids accidentally clicking confirm. |

---

## TeleportButton

**Description**: Teleport button, located in the lower right corner of the **map interface**.

<!-- Screenshot: docs/developers/images/common-buttons/TeleportButton.png -->

![TeleportButton](https://github.com/user-attachments/assets/f1b0e309-e587-4bbd-89c8-7828e1d861db)

| Item | Description |
| ---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | `And`: first confirm the map interface via `SceneManager/MapMissionReminder.png`, then match the white confirmation button templates (normal and Hover states) within ROI `[1160, 600, 120, 120]` (720p). |
| **Search Range** | **Restricted area** ROI `[1160, 600, 120, 120]` (720p); only effective on the map interface. |
| **Action** | Single click. |
| **Usage Condition** | Use when the teleport button in the lower right corner must be clicked on the map interface. If the button sits elsewhere (for example the quick-teleport button), use `QuickTeleportButton`. |

---

## QuickTeleportButton

**Description**: Go-to-teleport button (quick teleport), sitting to the left of the teleport button in the lower right corner of the map interface.

<!-- Screenshot: tests/MaaEndTestset/Win32/Official_CN/武陵_地图_拍摄巨兽心脏_前往传送.png -->

![QuickTeleportButton](https://raw.githubusercontent.com/MaaEnd/MaaEndTestset/main/Win32/Official_CN/%E6%AD%A6%E9%99%B5_%E5%9C%B0%E5%9B%BE_%E6%8B%8D%E6%91%84%E5%B7%A8%E5%85%BD%E5%BF%83%E8%84%8F_%E5%89%8D%E5%BE%80%E4%BC%A0%E9%80%81.png)

| Item | Description |
| ---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | Same as `TeleportButton`, with button ROI `[1000, 600, 120, 120]` (720p) and threshold 0.87 (0.7 in the ADB resource). |
| **Search Range** | **Restricted area** ROI `[1000, 600, 120, 120]` (720p); only effective on the map interface. |
| **Action** | Single click. |
| **Usage Condition** | Use when the go-to-teleport button must be clicked on the map interface (it pans the map viewport to the target teleport point); usually followed by a click on `TeleportButton` to finish the teleport. |

---

## CloseRewardsButton

**Description**: Button to close the reward interface, located slightly below the middle of the interface, no text, with a checkmark sign.

<!-- Screenshot: docs/developers/images/common-buttons/CloseRewardsButton.png -->

![CloseRewardsButton](https://github.com/user-attachments/assets/bf8672db-b861-4283-b2ef-7ffd17544a5e)

| Item | Description |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Recognition Method** | Template matching within ROI `[540, 538, 209, 182]` (720p) with a threshold of 0.9 (0.7 in the ADB resource), supporting normal and Hover states. |
| **Search Range** | **Restricted area** ROI `[540, 538, 209, 182]` (720p). It only recognizes within that area, and identical buttons appearing in other positions will not be clicked. |
| **Action** | None. The node has no `action` (defaults to `DoNothing`); reference it from your own node via `And` and set `Click` there, as `SceneNoticeRewardsConfirmClick` does. |
| **Usage Condition** | Use when a close button with **no text + checkmark icon** appears slightly below the middle in the **reward/claim result interface**. If the reward interface layout or button style is different, create a separate template or adjust the ROI. |

---

## CloseButtonType1

**Description**: General close/exit button, the **X in the upper right corner** of the interface. **Cannot close the ESC menu** (only matches the X button, does not recognize the ESC menu interface—if the current interface is the ESC menu, recognition will fail and cause a freeze).

<!-- Screenshot: docs/developers/images/common-buttons/CloseButtonType1.png -->

![CloseButtonType1](https://github.com/user-attachments/assets/46fc3d2d-d673-4d31-942c-ff7fef8412c9)

| Item | Description |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recognition Method** | `Or`: tries five upper-right X templates in order, Type1 → Type5 (`CloseButtonType1.png`–`CloseButtonType5.png`), each with its own ROI: `[1186, 1, 94, 78]` for Type1/3/5, `[1056, 42, 208, 121]` for Type2, `[1054, 0, 226, 158]` for Type4 (720p). |
| **Search Range** | **Restricted area**: the upper right corner, as defined by the variant ROIs above; identical buttons elsewhere will not be clicked. |
| **Action** | Single click, followed by an 80ms freeze wait. |
| **Usage Condition** | Use when you need to close the **current interface** and confirm that the **ESC menu will not appear**. If the ESC menu may pop up in the same scenario, handle it separately (`InESCMenu` recognizes it); otherwise the process will freeze. |

---

## Summary

| Node Name | Typical Usage Scenarios |
| -------------------------- | -------------------------------------------------------------------------------------------------------- |
| `WhiteConfirmButtonType1` | Confirm button with white background + circular ring icon |
| `WhiteConfirmButtonType2` | Confirm button with white background + checkmark icon |
| `YellowConfirmButtonType1` | Confirm button with yellow background + circular ring icon |
| `YellowConfirmButtonType2` | Confirm button with yellow background + checkmark icon |
| `WhiteCancelButton` | Cancel button with white background + X icon |
| `GrayCancelButton` | Cancel button with gray background + X icon |
| `TeleportButton` | Teleport button in the lower right corner of the map interface |
| `QuickTeleportButton` | Quick-teleport button to the left of the teleport button on the map interface |
| `CloseRewardsButton` | Checkmark close button in the lower middle of the reward interface |
| `CloseButtonType1` | Upper right X that only closes the interface (freezes under ESC) |

## Q&A

### What are Normal and Hover states?

**Normal state**: The default appearance of a button when untouched. **Hover state**: The highlighted appearance when the cursor/finger rests on the button. An action in the previous frame may leave the cursor on top of a button, so the next frame's screenshot captures the Hover state, which looks different from the Normal state.

### Why do we need dual templates?

Template matching compares a pre-captured button image against the screen. If only the Normal-state template is saved, the different appearance in Hover state will cause a match failure and stall the process. That's why MaaEnd button nodes include both Normal and Hover state templates — a hit on either one counts as a successful recognition.

### What is ROI?

ROI stands for "Region of Interest," using the format `[x, y, width, height]`. `x` and `y` are the coordinates of the region's top-left corner on screen (top-left of the screen is the origin); `width` and `height` are the region's dimensions. For example, `[882, 0, 398, 335]` means starting 882 pixels right and 0 pixels down from the top-left corner, define a 398×335 rectangular area — recognition only searches within this box.

> See also: [Region of Interest on Wikipedia](https://en.wikipedia.org/wiki/Region_of_interest)
