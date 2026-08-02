# IMS（物品管理系统）

IMS（Item Management System）在 go-service 进程内维护培养道具的数量缓存，供任务做「够不够」「要不要刷」等判断。流程编排仍由 Pipeline 负责；IMS 只提供识别与动作。

整套能力一共 **2 个识别器 + 3 个动作**：

| 代号 | 注册名 | 角色 |
| --- | --- | --- |
| **A2** | `SyncItemData` | 核心：扫当前界面，写入整表缓存 |
| **A1** | `UpdateItemQuantity` | 对单个物品做加减 |
| **A3** | `AddItemData` | 扫当前界面，把识别到的数量**累加**进缓存 |
| **R1** | `ItemQuantitySatisfied` | 用条件表达式判断缓存数量是否达标 |
| **R2** | `ItemDataReady` | 判断整份缓存是否可用（有没有、过没过期） |

代号 `A1` / `A2` / `A3`、`R1` / `R2` 只表示实现顺序，不表示优先级。其中 **A2 虽是第二个写的动作，却是整个 IMS 的核心**。

落盘路径：`./debug/record/IMS.json`（相对运行目录）。

---

## A2：`SyncItemData`（核心）

A2 负责「看一眼当前界面，把物品数量记下来」。业务侧**不要自建扫库流程**，统一调用预留的 Pipeline 入口节点 `SyncItemData`（任意界面 → 贵重品库培养素材页 → 执行 Custom Action）。

### 调用规范（必读）

凡是需要使用 IMS 缓存的任务，**在业务逻辑执行前都必须主动更新一次缓存**：执行一次预留节点 `SyncItemData` 即可。

这样设计的效果是：

1. **每个 IMS 任务都会声明「我要最新缓存」**，不会默默沿用磁盘上可能过旧的数据。
2. **同一次 Resource 生命周期里，只有第一个真正执行扫库**；A2 成功后会关闭后续扫库入口。
3. **后续 IMS 任务同样调用该节点，但会直接跳过**，复用第一个任务刚写入的缓存。

不要因为「感觉缓存应该还在」而跳过入口调用；也不要在任务里另写一套扫库。统一走 `SyncItemData`，才能既防数据过旧，又避免每个任务都重复进培养素材页。

### 参数：`items` 字典

| 字段 | 说明 |
| --- | --- |
| **键** | 物品 ID（写入缓存与 `IMS.json` 时用这个名字） |
| **值** | 用来识别「这件物品 + 它的数量」的 Pipeline 节点名 |

节点通常是 `And`（品质色 / 模板 / 数量 OCR 等拼在一起）。若界面上该物品只有数字、没有图标链路，也可以直接用 OCR 节点。

**关键约束**：节点的 `box_index` 必须最终指向「数量」那一层识别结果（纯数字）。A2 沿 `box_index` 链取到的文本必须是合法数字，才会记入缓存。

示例：

```json
{
    "items": {
        "PROTODISK": "PROTODISK",
        "T_CREDS": "T_CREDS_NUMBER"
    },
    "page_dedup": false
}
```

### 运行时做了什么

1. 按 `items` 的键名排序，依次跑对应识别节点。
2. **命中且数量合法**：记录 `物品 ID → 数量`。
3. **未命中**：本轮不记录该 ID（见下方「重新生成 / 覆写」）。
4. 全部节点跑完后，把结果写入内存，并落盘到 `./debug/record/IMS.json`，同时写入时间戳 `updated_at`，表示这份数据是何时生成的。

命中时会通过 UI Focus 打出本地化物品名与数量。

### 重新生成模式 vs 覆写模式（`page_dedup`）

| `page_dedup` | 模式 | 行为 |
| --- | --- | --- |
| `false`（默认） | **重新生成** | 以本轮命中结果为准，**整表重建**缓存。本轮没扫到的 ID 不会出现在新表里。 |
| `true` | **覆写** | 在已有缓存上，按本轮命中的 ID **覆盖数量**；没扫到的 ID **保留旧值**。 |

覆写适合「列表要翻页」的场景。预留入口 `SyncItemData` 的默认链路为：

```text
初次：SyncItemDataRunFull（page_dedup = false，整表重建）
  next[0]：[JumpBack]SyncItemDataScrollPage → 滑动后 SyncItemDataRunInc（page_dedup = true）
  next[1]：SyncItemDataLock（扫描结束）
```

额外翻页次数只改 `SyncItemDataScrollPage.max_hit`（当前为 1）。`max_hit` 用尽后 JumpBack 不再命中，走 Lock。该节点在 Win32-Front 默认 `enabled=false`，ADB 资源开启并覆盖为上滑。

### 同 Resource 只真正扫库一次（实现细节）

