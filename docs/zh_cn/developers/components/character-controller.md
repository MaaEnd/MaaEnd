# 开发手册 - CharacterController 参考文档

## 简介

此文档介绍了如何使用 CharacterController 相关的节点。

**CharacterController** 提供了一组用于**控制游戏角色**的自定义 Action，包括视角旋转、前后移动、绕圈微调查找以及朝向目标自动移动等功能。这些节点通常与 MapNavigator 配合使用，实现更精确的角色控制。

> [!IMPORTANT]
>
> 视角类节点（Yaw / Pitch / MoveToTarget 等）依赖键盘/鼠标输入，**必须在前台模式（Seize）下运行**，否则输入事件无法正确传递至游戏。  
> 轴向移动节点（`CharacterControllerForwardAxisAction`、`CharacterSearchAction` 所用的 `__CharacterControllerAxisLongPress*Action`）在 **ADB** 下由 [`resource_adb/.../CharacterController/Action.json`](../../../assets/resource_adb/pipeline/Common/Private/CharacterController/Action.json) 映射为虚拟摇杆 `LongPress`，无需 Seize。

## 节点说明

下面将详细介绍 CharacterController 提供的节点的具体用法。这些节点都是 Custom 类型的节点，需要在 pipeline 中指定 `custom_action` 来使用。

---

### Action: CharacterControllerYawDeltaAction

↔️ 在水平方向（偏航角/Yaw）旋转玩家视角。

#### 节点参数

必填参数：

- `delta`：整数，旋转角度（度）。正值向右旋转，负值向左旋转。会自动对 360 取模。

---

### Action: CharacterControllerPitchDeltaAction

↕️ 在垂直方向（俯仰角/Pitch）旋转玩家视角。

#### 节点参数

必填参数：

- `delta`：整数，旋转角度（度）。正值向下旋转，负值向上旋转。会自动对 360 取模。

---

### Action: CharacterControllerForwardAxisAction

🚶 控制角色沿前后方向移动。

#### 节点参数

必填参数：

- `axis`：整数。正值向前移动，负值向后移动，`0` 表示不移动。实际移动时长为 `|axis| × 100` 毫秒。

---

### Action: CharacterMoveToTargetAction

🎯 根据识别结果，自动调整朝向并向目标移动。每次调用执行一步调整（旋转或前进/后退），需要在循环节点中反复调用直到到达目标。

#### 节点参数

可选参数：

- `align_threshold`：正整数，默认 `120`。水平对中的像素容忍范围。当目标中心与屏幕中心的水平偏移量小于此值时，认为已对齐，转为前进/后退操作。
- `far_target_width`：正整数。识别框宽度小于此值时，认为目标距离过远，直接向前移动，跳过旋转与对齐逻辑。未设置时不启用此判断。

#### 行为说明

每次调用时，根据当前帧识别结果执行以下逻辑之一：

| 条件 | 执行动作 |
| -------------------------------------------------------------- | ------------ |
| 识别框宽度 < `far_target_width`（且已设置 `far_target_width`） | 向前进 |
| 目标在屏幕中心左侧（超出 `align_threshold`） | 向左旋转视角 |
| 目标在屏幕中心右侧（超出 `align_threshold`） | 向右旋转视角 |
| 目标已对齐，但 Y 坐标 > 480（目标在屏幕下半部，已过） | 向后退 |
| 目标已对齐，且 Y 坐标 ≤ 480（目标在屏幕上半部） | 向前进 |

---

### Action: CharacterSearchAction

🔍 找不到交互点时，按固定绕圈路径微调位置并反复识别目标节点。任一 `wait_nodes` 命中即返回成功；走完整圈仍未命中或任务 Stopping 时返回失败。**起点先识别一次**，未命中后再进入「移动 → 等待 → 识别」循环。

底层通过 `__CharacterControllerAxisLongPress*Action` 执行单步移动：默认资源为 WASD `LongPressKey`；ADB 资源覆盖为虚拟摇杆 `LongPress`（见上文 IMPORTANT）。

#### 节点参数

必填参数：

- `wait_nodes`：字符串数组。目标查找的 Pipeline 节点名列表；任一命中即成功。

#### 绕圈路径

一步固定 100ms（与 `CharacterControllerForwardAxisAction` 的 `axis: 1` 一致）；每步后固定等待再识别。方向映射：前/上 = W（或摇杆上），后/下 = S（或摇杆下），左 = A（或摇杆左），右 = D（或摇杆右）。

```text
起点识别 → 前 前 | 左 左 | 下 下 下 下 | 右 右 右 右 | 上 上 上 上 | 左 左
                 ^每步后：等待 → 截图 → 识别 wait_nodes
```

共 18 步移动，最多 19 次查找（含起点）。

## 完整示例

完整的用法示例请参阅 `assets/resource/pipeline/Interface/Example/CharacterController.json`。
