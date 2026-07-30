# IMS 培养道具管理

IMS（Item Management System）在 go-service 进程内维护培养道具数量缓存，供任务启动门禁与数量判断使用。流程编排仍由 Pipeline 负责。

> [!NOTE]
> 当前已落地：`ItemDataReady`（R2）、`ItemQuantitySatisfied`（R1）、`UpdateItemQuantity`（A1）、`SyncItemData`（A2）、`AddItemData`（A3）。

## 实现位置

| 路径 | 说明 |
| --- | --- |
| `agent/go-service/ims/` | Custom 组件与缓存 |
| `assets/resource/pipeline/IMS/` | Pipeline：接口分文件 |
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
| `common.json` | 通用品质色（黄等）ColorMatch |
| `item/*.json` | 各培养道具识别节点 |

## Recognition：`ItemDataReady`

判断库存缓存是否可用。只读、无副作用。

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

入口检查可参考 `EnsureItemDataReadyMain`。

## Recognition：`ItemQuantitySatisfied`

判断缓存中指定物品数量是否满足要求。只读、无副作用；**不检查就绪**（未同步时缺失物品按 0）。需要「数据就绪且数量满足」时，用 `And` 同时引用 `ItemDataReady` 与本识别。

### 命中条件

缓存数量 `>= quantity`。未命中原因（日志 `reason`）：`insufficient`。

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

### Action 参数

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `items` | `object` | 必填 | 字典：键=物品 ID，值=And 识别节点名（按键名排序执行） |
| `page_dedup` | `bool` | `false` | `false`：本轮结果整表创建；`true`：翻页去重，在已有缓存上按 ID **覆盖**数量 |

数量 = 识别节点 `box_index` 指向的 OCR 子结果。物品 ID 取自 `items` 的键。

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

### Action 参数

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `items` | `object` | 必填 | 字典：键=物品 ID，值=识别节点名（按键名排序执行） |

未命中或数量 `<= 0` 的项跳过。对外 Focus：

- 每种命中：`ims.add_item_found`（物品名 × 数量）
- 全部结束后汇总：`ims.add_item_summary`（种类数 + 列表）；无命中则 `ims.add_item_none`

> [!IMPORTANT]
> 培养素材页的 `IMS/item/*` 节点 ROI 可能不适用于奖励界面。业务侧应传入**适配当前画面**的识别节点。

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

任务入口可跑 `EnsureItemDataReadyMain`，或在需要强制刷新时直接调 `SyncItemData`。

## 缓存约定（已定）

- 仅进程内内存；进程重启后视为无数据，需再次同步。
- 「永不更新」≠ 永不扫描：无数据时仍会触发扫描。
- 缓存允许小幅偏差，靠周期同步纠偏。
- IMS 不区分写入来源；异常靠调用方日志排查。

## Go 辅助 API（测试）

| 函数 | 说明 |
| --- | --- |
| `ims.MarkSynced(at, items)` | 标记一次成功同步（置 `hasData`、更新时间戳与物品表） |
| `ims.ClearCache()` | 清空缓存（测试 / 账号切换等） |
| `ims.ItemsSnapshot()` | 返回缓存物品数量副本 |

