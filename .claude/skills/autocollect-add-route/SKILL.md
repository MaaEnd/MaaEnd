---
name: autocollect-add-route
description: 新增或改写 MaaEnd 的 AutoCollect 自动采集路线。默认包含路线文件创建、AutoCollect 主入口接线、任务选项注册和多语言文案补充。适用于“参考现有 AutoCollect 路线新建一条采集线”“给定传送点与路径点位生成 AutoCollectRouteX.json 并注册到任务中”等场景。注意：新路线建立仅适用于 MapNavigator 寻路，也就是 `MapNavigateAction` 风格；不适用于 `MapTrackerMove` 路线。定位节点必须使用 `MapLocateAssertLocation`，写法参考当前 `AutoCollectRoute6.json`。需要用户提供任务名，形式类似“路线6：四号谷地-轻红柱状菌”，并从其中的“路线6”解析路线编号、文件命名和注册键名。若缺少必要参数，先提醒并向用户询问。
argument-hint: 推荐直接提供：任务名（如“路线6：四号谷地-轻红柱状菌”）、第一传送点（如 `SceneEnterWorldValleyIVTheHub2`）、后续路径点列表，以及“点击采集”或“挖掘类路线”。若缺少这些必要参数，先提醒并向用户询问。
---

# AutoCollect 新增路线

## 目的

在 MaaEnd 现有的 AutoCollect 体系中，新增一条完整可用的采集路线，并默认同步完成以下修改：

- `assets/resource/pipeline/AutoCollect/AutoCollectRouteX.json`
- `assets/resource/pipeline/AutoCollect.json`
- `assets/tasks/AutoCollect.json`
- `assets/locales/interface/*.json`

这里的“新路线建立”仅指 `MapNavigator` 风格路线，也就是使用 `MapNavigateAction` 的 AutoCollect 路线；定位节点固定使用 `MapLocateAssertLocation`，字段组织与参数写法参考当前 `assets/resource/pipeline/AutoCollect/AutoCollectRoute6.json` 里的 `AutoCollectRoute6AssertLocation`。

## 适用范围

只要需求属于以下任一类型，就使用本 skill：

- 参考现有 AutoCollect 路线新增一条 `AutoCollectRouteX.json`
- 用户提供一串地图点位，希望生成并注册一条 AutoCollect 路线
- 用户要求补齐 AutoCollect 路线的任务注册与多语言文案

本 skill 不适用于：

- `EnvironmentMonitoring` 路线
- `AutoEssence` 路线
- `AutoEcoFarm` 路线
- Go Service 算法逻辑扩展
- `MapTrackerMove` 风格的 AutoCollect 新路线建立

## 先做什么

开始实现前，优先读取并对照以下文件：

1. `assets/resource/pipeline/AutoCollect/AutoCollectRoute1.json`
2. `assets/resource/pipeline/AutoCollect/AutoCollectRoute6.json`
3. `assets/resource/pipeline/AutoCollect/AutoCollectRoute5.json`
4. `assets/resource/pipeline/AutoCollect.json`
5. `assets/tasks/AutoCollect.json`
6. `assets/locales/interface/zh_cn.json`

必要时再补读：

- `assets/locales/interface/zh_tw.json`
- `assets/locales/interface/en_us.json`
- `assets/locales/interface/ja_jp.json`
- `assets/locales/interface/ko_kr.json`
- `assets/resource/pipeline/Interface/SceneValleyIV.json`
- 其他 `SceneManager` / `Interface` 相关文件

## 必要参数

默认流程下，以下 4 类参数视为必要参数：

1. 任务名
   - 例如：`路线6：四号谷地-轻红柱状菌`
   - 必须从任务名里解析出路线编号
   - `路线6` 决定：
     - 文件名：`AutoCollectRoute6.json`
     - 节点前缀：`AutoCollectRoute6...`
     - 子入口：`AutoCollectRoute6Sub`
     - 任务 case：`Route6`
     - locale 键：`option.AutoCollectRoute6.label`
