---
name: autocollect-add-route
description: 新增或改写 MaaEnd 的 AutoCollect 自动采集路线说明。默认包含路线文件创建、AutoCollect 主入口接线、任务选项注册和多语言文案补充。适用于“参考现有 AutoCollect 路线新建一条采集线”“给定传送点与路径点位生成 AutoCollectRouteX.json 并注册到任务中”“用户已经写好路线文件，只需要补注册接口”等场景。注意：本 skill 仅适用于参考 `assets/resource/pipeline/AutoCollect/AutoCollectRoute6.json` 的单段 `MapNavigateAction` 路线，不适用于 `MapTrackerMove` 路线，也不再处理多段 `GotoFindN` 路线；定位节点必须使用 `MapLocateAssertLocation`。
argument-hint: 如果是新建路线，推荐直接提供任务名（如“路线6：四号谷地-轻红柱状菌”）、第一个传送点（如 `SceneEnterWorldValleyIVTheHub2`）、断言点和完整 path 点列，以及“点击采集”或“挖掘类路线”。如果用户已经有路线文件、只需要注册接口，则只提供任务名即可。
---

# AutoCollect 新增路线

## 目的

在 MaaEnd 现有的 AutoCollect 体系中，支持两类常见工作：

- 新建一条完整可用的自动采集路线，并同步完成相关注册
- 在已有 `AutoCollectRouteX.json` 的前提下，只补齐注册接口和多语言文案

默认涉及的文件包括：

- `assets/resource/pipeline/AutoCollect/AutoCollectRouteX.json`
- `assets/resource/pipeline/AutoCollect.json`
- `assets/tasks/AutoCollect.json`
- `assets/locales/interface/*.json`

这里的“新路线建立”仅指参考 `AutoCollectRoute6.json` 的单段路线，也就是：

- 定位节点使用 `MapLocateAssertLocation`
- 导航节点固定为单个 `AutoCollectRouteXGoto`
- `MapNavigateAction` 的采集点通过同一条 `path` 中的动作点控制
- 最终 `next` 直接指向 `AutoCollectRouteXEnd`

## 适用范围

以下场景使用本 skill：

- 参考现有 AutoCollect 单段路线新增一条 `AutoCollectRouteX.json`
- 用户提供传送点和路径点位，要求生成并注册一条单段 AutoCollect 路线
- 用户已经写好 `AutoCollectRouteX.json`，只要求补齐 `AutoCollect.json`、`assets/tasks/AutoCollect.json` 和 locale 注册
- 用户要求补齐 AutoCollect 路线的任务注册、多语言文案和主入口接线

以下场景不适用本 skill：

- `EnvironmentMonitoring` 路线
- `AutoEssence` 路线
- `AutoEcoFarm` 路线
- Go Service 算法扩展
- 使用 `MapTrackerMove` 的 AutoCollect 路线
- 任何多段 `GotoFind1/2/3...` 结构的 AutoCollect 路线

## 开始前先读什么

优先阅读并对照以下文件：

1. `assets/resource/pipeline/AutoCollect/AutoCollectRoute6.json`
2. `docs/zh_cn/developers/components/map-navigator.md`
3. `assets/resource/pipeline/AutoCollect.json`
4. `assets/tasks/AutoCollect.json`
5. `assets/locales/interface/zh_cn.json`

必要时补充阅读：

- `assets/locales/interface/zh_tw.json`
- `assets/locales/interface/en_us.json`
- `assets/locales/interface/ja_jp.json`
- `assets/locales/interface/ko_kr.json`
- `assets/resource/pipeline/Interface/SceneValleyIV.json`
- 其他 `SceneManager` / `Interface` 相关文件

## 先判断任务类型

动手前，先判断当前需求属于哪一种：

### 1. 新建路线文件

适用于：用户还没有 `AutoCollectRouteX.json`，希望你生成路线文件并完成注册。

这一类需要完整参数：

- 任务名
- 第一个传送点
- 断言点
- 完整路径点列
- 采集类型

### 2. 仅注册接口

适用于：用户已经有现成的 `AutoCollectRouteX.json`，只希望把它接入任务入口和选项。

这一类默认只需要：

- 任务名

然后直接完成以下注册：

- `assets/resource/pipeline/AutoCollect.json`
- `assets/tasks/AutoCollect.json`
- `assets/locales/interface/*.json`

不需要再追问传送点、断言点、path 或采集类型。

## 必要参数

### 场景 A：新建路线文件

默认流程下，以下 5 类信息是必需的：

1. 任务名
2. 第一个传送点
3. 断言点与完整路径点列
4. 采集类型

