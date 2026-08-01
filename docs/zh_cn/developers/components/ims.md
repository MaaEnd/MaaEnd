# IMS 培养道具管理

IMS（Item Management System）在 go-service 进程内维护培养道具数量缓存，供任务启动门禁与数量判断使用。流程编排仍由 Pipeline 负责。

> [!NOTE]
> 当前已落地：`ItemDataReady`（R2）、`ItemQuantitySatisfied`（R1）、`UpdateItemQuantity`（A1）、`SyncItemData`（A2）、`AddItemData`（A3）。

## 实现位置

| 路径 | 说明 |
| --- | --- |
| `agent/go-service/ims/` | Custom 组件与缓存 |
| `assets/resource/pipeline/IMS/` | Pipeline：接口分文件 |
| `assets/resource/image/IMS/item/` | 物品模板图（`*_TEMPLATE.png`） |
| `tools/SupplyPlan/mask_ims_item_corner.py` | 模板左上角涂绿工具（协议空间奖励角标） |
| `tools/schema/components/ims.schema.json` | 参数 JSON Schema |
| `tools/schema/custom.recognition.schema.json` | 注册 Recognition 并引用上述 Schema |
| `tools/schema/custom.action.schema.json` | 注册 Action 并引用上述 Schema |

### Pipeline 文件划分

| 文件 | 内容 |
| --- | --- |
| `ItemDataReady.json` | R2 `ItemDataReady` + `EnsureItemDataReady*`（未就绪时调 `SyncItemData`） |
| `ItemQuantitySatisfied.json` | R1 `ItemQuantitySatisfied`（调用方覆盖 `item` / `quantity`） |
| `UpdateItemQuantity.json` | A1 `UpdateItemQuantity`（调用方覆盖 `item` / `delta`） |
| `SyncItemData.json` | A2 入口 `SyncItemData`（任意位置 → 培养素材页 → 扫描） |
| `AddItemData.json` | A3 最佳实践：`CloseRewardsButton` 下累加识别数量 → 关闭奖励 |
| `common.json` | 通用品质色 ColorMatch |
| `item/*.json` | 各培养道具：品质色 → 模板 → 数量（灰色文字 And OCR）；模板匹配开启 `green_mask` |

## 物品模板绿幕

协议空间奖励界面物品图标左上角常有角标，会干扰培养素材页裁出的模板匹配。因此：

1. 用工具把 `assets/resource/image/IMS/item/*_TEMPLATE.png` **左上角 31×18** 涂为 RGB `(0, 255, 0)`。
2. 对应 `__*_TEMPLATE` 节点开启 `"green_mask": true`，匹配时跳过绿色区域。

```bash
python tools/SupplyPlan/mask_ims_item_corner.py
# 预览：python tools/SupplyPlan/mask_ims_item_corner.py --dry-run
```

新增物品模板入库前应先跑该工具，并在 TemplateMatch 上保留 `green_mask`。

## Recognition：`ItemDataReady`

判断库存缓存是否可用。业务上只读；冷启动首次访问可能一次性从磁盘 hydrate 到内存。

### 命中条件

1. 已有至少一次成功同步（`hasData`；后续由 A2 写入）。
2. 未超过 `refresh_days` 指定的 TTL。`0` 表示有数据后不因过期失效；无数据时仍未命中。

未命中原因（日志 `reason`）：`no_data` / `stale`。

### 参数

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `refresh_days` | `0` / `1` / `7` / `30` | `7` | 同步后的有效天数；`0` = 永不因过期失效 |

省略 `custom_recognition_param` 或省略字段时，按默认 `7` 天。

### Pipeline 示例

```json
"ItemDataReady": {
    "recognition": {
        "type": "Custom",
        "param": {
            "custom_recognition": "ItemDataReady",
            "custom_recognition_param": {
                "refresh_days": 7
            }
        }
    },
    "pre_delay": 0,
    "post_delay": 0,
    "rate_limit": 0
}
```

