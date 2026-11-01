---
name: autocollect-add-route
description: 新增或改写 MaaEnd 的 AutoCollect 自动采集路线。默认包含路线文件创建、在路线登记表中登记，并运行生成器产出调度节点、任务选项与多语言文案。适用于“参考现有 AutoCollect 路线新建一条采集线”“给定传送点与路径点位生成路线文件并注册到任务中”“用户已经写好路线文件，只需要登记”等场景。注意：本 skill 仅适用于参考 `assets/resource/pipeline/AutoCollect/ValleyIV/ValleyIVPinkBolete1.json` 的单段 `MapNavigateAction` 路线，不处理多段 `GotoFindN` 路线；定位节点必须使用 `MapLocateAssertLocation`。
argument-hint: 新建路线时提供地区（`ValleyIV` 或 `Wuling`）、采集物（如“星门菌”或 `item_plant_mushroom_2_2`）、第一个传送点（如 `SceneEnterWorldValleyIVTheHubRockhillPassage`）、断言点和完整 path 点列，以及“点击采集”或“挖掘类路线”。如果用户已经有路线文件、只需要登记，则只提供路线文件路径即可。
---

# AutoCollect 新增路线

## 目的

在 MaaEnd 现有的 AutoCollect 体系中，支持两类常见工作：

- 新建一条完整可用的自动采集路线，并完成登记
- 在已有路线文件的前提下，只完成登记

任务的调度节点、采集物选项和多语言文案全部由路线登记表生成，不手改。涉及的文件：

- `assets/resource/pipeline/AutoCollect/<地区>/<路线 id>.json`：路线文件，手写或由工具生成
- `tools/AutoCollectRoute/routes.json`：路线登记表，手写
- `assets/resource/pipeline/AutoCollect/Dispatch.json`、`assets/tasks/AutoCollect.json`、`assets/locales/interface/*.json`：由 `uv run autocollect-route generate` 生成

这里的“新路线”仅指参考 `ValleyIVPinkBolete1.json` 的单段路线：

- 定位节点使用 `MapLocateAssertLocation`
- 导航节点固定为单个 `AutoCollect<路线 id>Goto`
- `MapNavigateAction` 的采集点通过同一条 `path` 中的动作点控制
- 最终 `next` 直接指向 `AutoCollect<路线 id>End`

在地图上框选采集物自动生成路线时，优先让用户使用 `uv run autocollect-route`（见 `tools/AutoCollectRoute/README.md`），它会一次完成路线文件、登记与生成。

## 适用范围

以下场景使用本 skill：

- 参考现有 AutoCollect 单段路线新增一条路线
- 用户提供传送点和路径点位，要求生成并登记一条单段 AutoCollect 路线
- 用户已经写好路线文件，只要求登记到任务中
- 同一采集物再增加一条路线

以下场景不适用本 skill：

- `EnvironmentMonitoring` 路线
- `AutoEssence` 路线
- `AutoEcoFarm` 路线
- Go Service 算法扩展
- 任何多段 `GotoFind1/2/3...` 结构的 AutoCollect 路线

## 开始前先读什么

1. `assets/resource/pipeline/AutoCollect/ValleyIV/ValleyIVPinkBolete1.json`
2. `tools/AutoCollectRoute/routes.json`
3. `tools/AutoCollectRoute/README.md` 的「路线登记表」一节
4. `docs/zh_cn/developers/components/map-navigator.md`

采集物 id 与英文名查 `tools/zmdmap/data/collect_points.json` 的 `types`：`item` 是采集物 id，`item_names.en_us` 是英文名。稀有与常规由生成器按采集物自动归类，不用手填。

## 路线 id 与命名

路线 id = 地区 + 采集物英文名（PascalCase，去掉空格与符号）+ 该组合下的序号：

- 武陵第一条星门菌（`Cosmagaric`）路线：`WulingCosmagaric1`；再加一条是 `WulingCosmagaric2`
- 一条线覆盖多种采集物时按 `items` 顺序拼接：`WulingBloodcapWulingstone1`
- 序号取 `routes.json` 中同一前缀已有的最大序号加 1

路线 id 决定：