### 场景 B：仅注册接口

默认只需要 1 项：

1. 任务名

### 1. 任务名

- 示例：`路线6：四号谷地-轻红柱状菌`
- 必须能从任务名里解析出路线编号
- `路线6` 将决定：
    - 文件名：`AutoCollectRoute6.json`
    - 节点前缀：`AutoCollectRoute6...`
    - 子入口：`AutoCollectRoute6Sub`
    - 任务 case：`Route6`
    - locale 键：`option.AutoCollectRoute6.label`

### 2. 第一个传送点

- 示例：`SceneEnterWorldValleyIVTheHub2`
- 它决定 `Start.next` 中的 `[JumpBack]SceneEnterWorldXxx`
- 仅在“新建路线文件”场景需要

### 3. 断言点与完整路径点列

- 断言点用于生成 `MapLocateAssertLocation.target`
- 完整路径点列用于生成单个 `AutoCollectRouteXGoto.action.param.custom_action_param.path`
- 用户通常会把采集点标记成 `[x, y, true]`
- 本 skill 需要在写入前把这些 `true` 转换成具体动作字符串
- 仅在“新建路线文件”场景需要

### 4. 采集类型

- 点击采集路线：将 `[x, y, true]` 转换为 `[x, y, "COLLECT"]`
- 挖掘类路线：将 `[x, y, true]` 转换为 `[x, y, "DIG"]`
- 仅在“新建路线文件”场景需要

### 5. 是否需要一并注册接口

- 如果用户明确说“只写路线文件，不注册”，那就只生成路线文件
- 如果用户没有特别说明，默认同时完成注册

## 缺少参数时的处理

### 新建路线文件时

遇到以下情况时，先向用户确认，不要直接生成路线：

- 没有任务名，无法确定路线编号和文件命名
- 任务名不符合“路线X：xxx”格式，无法安全解析编号
- 没有第一个传送点，无法确定 `[JumpBack]SceneEnterWorldXxx`
- 没有断言点，无法生成 `MapLocateAssertLocation.target`
- 没有完整路径点列，无法生成 `path`
- 没有采集类型，无法把 `true` 转换成 `COLLECT` 或 `DIG`

推荐追问方式尽量简短，例如：

- “还缺任务名，请给我类似 `路线6：四号谷地-轻红柱状菌` 的名称。”
- “还缺第一个传送点，请给我类似 `SceneEnterWorldValleyIVTheHub2` 的入口名。”
- “还缺断言点，请给我传送后用于定位的坐标。”
- “还缺完整 path 点列，请把整条单段路线的点位发我。”
- “还缺采集类型，请确认是点击采集还是挖掘路线。”

### 仅注册接口时

遇到以下情况时，先向用户确认，不要直接注册：

- 没有任务名，无法确定路线编号和注册键名
- 任务名不符合“路线X：xxx”格式，无法安全解析编号

只要任务名齐全，就直接执行注册，不需要追问其它路线参数。

## 核心判断规则

### 1. 先判断是“新建路线”还是“只注册”

优先观察用户给出的关键信息：

- 如果用户明确说“已经有路线文件，只注册接口”，则直接走注册流程
- 如果用户给出 `AutoCollectRouteX.json` 文件路径并要求接入任务，也按“只注册”处理
- 如果用户要求生成 `path`、断言点或整条路线文件，则走“新建路线”流程

### 2. 只处理 `Route6` 风格单段路线

优先观察用户给出的关键信息：

- 如果用户明确说“参考 Route6”，通常就是本 skill 的目标场景
- 如果路线使用 `MapNavigateAction + MapLocateAssertLocation + AutoCollectRouteXGoto`，说明符合本 skill 范围
- 如果用户要求拆成 `GotoFind1/2/3...`，说明这不属于本 skill 的范围，应先澄清
- 如果路线使用 `MapTrackerMove`，说明这不属于本 skill 的主流程，应先澄清

本 skill 仅处理以下结构：

- `AutoCollectRouteXAssertLocation`
- `AutoCollectRouteXGoto`
- `AutoCollectRouteXEnd`

不要在本 skill 中生成以下结构：

- `AutoCollectRouteXGotoFind1`
- `AutoCollectRouteXGotoFind2`
- 任意 `anchor` 串联的多段节点
- `MapTrackerMove`

### 3. 传送入口与断言点不要混淆

用户可能会说“传送点参考某条路线”，这通常只表示：

- 复用该路线的 `SceneEnterWorldXxx`
- 复用同一区域的大地图入口

这不一定表示复用旧路线的断言坐标。

