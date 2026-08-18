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

A2 负责「看一眼当前界面，把物品数量记下来」。业务侧**不要自建扫库流程**，按所需地区调用预留入口：

| 入口 | 场景 |
| --- | --- |
| `SyncItemData` | 贵重品库 **培养素材页** |
| `SyncShopItemData` | **采购中心**（衍质源石 / 嵌晶玉顶部数量） |
| `SyncValuablesItemData` | 贵重品库 **珍贵物品页**（如特许寻访凭证） |

需要声明「我要最新缓存」的任务应显式调用对应入口（已扫过会走 Skipped）。

### 调用规范（必读）

凡是需要使用某地区 IMS 缓存的任务，**在业务逻辑执行前都必须主动更新一次该地区缓存**：执行一次对应预留入口即可。

这样设计的效果是：

1. **每个 IMS 任务都会声明「我要最新缓存」**，不会默默沿用磁盘上可能过旧的数据。
2. **同一次 Resource 生命周期里，每个地区入口只有第一个真正执行扫库**；A2 成功后会关闭后续扫库入口。
3. **后续任务同样调用该节点，但会直接跳过**，复用第一个任务刚写入的缓存。

不要因为「感觉缓存应该还在」而跳过入口调用；也不要在任务里另写一套扫库。统一走预留入口，才能既防数据过旧，又避免每个任务都重复进同一界面。

### 参数（IconRecognition ID）

缓存键与识别 ID 统一使用 [`IconRecognition`](./icon-recognition.md) catalog 顶层 key（如 `item_char_break_stage_1_2`）。IMS **不**维护物品白名单：屏上扫到什么就缓存/播报什么。

| 字段 | 说明 |
| --- | --- |
| `grid_type` | 网格界面。培养/珍品页用 `valuables`；走 IconRecognition 扫库时必填 |
| `roi` | 可选；省略则用 IconRecognition 参考 ROI（贵重品库 Win32 `[24,76,950,570]` / ADB `[100,85,790,540]`） |
| `item_filters` | 缩小 IconRecognition **候选模板**（如培养页 `ValuableDepot:SpecialItem`）；不限制「只缓存哪些 ID」 |
| `items` | **定点数量节点**（地区重建仍需保留）：缓存 ID → Pipeline 识别节点。节点可以是纯 OCR，或 And 且 `box_index` 指向 OCR 数字结果（顶栏货币、采购中心等） |
| `deduplicate` | IconRecognition 去重；A2 默认 `true` |
| `page_dedup` / `notify_ui` | 语义同前 |

提供 `grid_type`（IconRecognition 扫库）与 `items` 至少其一。采购中心等可只传 `items`（如 `item_originium_recharge` / `item_diamond`）。`items` 里的键在 `page_dedup=false` 时一律参与地区重建（未命中则从缓存删除）。

示例（培养素材页）：

```json
{
    "grid_type": "valuables",
    "item_filters": ["ValuableDepot:SpecialItem"],
    "items": {
        "item_gold": "item_gold_NUMBER",
        "item_diamond": "item_diamond_NUMBER"
    },
    "page_dedup": false
}
```

### 运行时做了什么

1. 若有 `grid_type`：用 `item_filters`（或 grid 默认候选）**一次整屏扫格**，对每个命中格子按 `cell_box` ROI 偏移 OCR 数量并写入缓存。
2. 若有 `items`：按键名排序跑定点识别节点，沿 `box_index` 取 OCR 数量。
3. **命中且数量合法**：记录 `物品 ID → 数量`。
4. **未命中**：本轮不记录该 ID（见下方「地区重建 / 覆写」）。
5. 全部跑完后写入内存与 `./debug/record/IMS.json`，并更新 `updated_at`。

命中时默认会通过 UI Focus 打出本地化物品名与数量（`ims.sync_item_found`）。可用参数 `notify_ui: false` 关闭（省略默认 `true`）；商店万能跳转顺手缓存使用 `SyncShopItemDataRunNoNotify`。

### 地区重建模式 vs 覆写模式（`page_dedup`）

| `page_dedup` | 模式 | 行为 |
| --- | --- | --- |
| `false`（默认） | **地区重建** | 清空本地区候选后再写入命中：① `item_filters`（或 grid 默认）在 IconRecognition `recognition_items.json` 展开的 ID；② 本轮 `items` 的全部键（定点 OCR / And+`box_index`）。未命中则删除；**其他地区**已缓存的 ID 保留。 |
| `true` | **覆写** | 在已有缓存上，按本轮命中的 ID **覆盖数量**；没扫到的 ID **保留旧值**。 |

