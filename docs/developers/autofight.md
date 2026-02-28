# 开发手册 - AutoFight 参考文档

## 1. AutoFight 介绍

**AutoFight** 是 MaaEnd 中的战斗内自动操作模块，在用户已进入游戏战斗场景后，自动完成普攻、技能、连携技、终结技、闪避、锁定目标等操作，直至战斗结束退出。

### 核心概念

- **入口识别**：通过自定义识别 `AutoFightEntryRecognition` 判断当前是否处于「可自动战斗」的战斗场景（能量条可见、4 名干员技能图标就绪、未处于角色等级结算等）。
- **主循环**：进入战斗后进入 `__AutoFightLoop`，每帧在「暂停」「退出」「执行」三者之间分支；识别到非战斗空间（如放大招过场）时进入暂停，识别到结算界面时退出，否则执行一次战斗操作。
- **执行逻辑**：Go Service 中的 `AutoFightExecuteRecognition` 根据当前画面（敌人、能量、连携/终结技等）将待执行动作入队，`AutoFightExecuteAction` 在动作节点中按时间顺序取出并执行（如点击普攻、技能键、闪避键等），由 Pipeline 中的 `__AutoFightAction*` 节点完成具体点击/按键。

### 实现分工

- **Pipeline**（`assets/resource/pipeline/AutoFight/`）：定义所有「单次操作」的识别与动作节点（如 `__AutoFightRecognition*`、`__AutoFightAction*`），以及主循环结构（`MainLoop.json`）。
- **Go Service**（`agent/go-service/autofight/autofight.go`）：实现入口/退出/暂停/执行四类 Custom 识别与执行动作，内部通过 `ctx.RunRecognition` / `ctx.RunTask` 调用上述 Pipeline 节点，并维护动作队列与优先级（连携 > 终结技 > 普通技能 > 普攻/闪避）。

### 锚点机制（Anchor）

入口节点（如 `AutoFight`、`AutoFightRealtimeTask`）通过 `anchor` 指定「普攻锚点」的替换：若配置了 `__AutoFightActionAttackAnchor` → `__AutoFightActionComboClick`，则普攻节点会先执行连携键（E 键）；若替换为空字符串，则只执行锚点占位，不触发普攻点击。任务层可通过 option 覆盖 `__AutoFightActionAttackAnchor` 在「全自动（带普攻）」与「半自动（不普攻）」之间切换。

## 2. AutoFight 使用方式

### 基本用法

在任务 Pipeline 中，将「AutoFight 接口节点」作为 `[JumpBack]` 或 `next` 使用。当业务逻辑需要进入战斗并自动打时，跳转到对应接口即可；接口内部会先做入口识别，通过后进入主循环，直到退出战斗。

### 接口一览

| 接口名                 | 说明                                                                 |
| ---------------------- | -------------------------------------------------------------------- |
| `AutoFight`            | 全自动战斗：带普攻锚点（默认指向连携键），自动普攻 + 技能 + 连携等。 |
| `AutoFightNoAttack`    | 半自动战斗：不普攻，仅执行技能/连携/终结技/闪避等。                   |
| `AutoFightRealtimeTask`| 开荒/实时任务用：通过任务 option 的 `__AutoFightActionAttackAnchor` 覆盖决定是否普攻。 |

上述接口定义在 `AutoFightInterface.json`。

### 示例：实时任务中挂载 AutoFight

在 `RealtimeTask.json` 中，将 `AutoFightRealtimeTask` 作为 `[JumpBack]` 节点，当处于战斗场景时会自动进入 AutoFight 流程，战斗结束后 JumpBack 返回：

```jsonc
{
    "RealtimeTaskEntry": {
        "next": [
            "[JumpBack]AutoFightRealtimeTask",
            "SomeOtherRealtimeLogic"
        ]
    }
}
```

## 3. AutoFight 接口约定

### 只使用 AutoFightInterface.json 中的接口

**请仅使用 `AutoFightInterface.json` 内定义的接口节点**：`AutoFight`、`AutoFightNoAttack`、`AutoFightRealtimeTask` 等。

### 禁止直接引用 __AutoFight* 内部节点

`AutoFight` 目录下的 `MainLoop.json`、`Action.json`、`Recognition.json` 中定义的 `__AutoFight*` 节点（如 `__AutoFightLoop`、`__AutoFightExecute`、`__AutoFightActionAttack` 等）属于 **内部实现**，用于支撑接口的识别与动作流程。

- **不要**在任务或其他 Pipeline 中直接引用 `__AutoFight*` 节点（如 `__AutoFightLoop`、`__AutoFightActionAttack` 等）。
- 这些节点的结构、名称、逻辑可能随版本更新而变更。
- 若需「自动战斗」能力，请使用上述三个接口之一。

## 4. 排轴实现与待办

排轴（技能轴/时间轴）指战斗内「在什么时机执行什么操作」的调度逻辑。当前实现**没有独立的排轴数据格式**，全部写在 `agent/go-service/autofight/autofight.go` 中，属于隐式、写死的规则。

### 已实现内容

- **动作队列结构**：`fightAction` 包含 `executeAt`（执行时间）、`action`（动作类型）、`operator`（干员下标 1–4，仅技能类使用）。队列按 `executeAt` 排序，执行时只取出已到期的动作依次 `RunTask`。
- **动作类型**：锁定目标、连携（E 键）、终结技（KeyDown/KeyUp）、普通技能（1–4 键轮转）、普攻、闪避。
- **优先级与入队逻辑**（在 `AutoFightExecuteRecognition` 内）：
  - 敌人首次出现在屏幕 → 入队「锁定目标」，`executeAt = now + 1ms`。
  - 有连携提示 → 入队「连携」，`executeAt = now`。
  - 否则若终结技可用 → 入队该干员终结技 KeyDown + 1.5s 后 KeyUp，只取第一个可用干员。
  - 否则若能量 ≥1 → 入队「普通技能」，干员按 `skillCycleIndex` 轮转 1→2→3→4→1，`executeAt = now`。
  - 攻击侧：若识别到敌人攻击 → 入队「闪避」，`executeAt = now + 100ms`；否则入队「普攻」，`executeAt = now`。
- **固定延时**：终结技长按 1500ms；闪避延迟 100ms 再触发，以配合识别结果。

### 未实现 / 局限

- **无排轴配置文件**：无法通过 JSON/YAML 等描述「第几秒放谁技能」或按关卡/阵容定制轴。
- **优先级与分支写死在代码中**：例如连携优先于终结技、终结技只取第一个可用等，改逻辑需改 Go 代码。
- **无绝对时间轴**：仅有「相对当前时刻的延迟」，没有「战斗开始后第 N 秒」这类绝对时间排轴。
- **普通技能轮转固定为 1→2→3→4**：无法配置按干员或顺序定制的技能轴。

### TODO

- [ ] **排轴格式**：设计一套排轴（时间轴/技能轴）数据格式，供内部使用，用于描述或配置战斗内技能释放顺序与时机，以支持按关卡或阵容配置轴、用户自定义技能顺序等。