如果用户另外提供了“第一个点用于传送后定位”或显式给出了定位坐标，则：

- `AutoCollectRouteXAssertLocation` 必须使用用户新给的坐标
- 不要继续沿用参考路线原本的 `target`

### 4. 首个断言点与导航路径要分离

如果用户明确说明：

- “第一个点只是定位点”
- “使用 `MapLocateAssertLocation` 进行判断”

则：

- 首个坐标先视为断言点 `[x, y]`，再换算为 `target: [x-10, y-10, 20, 20]`
- 不要把这个点再塞回 `path` 里
- 不要写成 `MapTrackerAssertLocation.expected` 结构；这里应直接写 `zone_id` 和 `target`

### 5. 把用户输入的 `true` 转换成动作字符串

根据 `map-navigator.md`，MapNavigator 采集语义使用动作字符串，而不是布尔值：

- `COLLECT`: 采集点
- `DIG`: 挖掘点

因此当用户给出 `[x, y, true]` 时，本 skill 应按采集类型做归一化：

- 点击采集路线：`[x, y, true]` -> `[x, y, "COLLECT"]`
- 挖掘类路线：`[x, y, true]` -> `[x, y, "DIG"]`

如果用户已经直接给出 `[x, y, "COLLECT"]` 或 `[x, y, "DIG"]`，则沿用原值，不要重复改写。

### 6. 动作点保留在单段 `path` 中

如果用户给出的路径中包含采集点标记，则应：

- 保留在同一个 `path` 数组中
- 保持单个 `AutoCollectRouteXGoto` 节点
- 不要因为出现多个采集点就拆成 `GotoFindN`
- 不要再使用旧的 `anchor + AutoCollectClickStart` / `AutoCollectDigStart` 链式写法

## 需要整理出的信息

### 新建路线文件时

动手前，尽量整理齐以下字段：

| 字段             | 说明                                                               |
| ---------------- | ------------------------------------------------------------------ |
| `TaskLabel`      | 任务名，例如 `路线6：四号谷地-轻红柱状菌`                          |
| `RouteId`        | 从 `TaskLabel` 解析出的编号，例如 `6`                              |
| `RouteFile`      | 目标文件名，例如 `AutoCollectRoute6.json`                          |
| `TemplateRoute`  | 参考路线，例如 `Route6`                                            |
| `EnterWorldNode` | 第一个传送点，例如 `SceneEnterWorldValleyIVTheHub2`                |
| `AssertTarget`   | 断言框坐标；若断言点为 `[x, y]`，默认换算为 `[x-10, y-10, 20, 20]` |
| `ZoneId`         | 通常来自 `path` 第一项 `{"action":"ZONE","zone_id":"..."}`         |
| `ActionType`     | 固定为 `MapNavigateAction`                                         |
| `CollectType`    | `Click` 或 `Dig`，决定 `true` 转成 `COLLECT` 还是 `DIG`            |
| `GotoNode`       | 固定为 `AutoCollectRouteXGoto`                                     |
| `Path`           | 完整单段路径；采集动作通过 `"COLLECT"` / `"DIG"` 标记表达          |

其中以下 4 项应优先确认：

- `TaskLabel`
- `EnterWorldNode`
- `AssertTarget`
- 原始完整路径点列

### 仅注册接口时

动手前至少确认：

- `TaskLabel`
- `RouteId`
- `RouteFile`
- `RouteSub`
- `RouteCase`
- `LocaleKey`

## 标准实施步骤

### 场景 A：新建路线文件

#### 第一步：从任务名解析路线编号

任务名必须形如：

- `路线6：四号谷地-轻红柱状菌`
- `路线12：武陵城-某某资源`

解析规则：

1. 从任务名开头提取 `路线X`
2. 得到 `RouteId = X`
3. 用 `RouteId` 推导以下命名：
    - `AutoCollectRouteX.json`
    - `AutoCollectRouteXStart`
    - `AutoCollectRouteXEnd`
    - `AutoCollectRouteXAssertLocation`
    - `AutoCollectRouteXGoto`
    - `AutoCollectRouteXSub`
    - `RouteX`
    - `option.AutoCollectRouteX.label`

如果任务名不满足上述格式，先让用户补一个合规名称，再继续。

#### 第二步：归一化用户给出的路径点

在生成 JSON 前，先按以下规则整理原始 path：

- 保留首个 `ZONE` 节点不变
- 保留普通坐标点 `[x, y]` 不变
- 如果用户给的是 `[x, y, true]`：
    - 点击采集路线改成 `[x, y, "COLLECT"]`
    - 挖掘类路线改成 `[x, y, "DIG"]`
