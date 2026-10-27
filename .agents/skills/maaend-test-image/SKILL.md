---
name: maaend-test-image
description: 为 MaaEnd 添加、导入或补充节点识别测试截图，并按仓库约定脱敏图片、放入 tests/MaaEndTestset、维护 tests/**/test_*.json 和运行 pnpm check/pnpm test 验证。用户提到“添加测试图片/截图”“把截图加入 ADB、CloudADB 或 Win32 测试集”“给节点补正例或反例”“更新 hits/box”“UID 打码后提交测试图”时使用；即使用户只说把某张游戏截图放进 MaaEnd 测试，也要用本技能避免未脱敏原图进入仓库。
---

# MaaEnd 测试图片维护

## 目标

把原始游戏截图安全地加入 MaaEnd 的节点测试。最终同时满足：

- 仓库内只出现脱敏后的 PNG，不先复制未打码原图；
- 图片位于 `tests/MaaEndTestset/<controller>/<resource>/`；
- 对应 `tests/**/test_*.json` 使用正确的 `controller`、`resource`、图片文件名和 `hits`；
- JSON、图片引用、`pnpm check` 和 `pnpm test` 通过；
- 需要提交时，测试图片提交到 `tests/MaaEndTestset` 子模块的 `main` 分支，主仓库再记录该提交指针；
- 不改动无关图片、用例或用户已有工作。

本技能适用于已有固定 ROI 的 `ADB`、`CloudADB` 和 `Win32`。`PlayCover` 或新分辨率没有可靠遮盖坐标时，先向用户确认脱敏规则，不要套用其它平台坐标。

## 开始前确认上下文

1. 定位 MaaEnd 根目录；应同时存在 `maatools.config.mts`、`tests/MaaEndTestset/`、`tests/scripts/loader.mts` 和 `assets/resource/model/ocr/`。
2. 运行 `git status --short`，记录已有修改和未跟踪文件。它们属于用户，不要批量处理测试截图。
3. 运行 `git -C tests/MaaEndTestset status --short` 和 `git -C tests/MaaEndTestset branch --show-current`，分别记录测试集子模块的已有修改和当前分支。需要提交测试图片时，提交前必须让子模块位于 `main`，不要在 detached HEAD 上创建提交。
4. 阅读 `docs/zh_cn/developers/node-testing.md`，再查看语义最接近的图片和 `tests/**/test_*.json`。以当前文档、schema 和 `maatools.config.mts` 为准，不沿用旧测试集格式。
5. 从用户输入或目标路径确定：
    - 原始截图路径；
    - `controller`：`ADB`、`CloudADB` 或 `Win32`；
    - `resource`：当前官服目录为 `Official_CN`，测试配置写 `官服`；
    - 无扩展名图片名和目标 `test_*.json`；
    - 期望命中节点 `hits`，以及确需校验时的 `box`。

只有缺失信息会改变图片落盘位置或测试含义时才追问。不要猜测节点名、正负例结果或精确 `box`。

## 路径和命名

图片使用：

```text
tests/MaaEndTestset/<controller>/<resource>/<图片名>.png
```

测试文件使用：

```text
tests/<功能或组件>/test_<测试主题>.json
```

`image` 只写文件名，必须显式包含 `.png`，不能包含目录：

```json
{
    "configs": {
        "name": "(Win32/ADB-官服)自动转交送货任务",
        "resource": [
            "官服"
        ],
        "controller": [
            "Win32",
            "ADB"
        ]
    },
    "cases": [
        {
            "image": "四号谷地_地区建设_仓储节点_送货任务_确认转交.png",
            "hits": [
                "DeliveryJobsConfirmTaskTransfer"
            ]
        }
    ]
}
```

优先追加到语义匹配的现有文件。只有没有合适测试主题时才新建文件，并仿照相邻测试。若 `controller` 或 `resource` 是数组，`tests/scripts/loader.mts` 会展开笛卡尔积；每种组合都必须有同名图片。同一张图可按不同测试目的出现在多个测试文件中。

## 脱敏并导入图片

使用本技能的单图脚本，从仓库外的原图直接生成目标文件：

