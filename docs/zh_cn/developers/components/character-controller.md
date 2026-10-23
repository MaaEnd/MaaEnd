# 开发手册 - CharacterController 参考文档

## 简介

此文档介绍了如何使用 CharacterController 相关的节点。

**CharacterController** 提供了一组用于**控制游戏角色**的自定义 Action，包括视角旋转、前后移动、绕圈微调查找以及朝向目标自动移动等功能。这些节点通常与 MapNavigator 配合使用，实现更精确的角色控制。

> [!IMPORTANT]
>
> 视角类节点（Yaw / Pitch / MoveToTarget 等）在 Win32 下依赖键盘/鼠标输入，**必须在前台模式（Seize）下运行**，否则输入事件无法正确传递至游戏。  
> 在 **ADB** 下，[`resource_adb/.../CharacterController/Action.json`](../../../../assets/resource_adb/pipeline/Common/Private/CharacterController/Action.json) 把轴向移动节点（`CharacterControllerForwardAxisAction`、`CharacterSearchAction` 所用的 `__CharacterControllerAxisLongPress*Action`）映射为虚拟摇杆 `LongPress`，视角拖动映射为触摸滑动、Shift 冲刺映射为冲刺按钮点击，均无需 Seize。

## ADB 输入能力与任务保护

`AdbShell` 的单指 `Swipe` 不能提供自动战斗所需的多触点。不能用移动成功证明战斗输入正常。

协议空间、自动基质、自动采集、环境监测、访问好友、干员赠礼及送货任务的全自动送货子流程，在业务执行前运行无参数的 `TouchInputCheck`，保护这些入口中的自动战斗和地图导航依赖；未命中的计划日仍正常跳过，送货的其他处理模式不因新增入口检查而全局禁用。`AutoFightMainAction` 在战斗循环前再次检查。当前 Go API 的 `GetInfo()` 仅提供输入方法配置，无法证明实际选中的后端。因此检查在 ADB 控制器上只释放一次普攻触点 `4`，不按下、点击或移动；`AdbShell` 不支持该触点 API，会直接拒绝。此预检只验证必要的触点 API，不代替游戏内多指操作验收。

生态农场在 `__AutoEcoFarmFilterClosed` 检查后才通过锚点查找设施，未启用的分支仍跳过；送货的 `AutoDelivery` 入口检查先于任务解析和回收站地图定位。两处均在进入 C++ `MapFind` 识别器前拒绝能力不足的输入，避免仅让内部缩放失败、外层定位却继续。检查不能放在 `MapFind` 所在节点的 action 中，因为 recognition 先于 action 执行。

公共地图缩放、ADB 拍照轮盘和送货对话中的光标移动通过 `TouchInputAction` 检查后再执行原动作；其唯一参数 `node` 是原动作节点名，原识别框和外层等待设置保持。公共菜单和遗留流程中的按键保持候选节点在 ADB 资源层也有此保护，仅在实际执行该节点时检查，不全局禁用所有引用公共菜单的任务。受保护不代表已验收其完整业务。失败提示写入运行日志及 Go 日志，明确说明检测到 ADB 回退或持续触控不可用、当前任务无法完成，并建议检查模拟器、重启并重新连接。接口不能确认实际后端名称，因此不把任意触控错误断言为已确认的后端切换。

能力拒绝按当前任务 ID 记录，并清空拒绝节点在当前上下文中的 `on_error`。`SubTask` 即使配置了忽略子任务失败，或 `FailureCollector` 配置了恢复路线，也必须传播此拒绝，停止本任务的依赖步骤、领奖与重试。普通路线失败保留原有处理方式。任务生命周期结束清理拒绝状态，不调用会把失败显示为成功的 `PostStop`；后续独立任务继续，但仍须验证自身场景前置条件。直接绕开这些 Pipeline 入口调用 C++ 导航不属于受保护的业务接线。

战斗循环中的输入动作失败会结束当前战斗回调；普通输入失败由上层原有错误策略决定是否恢复或继续，其提示不承诺整个任务已中断或不再重试。主战斗回调退出时有界释放本次请求过的触点，释放失败保留最初错误，并清空待执行动作，避免下次战斗继承旧队列。能力检查拒绝仍按前述任务级规则传播；恢复设备或触控后端后应重新连接，不能只重新运行已降级的控制器。

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
- `far_target_width`：正整数。识别框宽度小于此值时，认为目标距离过远，直接向前移动（前进 200ms 后再冲刺 3 次），跳过旋转与对齐逻辑。未设置时不启用此判断。

#### 行为说明

每次调用时，根据当前帧识别结果执行以下逻辑之一：

| 条件 | 执行动作 |
| -------------------------------------------------------------- | ------------ |
| 识别框宽度 < `far_target_width`（且已设置 `far_target_width`） | 向前进并冲刺 3 次 |
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