- 如果用户给的是 `[x, y, "COLLECT"]` 或 `[x, y, "DIG"]`，直接保留
- 不要把 `true` 原样写进最终 `path`

#### 第三步：生成 `AutoCollectRouteX.json`

文件骨架固定如下：

```json
{
    "AutoCollectRouteXStart": { ... },
    "AutoCollectRouteXEnd": { ... },
    "AutoCollectRouteXAssertLocation": { ... },
    "AutoCollectRouteXGoto": { ... }
}
```

不要生成任何 `GotoFindN` 节点。

##### `Start` 节点规则

- `next` 第一个目标是 `AutoCollectRouteXAssertLocation`
- `next` 第二个目标是对应的 `[JumpBack]SceneEnterWorldXxx`
- `focus.Node.Recognition.Succeeded` 使用中文，例如：`开始路线X`

##### `End` 节点规则

保持：

```json
"pre_delay": 0,
"post_delay": 0
```

##### `AssertLocation` 节点规则

- 必须使用 `MapLocateAssertLocation`
- `desc` 使用中文，例如：`传送到采集点`
- `recognition` 固定写成字符串：`"Custom"`
- `custom_recognition` 固定写成：`"MapLocateAssertLocation"`
- `zone_id` 不作为独立入参单独向用户追问，优先从 `path` 的第一个 `ZONE` 推导
- 如果用户给的是断言点 `[x, y]`，默认换算为 `[x-10, y-10, 20, 20]`
- 仅当用户明确给出其它容差矩形时，才不要使用上述默认换算
- `action` 固定为 `"DoNothing"`
- 不要写 `expected` 数组
- 不要写 `map_name`
- `next` 固定指向 `AutoCollectRouteXGoto`

标准结构参考当前 `Route6`：

```json
{
    "AutoCollectRouteXAssertLocation": {
        "desc": "传送到采集点",
        "recognition": "Custom",
        "custom_recognition": "MapLocateAssertLocation",
        "custom_recognition_param": {
            "zone_id": "ValleyIV_Base",
            "target": [
                520,
                687,
                20,
                20
            ]
        },
        "action": "DoNothing",
        "next": [
            "AutoCollectRouteXGoto"
        ]
    }
}
```

##### `Goto` 节点规则

本 skill 新建路线固定使用 `MapNavigateAction`：

- `action.type = "Custom"`
- `action.param.custom_action = "MapNavigateAction"`
- `action.param.custom_action_param.path` 第一项必须是：

```json
{
    "action": "ZONE",
    "zone_id": "..."
}
```

节点内容规则：

- 节点名固定为 `AutoCollectRouteXGoto`
- `desc` 使用中文，例如：`前往采集点`
- `path` 为完整单段路线
- 路径内的采集点应使用 `"COLLECT"` 或 `"DIG"`
- `next` 直接指向 `AutoCollectRouteXEnd`
- 不要写 `anchor`
- 不要拆分为多个导航节点
- 不要把 `[x, y, true]` 直接保留到最终 JSON 中

#### 第四步：按需注册接口

如果用户未明确要求“只生成路线文件”，则默认继续完成注册：

- `assets/resource/pipeline/AutoCollect.json`
- `assets/tasks/AutoCollect.json`
- `assets/locales/interface/*.json`

### 场景 B：仅注册接口

#### 第一步：从任务名解析注册命名

从任务名推导：

- `RouteFile`: `AutoCollectRouteX.json`
- `RouteSub`: `AutoCollectRouteXSub`
- `RouteCase`: `RouteX`
- `LocaleKey`: `option.AutoCollectRouteX.label`

#### 第二步：注册到 `assets/resource/pipeline/AutoCollect.json`

必须同步修改两处：

- 在 `AutoCollectStart.next` 中加入 `"[JumpBack]AutoCollectRouteXSub"`
- 新增 `AutoCollectRouteXSub` 节点并指向 `AutoCollectRouteXStart`

#### 第三步：注册到 `assets/tasks/AutoCollect.json`

必须同步修改两处：

- 如项目默认勾选所有路线，则把 `RouteX` 追加到 `default_case`
- 在 `cases` 中新增 `RouteX` 对应项

#### 第四步：补充 `assets/locales/interface/*.json`

至少新增以下语言：

- `zh_cn`
- `zh_tw`
- `en_us`
- `ja_jp`
- `ko_kr`

统一新增键：

```json
"option.AutoCollectRouteX.label": "..."
```

## 文案规范

路线文件内的提示文案应遵守以下规则：

