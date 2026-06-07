# 开发手册 - 基建任务维护文档

本文说明 `DijiangRewards` 的文件分布与执行流程。  
设计核心是「总控中枢分发 + 子阶段回跳」：四阶段彼此独立，选项只改阶段入口或分支，不动主流程骨架。  
该文档更新于 2026 年 6 月 6 日（已同步 [#2003](https://github.com/MaaEnd/MaaEnd/pull/2003)「再次种植」触发点调整）。

## 文件路径

| 路径                                                                 | 作用                                    |
| -------------------------------------------------------------------- | --------------------------------------- |
| `assets/interface.json`                                              | 任务挂载（`dijiang_ship` / `daily` 组） |
| `assets/tasks/DijiangRewards.json`                                   | 任务入口、阶段开关、会客室与培养舱选项  |
| `assets/resource/pipeline/DijiangRewards/Entry.json`                 | 进入帝江号总控中枢                      |
| `assets/resource/pipeline/DijiangRewards/MainFlow.json`              | 总控中枢按顺序分发各子阶段              |
| `assets/resource/pipeline/DijiangRewards/FastCollect.json`           | 总控中枢一键收取产物 / 线索             |
| `assets/resource/pipeline/DijiangRewards/RecoveryEmotion.json`       | 好友助力恢复心情                        |
| `assets/resource/pipeline/DijiangRewards/ReceptionRoom.json`         | 会客室线索收集、交流、赠予              |
| `assets/resource/pipeline/DijiangRewards/Manufacturing.json`         | 制造舱收菜、补货、助力                  |
| `assets/resource/pipeline/DijiangRewards/GrowthChamber.json`         | 培养舱领奖、再次种植、选材培养          |
| `assets/resource/pipeline/DijiangRewards/NeedCredit.json`            | 供信用点商店联动的补信用子流程          |
| `assets/resource/pipeline/DijiangRewards/Template/Location.json`     | 各舱室界面定位                          |
| `assets/resource/pipeline/DijiangRewards/Template/TextTemplate.json` | 按钮与状态 OCR 模板                     |
| `assets/resource/pipeline/DijiangRewards/Template/Status.json`       | 红点、数量、库存等辅助识别              |
| `assets/locales/interface/*.json`                                    | 任务、选项与 focus 文案                 |

## 执行流程

1. 从任务入口进入帝江号总控中枢（`Entry.json`）。
2. 在总控中枢按固定顺序尝试各子阶段（`MainFlow.json`）；每完成一阶段即回到中枢，再继续下一项：
    - （可选）[一键收取](#一键收取)产物与线索
    - [恢复心情](#恢复心情)
    - [会客室](#会客室)
    - 制造舱：领取产出 → 补货 → 助力 → 退出
    - [培养舱](#培养舱选项)（选项覆盖最多）
3. 各阶段均不再命中后结束任务。

各阶段可由 `StageTaskSetting` 单独开关；默认走推荐全流程。

## 子阶段说明

### 一键收取

实现位于 `FastCollect.json`，在总控中枢直接点击「产物」「线索」快捷收取，无需进入对应舱室。  
由 `StageTaskSetting` → `FastCollect` 开关控制，默认关闭。

### 恢复心情

总控中枢识别「需要助力」红点后进入助力界面：使用助力点 → 选择心情有空缺的干员 → 心情已满或点数用尽则收尾 → 回总控中枢。

### 会客室

进入会客室后依次尝试：处理交流结束弹窗 → 收集线索 → 接收线索 → 放置/替换线索 →（可选）开始线索交流 → 退出。

线索库存满时走[线索赠予](#线索赠予)分支，不是独立顶层阶段。  
是否主动「开始线索交流」由 `AutoStartExchange` 控制，默认关闭，留给信用点商店联动。

### 制造舱

进入后依次：领取产出 → 补货 → 使用助力 → 退出。维护重点在按钮识别稳定性，选项覆盖较少。

### 培养舱

默认行为：领取成熟奖励 → 普通选材培养 → 退出。「再次种植」默认关闭，须由选项显式开启。

详情页循环：领奖 →（可选）再次种植 →（可选）进入选材列表 → 找目标 → 确认培养或提取基核 → 回详情页继续。  
选材逻辑几乎全部由[培养舱选项](#培养舱选项)覆盖，是维护重点。

## 特殊处理

### 线索赠予

实现位于 `ReceptionRoom.json`。线索溢出时进入赠予流程：识别线索种类与库存数量 → 选出达到阈值的线索 → 结合好友缺失颜色或发送按钮完成赠予。

| 配置                     | 行为                                                   |
| ------------------------ | ------------------------------------------------------ |
| `ClueSetting=No`（默认） | 单次最多赠 3 次；每种线索库存 ≥ 3 才送（保留 2 个）    |
| `ClueSetting=Yes`        | 展开 `ClueSend`、`ClueStockLimit` 自定义次数与库存阈值 |

次数限制改赠予循环的 `max_hit`；库存阈值改数量 OCR 正则。

### 培养舱选项

实现位于 `GrowthChamber.json` + `DijiangRewards.json` 的 `pipeline_override`。

#### `SelectToGrow`：培养大方向

| 模式     | 实际行为                                                                                                                               |
| -------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| 不培养   | 只收成熟奖励，不进选材                                                                                                                 |
| 再次种植 | 关闭普通培养；领奖关闭后尝试「再次种植」并确认（[#2003](https://github.com/MaaEnd/MaaEnd/pull/2003) 后由领奖关闭触发，非详情页直接点） |
| 任意材料 | 白名单为全材料；展开排序与提取基核子选项；先排序再挑列表中第一个可用目标                                                               |
| 具体材料 | 白名单收窄到该材料多语言名；只展开提取基核；行识别绑定到目标整行，降低数量 OCR 抖动                                                    |

#### `AutoExtractSeed`：缺基核时怎么办

仅在「任意材料」或「具体材料」时出现。

| 配置 | 实际行为                                                                              |
| ---- | ------------------------------------------------------------------------------------- |
| 是   | 接受「有基核」或「有本体可提取」的目标；缺基核时走提取分支                            |
| 否   | 筛选收紧为必须有基核；误入提取入口则退回列表继续找（兼作连续种植后误返回的 fallback） |

#### `SortBy` / `SortOrder`

仅在「任意材料」时出现，只影响候选列表顺序，不改变「找谁」的语义。

维护培养舱问题时先确认三件事：当前 `SelectToGrow` 模式、是否开了排序、 `AutoExtractSeed` 是否改变了可接受目标范围。

### 选项层级

```text
DijiangRewards
├── AutoStartExchange          # 会客室是否主动开线索交流
├── StageTaskSetting           # 展开阶段细分开关
│   ├── FastCollect            # 一键收取
│   ├── RecoveryEmotionStage
│   ├── ReceptionRoomStage
│   ├── ManufacturingStage
│   └── GrowthChamberStage
├── ClueSetting                # 展开线索赠送次数 / 库存阈值
└── SelectToGrow               # 培养舱主模式
    ├── Any → AutoExtractSeed, SortBy, SortOrder
    └── 具体材料 → AutoExtractSeed
```

## 新增培养材料时需改的路径

1. `assets/tasks/DijiangRewards.json` — `SelectToGrow` 新增 case：`GrowthChamberSelectTarget.expected` + 行识别覆盖
2. `assets/locales/interface/*.json` — 材料名称文案
3. 若游戏按钮/舱室文案变化 — 同步 `Template/TextTemplate.json`、`Template/Location.json`

## 维护要点

| 现象             | 优先查                                                  |
| ---------------- | ------------------------------------------------------- |
| 进不了总控中枢   | `Entry.json`、SceneManager 跳转                         |
| 某阶段不执行     | `StageTaskSetting` 下对应阶段开关                       |
| 会客室不赠线索   | `ClueSetting=No` 默认覆盖是否与高级项一致               |
| 领奖后没再次种植 | `SelectToGrow=GrowAgain`；领奖关闭后的 next 链          |
| 培养点错材料     | `SelectToGrow` 白名单；`SortBy`/`SortOrder`（任意模式） |
| 有本体却不提取   | `AutoExtractSeed` 与 `CheckTargetNotEmpty` 联动覆盖     |
| OCR 识别漂移     | `Template/` 下三文件的多语言 `expected`                 |

维护时分三层：主流程层（去哪一舱）→ 阶段业务层（舱内做什么）→ 界面配置层（选项改哪些分支）。
