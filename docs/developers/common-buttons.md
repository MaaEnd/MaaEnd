<!-- markdownlint-disable MD060 -->

# 开发手册 - 通用按钮节点参考

> **提示**：文档中涉及的按钮（无论是否有文字），**文字内容不影响识别结果**。识别仅依赖按钮背景色、图标形状与位置，同一类按钮即使文案不同也可复用对应节点。
> **提示**：**不带 ROI 的节点**均为全屏搜索，会在画面任意位置查找并点击匹配的按钮；带 ROI 的节点仅在指定区域内识别。

---

## WhiteConfirmButtonType1

**说明**：通用确认按钮，有文字、白色底、**圆环形**图标。

<!-- 截图：docs/developers/images/common-buttons/WhiteConfirmButtonType1.png（参考资源：Common/Button/WhiteConfirmButtonType1.png） -->

![WhiteConfirmButtonType1](images/common-buttons/WhiteConfirmButtonType1.png)

| 项目         | 说明                                                                                                                          |
| ------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| **识别方式** | 先通过颜色匹配定位白色按钮背景（`WhiteButtonBackground`），再在背景 ROI 内做模板匹配（圆环图标），支持普通态与 Hover 态模板。 |
| **搜索范围** | **全屏搜索**，无固定 ROI，画面任意位置出现的该按钮都会识别并点击。                                                            |
| **动作**     | 单击。                                                                                                                        |
| **使用条件** | 界面中出现**白色底、带圆环图标**的确认按钮时使用。若按钮为对号形图标，请用 `WhiteConfirmButtonType2`。                        |

---

## WhiteConfirmButtonType2

**说明**：通用确认按钮，有文字、白色底、**对号形**图标。

<!-- 截图：docs/developers/images/common-buttons/WhiteConfirmButtonType2.png -->

![WhiteConfirmButtonType2](images/common-buttons/WhiteConfirmButtonType2.png)

| 项目         | 说明                                                                                             |
| ------------ | ------------------------------------------------------------------------------------------------ |
| **识别方式** | 与 Type1 相同，依赖 `WhiteButtonBackground` + 模板匹配，模板为对号图标（普通态与 Hover 态）。    |
| **搜索范围** | **全屏搜索**，无固定 ROI，画面任意位置出现的该按钮都会识别并点击。                               |
| **动作**     | 单击。                                                                                           |
| **使用条件** | 界面中出现**白色底、带对号图标**的确认按钮时使用。若为圆环图标，请用 `WhiteConfirmButtonType1`。 |

---

## YellowConfirmButtonType1

**说明**：通用确认按钮，有文字、**黄色底**、**圆环形**图标。

<!-- 截图：docs/developers/images/common-buttons/YellowConfirmButtonType1.png -->

![YellowConfirmButtonType1](images/common-buttons/YellowConfirmButtonType1.png)

| 项目         | 说明                                                                                                                           |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| **识别方式** | 先通过颜色匹配定位黄色按钮背景（`YellowButtonBackground`），再在背景 ROI 内做模板匹配（圆环图标），支持普通态与 Hover 态模板。 |
| **搜索范围** | **全屏搜索**，无固定 ROI，画面任意位置出现的该按钮都会识别并点击。                                                             |
| **动作**     | 单击。                                                                                                                         |
| **使用条件** | 界面中出现**黄色底、带圆环图标**的确认按钮时使用。若按钮为对号形图标，请用 `YellowConfirmButtonType2`。                        |

---

## YellowConfirmButtonType2

**说明**：通用确认按钮，有文字、**黄色底**、**对号形**图标。

<!-- 截图：docs/developers/images/common-buttons/YellowConfirmButtonType2.png -->

![YellowConfirmButtonType2](images/common-buttons/YellowConfirmButtonType2.png)

| 项目         | 说明                                                                                              |
| ------------ | ------------------------------------------------------------------------------------------------- |
| **识别方式** | 与 Type1 相同，依赖 `YellowButtonBackground` + 模板匹配，模板为对号图标（仅普通态模板）。         |
| **搜索范围** | **全屏搜索**，无固定 ROI，画面任意位置出现的该按钮都会识别并点击。                                |
| **动作**     | 单击。                                                                                            |
| **使用条件** | 界面中出现**黄色底、带对号图标**的确认按钮时使用。若为圆环图标，请用 `YellowConfirmButtonType1`。 |

---

## CancelButton

**说明**：通用取消按钮，有文字、白色底、**X 形**图标。

<!-- 截图：docs/developers/images/common-buttons/CancelButton.png -->

![CancelButton](images/common-buttons/CancelButton.png)

| 项目         | 说明                                                                                                                        |
| ------------ | --------------------------------------------------------------------------------------------------------------------------- |
| **识别方式** | 先通过颜色匹配定位白色按钮背景（`WhiteButtonBackground`），再在背景 ROI 内做模板匹配（X 图标），支持普通态与 Hover 态模板。 |
| **搜索范围** | **全屏搜索**，无固定 ROI，画面任意位置出现的该按钮都会识别并点击。                                                          |
| **动作**     | 单击。                                                                                                                      |
| **使用条件** | 界面中出现**白色底、带 X 图标**的取消按钮时使用。与确认按钮（圆环/对号）区分使用，避免误点确认。                            |

---

## TeleportButton

**说明**：传送按钮，固定位于屏幕右下区域。