2. 第一传送点
   - 例如：`SceneEnterWorldValleyIVTheHub2`
   - 它决定 `Start.next` 中的 `[JumpBack]SceneEnterWorldXxx`
3. 后续路径
   - 至少要能提供断言点和后续导航点
   - 如果用户给的是完整路径点列，要进一步判断第一个点是否只用于定位，以及 `true` 断点如何切段
4. 采集类型
   - 点击采集路线：`AutoCollectClickStart` + `AutoCollectClickAfter`
   - 挖掘类路线：`AutoCollectDigStart` + `AutoCollectDigAfter`

如果这些参数中任一缺失，不要自行拍脑袋补全，必须及时提醒并向用户询问。

## 缺失参数时的处理

遇到以下情况时，必须先向用户追问，而不是直接生成路线：

- 没有任务名，无法确定路线编号和文件命名
- 任务名不符合“路线X：xxx”形式，无法安全解析编号
- 没有第一传送点，无法确定 `SceneEnterWorldXxx`
- 没有后续路径，无法生成 `path`
- 没有说明是点击采集还是挖掘类路线，无法确定 `next` 和 `anchor`

推荐追问方式尽量简短，例如：

- “还缺任务名，请给我类似 `路线6：四号谷地-轻红柱状菌` 的名称。”
- “还缺第一传送点，请给我类似 `SceneEnterWorldValleyIVTheHub2` 的入口名。”
- “还缺后续路径点位，请把断言点和导航点列表发我。”
- “还缺采集类型，请确认这条是点击采集还是挖掘类路线。”

## 核心判断规则

### 1. 先判断是否属于本 skill 的 MapNavigator 范围

优先观察用户给出的关键信息：

- 如果用户明确说“参考路线3”，优先沿用 `Route3` 的组织方式
- 如果路径使用 `MapNavigateAction`，通常应参考 `Route1/2/3`
- 如果路径使用 `MapTrackerMove`，说明这不属于本 skill 的主流程，应先向用户澄清
- 如果用户要求 `next` 为 `AutoCollectClickStart`，说明是点击采集路线
- 如果用户要求 `next` 为 `AutoCollectDigStart`，说明是挖掘类路线

本 skill 的新路线建立只允许使用：

- `MapNavigateAction + AutoCollectClickStart`
- `MapNavigateAction + AutoCollectDigStart`

不要在本 skill 内为新路线建立使用：

- `MapTrackerMove + AutoCollectClickStart`
- `MapTrackerMove + AutoCollectDigStart`

### 2. 传送入口与判断点不要混淆

用户可能会说“传送点参考某条路线”，这通常只表示：

- 复用该路线的 `SceneEnterWorldXxx`
- 复用同区域 `map_name`

不一定表示复用该路线原本的断言坐标。

如果用户另外提供了“第一个点为传送判断点”或显式给出判断坐标：

- `AutoCollectRouteXAssertLocation` 必须使用用户新给的坐标
- 不要继续沿用参考路线的旧 `target`

### 3. 首个判断点与实际导航路径要分离

如果用户明确说：

- “第一个点为传送判断点”
- “使用 `MapLocateAssertLocation` 进行判断”

则：

- 第一个坐标先视为断言点 `[x, y]`，再换算成 `target: [x-10, y-10, 20, 20]`
- 不要把这个点再塞回第一段 `path`
- 不要写成 `MapTrackerAssertLocation` 那种 `expected -> map_name / target` 结构；这里应直接写 `zone_id` 和 `target`

### 4. 带 `true` 的点必须切段

如果用户给出的路径点中包含形如 `[x, y, true]` 的终点：

- 每个带 `true` 的点都必须作为一个独立采集段的结束点
- 下一段从该点后面的下一个坐标重新开始
- 每一段都要有独立节点：`AutoCollectRouteXGotoFind1/2/3...`
- 每一段都要通过 `anchor` 串到下一段

不要把多个 `true` 点保留在一个 `GotoFind` 节点里。

## 需要收集或推断的信息

在动手前，尽量把以下信息整理齐：

