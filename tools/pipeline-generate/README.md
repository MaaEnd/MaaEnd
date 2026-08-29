# Pipeline 生成数据

MaaEnd 使用四份由 zmdmap 数据 CI 生成并发布的精简游戏数据：

- `data/delivery_jobs.json`：地区、仓储节点及其可装箱物品；
- `data/delivery_destinations.json`：仓储和送货终点的多语言文本、坐标与归属关系；
- `data/environment_monitoring.json`：监测终端、观察点与五语言名称；
- `data/sell_product.json`：据点、可售物品、据点特性与匹配干员。

这些文件都是按任务需要裁剪出的游戏数据，不是完整的上游数据快照，也不包含数据生产端的版本或来源元数据。MaaEnd 通过 `data/version.txt` 单独记录当前数据版本，`fetch-data.mjs` 根据 zmdmap 版本接口下载四份文件；版本未变化且本地缓存完整时跳过下载，网络检查失败时保留当前缓存。

```text
BeyondTableCfg/TableCfg ─┐
                        ├→ data/scripts/*.py（本地生成）→ data/*.json
BeyondMemoryPack/JsonData┘

BeyondTableCfg + BeyondMemoryPack → endfield-workflow（CI）→ zmdmap 发布精简游戏数据
                                                        → fetch-data.mjs（MaaEnd）
                                                        → data/*.json → 各任务生成器
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

`data/scripts/` 中的四个 Python 入口用于从本地解包仓库生成精简游戏数据，与
`endfield-workflow` 中的 CI 生成逻辑保持一致。默认目录按当前 MaaEnd 仓库的兄弟目录解析：

- `../BeyondTableCfg/TableCfg`；
- `../BeyondMemoryPack/JsonData`；
- 送货点坐标另外读取 MaaEnd 当前 `assets/resource/model/map/navmesh/base.nav.gz`。

因此三个仓库位于同一 `Endfield` 目录时，无需先复制文件到下载目录，也无需传入路径：

```shell
uv run tools/pipeline-generate/data/scripts/delivery_jobs_data.py
uv run tools/pipeline-generate/data/scripts/delivery_destinations_data.py
uv run tools/pipeline-generate/data/scripts/environment_monitoring_data.py
uv run tools/pipeline-generate/data/scripts/sell_product_data.py
```

仓库不在默认相对位置时，可用 `--table-cfg-dir` 覆盖 TableCfg；送货点脚本还可用
`--gameplay-config-dir`、`--level-data-dir` 和 `--nav` 分别覆盖 BeyondMemoryPack 与
BaseNav 路径。四个入口都支持 `--output`，可将结果写入临时目录做本地/CI 等价校验。
生成结果与已有文件相同时跳过写入；`--force` 可强制重写。
`data/scripts/tablecfg_utils.py` 只负责本地仓库路径、五语言文本引用、结果比较和 JSON
写入，任务业务规则分别留在四个入口中。

## 鸣谢

感谢 [zmdmap](https://zmdmap.com/) 为 MaaEnd 提供游戏数据整理与发布支持。