- `focus.Node.Recognition.Succeeded` 用中文
- `desc` 用中文
- `AssertLocation` 必须使用 `MapLocateAssertLocation`
- `Goto` 使用单段命名和单段文案

推荐写法：

- `Start.focus`: `开始路线X`
- `AssertLocation.desc`: `传送到采集点`
- `Goto.desc`: `前往采集点`

不要写英文内部提示，例如：

- `Start Route X`
- `Arrive at collection point`
- `Go to collection point`

## 检查清单

提交前检查：

### 新建路线文件时

- [ ] 任务名满足“路线X：xxx”格式
- [ ] `RouteId` 是从任务名解析得到的
- [ ] 新路线文件名、节点名、选项名、文案键一致
- [ ] `Start` 使用了正确的 `[JumpBack]SceneEnterWorldXxx`
- [ ] 新路线使用的是 `MapNavigateAction`，而不是 `MapTrackerMove`
- [ ] `AssertLocation` 使用了新的断言点，没有误复用旧坐标
- [ ] `AssertLocation` 使用的是 `MapLocateAssertLocation` 的扁平结构，而不是 `MapTrackerAssertLocation.expected`
- [ ] 若首个点仅用于断言，则没有混入 `path`
- [ ] 路线只有一个 `AutoCollectRouteXGoto`
- [ ] 用户给出的 `[x, y, true]` 已按采集类型转换成 `"COLLECT"` 或 `"DIG"`
- [ ] 最终 `path` 中不再残留布尔 `true` 作为采集标记
- [ ] 没有出现 `GotoFindN` 或 `anchor`

### 需要注册接口时

- [ ] `AutoCollect.json` 已接入 `AutoCollectRouteXSub`
- [ ] `assets/tasks/AutoCollect.json` 已注册 `RouteX`
- [ ] 5 份 locale 已新增 `option.AutoCollectRouteX.label`
- [ ] `focus` / `desc` 使用中文
- [ ] 文档和文件均为 UTF-8，无乱码

## 验证建议

### 新建路线文件时

至少做以下验证：

1. 检查路线执行顺序是否为：传送 -> 位置断言 -> 单段导航/采集 -> 结束
2. 检查 `path` 中的采集点都已从 `true` 正确转换为 `"COLLECT"` 或 `"DIG"`
3. 检查最终结束链路是否正确

### 只注册接口时

至少做以下验证：

1. 检查新增路线是否能在 `AutoCollectRoutes` 中显示
2. 检查勾选 `RouteX` 后是否能启用 `AutoCollectRouteXSub`

静态验证时，优先确认以下文件能被正常解析：

- `assets/resource/pipeline/AutoCollect.json`
- `assets/tasks/AutoCollect.json`
- `assets/locales/interface/*.json`

如果同时新建路线文件，再额外确认：

- `assets/resource/pipeline/AutoCollect/AutoCollectRouteX.json`

## 输出模板

完成后，优先按以下结构汇报：

```markdown
## 已完成

- 新增或复用路线文件：`AutoCollectRouteX.json`
- 已接入 `assets/resource/pipeline/AutoCollect.json`
- 已接入 `assets/tasks/AutoCollect.json`
- 已补充 `assets/locales/interface/*.json`

## 关键实现

- 从任务名 `路线X：...` 解析出路线编号并推导命名
- 如果需要新建路线：复用 `SceneEnterWorld...` 作为传送入口
- 如果需要新建路线：使用 `MapLocateAssertLocation` 校验断言点换算得到的 `[x-10, y-10, 20, 20]`
- 如果需要新建路线：将用户输入的 `[x, y, true]` 按采集类型转换为 `"COLLECT"` 或 `"DIG"`
- 已完成 `AutoCollect.json` / `assets/tasks/AutoCollect.json` / locale 注册

## 校验

- 核心 JSON 解析通过 / 未通过
- 已确认 `RouteX` 注册链路存在
```

## 约束

- 不要对现有 `RouteX` 做无关重构
- 不要为了“更通用”而提前抽象自动生成器，除非用户明确要求
- 不要把点击采集路线改成挖掘路线，或反过来
- 不要把本 skill 的新路线建立流程扩展到 `MapTrackerMove`
- 不要把定位节点写回 `MapTrackerAssertLocation`
- 不要省略任务注册与多语言注册，除非用户明确说不需要
- 不要把内部提示文案写成英文
- 不要生成任何多段 `GotoFindN` 结构
- 不要把用户输入的 `[x, y, true]` 原样写入最终 `path`
- 如果用户已经有路线文件且只要求注册接口，不要继续追问传送点、断言点、path 或采集类型
