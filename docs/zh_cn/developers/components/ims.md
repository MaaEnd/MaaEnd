# IMS 培养道具管理

IMS（Item Management System）在 go-service 进程内维护培养道具数量缓存，供任务启动门禁与数量判断使用。流程编排仍由 Pipeline 负责。

> [!NOTE]
> 当前已落地：`ItemDataReady`（R2）、`SyncItemData`（A2）。数量增减（A1）、数量是否满足（R1）待后续接入。

## 实现位置

| 路径 | 说明 |
| --- | --- |
| `agent/go-service/ims/` | Custom 组件与缓存 |
| `assets/resource/pipeline/IMS/` | Pipeline：四接口分文件 |
| `tools/schema/components/ims.schema.json` | 参数 JSON Schema |
| `tools/schema/custom.recognition.schema.json` | 注册 `ItemDataReady` 并引用上述 Schema |

### Pipeline 文件划分

| 文件 | 内容 |
| --- | --- |
| `ItemDataReady.json` | R2 `ItemDataReady` + `EnsureItemDataReady*`（未就绪时调 `SyncItemData`） |
| `ItemQuantitySatisfied.json` | R1 占位 |
| `UpdateItemQuantity.json` | A1 占位 |
| `SyncItemData.json` | A2 入口 `SyncItemData`（任意位置 → 培养素材页 → 扫描） |
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

## A2：`SyncItemData`

业务侧**只调用** Pipeline 节点 `SyncItemData`：任意界面 → 培养素材页 → Custom Action `SyncItemData`。

### Action 参数

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `items` | `string[]` | 必填 | And 识别节点名，按顺序执行 |
| `page_dedup` | `bool` | `false` | `false`：本轮结果整表创建；`true`：翻页去重，在已有缓存上按 ID **覆盖**数量 |

物品 ID = 该 And 节点 `box_index` 指向的**数量子节点名**；数量 = 该子结果 OCR。

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
        "SomeQtyNode": 12
    }
}
```

`EnsureItemDataReadyMain` 未就绪时直接 `next` 到 `SyncItemData`。

> [!NOTE]
> `SyncItemDataRun` 里 `items` 需填入实际 And 节点名（或由任务 PipelineOverride 注入）。切换未选中培养页依赖 `SceneManager/ProgressionTabNotChoose.png`。

### 与业务数量判断配合

需要「数据就绪且数量满足」时，用 `And` 同时引用 `ItemDataReady` 与后续 R1，避免把「未就绪」当成「数量不足去刷」。

任务入口可跑 `EnsureItemDataReadyMain`，或在需要强制刷新时直接调 `SyncItemData`。

## 缓存约定（已定）

- 仅进程内内存；进程重启后视为无数据，需再次同步。
- 「永不更新」≠ 永不扫描：无数据时仍会触发扫描。
- 缓存允许小幅偏差，靠周期同步纠偏。
- IMS 不区分写入来源；异常靠调用方日志排查。

## Go 辅助 API（给后续 A2 / 测试）

| 函数 | 说明 |
| --- | --- |
| `ims.MarkSynced(at, items)` | 标记一次成功同步（置 `hasData`、更新时间戳与物品表） |
| `ims.ClearCache()` | 清空缓存（测试 / 账号切换等） |

## 规划中的接口

| 类型 | 名称 | Pipeline 文件 | 作用 |
| --- | --- | --- | --- |
| Action | `UpdateItemQuantity` | `UpdateItemQuantity.json` | 按物品名增量改缓存（占位） |
| Action | `SyncItemData` | `SyncItemData.json` | 进培养素材页并扫描写入 `IMS.json` |
| Recognition | `ItemQuantitySatisfied` | `ItemQuantitySatisfied.json` | 只读比较缓存数量与要求值（占位） |
| Recognition | `ItemDataReady` | `ItemDataReady.json` | 缓存是否就绪 |
