# 转交委托

使用 `MAA-pipeline-generate` 从地区、仓储节点与可装箱物品模型生成转交委托的 Pipeline 和任务配置。

`tools/pipeline-generate/data/delivery_jobs.json` 是 zmdmap 数据 CI 从 TableCfg 裁剪并发布的精简游戏数据，MaaEnd 通过
`fetch-data.mjs` 下载。文件包含全部仓储节点及其可装箱物品；统一的数据流与来源边界见[生成数据总览](../README.md)。

## 运行方式

在仓库根目录运行：

```bash
pnpm generate:DeliveryJobs

# 仅同步 zmdmap 精简游戏数据
pnpm fetch:zmdmap
```

生成内容：

- `assets/resource/pipeline/DeliveryJobs.json`：通用任务入口与地区调度；
- `assets/resource/pipeline/DeliveryJobs/Region/*.json`：各地区入口、循环与界面判定；
- `assets/resource/pipeline/DeliveryJobs/Depot/**/*.json`：每个仓储节点的任务、货物识别和进入节点；
- `assets/tasks/DeliveryJobs.json`：地区、仓储节点处理方式和装箱物品选项。

每个仓储节点提供五种处理方式：

- **接取并转交**：装箱、接取任务，并在返回仓储节点后转交；
- **按报价处理**：识别当前选中的报价，并分别按“达到或高于阈值时”和“低于阈值时”的配置执行；
- **仅接取委托**：完成装箱并接取任务，不转交，返回仓储节点后继续处理其他委托；
- **仅装箱货物**：完成装箱后从调度申请界面返回，不接取任务；
- **不处理**：跳过该仓储节点已有的送货任务和装箱入口。

按报价处理默认以整数 `119000`（即 11.9 万）为阈值。“达到或高于阈值时”默认接取并转交，“低于阈值时”默认仅接取委托；两侧均可改为“接取并转交”“仅接取委托”或“不处理”。“仅接取委托”会在接取后返回仓储节点继续遍历，“不处理”会关闭报价页并继续遍历。报价 OCR 失败时会停在报价页并提示用户，不会自动接取。以后接入自动送货时，可在共用的报价动作选项中增加对应动作，让两个报价分支按需选择。

阈值输入虽然只允许数字，但 `pipeline_type` 必须保持为 `string`，因为它会被插入完整的 `ExpressionRecognition.expression` 字符串；设为 `int` 会尝试把整个表达式转换为整数，最终得到 `null`。

“仅装箱货物”遇到已有待运送货物时会关闭当前页面并继续遍历下一个仓储节点；其他需要接取或转交的模式仍会停止任务并提示先完成送货。

旧版的全局“仅接取任务”和“仅装箱货物”开关已由逐仓储节点选项取代，升级后的已有配置需要重新选择各节点的处理方式。

## 新增地区或仓储节点

1. 在 `model.mjs` 中补充地区或仓储节点，并确认五个 `assets/locales/interface/*.json` 中存在对应的
   `global.region.*` 文案。
2. 如果新地区支持指定装箱物品，在地区的 `FillItems` 中登记已有的 `item.*` key，并准备对应模板图。
3. 运行 `pnpm generate:DeliveryJobs`，然后依次运行 `pnpm check` 和 `pnpm test`。

生成的 Pipeline 和 Task 文件不应手工修改；流程级公共节点仍在 `PackCargo.json` 和 `TransferJob.json` 中维护。
