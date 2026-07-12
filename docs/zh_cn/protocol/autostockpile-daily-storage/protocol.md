# AutoStockpile 本地每日商品价格记录 — 第三方读取协议

`AutoStockpile` 任务启用 `AutoStockpileAllowDataUpload` 后，Go Service 会在每轮商品识别成功、地区与服务器日解析完成后，将当轮识别到的商品价格写入 `debug/record/ElasticGoodsPrices.json`。该路径只用于本地文件记录，不会触发远程上传。

本文档定义文件格式与路径解析规则，供第三方工具（数据分析面板、Web 前端、用户）可靠读取。

---

## JSON Schema

文件顶层结构固定为：

```json
{
    "schema_version": 2,
    "records": [
        {
            "server_date": "2026-05-04",
            "weekday": 1,
            "utc_time": "2026-05-04T12:00:00Z",
            "region": "Wuling",
            "uid": "abc123def4567890",
            "goods": [
                {
                    "id": "Wuling/WulingFrozenPears.Tier1",
                    "name": "武陵冻梨",
                    "tier": "Wuling.Tier1",
                    "price": 1000
                }
            ]
        }
    ]
}
```

我们提供了相对应的 JSON Schema 文件，供第三方工具验证数据格式:
[daily_storage.schema.json](./daily_storage.schema.json)

### 顶层字段

- `schema_version: int`：Schema 版本号，当前固定为 `2`。协议升级时递增，只加字段不删不改旧字段。
- `records: array`：价格记录数组，元素见下。

### `records[]` 字段

- `server_date: string`：服务器日期，格式 `YYYY-MM-DD`。按目标时区 `04:00` 边界计算（`04:00 ~ 次日 03:59` 属同一服务器日）。
- `weekday: int`：服务器日对应的星期，`1`=周一 ~ `7`=周日。
- `utc_time: string`：记录写入时的 UTC 时间，RFC 3339 格式（如 `2026-05-04T12:00:00Z`）。
- `region: string`：地区标识。当前可能值：`Wuling`（武陵）、`ValleyIV`（四号谷地）。
- `uid: string`：玩家标识。由游戏内 UID 数字部分经 SHA256 加盐哈希后取前 16 位十六进制。不可逆。无有效 UID 时回退为 `"unknown"`。
- `goods: array`：本轮识别到的商品数组，元素见下。

### `goods[]` 字段

- `id: string`：商品内部 ID，格式 `{Region}/{BaseName}.Tier{N}`，如 `Wuling/WulingFrozenPears.Tier1`。
- `name: string`：商品中文名称，如"武陵冻梨"。
- `tier: string`：价值变动幅度标识，格式 `{Region}.Tier{N}`，如 `Wuling.Tier1`。
- `price: int`：商品当轮识别到的价格（弹性需求物资单价）。

### 读写约束

- 记录只包含 `server_date`、`weekday`、`utc_time`、`region`、`uid` 和 `goods`，**不包含** `quota` 或其他额外用户数据。第三方不得假设其他字段存在。
- **不包含旧字段** `captured_at_utc`。`schema_version ≥ 2` 的文件中该字段已废弃。
- 同一 `server_date + region + uid` 的新记录会**覆盖**旧记录。不同 `uid` 在同一天同一地区的记录独立保留。
- 最多保留 **120 个不同的** `server_date`。超出时丢弃最早的日期及其所有地区记录。
- 写入使用同目录临时文件 + rename 的**原子写**流程。读取方在任何时刻读取都不会读到半截文件。
- 写入失败只记录 warning 日志并继续 AutoStockpile，不会中止任务。

---

## 路径解析

### 目标文件

```text
debug/record/ElasticGoodsPrices.json
```

路径为相对于 MaaEnd 工作目录的固定路径，不再进行向上查找或环境变量解析。目录不存在时由 `MkdirAll` 自动创建。

### 原子写入流程

1. 在同一目录下创建临时文件（命名模式 `.{ElasticGoodsPrices.json}.*.tmp`）
2. 写入完整 JSON 内容
3. `chmod` 设置权限为 `0644`
4. `Sync` 刷盘
5. `Close` 关闭文件句柄
6. `os.Rename` 原子替换目标文件

读取方可安全地在任何时刻读取 `ElasticGoodsPrices.json`，不会读到部分写入的内容。

### 写入失败行为

- 写入失败只记录 **warning 级别日志**
- 不会中止 AutoStockpile 任务流程
- 不会重试

---

## UID 哈希算法

第三方如需验证或比对 UID，算法如下：

1. 从游戏内 UID 的 OCR 结果中提取所有连续数字片段
2. 将所有数字片段拼接为一个字符串（如 `"123456789"`）
3. 计算 `SHA256(数字字符串 + "AutoStockpile")`
4. 取十六进制摘要的前 16 个字符作为最终 UID

无法提取有效数字时，UID 为 `"unknown"`。

> 该哈希为**不可逆加盐摘要**，无法从文件中的 UID 反推游戏内原始 UID。

---

## 服务器日计算

- **默认时区**：`UTC+8`
- **日边界**：`04:00`（即每天 `04:00 ~ 次日 03:59` 属于同一服务器日）
- **weekday 映射**：Go 标准库 `time.Weekday` 映射为 `1`（周一）到 `7`（周日）

用户可通过 `AutoStockpileServerTime` 任务选项覆盖时区偏移。当前选项映射为：国服/亚服 `UTC+8`、美服/欧服 `UTC-5`。

---

## 版本兼容性

- `schema_version` 字段供用户先读取以决定解析策略。
- 版本递增时只增加新字段，不修改已有字段语义。
- `schema_version: 1` 的记录中 `uid` 可能为空字符串；Go Service 在读取后会自动将空 `uid` 规范化为 `"unknown"` 再写回。第三方读取时也应该兼容空 `uid`。
