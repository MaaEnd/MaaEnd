# 开发手册 - 节点测试

本文介绍 MaaEnd 当前使用的节点测试写法。内容以 `maatools.config.mts`、`tools/schema/test.schema.json` 和现有 `tests/` 用例为准。

## 测试目标

节点测试的核心用途，是验证某一张静态截图上，哪些节点应该命中、哪些节点不应该命中。

- 适合验证识别节点、通用节点、场景判断节点是否稳定。
- 适合给复用节点补回归测试，避免改模板或 ROI 后误伤旧场景。
- 不用于验证完整流程时序；流程正确性仍应结合开发工具和实机调试确认。

## 目录结构

当前仓库约定如下：

```text
tests/
|- MaaEndTestset/
|  |- Win32/Official_CN/*.png
|  `- ADB/Official_CN/*.png
|- Common/Button/test_button.json
|- DeliveryJobs/test_region.json
`- ...
```

- 测试定义文件放在 `tests/` 下任意子目录，文件名必须匹配 `test_*.json`。
- 测试截图放在 `tests/MaaEndTestset/`。
- `image` 字段填写对应图片名，也可以直接写带扩展名的文件名。
- 实际截图路径由 `maatools.config.mts` 拼出；通常会落到 `tests/MaaEndTestset/<controller>/<resource>/` 下，并按 `image` 字段匹配具体图片文件。

以当前配置为例：

- `controller = "Win32"` 时，截图目录为 `tests/MaaEndTestset/Win32/`。
- `controller = "ADB"` 时，截图目录为 `tests/MaaEndTestset/ADB/`。
- `resource = "官服"` 时，截图目录为 `tests/MaaEndTestset/*/Official_CN/`。

如果新增了新的资源服或控制器枚举，除了补测试文件，还要同步更新 `maatools.config.mts` 中的映射关系。

## 文件结构

测试文件结构受 `tools/schema/test.schema.json` 约束，顶层必须包含 `configs` 和 `cases`。

```jsonc
{
    "configs": {
        "name": "(Win32-官服)通用按钮",
        "resource": "官服",
        "controller": "Win32",
    },
    "cases": [
        {
            "name": "可选的用例名",
            "image": "帝江号_大世界",
            "hits": [
                "InWorld",
                {
                    "node": "RegionalDevelopmentButton",
                    "box": [223, 32, 32, 19],
                },
            ],
        },
    ],
}
```

### `configs`

- `name`：测试组名称，可选，但建议填写，方便查看测试输出。
- `resource`：资源服名称。当前仓库实际使用的是 `官服`。
- `controller`：控制器类型。当前仓库实际使用的是 `Win32` 和 `ADB`。

### `cases`

- `cases` 是数组，至少要有 1 个用例。
- 每个用例必须包含 `image` 和 `hits`。
- `name` 可选，建议在同一组里截图含义不够直观时补充。
- `image` 对应截图文件名；根据当前 schema，可不写扩展名。

### `hits`

`hits` 表示这张图上“期望命中的节点列表”，支持两种写法。

1. 只校验节点命中：

```json
"hits": ["InWorld", "CloseButtonType1"]
```

1. 同时校验节点和识别框：

```json
"hits": [
    {
        "node": "RegionalDevelopmentButton",
        "box": [223, 32, 32, 19]
    }
]
```

其中 `box` 的格式固定为 `[x, y, width, height]`，四个值都必须是大于等于 0 的整数。

如果某张图不应该命中任何节点，写空数组即可：

```json
"hits": []
```

这类负例很重要，尤其适合防止误识别。

## 编写建议

### 1. 一个测试文件只测一类能力

建议像现有用例一样，以模块或节点族为单位拆分，例如：

- `tests/Common/Button/test_button.json`
- `tests/DeliveryJobs/test_region.json`
- `tests/EnvironmentMonitoring/test_job.json`

这样更容易定位失败原因，也更适合后续补回归样本。

### 2. 截图名直接描述场景

推荐沿用现有风格：把地点、页面层级、关键状态串起来，例如：

- `帝江号_大世界`
- `四号谷地_地区建设_仓储节点_货物装箱_填充至满`
- `武陵_拍照模式_拍摄目标未达成`

截图名越具体，后续维护测试越轻松。

### 3. 正例和负例都要有

只写“能命中”的测试不够。对于容易串台、容易误判的节点，建议至少补一张“不应命中”的图。

例如：

- 某按钮模板容易和别的按钮混淆。
- 某地区识别节点在相邻地区 UI 上可能误命中。
- 某高亮态按钮需要确认普通态下不会误识别。

### 4. 需要时再校验 `box`

如果你只关心“有没有命中”，直接写节点名即可。

如果你还关心“是否命中了正确位置”，尤其是全屏搜索节点、多个相似目标并存的节点，建议补 `box` 断言。

### 5. 样本要覆盖真实易错场景

结合现有开发规范，优先补这些截图：

- 过渡动画前后容易看错的一帧。
- 同屏有多个可点击元素、容易点错的界面。
- Hover 态、选中态、禁用态、奖励弹窗等特殊状态。
- ADB 与 Win32 表现略有差异的界面。

## 与开发规范的关系

节点测试不是孤立存在的，它应服务于当前的 Pipeline 编写规范：

- 如果一个节点承担“关键状态识别”，最好配测试，保证 `next` 第一轮就能稳定命中。
- 如果你为了避免 `pre_delay` / `post_delay` 增加了中间识别节点，建议顺手为这些中间节点补测试。
- 如果一个通用节点准备给多个任务复用，更应该先补测试，再推广使用。

换句话说，测试越完善，越容易把“靠感觉能跑”变成“可回归、可维护”。

## 运行测试

安装依赖后，可在项目根目录执行：

```bash
pnpm test
```

当前仓库中的 `package.json` 会调用 `maa-tools test`，CI 也会执行同一条命令。

测试日志默认输出到：

```text
tests/maatools/
```

更具体的错误明细会写到：

```text
tests/maatools/error_details.json
```

如果只是想先检查配置和资源是否符合约定，也可以执行：

```bash
pnpm check
```

## 编辑器支持

仓库已在 `.vscode/settings.json` 中为 `tests/**/*.json` 关联了 `tools/schema/test.schema.json`，并按 `jsonc` 处理。

这意味着：

- 新建测试文件时可以直接获得 schema 校验与补全。
- 可以像现有用例一样写注释，便于临时保留负例说明。

不过无论是否允许注释，提交前仍应保持文件可读，避免留下无意义的注释块。

## 一个完整示例

```jsonc
{
    "configs": {
        "name": "(Win32-官服)示例节点测试",
        "resource": "官服",
        "controller": "Win32",
    },
    "cases": [
        {
            "name": "大世界主页",
            "image": "帝江号_大世界",
            "hits": ["InWorld"],
        },
        {
            "name": "验证识别框位置",
            "image": "帝江号_大世界",
            "hits": [
                {
                    "node": "RegionalDevelopmentButton",
                    "box": [223, 32, 32, 19],
                },
            ],
        },
        {
            "name": "负例：不应命中",
            "image": "武陵_拍照模式_拍摄目标未达成",
            "hits": [],
        },
    ],
}
```

## 提交前检查

在新增或修改节点测试后，建议至少自查以下几点：

- 测试文件名是否符合 `test_*.json`。
- `configs.resource` 和 `configs.controller` 是否能在 `maatools.config.mts` 中找到映射。
- `image` 是否能在对应目录找到对应截图；如果没写扩展名，也要确认文件名主体一致。
- `hits` 是否只保留本图真正应该命中的节点。
- 若节点位置也重要，是否补了 `box`。
- 是否包含了足够的负例，能拦住常见误识别。

这样写出来的节点测试，才真正能在后续重构、调阈值、换模板时帮你兜底。