使用 IMS 的任务入口应主动调用 `SyncItemData`（同 Resource 仅首次真正扫库）。仅在「过期才同步」时用 `EnsureItemDataReadyMain`。

## Recognition：`ItemQuantitySatisfied`

判断缓存中指定物品数量是否满足要求。业务上只读（冷启动可 hydrate）；**不检查就绪**（未同步时缺失物品按 0）。需要「数据就绪且数量满足」时，用 `And` 同时引用 `ItemDataReady` 与本识别。

### 命中条件

缓存数量 `>= quantity`。未命中原因（日志 `reason`）：`insufficient`。

对比时会向 UI Focus 输出当前库存与目标（`ims.quantity_ok` / `ims.quantity_short`），相同文案约 10 秒内节流，避免调度扫描刷屏。

### 参数

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `item` | `string` | 必填 | 物品 ID（与 `SyncItemData.items` / `IMS.json` 键一致） |
| `quantity` | `integer` | 必填 | 要求的最小数量（含等号）；`>= 0` |

### Pipeline 示例

```json
"MyItemEnough": {
    "recognition": {
        "type": "Custom",
        "param": {
            "custom_recognition": "ItemQuantitySatisfied",
            "custom_recognition_param": {
                "item": "PROTODISK",
                "quantity": 10
            }
        }
    },
    "pre_delay": 0,
    "post_delay": 0,
    "rate_limit": 0
}
```

也可直接覆盖节点 `ItemQuantitySatisfied` 的 `custom_recognition_param`。

## Action：`UpdateItemQuantity`

在已知获得/消耗后，对缓存做增量修正，避免立刻整表重扫。**不改变就绪状态**（`hasData` / `last_sync` 仅由 A2 更新）；结果下限为 `0`。

### 参数

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `item` | `string` | 必填 | 物品 ID |
| `delta` | `integer` | 必填 | 有符号变化量（正数增加、负数减少） |

成功后写回内存并更新 `debug/record/IMS.json` 中的 `items`（保留原同步时间戳）。

### Pipeline 示例

```json
"GainOneProtoDisk": {
    "recognition": "DirectHit",
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "UpdateItemQuantity",
            "custom_action_param": {
                "item": "PROTODISK",
                "delta": 1
            }
        }
    },
    "pre_delay": 0,
    "post_delay": 0,
    "rate_limit": 0
}
```

## A2：`SyncItemData`

业务侧**只调用** Pipeline 节点 `SyncItemData`：任意界面 → 培养素材页 → Custom Action `SyncItemData`。

> [!IMPORTANT]
> **同 Resource 只同步一次**：`SyncItemDataRun` 成功后会经 `SyncItemDataLock` 用 Resource 级 `PipelineOverride` 关闭 `SyncItemDataBegin`，且**不恢复**。后续任务仍应主动调用 `SyncItemData`，但会命中 `SyncItemDataSkipped` 直接跳过。重新加载 Resource / 新开客户端后恢复默认可同步。
> 需要「缓存过期才同步」时用 `EnsureItemDataReadyMain`（未就绪才会进入 `SyncItemData`；一旦某次 A2 成功，后续同样走上述锁定）。

### Action 参数

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `items` | `object` | 必填 | 字典：键=物品 ID，值=And 识别节点名（按键名排序执行） |
| `page_dedup` | `bool` | `false` | `false`：本轮结果整表创建；`true`：翻页去重，在已有缓存上按 ID **覆盖**数量 |

数量 = 识别节点 `box_index` 链最终指向的 OCR 子结果（物品 And → 通用数量 And → OCR）。物品 ID 取自 `items` 的键。

命中时 Focus 输出本地化物品名与数量（`ims.sync_item_found` + `ims.item.<ID>`）。

未命中的 item 跳过（`page_dedup=true` 时保留旧值）。全部跑完后写入 `debug/record/IMS.json`（含 `updated_at`）并更新内存缓存。

### 翻页用法

```text
第 1 页：page_dedup=false
翻页后：page_dedup=true（覆盖已见 ID，保留其它）
```

### 落盘格式

