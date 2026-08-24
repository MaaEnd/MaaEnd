# Pipeline 生成数据

MaaEnd 使用四份由 zmdmap 数据 CI 生成并发布的精简游戏数据：

- `data/delivery_jobs.json`：地区、仓储节点及其可装箱物品；
- `data/delivery_destinations.json`：仓储和送货终点的多语言文本、坐标与归属关系；
- `data/environment_monitoring.json`：监测终端、观察点与五语言名称；
- `data/sell_product.json`：据点、可售物品、据点特性与匹配干员。

这些文件都是按任务需要裁剪出的游戏数据，不是完整的上游数据快照，也不包含数据生产端的版本或来源元数据。MaaEnd 通过 `data/version.txt` 单独记录当前数据版本，`fetch-data.mjs` 根据 zmdmap 版本接口下载四份文件；版本未变化且本地缓存完整时跳过下载，网络检查失败时保留当前缓存。

```text
TableCfg → data/scripts/*.py（zmdmap 数据 CI）→ zmdmap 发布精简游戏数据
         → fetch-data.mjs（MaaEnd）→ data/*.json → 各任务生成器
```

```shell
pnpm fetch:zmdmap
node tools/pipeline-generate/fetch-data.mjs --force
node tools/pipeline-generate/fetch-data.mjs --cache-bust
```

完整生成某个任务时使用对应的 pnpm 命令。四个命令都会先同步 zmdmap 精简游戏数据，再执行当前任务自己的生成链路：

```shell
pnpm generate:AutoDelivery
pnpm generate:DeliveryJobs
pnpm generate:EnvironmentMonitoring
pnpm generate:SellProduct
```

## 精简游戏数据生成脚本

`data/scripts/` 中的四个 Python 入口供 zmdmap 数据 CI 使用，不是 MaaEnd 日常生成流程的一部分。它们彼此独立，只读取当前任务需要的 TableCfg、GameplayConfig 与 BaseNav，并输出对应的精简游戏数据：

```shell
uv run tools/pipeline-generate/data/scripts/delivery_jobs_data.py --table-cfg-dir C:\path\to\TableCfg
uv run tools/pipeline-generate/data/scripts/delivery_destinations_data.py --table-cfg-dir C:\path\to\TableCfg
uv run tools/pipeline-generate/data/scripts/environment_monitoring_data.py --table-cfg-dir C:\path\to\TableCfg
uv run tools/pipeline-generate/data/scripts/sell_product_data.py --table-cfg-dir C:\path\to\TableCfg
```

未指定 `--table-cfg-dir` 时默认读取当前用户的 `Downloads/TableCfg`。生成结果与已有文件相同时跳过写入；`--force` 可强制重写。`data/scripts/tablecfg_utils.py` 只负责本地文件读取、五语言文本引用、结果比较和 JSON 写入，任务业务规则分别留在四个入口中。

## 鸣谢

感谢 [zmdmap](https://zmdmap.com/) 为 MaaEnd 提供游戏数据整理与发布支持。
