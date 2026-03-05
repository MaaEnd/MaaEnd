# 开发手册 - CharacterController 参考文档

## 简介

此文档介绍了如何使用 CharacterController 相关的节点。

**CharacterController** 提供了一组用于**控制游戏角色**的自定义 Action，包括视角旋转、前后移动以及朝向目标自动移动等功能。这些节点通常与 MapTracker 配合使用，实现更精确的角色控制。

> [!IMPORTANT]
>
> CharacterController 的所有节点依赖键盘/鼠标输入，**必须在前台模式（Seize）下运行**，否则输入事件无法正确传递至游戏。请在 `interface.json` 或用户配置中确保控制器使用 `Seize` 连接方式。

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

#### 行为说明

每次调用时，根据当前帧识别结果执行以下逻辑之一：

| 条件 | 执行动作 |
|---|---|
| 目标在屏幕中心左侧（超出 `align_threshold`） | 向左旋转视角 |
| 目标在屏幕中心右侧（超出 `align_threshold`） | 向右旋转视角 |
| 目标已对齐，但 Y 坐标 > 480（目标在屏幕下半部，已过） | 向后退 |
| 目标已对齐，且 Y 坐标 ≤ 480（目标在屏幕上半部） | 向前进 |

## 完整示例

完整的用法示例请参阅 `assets/resource/pipeline/Interface/Example/CharacterController.json`。
