# 开发手册 - AutoFight 参考文档

## 1. AutoFight 介绍

**AutoFight** 是 MaaEnd 中的战斗内自动操作模块，在用户已进入游戏战斗场景后，自动完成普攻、技能、连携技、终结技、闪避、锁定目标等操作，直至战斗结束退出。

### 核心概念

- **入口识别**：通过自定义识别 `AutoFightEntryRecognition` 判断当前是否处于「可自动战斗」的战斗场景（能量条可见、至少一名干员连携技就绪、干员等级未显示）。
- **主循环**：入口识别通过后执行自定义动作 `AutoFightMainAction`，主循环在 Go 内完成：每帧截图后在「暂停」「退出」「执行」三者之间分支；识别到非战斗空间（如放大招过场）时进入暂停，暂停超过 10 秒或暂停期间命中 `pause_exit_nodes` 时退出；战斗空间内识别到干员等级重新显示（战斗已结束）时也退出，否则执行一次战斗操作。
- **执行逻辑**：`AutoFightMainAction` 根据当前画面（敌人、能量、连携/终结技、血量等）和 `attach` 参数将待执行动作入队，每帧末尾按时间顺序取出已到期的动作并执行（如技能键、闪避键、锁定目标等），由 Pipeline 中的 `__AutoFightAction*` 节点完成具体点击/按键。

### 实现分工

- **Pipeline**（`assets/resource/pipeline/AutoFight/`）：定义所有「单次操作」的识别与动作节点（如 `__AutoFightRecognition*`、`__AutoFightAction*`）；接口节点 `AutoFight` 位于 `assets/resource/pipeline/Interface/AutoFight.json`，ADB 下的触屏动作与部分识别区域由 `assets/resource_adb/pipeline/AutoFight/` 覆盖。
- **Go Service**（`agent/go-service/autofight/`）：`autofight.go` 实现入口识别 `AutoFightEntryRecognition` 与主循环动作 `AutoFightMainAction`（含暂停/退出判定与动作队列）；`screenanalyzer.go` 中的 `ScreenAnalyzer` 每帧运行 ONNX 检测节点 `__AutoFightRecognitionScreen`，按最近若干帧的检测结果判断能量、连携、终结技、敌人等状态；`endaxistimeline.go` 实现 Endaxis 排轴。内部通过 `ctx.RunRecognition` / `ctx.RunAction` 调用上述 Pipeline 节点。

### 参数（attach）

`AutoFightMainAction` 启动时通过 `ctx.GetNodeJSON` 读取当前节点的 `attach` 作为参数，默认值见 `Interface/AutoFight.json`：`enable_attack`（普攻）、`enable_combo`（连携）、`enable_skill` / `reserve_skill_level` / `enable_break_accumulating_power`（战技）、`enable_end_skill`（终结技）、`enable_dodge` / `enable_dodge_compat`（闪避）、`enable_lock_target`（锁定目标）、`enable_health_dangerous_switch`（低血量切换干员）、`end_axis_timeline_code` / `skip_combo_cooldown`（排轴），另有默认不设置的 `pause_exit_nodes`（节点名列表，暂停期间任一节点命中即退出，如选剑演武任务传入 `TrialOfSwordmancyFightFinish`）。任务层通过 option 的 `pipeline_override` 覆盖 `AutoFight` 的 `attach` 字段来切换行为，例如 `assets/tasks/RealTimeTask.json` 中的 `AutoFightAttack` 选项将 `enable_attack` 设为 `false` 即不普攻。

## 2. AutoFight 使用方式

### 基本用法

在任务 Pipeline 中，将「AutoFight 接口节点」作为 `[JumpBack]` 或 `next` 使用。当业务逻辑需要进入战斗并自动打时，跳转到对应接口即可；接口内部会先做入口识别，通过后进入主循环，直到退出战斗。

### 接口一览

| 接口名 | 说明 |
| ----------------------- | -------------------------------------------------------------------------------------- |
| `AutoFight` | 自动战斗：入口识别通过后执行主循环，普攻、连携、战技、终结技、闪避、锁定目标等是否启用由 `attach` 参数控制。 |

上述接口定义在 `assets/resource/pipeline/Interface/AutoFight.json`。

### 示例：实时任务中挂载 AutoFight

在 `assets/resource/pipeline/RealtimeTask.json` 中，`AutoFight` 列在 `RealTimeTaskMain` 的 `custom_action_param.nodes` 里，由自定义动作 `RealTimeTaskAction` 轮询；当处于战斗场景时会自动进入 AutoFight 流程，战斗结束后继续轮询（节选）：

```jsonc
{
    "RealTimeTaskMain": {
        "action": "Custom",
        "custom_action": "RealTimeTaskAction",
        "custom_action_param": {
            "nodes": ["RealTimeAutoSkip", "AutoFight", "RealTimeSleep"],
        },
    },
}
```

## 3. AutoFight 接口约定

### 只使用 Interface/AutoFight.json 中的接口

**请仅使用 `Interface/AutoFight.json` 内定义的接口节点**：`AutoFight`，需要不同行为时覆盖其 `attach` 参数。

### 禁止直接引用 \_\_AutoFight\* 内部节点

`AutoFight` 目录下的 `Action.json`、`Recognition.json` 中定义的 `__AutoFight*` 节点（如 `__AutoFightRecognitionScreen`、`__AutoFightActionDodge`、`__AutoFightActionAttackTouchDown` 等）属于 **内部实现**，用于支撑接口的识别与动作流程。

