# CombatProtocol 循环作战协议维护

本文说明「循环作战协议」任务的槽位轮换机制、状态文件与接入方式，最后更新：2026-10-16。  
维护前请先阅读 [Custom 动作与识别参考](../custom.md) 中的 CombatProtocol 一节。

## 任务概览

`CombatProtocol`（循环作战协议）是一个容器任务：拖入容器的**每个作战任务条目都是一个独立的轮换槽位**（可以有多个同型任务，例如三个配置不同的协议空间），按下面两种模式之一在槽位间自动轮换。纳入后各任务不再经过自身的星期计划门控，由容器直接进入槽位对应任务的主流程（`AutoEssenceMain` / `ProtocolSpaceEntry`）。

- 基于理智累计消耗循环：每个槽位有独立阈值（来自该条目自己的「循环作战协议阈值」选项，推荐 80 的倍数）。每局完成时按是否使用理智消耗许可累计 80 或 160；某个槽位累计达标后清空其累计、标记其轮次完成，并**结束本次运行**——客户端（MXU）检测到槽位已推进后，会自动带着下一个槽位自己的配置启动下一次运行；如此接力，直到某次运行没有推进槽位（理智不足等自然停止）。
- 基于日期循环：按游戏日（凌晨 4 点边界）每天执行一个槽位，当天的槽位跑到理智不足为止，次日自动轮到下一个，轮完回到第一个。

失败处理：某次运行失败时把当前槽位标记为失败，并在理智模式下推进到下一个槽位（日期模式下只标记，轮换由日期驱动）；失败不触发自动接力。单轮中每个槽位的状态（未执行 / 成功 / 失败）持久化保存，全部槽位都轮过一遍后清空。

## 状态文件

轮换状态保存在 `debug/record/CombatProtocol.json`（槽位索引即条目在容器中的位置）：

```json
{
    "schema_version": 2,
    "mode": "sanity",
    "slots": [
        { "task": "ProtocolSpace", "threshold": 240 },
        { "task": "ProtocolSpace", "threshold": 240 },
        { "task": "AutoEssence", "threshold": 480 }
    ],
    "current_index": 2,
    "accum": [0, 0, 320],
    "progress": ["success", "success", "pending"],
    "last_run_date": "2026-10-16",
    "pending_coupon": false,
    "rotated_this_battle": false,
    "updated_at": "2026-10-16T04:00:00Z"
}
```

- `slots` 是装配时写入的槽位表（任务与阈值）；`accum` / `progress` 与 `slots` 按下标一一对应。
- `current_index` 指向即将执行（或正在执行）的槽位。
- `progress` 取值 `pending` / `success` / `failed`；全部非 `pending` 时整体重置。
- `rotated_this_battle` 是运行内瞬态标记：达标轮换后置位，流水线据此结束本次运行，下次进入时清除。
- 槽位表或模式变化、文件损坏、schema 版本不符时会重置轮换；删除该文件等于重置全部进度。

## 选项与配置传递

容器任务自身只暴露「轮换模式」选项；收纳集合与轮换顺序由 MXU 的拖拽界面维护。装配运行时 MXU 会：

1. 写一条合成覆写，把槽位表写入容器入口节点的 `attach`：`slots`（任务名数组，按条目顺序）与 `slot_thresholds`（逐槽位阈值，读取各条目自己的「循环作战协议阈值」选项值）；
2. **只合并当前槽位那个条目的选项覆写**（子任务按自身配置执行），并把容器的静态覆写排在其后（容器覆写会把循环次数上限恢复为无上限）。

两种模式各带一份相同的静态覆写（仅 `attach.mode` 不同），把下面的接入点挂进各任务的主流程：

| 覆写节点 | 作用 |
| --- | --- |
| `AutoEssenceOnSuccess` | 一局完成后进入累计节点 |
| `AutoEssenceChooseScalingPre` | 每局领奖前清空消耗券标记 |
| `AutoEssenceChooseScaling2` | 双倍领取时标记消耗券 |
| `AutoEssenceLowSpFallbackScaling1` | 双倍回退单倍时清除标记 |
| `ProtocolSpaceRewardClaimSuccessNext` | 一局领完后进入累计节点 |
| `ProtocolSpaceRewardSelectClaim` | 每局领奖前清空消耗券标记 |
| `ProtocolSpaceRewardUsePermit` | 使用许可时标记消耗券 |
| `ProtocolSpaceRewardFallbackSingle` | 双倍回退单倍时清除标记 |

累计节点之后由 `CombatProtocolStopAfterRotation`（识别 `expect: "rotated"`）在达标时执行 `StopTask` 结束本次运行；未达标则回到当前槽位继续（基质刷取走 `CombatProtocolRunAutoEssence`，协议空间在领奖界面点 `CombatProtocolContinueProtocolSpace`）。

## 已知边界

- 控制器支持取任务的支持交集：ADB / 云 ADB 下「基质刷取」暂未开放，容器同样不提供。
- 协议空间的「目标库存（`TargetInventory`）」与「不领取奖励」模式在容器内不提供；容器始终走按次数领取流程。
- 各任务的循环次数选项（`AutoEssenceRepeatCount` 等）在容器内不生效：容器的静态覆写会把循环次数上限恢复为无上限；循环由槽位阈值（理智模式）或理智自然耗尽（两种模式）控制。
- 「一次运行只执行一个槽位」：换槽位即换配置，靠运行边界保证各条目的配置互不串扰；自动接力由 MXU 完成，其他客户端不支持时需手动逐次启动。
- 未装配槽位表时（如不支持容器装配的客户端）回退为默认顺序：基质刷取（480）→ 协议空间（240），任务仍可基本使用。

## 扩展新的作战任务

1. 在 `agent/go-service/combatprotocol/` 中登记任务标识（`state.go` 的常量与 `taskLabelKey`、`options.go` 的默认阈值）。
2. 在该任务的任务定义中提供「循环作战协议阈值」输入选项（MXU 从条目选项值读取并写入 `slot_thresholds`；选项自带的 `attach` 覆写仅作后备）。
3. 在 `assets/tasks/CombatProtocol.json` 中按上面的接入点表为其流程添加覆写，并在 `assets/resource/pipeline/CombatProtocol.json` 中补充对应分支节点（进入主流程、累计、继续、消耗券标记）。
4. 同步 `tools/schema/` 中 `CombatProtocolAccumulate` / `CombatProtocolState` 的 `task` / `expect` 取值枚举与五语言文案。