覆写适合「列表要翻页」的场景：第 1 页地区重建；后续页只覆写当页可见 ID，避免抹掉前面页。预留入口 `SyncItemData` 的默认链路为：

```text
初次：SyncItemDataRunFull（page_dedup = false，地区重建）
  next[0]：[JumpBack]SyncItemDataScrollPage → 滑动后 SyncItemDataRunInc（page_dedup = true）
  next[1]：SyncItemDataLock（扫描结束）
```

额外翻页次数只改 `SyncItemDataScrollPage.max_hit`（当前为 1）。`max_hit` 用尽后 JumpBack 不再命中，走 Lock。该节点在 Win32-Front 默认 `enabled=false`，ADB 资源开启并覆盖为上滑。

### 同 Resource 只真正扫库一次（实现细节）

上述规范由各入口节点内部自动完成，业务侧只需反复调用对应入口：

1. **首次调用**：真正进入目标界面扫库，写好缓存后，用 Resource 级覆盖把「再扫一次」的通路关掉，且本次运行内不再打开。
2. **再次调用**：入口仍可进入，但会立刻判定「本轮已经同步过」，跳过扫库并继续后续业务。
3. **下次冷启动**：重新加载 Resource / 重启客户端后，通路恢复；下一个 IMS 任务会重新扫库。

> 预留入口与扫库参数见 `assets/resource/pipeline/IMS/SyncItemData.json`、`SyncShopItemData.json`、`SyncValuablesItemData.json`。
>
> `EnsureItemDataReadyMain` 仅用于「按 R2 过期策略决定要不要进 A2」的特殊场景；常规 IMS 任务应直接调对应 `Sync*ItemData`，而不是只靠过期门禁。

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

A3 在**奖励播报界面**用与 A2 相同的路径：一次 IconRecognition（默认 `grid_type: rewards`，`deduplicate: false` 保留同屏多堆）解析命中，再对每个 `cell_box` OCR 数量，作为**正增量**写入缓存。

| 参数 | 说明 |
| --- | --- |
| `grid_type` | 默认 `rewards` |
| `roi` | 可选；默认奖励界面参考 ROI（Win32 `[39,82,1205,511]` / ADB `[178,140,935,440]`） |
| `item_filters` | 可选；省略则用 rewards 默认候选（`Isolate:*` + `ValuableDepot:*`） |
| `item_ids` | 可选；与 `item_filters` **取并集**（先展开 filter 再合并 ID）。仅有 `item_ids` 时按 catalog 推导覆盖用 filter。用于从大类中精确追加子集（如基建快速收取只要武器检查单元/装置，不要模具/套组） |

`custom_action_param` 可为 `{}`。**不会**更新同步时间戳 / 就绪状态。

### 和 A2 的本质区别

| | A2 | A3 |
| --- | --- | --- |
| 写入方式 | **绝对值**：把扫到的数量记成当前库存 | **计算**：把扫到的数量加到已有库存上 |
| 典型场景 | 贵重品库整表同步（`valuables`） | 奖励弹窗「又获得了多少」（`rewards`） |
| 是否建立就绪 | 是（写 `updated_at`，置 `hasData`） | 否 |

### 不依赖缓存也能跑

A3 与其它动作 / 识别器不同：**不要求 IMS 缓存已经存在**。

若从未成功做过 A2（`hasData=false`），A3 仍会识别奖励，但**不写入缓存**，且动作仍返回成功，避免卡住关奖励等后续流程。空奖励（IconRecognition `no_match` / `grid_detection_failed`）以及磁盘 hydrate 失败同样视为成功。命中物品时按件播报（如「获得 xxx ×n」），不提示「未初始化 / 不写入缓存」等 IMS 头尾信息，也不再播汇总。

有缓存时同样按件播报；不叠 Pipeline Starting/Succeeded focus，也不播汇总句。