- 文件：`assets/resource/pipeline/AutoCollect/<地区>/<路线 id>.json`
- 节点前缀：`AutoCollect<路线 id>`，如 `AutoCollectWulingCosmagaric2Start`
- `Start` 的 focus 文案键：`$task.AutoCollect.route.<路线 id>.label`

生成器会校验 id 是否与地区、`items` 一致，不一致时直接报错。

## 必要参数

### 场景 A：新建路线文件

1. 地区：`ValleyIV` 或 `Wuling`
2. 采集物：名称或 id，可以有多种；稀有与常规采集物不能放在同一条路线
3. 第一个传送点：`SceneEnterWorldXxx` 入口名
4. 断言点与完整路径点列
5. 采集类型：点击采集或挖掘

### 场景 B：仅登记

只需要已有路线文件的路径。从文件名得到路线 id，从所在目录得到地区，再向用户确认覆盖哪些采集物。

## 缺少参数时的处理

缺少以下任一项时先向用户确认，不要直接生成路线：

- 地区或采集物，无法确定路线 id
- 第一个传送点，无法写 `Start` 的 `SubTask`
- 断言点，无法生成 `MapLocateAssertLocation.target`
- 完整路径点列，无法生成 `path`
- 采集类型，无法把 `true` 转换成 `COLLECT` 或 `DIG`

追问尽量简短，例如：“还缺第一个传送点，请给我类似 `SceneEnterWorldValleyIVTheHubRockhillPassage` 的入口名。”

## 核心判断规则

### 1. 只处理单段路线

本 skill 仅生成以下结构：

- `AutoCollect<路线 id>Start`
- `AutoCollect<路线 id>AssertLocation`
- `AutoCollect<路线 id>Goto`
- `AutoCollect<路线 id>End`

用户要求拆成 `GotoFind1/2/3...` 或使用 `anchor` 串联多段节点时，不属于本 skill 范围，先澄清。

### 2. 传送入口与断言点不要混淆

用户说“传送点参考某条路线”，通常只表示复用该路线的 `SceneEnterWorldXxx`，不表示复用旧路线的断言坐标。用户另外给出定位坐标时，`AssertLocation` 必须使用新坐标。

### 3. 首个断言点与导航路径分离

用户说明“第一个点只是定位点”时：

- 首个坐标视为断言点 `[x, y]`，换算为 `target: [x-10, y-10, 20, 20]`
- 不要把这个点再放进 `path`
- `MapLocateAssertLocation` 直接写 `zone_id` 和 `target`，不要包一层 `expected` 数组

### 4. 把用户输入的 `true` 转换成动作字符串

- 点击采集路线：`[x, y, true]` -> `[x, y, "COLLECT"]`
- 挖掘类路线：`[x, y, true]` -> `[x, y, "DIG"]`

用户已经给出 `"COLLECT"` 或 `"DIG"` 时沿用原值。多个采集点都保留在同一个 `path` 里，不要拆成 `GotoFindN`，也不要使用旧的 `anchor + AutoCollectClickStart` / `AutoCollectDigStart` 链式写法。

## 标准实施步骤

### 第一步：确定路线 id

按「路线 id 与命名」得到路线 id，并确认 `routes.json` 里没有同名路线。

### 第二步：生成路线文件（场景 A）

文件骨架：

```json
{
    "AutoCollect<路线 id>Start": { ... },
    "AutoCollect<路线 id>End": { ... },
    "AutoCollect<路线 id>AssertLocation": { ... },
    "AutoCollect<路线 id>Goto": { ... }
}
```

`Start` 节点：

- `"enabled": false`，由任务选项按勾选启用
- `action` 为 `"Custom"`，`custom_action` 为 `"SubTask"`，第一个传送点放进 `custom_action_param.sub`
- `next` 指向 `AutoCollect<路线 id>AssertLocation`
- `focus.Node.Recognition.Succeeded` 为 `$task.AutoCollect.route.<路线 id>.label`

`End` 节点保持 `"pre_delay": 0, "post_delay": 0`。

`AssertLocation` 节点：