- **不要**在任务或其他 Pipeline 中直接引用 `__AutoFight*` 节点（如 `__AutoFightRecognitionScreen`、`__AutoFightActionDodge` 等）。
- 这些节点的结构、名称、逻辑可能随版本更新而变更。
- 若需「自动战斗」能力，请使用 `AutoFight` 接口。

## 4. 排轴实现与待办

排轴（技能轴/时间轴）指战斗内「在什么时机执行什么操作」的调度逻辑。当前有两套调度：默认使用 `agent/go-service/autofight/autofight.go` 中写死的规则；`attach.end_axis_timeline_code` 填入有效的 Endaxis 排轴数据码时，战技与终结技改为按 `agent/go-service/autofight/endaxistimeline.go` 解析出的时间轴派发，数据码无效则回退到默认规则。

### 已实现内容

- **动作队列结构**：`fightAction` 包含 `executeAt`（执行时间）与 `action`（动作类型，干员编号已编入类型，如 `ActionSkill1`–`ActionSkill4`）。队列按 `executeAt` 排序，每帧末尾由 `drainActionQueue` 取出已到期的动作，依次 `RunAction` 对应的 `__AutoFightAction*` 节点。
- **动作类型**：锁定目标、连携（E 键）、战技（1–4 键）、终结技（长按 1–4 键）、闪避、切换干员（F1–F4）、向前/后/左/右冲刺（WASD + 闪避）、等待 1 秒，以及 ADB 下的普攻补按；连携、战技与终结技、切换干员的按键可在全局改键选项 `KeymapFight`（`assets/tasks/setting/Keymap.json`）中修改。普攻不入队：开启 `enable_attack` 时整场按住（`__AutoFightActionAttackTouchDown`），结束时松开。
- **入队逻辑**（在 `AutoFightMainAction` 的循环内，每帧依次判断）：
    - 开启 `enable_dodge` 且识别到敌人攻击 → 入队「闪避」，`executeAt = now + 1ms`；`enable_dodge_compat` 为 `true` 时忽略中心落在画面中部的攻击提示，地面攻击仍会闪避。
    - 开启 `enable_lock_target` 时：未锁定敌人则战斗开始时先锁定一次，之后未锁定满 3 秒再次锁定、满 6 秒朝敌人所在方向冲刺（无方向提示时向前）后锁定、满 9 秒重新计时；已锁定但 5 秒内没有闪避或冲刺则向前冲刺一次，防止敌人跑远。关闭时，下文「已锁定敌人」的条件视为始终满足。
    - 开启 `enable_health_dangerous_switch` 且当前干员血量危险 → 入队「切换干员」，切到第一个血量正常的干员。
    - 开启 `enable_combo` 且识别到连携提示 → 入队「连携」，`executeAt = now`。
    - 开启 `enable_end_skill`、已锁定敌人且有终结技就绪 → 入队该干员的终结技，只取第一个就绪的干员。
    - 开启 `enable_skill` 且能量 ≥1 → 识别到敌人蓄力且开启 `enable_break_accumulating_power` 时直接释放战技打断，否则能量大于 `reserve_skill_level` 且已锁定敌人时才释放；干员按 `skillCycleIndex` 轮转 1→2→…→N→1（N 为队伍人数），`executeAt = now`。
- **排轴（Endaxis）**：`end_axis_timeline_code` 为 Endaxis 网站（`www.end-axis.com`）「复制数据码」得到的字符串，即 gzip 压缩后再做 URL-safe base64 编码的项目 JSON。启用后上面连携、终结技、战技三条不再生效：已锁定敌人且没有正在运行的方案时，`SelectScenario` 先等全员连携就绪（`skip_combo_cooldown` 为 `true` 时不等），再从数据中选出第一个「带终结技的干员终结技均已就绪、且含战技或终结技动作」的方案；之后按时间轴依次派发战技与终结技，条件不满足（终结技未就绪、能量不足、未锁定敌人）时暂停计时等待；方案运行期间识别到连携提示即释放连携。
- **固定延时**：终结技长按 1500ms；闪避、锁定目标、冲刺、切换干员 `executeAt = now + 1ms`；暂停超过 10 秒退出；ADB 下每 5 秒补按一次普攻；退出后继续截图 3 秒，等最后的动作结束。

### 未实现 / 局限

- **排轴只派发战技与终结技**：Endaxis 数据中只派发 `ultimate`、`skill` / `battleSkill` 类型的动作，其余类型不派发，只参与计算方案的结束时间（所有动作的最晚结束帧）；排轴模式下连携仍按识别结果触发。
- **默认规则写死在代码中**：例如终结技只取第一个就绪的干员、战技按能量与 `reserve_skill_level` 释放等，改逻辑需改 Go 代码。
- **排轴时间以选中方案为起点**：时间轴从 `SelectScenario` 选中方案时开始计时（对应 Endaxis 的第 300 帧，按 60 fps 换算），队首动作暂时无法执行时计时暂停，因此不等同于「战斗开始后第 N 秒」。
- **默认战技轮转固定为 1→2→…→N**：自定义释放顺序需使用排轴。

### TODO

- [x] **排轴格式**：已接入 Endaxis 排轴数据码（见上文），可配置战斗内战技、终结技的释放顺序与时机；尚不支持按关卡或阵容自动选用不同排轴。