上述规范由入口节点内部自动完成，业务侧只需反复调用 `SyncItemData`：

1. **首次调用**：真正进入培养素材页扫库，写好缓存后，用 Resource 级覆盖把「再扫一次」的通路关掉，且本次运行内不再打开。
2. **再次调用**：入口仍可进入，但会立刻判定「本轮已经同步过」，跳过扫库并继续后续业务。
3. **下次冷启动**：重新加载 Resource / 重启客户端后，通路恢复；下一个 IMS 任务会重新扫库。

> 预留入口与扫库参数见 `assets/resource/pipeline/IMS/SyncItemData.json`。
>
> `EnsureItemDataReadyMain` 仅用于「按 R2 过期策略决定要不要进 A2」的特殊场景；常规 IMS 任务应直接调 `SyncItemData`，而不是只靠过期门禁。

---

## A1：`UpdateItemQuantity`

在已知「刚获得 / 刚消耗了多少」时，不必整表重扫，直接改缓存里某一个物品。

| 参数 | 说明 |
| --- | --- |
| `item` | 物品 ID |
| `delta` | 有符号变化量：正数增加，负数减少 |

结果下限为 `0`。成功后写回内存并更新 `IMS.json` 的 `items`，**但不会改**就绪状态与同步时间戳（`hasData` / `updated_at` 仍只由 A2 确立）。

---

## A3：`AddItemData`

A3 的入参与 A2 相同：一个 `items` 字典（键 = 物品 ID，值 = 识别节点；`box_index` 同样要指向数量）。

它会在**当前画面**识别物品与数量，再和 IMS 缓存做计算：把识别到的数量当作**正增量**加进缓存（等价于多次 A1 的 `+n`）。**不会**更新同步时间戳 / 就绪状态。

### 和 A2 的本质区别

| | A2 | A3 |
| --- | --- | --- |
| 写入方式 | **绝对值**：把扫到的数量记成当前库存 | **计算**：把扫到的数量加到已有库存上 |
| 典型场景 | 培养素材页整表同步 | 领奖弹窗「又获得了多少」 |
| 是否建立就绪 | 是（写 `updated_at`，置 `hasData`） | 否 |

### 不依赖缓存也能跑

A3 与其它动作 / 识别器不同：**不要求 IMS 缓存已经存在**。

若从未成功做过 A2（`hasData=false`），A3 仍会识别奖励，但**不写入缓存**，且动作仍返回成功，避免卡住关奖励等后续流程。命中物品时按件播报（如「获得 xxx ×n」），不提示「未初始化 / 不写入缓存」等 IMS 头尾信息，也不再播汇总。

有缓存时同样按件播报；不叠 Pipeline Starting/Succeeded focus，也不播汇总句。

> 培养素材页的 `IMS/item/*` 节点 ROI 往往不适合奖励界面，请传入适配当前画面的识别节点。奖励弹出入场动画期间，调用前应对物品区域使用 `pre_wait_freezes`（协议空间见 `ProtocolSpaceRewardAddItemData`）。
>
> 参考 Pipeline：`AddItemDataOnRewards` → `AddItemDataCloseRewards`。
>
> 已接入 A3 的关闭奖励路径：`SceneNoticeRewardsConfirm`（日常奖励 / 基建快速收取等）、`CreditShoppingClaimConfirm`、`MFGCabinClaimRewardClose`、`GrowthChamberClaimRewardClose`。

---

## R1：`ItemQuantitySatisfied`

判断缓存里的物品数量是否满足条件表达式。

