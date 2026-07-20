# 售卖产品

据点数据通过 zmdmap API 获取，存储在 `tools/pipeline-generate/data/` 目录。

`model.mjs` 统一维护据点命名和国际化键；生成器各自消费最小数据投影：

- `pipeline-data.mjs`：Win32 Pipeline 据点与识别框；
- `pipeline-adb-data.mjs`：ADB Pipeline 据点与识别框；
- `sell-data.mjs`：区域售卖入口与区域内据点列表；
- `session-data.mjs`：自动干员会话的据点注册；
- `task-data.mjs`：Task 中按缓存强制刷新、售卖优先级、保留规则、地区/据点排列的选项；
- `selection-data.mjs`：把上游贸易数据预计算为 `assets/data/SellProduct/selection_data.json`，供 Go Service 运行时使用。

据点 `LocationId` 由 zmdmap 英文名称自动派生；只有存在实际 OCR 误识证据时才在 `model.mjs` 追加识别候选。某个模板独有的参数留在对应投影文件中。

```shell
# 在仓库根目录运行（自动拉取最新数据并生成）
pnpm generate:SellProduct

# 仅更新数据文件
pnpm fetch:zmdmap

# 使用已缓存的数据补齐五语言据点和干员键
node tools/pipeline-generate/SellProduct/sync-locales.mjs

# 使用已缓存的数据生成部署所需的最小选品数据
node tools/pipeline-generate/SellProduct/selection-data.mjs

# 等价于在当前目录运行
npx @joebao/maa-pipeline-generate --config pipeline-config.json
npx @joebao/maa-pipeline-generate --config sell-config.json
npx @joebao/maa-pipeline-generate --config session-config.json
npx @joebao/maa-pipeline-generate --config task-config.json
# 需要生成安卓端（ADB）专用流水线时使用
npx @joebao/maa-pipeline-generate --config pipeline-adb-config.json
```

`pnpm generate:SellProduct` 会在渲染前根据 `settlement_trade.json` 按游戏据点顺序重排五语言 locale 的据点键，据点名始终覆盖为 zmdmap 当前官方译文，并补齐缺失的据点和干员键；随后生成随应用发布的 `selection_data.json`。

`task-template.jsonc` 的任务选项依次为启用干员自动切换、优先售卖配置、物品保留规则和地区/据点售卖开关。启用干员自动切换默认开启，关闭后跳过售前切换、售后生产派驻与缓存扫描，售卖本身不受影响；强制刷新干员缓存作为其子选项，仅在开启时出现。售后会为所有启用据点重新计算生产干员分配。优先售卖配置包含 6 个槽位，用户指定的物品按槽位 1 至 6 排在默认顺序之前；不属于当前据点的配置会跳过，重复配置采用最靠前的槽位。`selection_data.json` 按每个据点的稀有度、单价降序记录默认顺序。Pipeline 持续回环，直到据点券耗尽或没有剩余候选；某据点确认物品缺货后，会通过据点锚点把该 `itemId` 标记为本次任务全局缺货，使后续据点直接跳过，下一次任务初始化时自动清空。

干员缓存按 UID 保存完整扫描快照。当前 UID 没有快照时，Pipeline 会先扫描 `operators` 中的全部干员，再开始售卖；存在快照时直接使用。启用强制刷新后，本次任务始终重新扫描完整干员表。只有首次建立或主动强制刷新的全局扫描可以写入缓存；据点内找人的局部扫描只服务于当次流程，不会覆盖已有快照。

`SellProductCache.json` 在每个 UID 的账号分区中同时保存 `operators` 和 `locations`，两者都使用 `selection_data.json` 中的稳定 ID。账号键直接使用 CaptureUID 已生成的 16 位小写十六进制加盐哈希，未捕获时使用 `unknown`，SellProduct 不再做可能发生碰撞的二次规范化。`operators` 是由 `updated_at` 与 `ids` 组成的完整扫描快照：字段缺失或为 `null` 表示尚未完整扫描，`ids` 为空数组才表示完整扫描后没有相关干员。干员扫描时间只随完整快照刷新，不会被 `locations` 的据点发展值更新改写。缓存不设置格式版本，也不迁移旧版 `SellProductOwnedOperators.json`、扁平干员数组或旧中文缓存；JSON 损坏、顶层结构不兼容或顶层包含未知字段时整份缓存视为不存在，顶层结构正常时则只丢弃 UID、账号字段、时间或 ID 不合法的账号分区，其他账号缓存继续使用。任务初始化时会加载上次确认的 `locations`，使第一个据点开始规划时便能为后续未满据点预留发展值加成干员；未缓存的据点按未满处理。每次进入据点仍以右下角图标的最新识别结果为准，状态变化会立即更新当前会话和缓存，后续尚未完成的据点自动按新状态重新规划，已经完成的售后生产派驻保持锁定。

物品保留规则提供 6 个独立槽位，每个槽位选择具体货品和最低保留数量。物品 case 通过 `attach` 提供 `item_id`，数量 input 通过 `custom_action_param.quantity` 提供整数。动态选品确认成功后记录实际 `itemId`，保留规则据此生效；数量 `0` 表示全部可售，同一物品重复配置时后面的槽位覆盖前面的槽位。

`selection_data.json` 的 `names` 映射提供五语言 OCR 候选和 UI 展示名，Go 按固定语言顺序展开并去重 OCR 候选。售卖候选同时保留据点发展值（Outpost Prosperity）未满和已满时的两套加成档位。Pipeline 通过右下角据点发展值图标判断当前据点仍可获得发展值：未满时优先同时具备发展值与交易收益加成的干员；只有单一有效词条时，发展值加成优先于交易收益加成。已满时忽略发展值加成，只比较交易收益。“最高加成档”再与当前据点的售后生产候选取交集形成完美候选，因此据点发展值已满时，具备交易收益和售后生产两个有效词条的干员也属于完美匹配。账号存在完美候选时只从该集合规划，没有时才回退到当前据点的最高加成档。同档候选优先沿用当前派驻，需要更换时由全局生产分配方案决定，并优先形成后续任务可直接沿用的稳定派驻。

运行期 UI 日志在任务复用完整干员缓存时输出该账号缓存的本地更新时间；确认进入据点时输出当前据点的售卖干员与售后生产派驻目标、货品顺序和保留规则，随后在状态确认节点输出干员实际沿用或切换、货品切换、交易完成、派驻冲突后的重新规划、完整扫描后的新干员方案、售后派驻跳过和关键扫描失败。Pipeline 的固定提示引用 interface i18n，Go Service 根据 `selection_data.json` 和 go-service i18n 输出当前语言的据点、干员、货品及动态状态。

据点开关同时控制 `SellProductRegisterLocation{LocationId}`，启用据点构成自动恢复分配和派驻冲突接管的规划范围。售卖始终按“四号谷地 → 武陵”以及各地区内生成模型的固定据点顺序进行。完美候选被其他启用据点占用时会确认调至当前据点；被未启用据点占用或来源无法识别时会取消切换并临时排除，避免干扰用户自己的派驻。自动干员选择和售卖后恢复由“启用干员自动切换”任务开关控制，默认开启。

## 致谢

- 感谢 `zmdmap` 提供的数据