| 字段 | 说明 |
| --- | --- |
| `TaskLabel` | 任务名，例如 `路线6：四号谷地-轻红柱状菌` |
| `RouteId` | 从 `TaskLabel` 解析出的编号，例如 `6` |
| `RouteFile` | 目标文件名，例如 `AutoCollectRoute6.json` |
| `TemplateRoute` | 参考路线，例如 `Route3` |
| `EnterWorldNode` | 第一传送点，例如 `SceneEnterWorldValleyIVTheHub2` |
| `MapName` | 导航使用的地图名，例如 `map01_lv001` |
| `AssertTarget` | 断言框坐标；如果断言点为 `[x, y]`，则换算为 `[x-10, y-10, 20, 20]`，例如点 `[530, 697]` 对应 `[520, 687, 20, 20]` |
| `ZoneId` | 不单独向用户索取；默认从 `path` 第一项 `{"action":"ZONE","zone_id":"..."}` 推导，例如 `ValleyIV_Base` |
| `ActionType` | 固定为 `MapNavigateAction` |
| `CollectNext` | `AutoCollectClickStart` 或 `AutoCollectDigStart` |
| `CollectAnchor` | `AutoCollectClickAfter` 或 `AutoCollectDigAfter` |
| `PathSegments` | 拆分后的多段路径 |

其中以下 4 项必须优先确认：

- `TaskLabel`
- `EnterWorldNode`
- 原始路径点列
- `CollectNext` / `CollectAnchor`

## 标准实现步骤

### 第一步：从任务名解析路线编号

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
   - `AutoCollectRouteXGotoFind1`
   - `AutoCollectRouteXSub`
   - `RouteX`
   - `option.AutoCollectRouteX.label`

如果任务名不满足上述格式，先让用户补一个合规任务名，再继续。

### 第二步：生成 `AutoCollectRouteX.json`

文件骨架应保持和现有路线一致：

```json
{
    "AutoCollectRouteXStart": { ... },
    "AutoCollectRouteXEnd": { ... },
    "AutoCollectRouteXAssertLocation": { ... },
    "AutoCollectRouteXGotoFind1": { ... }
}
```

#### `Start` 节点规则

- `next` 第一个目标是 `AutoCollectRouteXAssertLocation`
- `next` 第二个目标是对应的 `[JumpBack]SceneEnterWorldXxx`
- `focus.Node.Recognition.Succeeded` 使用中文，例如：`开始路线6`

#### `End` 节点规则

- 保持：

```json
"pre_delay": 0,
"post_delay": 0
```

#### `AssertLocation` 节点规则

- 必须使用 `MapLocateAssertLocation`
- `desc` 必须写中文：`传送到采集点`
- `recognition` 固定写成字符串：`"Custom"`
- `custom_recognition` 固定写成：`"MapLocateAssertLocation"`
- `zone_id` 不作为独立输入参数向用户单独询问
- 直接从第一段 `path` 的首个 `ZONE` 声明中取值
- 如果用户给的是一个断言点 `[x, y]`，则 `target` 需要换算为 `[x-10, y-10, 20, 20]`
- 只有在用户明确给了别的容差矩形时，才不要使用上述默认换算
- `action` 固定写成：`"DoNothing"`
- 不要写 `expected` 数组
- 不要写 `map_name`

`MapLocateAssertLocation` 在本 skill 中的固定理解如下：