> 奖励弹出入场动画期间，调用前应对物品区域使用 `pre_wait_freezes`（协议空间见 `ProtocolSpaceRewardAddItemData`）。
>
> 参考 Pipeline：`AddItemDataOnRewards` → `AddItemDataCloseRewards`。
>
> 已接入 A3 的关闭奖励路径：`SceneNoticeRewardsConfirm`（日常奖励等）、`DijiangRewardsFastCollectAddItemData`（基建快速收取，独立候选）、`CreditShoppingClaimConfirm`（信用商店：玉/折金票/武库配额/信用 + `ValuableDepot:SpecialItem`）、`MFGCabinClaimRewardClose`、`GrowthChamberClaimRewardClose`。

---

## R1：`ItemQuantitySatisfied`

判断缓存里的物品数量是否满足条件表达式；也可进入只播报模式。

与通用识别器 [`ExpressionRecognition`](../custom.md#expressionrecognition) 语法相同，但占位符读取的是 **IMS 缓存中的 IconRecognition 物品 ID**，而不是画面 OCR 节点。

| 参数 | 说明 |
| --- | --- |
| `expression` | 布尔表达式；用 `{物品ID}` 引用缓存数量（缺失按 `0`）。`report_only` 时必须恰好包含一个 `{物品ID}` |
| `notify_ui` | 是否向 UI 播报展开后的表达式；默认 `false`（关闭）。`report_only` 时忽略（始终播报） |
| `report_only` | 只播报模式：拒绝多物品表达式，输出「当前 XXX：数量」，并**始终命中**；默认 `false` |

### 条件判断（默认）

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
        "expression": "({item_char_break_stage_1_2}+{item_weapon_break_low})>=100",
        "notify_ui": false
    }
}
```

再例如：

- `{item_char_break_stage_1_2}>=40`
- `{item_char_break_stage_1_2}+{item_weapon_break_low}>=100 && {item_gold}<50`
- `!({item_weapon_break_high}<10)`

表达式结果必须是布尔值。

仅当 `notify_ui` 为 `true` 时，才会向 UI Focus 输出展开后的表达式（约 10 秒内相同文案会节流）。调度类 `next` 扫描建议保持默认关闭，避免刷屏。

### 只播报（`report_only`）

用于「读缓存并告诉用户当前数量」，不参与够不够判断：

```json
{
    "custom_recognition": "ItemQuantitySatisfied",
    "custom_recognition_param": {
        "expression": "{item_gold}",
        "report_only": true
    }
}
```

约束与行为：

- `expression` 必须恰好包含一个 `{物品ID}`；`{a}+{b}` 等会被拒绝
- UI 播报：`当前 折金票：40`（文案键 `ims.item_current`）
- 识别结果**始终返回 true**（缓存缺失按 `0`）
- 约 10 秒内相同文案会节流

R1 **不检查**缓存是否就绪。若需要「数据可用且数量够」，用 `And` 同时挂上 R2（`ItemDataReady`）与 R1，避免把「还没同步」误判成「数量不够去刷」。

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
| `agent/go-service/pkg/iconqty/` | A2/A3 共用：IconRecognition 扫格 + `cell_box` 数量 OCR |
| `assets/data/IconRecognition/recognition_items.json` | IconRecognition 物品 catalog；A2 地区重建按 `item_filters` 展开 |
| `assets/resource/pipeline/IMS/` | Pipeline（按接口分文件） |
| `assets/resource/pipeline/IMS/item/` | 定点 OCR 节点（如 `item_gold` / `item_diamond` / `ORIGEOMETRY.json`） |
| `tools/schema/components/ims.schema.json` | 参数 JSON Schema |
| [`IconRecognition`](./icon-recognition.md) | 图标识别与多语言名 `iconRecognition.name.*` |

| Pipeline 文件 | 内容 |
| --- | --- |
| `SyncItemData.json` | A2 培养素材页入口与同 Resource 锁定 |
| `SyncShopItemData.json` | A2 采购中心入口 |
| `SyncValuablesItemData.json` | A2 珍贵物品页入口 |
| `UpdateItemQuantity.json` | A1 |
| `AddItemData.json` | A3 最佳实践（领奖后关闭） |
| `ItemQuantitySatisfied.json` | R1（调用方覆盖 `expression`） |
| `ItemDataReady.json` | R2 + `EnsureItemDataReady*` |
| `item/*.json` | 定点 OCR（`item_gold` / `item_diamond` / `item_originium_recharge`） |

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
        "item_expcard_stage2_high": 12,
        "item_char_break_stage_1_2": 40
    }
}
```