- `desc` 写 `传送到采集点`
- `recognition` 写 `"Custom"`，`custom_recognition` 写 `"MapLocateAssertLocation"`
- `zone_id` 从 `path` 第一项 `ZONE` 推导，不单独追问
- 断言点 `[x, y]` 默认换算为 `[x-10, y-10, 20, 20]`，用户给出其它容差矩形时按用户的写
- `action` 为 `"DoNothing"`，`next` 指向 `AutoCollect<路线 id>Goto`

`Goto` 节点：

- `desc` 写 `前往采集点`
- `action.type` 为 `"Custom"`，`action.param.custom_action` 为 `"MapNavigateAction"`
- `path` 第一项是 `{"action": "ZONE", "zone_id": "..."}`，之后是完整单段路线，采集点用 `"COLLECT"` / `"DIG"`
- `next` 指向 `AutoCollect<路线 id>End`
- 不写 `anchor`，不拆分导航节点

结构以 `ValleyIVPinkBolete1.json` 为准。

### 第三步：登记路线

在 `tools/AutoCollectRoute/routes.json` 的 `routes` 末尾追加：

```json
{
    "id": "WulingCosmagaric2",
    "region": "Wuling",
    "items": [
        "item_plant_mushroom_2_2"
    ]
}
```

新的稀有采集物默认目标库存不是 32 时，在 `default_limits` 中补一项；常规采集物默认 0，不用写。

### 第四步：生成任务注册

```powershell
uv run autocollect-route generate
```

它会重写 `Dispatch.json`、`assets/tasks/AutoCollect.json` 和五份 locale，并校验登记表。报错时按提示修正路线文件或登记表，不要手改生成的文件。

## 检查清单

- [ ] 路线 id 与地区、`items` 一致，文件放在对应地区目录
- [ ] 节点名都以 `AutoCollect<路线 id>` 开头
- [ ] `Start` 使用了正确的传送入口，focus 指向 `$task.AutoCollect.route.<路线 id>.label`
- [ ] `AssertLocation` 使用 `MapLocateAssertLocation` 的扁平结构和新的断言点
- [ ] 若首个点仅用于断言，则没有混入 `path`
- [ ] 路线只有一个 `Goto`，没有 `GotoFindN` 或 `anchor`
- [ ] `path` 中没有残留布尔 `true` 作为采集标记
- [ ] `routes.json` 已登记，`uv run autocollect-route generate` 运行成功
- [ ] `desc` / focus 文案使用中文

## 验证建议

1. 检查路线执行顺序：传送 -> 位置断言 -> 单段导航/采集 -> 结束
2. 检查生成后的 `assets/tasks/AutoCollect.json` 中，对应地区的采集物选项里出现了该采集物，勾选它会启用 `AutoCollect<路线 id>Start`
3. 检查 `Dispatch.json` 中该采集物的调度节点 `next` 含 `[JumpBack]__AutoCollect<路线 id>Run`
4. 新路线需经实机运行确认

## 输出模板

完成后按以下结构汇报：

````markdown
## 已完成

- 路线文件：`assets/resource/pipeline/AutoCollect/<地区>/<路线 id>.json`
- 已登记到 `tools/AutoCollectRoute/routes.json`
- 已运行 `uv run autocollect-route generate`

## 关键实现

- 路线 id 由地区、采集物英文名与序号组成
- 使用 `MapLocateAssertLocation` 校验断言点换算得到的 `[x-10, y-10, 20, 20]`
- 将用户输入的 `[x, y, true]` 按采集类型转换为 `"COLLECT"` 或 `"DIG"`

## 校验

- 生成器运行通过 / 未通过
- 采集物选项与调度节点已包含新路线
````

## 约束

- 不要对现有路线做无关重构
- 不要手改 `Dispatch.json`、`assets/tasks/AutoCollect.json` 中生成的部分和生成的 locale 文案
- 不要把点击采集路线改成挖掘路线，或反过来
- 不要把内部提示文案写成英文
- 不要生成任何多段 `GotoFindN` 结构
- 不要把用户输入的 `[x, y, true]` 原样写入最终 `path`
- 用户已经有路线文件且只要求登记时，不要继续追问传送点、断言点、path 或采集类型