<!-- 截图：docs/developers/images/common-buttons/TeleportButton.png -->

![TeleportButton](images/common-buttons/TeleportButton.png)

| 项目         | 说明                                                                                                                                          |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------------------- |
| **识别方式** | 在固定 ROI `[1181, 611, 94, 102]`（720p，蓝色区域）内做模板匹配，支持普通态与 Hover 态。                                                      |
| **搜索范围** | **限定区域** ROI `[1181, 611, 94, 102]`（720p），仅在蓝色区域内识别，其他位置出现的相同按钮不会点击。                                         |
| **动作**     | 单击。                                                                                                                                        |
| **使用条件** | 仅当**传送按钮出现在该固定位置**（右下角约 1181,611 起 94×102 区域）时使用。若界面布局或按钮位置不同，请勿引用此节点，需自行定义 ROI 与模板。 |

---

## CloseRewardsButton

**说明**：关闭奖励界面按钮，位于界面中部偏下，无文字、对号标志。

<!-- 截图：docs/developers/images/common-buttons/CloseRewardsButton.png -->

![CloseRewardsButton](images/common-buttons/CloseRewardsButton.png)

| 项目         | 说明                                                                                                                              |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------- |
| **识别方式** | 在 ROI `[571, 594, 139, 126]`（720p，蓝色区域）内模板匹配，阈值 0.9，支持普通态与 Hover 态。                                      |
| **搜索范围** | **限定区域** ROI `[571, 594, 139, 126]`（720p），仅在蓝色区域内识别，其他位置出现的相同按钮不会点击。                             |
| **动作**     | 单击。                                                                                                                            |
| **使用条件** | **奖励/领取结果界面**中，中部偏下出现**无文字、对号图标**的关闭按钮时使用。若奖励界面布局或按钮样式不同，需单独做模板或调整 ROI。 |

---

## CloseButtonType1

**说明**：通用关闭/退出按钮，界面**右上角的 X**。**无法关闭 ESC 菜单**（仅匹配 X 按钮，不识别 ESC 菜单界面，若当前是 ESC 菜单会识别不到导致卡死）。

<!-- 截图：docs/developers/images/common-buttons/CloseButtonType1.png -->

![CloseButtonType1](images/common-buttons/CloseButtonType1.png)

| 项目         | 说明                                                                                                                                                   |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **识别方式** | 在 ROI `[882, 0, 398, 335]`（720p，蓝色区域）内模板匹配，支持多种 X 样式（Type1～Type4）。                                                             |
| **搜索范围** | **限定区域** ROI `[882, 0, 398, 335]`（720p），仅在蓝色区域内识别，其他位置出现的相同按钮不会点击。                                                    |
| **动作**     | 单击。                                                                                                                                                 |
| **使用条件** | 需要关闭**当前界面**且确定**不会出现 ESC 菜单**时使用。若同一场景可能弹出 ESC 菜单，必须使用 `CloseButtonType2`，否则无法关闭 ESC 菜单会导致流程卡死。 |

---

## CloseButtonType2

**说明**：通用关闭/退出按钮，同样是右上角 X，但**兼容 ESC 菜单**（识别到 X 或 ESC 菜单都会点击，可关闭界面也可关闭 ESC 菜单，避免因无法关闭 ESC 导致卡死）。

<!-- 截图：docs/developers/images/common-buttons/CloseButtonType2.png -->

![CloseButtonType2](images/common-buttons/CloseButtonType2.png)

| 项目         | 说明                                                                                                                                                                                  |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **识别方式** | `Or`：任意命中 `CloseButtonType1`（右上角蓝色区域）或 `ESCMenu`（左下角蓝色区域内的探索等级文字）。                                                                                   |
| **搜索范围** | **限定区域**，由子节点 `CloseButtonType1`、`ESCMenu` 的 ROI 决定，仅在各子节点指定区域内识别。                                                                                        |
| **动作**     | 右键单击（`contact: 1`），ADB模式下为Back键。                                                                                                                                         |
| **使用条件** | 需要**统一处理“关界面”和“关 ESC 菜单”**时使用。例如：点击关闭后可能是关掉弹窗，也可能打开 ESC 菜单，下一帧再点一次即可关 ESC。若场景绝不会出现 ESC 菜单，用 `CloseButtonType1` 即可。 |

---

## 小结

| 节点名                     | 典型使用场景                                               |
| -------------------------- | ---------------------------------------------------------- |
| `WhiteConfirmButtonType1`  | 白色底 + 圆环图标的确认按钮                                |
| `WhiteConfirmButtonType2`  | 白色底 + 对号图标的确认按钮                                |
| `YellowConfirmButtonType1` | 黄色底 + 圆环图标的确认按钮                                |
| `YellowConfirmButtonType2` | 黄色底 + 对号图标的确认按钮                                |
| `CancelButton`             | 白色底 + X 图标的取消按钮                                  |
| `TeleportButton`           | 右下角固定位置的传送按钮                                   |
| `CloseRewardsButton`       | 奖励界面中下部的对号关闭按钮                               |
| `CloseButtonType1`         | 仅关界面、无法关闭 ESC 菜单（ESC 下会卡死）的右上角 X      |
| `CloseButtonType2`         | 关界面或关 ESC 的通用右上角 X（有 ESC 时用此节点避免卡死） |

<!-- markdownlint-enable MD060 -->