- 作用：传送完成后，在大地图中校验当前位置是否落在用户提供的断言点附近
- `zone_id` 来源：第一段导航 `path` 的首个 `ZONE.zone_id`
- `target` 来源：用户给的断言点 `[x, y]`，默认换算为 `[x-10, y-10, 20, 20]`
- 写法特征：扁平结构，直接写 `custom_recognition_param.zone_id` 与 `custom_recognition_param.target`
- 参考实现：`assets/resource/pipeline/AutoCollect/AutoCollectRoute6.json` 的 `AutoCollectRoute6AssertLocation`

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
            "AutoCollectRouteXGotoFind1"
        ]
    }
}
```

- 其中 `zone_id` 必须与第一段 `path` 的首个 `ZONE` 保持同值
- 注意：这里是“从 path 推导并复制同值”，不是在 JSON 运行时动态引用；`MapLocateAssertLocation` 仍然需要显式写出 `zone_id`

#### `GotoFindN` 节点规则

本 skill 中新路线建立固定使用 `MapNavigateAction`：

- `action.type` = `Custom`
- `custom_action` = `MapNavigateAction`
- `custom_action_param.map_name` = 推断出的 `map_name`
- `path` 第一项必须是：

```json
{
    "action": "ZONE",
    "zone_id": "..."
}
```

每个 `GotoFindN` 还必须满足：

- `desc` 用中文并按顺序编号：
  - `前往采集点1`
  - `前往采集点2`
  - `前往采集点3`
  - ...
- `next` 统一指向采集开始节点：
  - 点击采集：`AutoCollectClickStart`
  - 挖掘类路线：`AutoCollectDigStart`
- `anchor` 对应串联到下一段：
  - 点击采集：`AutoCollectClickAfter`
  - 挖掘类路线：`AutoCollectDigAfter`

最后一段的 `anchor` 指向 `AutoCollectRouteXEnd`。

### 第三步：按 `true` 切分路径

如果原始点列形如：

```json
[
    {"action": "ZONE", "zone_id": "ValleyIV_Base"},
    [532, 697],
    [585, 723, true],
    [586, 723],
    [579, 734, true]
]
```

则必须拆成：

- `GotoFind1`: `[532,697] -> [585,723,true]`
- `GotoFind2`: `[586,723] -> [579,734,true]`

拆分原则：

1. 第一段从第一个实际导航点开始
2. 每遇到一个 `true` 就结束当前段
3. 下一段从 `true` 后面的下一个普通点开始
4. 如果用户说“第一个点只做判断”，那第一段不能再包含这个点

### 第四步：注册到 `assets/resource/pipeline/AutoCollect.json`

必须同步修改两处：

#### `AutoCollectStart.next`

把：

```json
"[JumpBack]AutoCollectRouteXSub"
```

加入主链路，通常放在已有最后一条路线之后、`AutoCollectEnd` 之前。

#### 新增 `AutoCollectRouteXSub`

结构对齐现有子入口：

```json
"AutoCollectRouteXSub": {
    "enabled": false,
    "max_hit": 1,
    "pre_delay": 0,
    "post_delay": 0,
    "next": [
        "AutoCollectRouteXStart"
    ],
    "focus": {
        "Node.Recognition.Succeeded": "$option.AutoCollectRouteX.label"
    }
}
```

### 第五步：注册到 `assets/tasks/AutoCollect.json`

必须同步修改两处：

#### `default_case`

如果项目当前默认全开已有路线，就把 `RouteX` 一并加入末尾。

#### `cases`

新增：

```json
{
    "name": "RouteX",
    "label": "$option.AutoCollectRouteX.label",
    "pipeline_override": {
        "AutoCollectRouteXSub": {
            "enabled": true
        }
    }
}
```

### 第六步：补充 `assets/locales/interface/*.json`

至少要新增：

- `zh_cn`
- `zh_tw`
- `en_us`
- `ja_jp`
- `ko_kr`

统一新增键：

```json
"option.AutoCollectRouteX.label": "..."
```

处理原则：

- `zh_cn` 按用户提供的任务名原文精确填写
- 其他语言优先复用仓库中已有地区名和物品名译法
- 保持与 `Route1~5` 相同的句式风格

## 文案规范

路线文件里的内部提示文案必须遵守以下规则：

- `focus.Node.Recognition.Succeeded` 用中文
- `desc` 用中文
- `GotoFind` 文案必须按顺序编号
- `AssertLocation` 必须使用 `MapLocateAssertLocation`

推荐写法：

- `Start.focus`: `开始路线X`
- `AssertLocation.desc`: `传送到采集点`
- `GotoFind1.desc`: `前往采集点1`
- `GotoFind2.desc`: `前往采集点2`
- `GotoFind3.desc`: `前往采集点3`

不要写英文：

- `Start Route X`
- `Arrive at collection point`
- `Go to collection point`

## 检查清单

提交前必须检查：

- [ ] 任务名满足“路线X：xxx”形式
- [ ] `RouteId` 是从任务名里解析出来的，而不是手填错位
- [ ] 新路线文件名、节点名、选项名、文案编号一致
- [ ] `Start` 使用了正确的 `[JumpBack]SceneEnterWorldXxx`
- [ ] 这条新路线建立使用的是 `MapNavigateAction`，而不是 `MapTrackerMove`
- [ ] `AssertLocation` 使用了新的断言点，而不是误复用旧坐标
- [ ] `AssertLocation` 使用的是 `MapLocateAssertLocation` 扁平结构，而不是 `MapTrackerAssertLocation` 的 `expected` 结构
- [ ] 用户声明为“判断点”的第一个坐标没有混进导航路径
- [ ] 每个带 `true` 的点都拆成了独立采集段
- [ ] 每一段的 `anchor` 都正确串到下一段或 `End`
- [ ] `next` 使用了正确的采集节点：`AutoCollectClickStart` 或 `AutoCollectDigStart`
- [ ] 主入口 `AutoCollect.json` 已接入 `AutoCollectRouteXSub`
- [ ] 任务选项 `assets/tasks/AutoCollect.json` 已注册 `RouteX`
- [ ] 5 份 locale 已新增 `option.AutoCollectRouteX.label`
- [ ] `focus` / `desc` 使用中文
- [ ] `GotoFind` 文案编号连续

## 验证建议

至少做以下验证：

1. 检查路线执行顺序是否为：
   - 传送
   - 位置断言
   - 第 1 段导航并采集
   - 第 2 段导航并采集
   - ...
   - 结束
2. 检查每个 `true` 点确实形成单独采集段
3. 检查最后一段 `anchor` 是否能正常结束，而不是回环或断链
4. 检查新增路线是否能在 `AutoCollectRoutes` 中显示
5. 检查勾选 `RouteX` 后是否能启用 `AutoCollectRouteXSub`

如需做静态验证，优先确认以下文件能被 JSON 正常解析：

- `assets/resource/pipeline/AutoCollect/AutoCollectRouteX.json`
- `assets/resource/pipeline/AutoCollect.json`
- `assets/tasks/AutoCollect.json`

## 输出模板

完成后，优先按下面结构汇报：

```markdown
## 已完成

- 新增路线文件：`AutoCollectRouteX.json`
- 已接入 `assets/resource/pipeline/AutoCollect.json`
- 已接入 `assets/tasks/AutoCollect.json`
- 已补充 `assets/locales/interface/*.json`

## 关键实现

- 从任务名 `路线X：...` 解析出路线编号并推导文件命名
- 复用了 `SceneEnterWorld...` 作为传送入口
- 使用 `MapLocateAssertLocation` 校验由断言点换算得到的 `[x-10, y-10, 20, 20]`
- `MapLocateAssertLocation` 的 `zone_id` 来自首段 `path` 的第一个 `ZONE`
- 按 `true` 断点拆成 N 段采集
- 每段通过 `AutoCollectClickAfter` / `AutoCollectDigAfter` 串联

## 校验

- 核心 JSON 解析通过 / 未通过
- 已确认 `RouteX` 注册链路存在
```

## 约束

- 不要对现有 `RouteX` 做无关重构
- 不要为了“更通用”去抽象自动生成器，除非用户明确要求
- 不要把点击采集路线改成挖掘路线，或反过来
- 不要把本 skill 的新路线建立流程扩展到 `MapTrackerMove`
- 不要把定位节点写回 `MapLocateAssertLocation`
- 不要省略默认的任务注册与多语言注册，除非用户明确要求不要注册
- 不要把内部提示文案写成英文
- 不要漏掉 `desc` 的顺序编号
- 不要在未确认的情况下擅自修改已有路线编号或显示名称