```powershell
uv run .agents/skills/maaend-test-image/scripts/redact_test_image.py `
  "C:\path\raw.png" `
  "tests\MaaEndTestset\ADB\Official_CN\四号谷地_确认转交.png"
```

脚本会先判定原图平台，再用 MaaEnd 自带的 OCR 模型脱敏：

1. 校验截图为 1280×720；
2. 按左下状态行的 UID 文字判定原图来自 `ADB`、`CloudADB` 还是 `Win32`（见下节），并校验与目标路径是否一致；
3. 用纯绿色 `RGB(0, 255, 0)` 覆盖该平台的固定 ROI：ADB `[84, 690, 114, 18]`、CloudADB `[18, 690, 114, 18]`、Win32 `[70, 696, 90, 13]`；
4. 用 `assets/resource/model/ocr/` 查找包含 `UID` 或 `#` 的区域；
5. 用同样的纯绿色覆盖 OCR 命中框；
6. 原子写入目标 PNG，外部原图不会先进入仓库。

如果 OCR 漏掉其它私人信息，添加可重复的额外 ROI，格式为 `x,y,w,h`：

```powershell
uv run .agents/skills/maaend-test-image/scripts/redact_test_image.py `
  "C:\path\raw.png" `
  "tests\MaaEndTestset\Win32\Official_CN\功能_画面.png" `
  --extra-roi 1100,20,160,40 `
  --extra-roi 20,620,240,60
```

目标已存在时，只有用户明确要求替换才加 `--force`。原图本来就在目标路径且需要原地脱敏时也必须加 `--force`；脚本会先写临时文件再替换。

处理后必须查看最终图片，确认：

- UID、账号编号、昵称或其它私人信息均已完全遮住；
- 绿色块没有遮挡本次要测试的识别目标；
- 图片内容、方向和 1280×720 分辨率未被改变；
- 仓库中没有原始副本或 `.redacting-*` 临时文件。

固定 ROI 在 OCR 之前就已经涂掉，所以固定 ROI 没盖全 UID 时，剩下的数字不含 `UID` / `#` 标记词，OCR 只会返回空结果、不会提示漏涂。三个平台的 UID 文字位置不同：把 ADB 截图按 Win32 的 ROI 打码会漏出尾部数字；把 CloudADB 截图按 ADB 的 ROI 打码会漏出整段 `UID:` 前缀（云客户端没有延迟指示，UID 整行左移到 x≈18 起）；把图放错平台目录同样会让用例失准，因为 `hits` 是按控制器矩阵跑的，ADB 截图在 Win32 矩阵下识别会失败。脚本会在打码前判定原图平台，与目标路径不一致时直接报错（确认平台无误时可用 `--skip-platform-check` 跳过，并用 `--extra-roi` 补齐遮盖范围）。

OCR 只是辅助检查，视觉复核才是隐私安全的最终门槛。无法确认脱敏完整时，不要把图片加入测试或提交。

### 判断截图平台

需要单独确认某张原始截图的平台时：

```powershell
uv run .agents/skills/maaend-test-image/scripts/detect_screenshot_platform.py "C:\path\raw.png"
```

判定依据是左下状态行的文字布局，先看有几个文字段、再看 UID 文字范围：

- `CloudADB`：左下角**没有**"信号格 + xx ms"延迟指示，只有 UID 一段文字，且 UID 贴着左边缘（x≈18 起、右边界约 x=130、高约 10），落在 `[18, 690, 114, 18]` 内；
- `ADB`：有延迟指示，UID 跟在它右边（x≈88 起、右边界约 x=196、高约 10、上边界约 y=694），落在 `[84, 690, 114, 18]` 内；
- `Win32`：有延迟指示，字号最小（UID 从 x≈73 起、右边界约 x=157、高约 8、上边界约 y=700），落在 `[70, 696, 90, 13]` 内。

三套 ROI 和判定阈值都在这个脚本里，`redact_test_image.py` 直接复用。平台明确时退出码为 0，判不出来（`unknown` / `ambiguous`）时为 2。只能拿原始截图来判：打过码的图里 UID 已被色块盖住，脚本会报 `unknown`。

状态行是否出现延迟指示，也可以自己看一眼确认：同一界面下 CloudADB 左下角只有 `UID: 数字`，ADB 和 Win32 则是 `信号格 + 41ms + UID: 数字`。