```json
{
    "updated_at": "2026-07-29T12:00:00Z",
    "items": {
        "ADVANCED_COGNITIVE_CARRIER": 12
    }
}
```

`EnsureItemDataReadyMain` 未就绪时直接 `next` 到 `SyncItemData`。

> [!NOTE]
> `SyncItemDataRun.items` 示例：`{"ADVANCED_COGNITIVE_CARRIER": "ADVANCED_COGNITIVE_CARRIER"}`。切换未选中培养页依赖 `SceneManager/ProgressionTabNotChoose.png`。

## A3：`AddItemData`

在**当前画面**依次跑 `items` 中的识别节点，把 OCR 到的数量作为**正增量**写入缓存（等同多次 `UpdateItemQuantity` 的 `+n`）。**不改变就绪状态**。

与 A2 区别：A2 是培养素材页整表绝对值同步并置就绪；A3 是把识别结果累加进库存（典型场景：领奖弹窗）。

> [!NOTE]
> 若 IMS 数据尚未初始化（从未成功 A2 / 磁盘无同步时间戳，`hasData=false`），A3 **仍会识别奖励并输出到 UI**，但**不写入缓存**，仍返回成功，避免阻断后续关奖励等流程。

### Action 参数

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `items` | `object` | 必填 | 字典：键=物品 ID，值=识别节点名（按键名排序执行） |

未命中或数量 `<= 0` 的项跳过。对外 Focus：

- 每种命中：`ims.add_item_found`（物品名 × 数量）
- 全部结束后汇总：`ims.add_item_summary`（种类数 + 列表）；无命中则 `ims.add_item_none`

> [!IMPORTANT]
> 培养素材页的 `IMS/item/*` 节点 ROI 可能不适用于奖励界面。业务侧应传入**适配当前画面**的识别节点。
> 奖励界面弹出常有入场动画：调用 A3 前应对奖励物品区域使用 `pre_wait_freezes`（协议空间见 `ProtocolSpaceRewardAddItemData`）。

### 最佳实践

在 `CloseRewardsButton` 可见时执行累加，再 `next` 点击关闭：

```json
"AddItemDataOnRewards": {
    "recognition": {
        "type": "And",
        "param": {
            "all_of": ["CloseRewardsButton"]
        }
    },
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "AddItemData",
            "custom_action_param": {
                "items": {
                    "PROTODISK": "PROTODISK"
                }
            }
        }
    },
    "next": ["AddItemDataCloseRewards"]
}
```

可直接参考 Pipeline 节点 `AddItemDataOnRewards` / `AddItemDataCloseRewards`。

### 与业务数量判断配合

需要「数据就绪且数量满足」时，用 `And` 同时引用 `ItemDataReady` 与 `ItemQuantitySatisfied`，避免把「未就绪」当成「数量不足去刷」。

使用 IMS 的任务入口应**主动**调用 `SyncItemData`（同 Resource 内仅首次真正扫库）。仅在「过期才同步」场景用 `EnsureItemDataReadyMain`。

## 缓存约定（已定）

- 会话内以进程内存为权威数据源；读写热路径不反复读盘。
- 进程冷启动后，首次 IMS 访问会把 `debug/record/IMS.json` **lazy hydrate** 进内存一次；之后仍只走内存。
- A2 / A1 / A3 成功写入时同步落盘，供下次冷启动恢复。
- `ClearCache`（测试 / 账号切换）清空内存并视为故意无数据，**不会**再从磁盘灌回。
- 「永不更新」≠ 永不扫描：无数据时仍会触发扫描。
- 缓存允许小幅偏差，靠周期同步纠偏。
- IMS 不区分写入来源；异常靠调用方日志排查。

## Go 辅助 API（测试）

| 函数 | 说明 |
| --- | --- |
| `ims.MarkSynced(at, items)` | 标记一次成功同步（置 `hasData`、更新时间戳与物品表） |
| `ims.ClearCache()` | 清空缓存（测试 / 账号切换等）；不会从磁盘重新加载 |
| `ims.ItemsSnapshot()` | 返回缓存物品数量副本 |