与通用识别器 [`ExpressionRecognition`](../custom.md#expressionrecognition) 语法相同，但占位符读取的是 **IMS 缓存物品 ID**，而不是画面 OCR 节点。

| 参数 | 说明 |
| --- | --- |
| `expression` | 布尔表达式；用 `{物品ID}` 引用缓存数量（缺失按 `0`） |
| `notify_ui` | 是否向 UI 播报展开后的表达式；默认 `false`（关闭） |

支持的运算：

- 算术：`+` `-` `*` `/` `%`
- 比较：`<` `<=` `>` `>=` `==` `!=`
- 逻辑：`&&` `||` `!`
- 分组：`(...)`

示例：

```json
{
    "custom_recognition": "ItemQuantitySatisfied",
    "custom_recognition_param": {
        "expression": "({PROTODISK}+{CAST_DIE})>=100",
        "notify_ui": false
    }
}
```

再例如：

- `{PROTODISK}>=40`
- `{PROTODISK}+{CAST_DIE}>=100 && {T_CREDS}<50`
- `!({HEAVY_CAST_DIE}<10)`

表达式结果必须是布尔值。

R1 **不检查**缓存是否就绪。若需要「数据可用且数量够」，用 `And` 同时挂上 R2（`ItemDataReady`）与 R1，避免把「还没同步」误判成「数量不够去刷」。

仅当 `notify_ui` 为 `true` 时，才会向 UI Focus 输出展开后的表达式（约 10 秒内相同文案会节流）。调度类 `next` 扫描建议保持默认关闭，避免刷屏。

---

## R2：`ItemDataReady`

判断**整份** IMS 缓存当前是否合格、能不能拿来做业务判断。

### 检测条件

1. **有没有缓存**  
   至少成功做过一次 A2（内存 / 磁盘里有同步记录，`hasData=true`）。没有 → 未命中（日志 `reason=no_data`）。

2. **缓存过没过期**  
   用参数 `refresh_days` 看同步时间戳 `updated_at`（或内存中的上次同步时间）距今是否仍在有效期内。过期 → 未命中（日志 `reason=stale`）。

两项都满足才命中。

### 什么是「过期」

A2 落盘时会写下 `updated_at`。R2 用「现在 − 同步时间」是否超过 `refresh_days` 天来判断这份数据是否还新鲜：

| `refresh_days` | 含义 |
| --- | --- |
| `7`（默认） | 同步后 7 天内视为有效 |
| `1` / `30` | 同步后 1 天 / 30 天内有效 |
| `0` | **不因时间过期**：只要曾经成功同步过，就一直视为未过期 |

注意：

- `refresh_days = 0` 只表示「不因时间失效」，**没有数据时仍然未命中**。
- 「永不因过期失效」≠「永远不用扫库」：无数据时，`EnsureItemDataReadyMain` 仍会走向 A2。

冷启动后，首次访问 IMS 可能把磁盘上的 `IMS.json` 灌进内存一次（lazy hydrate）；之后热路径只读内存。

---

## 推荐用法（怎么串起来）

```text
任务入口
  └─ 执行一次 SyncItemData          ← 同 Resource 仅首次真正扫库
       └─ 业务判断
            ├─ R2 ItemDataReady          （可选：确认缓存合格）
            ├─ R1 ItemQuantitySatisfied  （够不够）
            ├─ A1 UpdateItemQuantity     （已知消耗/获得时微调）
            └─ A3 AddItemData            （领奖界面累加 / 播报）
```

需要「就绪且数量满足」时：

```json
"all_of": [
    "ItemDataReady",
    "ItemQuantitySatisfied"
]
```

---

## 附录

### 实现位置

| 路径 | 说明 |
| --- | --- |
| `agent/go-service/ims/` | Custom 组件与缓存 |
| `assets/resource/pipeline/IMS/` | Pipeline（按接口分文件） |
| `assets/resource/image/IMS/item/` | 物品模板图（`*_TEMPLATE.png`） |
| `tools/SupplyPlan/mask_ims_item_corner.py` | 模板左上角涂绿工具 |
| `tools/schema/components/ims.schema.json` | 参数 JSON Schema |

| Pipeline 文件 | 内容 |
| --- | --- |
| `SyncItemData.json` | A2 入口与同 Resource 锁定 |
| `UpdateItemQuantity.json` | A1 |
| `AddItemData.json` | A3 最佳实践（领奖后关闭） |
| `ItemQuantitySatisfied.json` | R1（调用方覆盖 `expression`） |
| `ItemDataReady.json` | R2 + `EnsureItemDataReady*` |
| `common.json` / `item/*.json` | 品质色与各物品识别节点 |

### 物品模板绿幕

协议空间奖励图标左上角常有角标，会干扰培养素材页裁出的模板。入库前请：

1. 用工具把模板 **左上角 31×18** 涂为 RGB `(0, 255, 0)`；
2. 对应 TemplateMatch 开启 `"green_mask": true`。

```bash
python tools/SupplyPlan/mask_ims_item_corner.py
# 预览：python tools/SupplyPlan/mask_ims_item_corner.py --dry-run
```

### 缓存约定

- 会话内以进程内存为准；热路径不反复读盘。
- A2 / A1 / A3（在 `hasData` 时）成功写入会同步落盘，供下次冷启动恢复。
- `ClearCache`（测试 / 账号切换）清空内存，且**不会**再从磁盘灌回。
- 缓存允许小幅偏差，靠周期 A2 纠偏。

### Go 辅助 API（测试）

| 函数 | 说明 |
| --- | --- |
| `ims.MarkSynced(at, items)` | 标记一次成功同步 |
| `ims.ClearCache()` | 清空缓存（不从磁盘重载） |
| `ims.ItemsSnapshot()` | 返回缓存物品数量副本 |

### 落盘格式示例

```json
{
    "updated_at": "2026-07-29T12:00:00Z",
    "items": {
        "ADVANCED_COGNITIVE_CARRIER": 12,
        "PROTODISK": 40
    }
}
```