## 编写测试用例

### `hits` 写法

只验证命中节点：

```json
"hits": [
    "NodeName"
]
```

验证命中节点和固定识别框：

```json
"hits": [
    {
        "node": "NodeName",
        "box": [
            91,
            587,
            274,
            48
        ]
    }
]
```

不同控制器使用不同识别框：

```json
"box": {
    "ADB": [
        91,
        587,
        274,
        48
    ],
    "CloudADB": [
        91,
        587,
        274,
        48
    ],
    "Win32": [
        100,
        600,
        250,
        50
    ]
}
```

反例：

```json
"hits": []
```

`box` 使用 `[x, y, width, height]` 的非负整数。只有用户提供了预期框，或实际测试输出给出了可确认的框时才写，不要从截图肉眼猜精确值。

### 修改规则

- `configs.controller` 与图片目录一致，可写字符串或数组。
- `configs.resource` 与 `tests/scripts/loader.mts` 的映射一致；当前官服写 `官服`。
- 不手写 `imageRoot`；loader 会根据矩阵自动生成。
- `image` 必须是实际文件名并带 `.png`，不得包含 `/` 或 `\`。
- `hits` 必须存在，允许空数组。
- 同一文件内不要重复添加相同 `image`；需要增加节点时合并到已有用例。
- 测试文件允许 JSONC 注释，格式必须遵循仓库 Prettier（通常为 4 空格）。
- 只编辑目标测试文件，不顺手修正其它已有测试。

## 验证

先做本次变更的直接检查：

```powershell
pnpm exec prettier --write "tests\功能\test_xxx.json"
pnpm check
pnpm test
```

随后确认：

1. 每个新增 `image` 在对应 controller/resource 的目录下都存在，且该目录的平台与原图平台一致（`redact_test_image.py` 导入时已校验，事后移动或补图时要复查）；
2. `tests/maatools/error_details.json` 没有本次用例的错误；
3. `git diff --check` 没有文本问题；
4. `git status --short` 和目标文件 diff 只包含预期变更。

若 `pnpm test` 失败，先区分：

- 图片路径、矩阵或配置错误：修正测试定义；
- `hits` 与实际画面不符：依据真实测试目的修正，不能为了变绿而删除必要断言；
- MaaEnd 节点本身识别失败：报告节点、ROI 或模板问题。除非用户同时要求修改 Pipeline，否则不要扩大范围。

## 提交测试图

`tests/MaaEndTestset` 是独立 Git 子模块。用户要求提交时，按以下顺序处理，确保图片提交可由主仓库引用并能正常推送：

1. 提交图片前确认子模块处于 `main`：

    ```powershell
    git -C tests/MaaEndTestset switch main
    git -C tests/MaaEndTestset branch --show-current
    ```

    第二条命令必须输出 `main`。若切换因已有修改或分支分歧失败，保留现场并报告，不要丢弃用户改动。

2. 只在子模块中暂存并提交本次脱敏测试图，使用中文约定式提交信息；提交后确认 `main` 指向新提交且子模块工作区干净。
3. 回到 MaaEnd 主仓库，暂存测试定义、相关代码和 `tests/MaaEndTestset` 子模块指针，再创建主仓库提交。不要只提交主仓库测试定义而遗漏子模块指针。
4. 若用户还要求推送，先推送子模块 `main`，确认远端已包含图片提交后，再推送主仓库分支。未获得推送授权时不要推送。

如果误在 detached HEAD 上创建了提交，先记录提交哈希，再将 `main` 快进到该提交；只有确认 `main` 是其祖先时才使用 `--ff-only`：

```powershell
git -C tests/MaaEndTestset switch main
git -C tests/MaaEndTestset merge --ff-only <detached-commit>
```

随后确认 `git -C tests/MaaEndTestset branch --show-current` 输出 `main`，且主仓库记录的子模块指针与该提交一致。

## 交付

简要说明新增或替换了哪些脱敏图片、更新了哪些测试文件与预期命中、额外遮盖区域，以及 `pnpm check`、`pnpm test` 的结果。不要声称图片“已安全脱敏”，除非已实际查看最终图片。
