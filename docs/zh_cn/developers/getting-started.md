# 快速开始

以「自动售卖物品」为例，走一遍从需求到合并的完整开发流程。

## 环境准备

- Git
- Python 3.10+
- Node.js 22
- pnpm 10+
- Go 1.25.6+

### 检查本地环境

```bash
git --version
python3 --version   # 或 python --version（注意区分 Python 2）
node --version
pnpm --version
go version
```

### 项目拉取与部署

```bash
git clone --recursive https://github.com/MaaEnd/MaaEnd.git
cd MaaEnd
python tools/setup_workspace.py
pnpm install
```

> [!NOTE]
>
> 如果 `setup_workspace.py` 出错，参考下方[手动配置指南](#手动配置指南)。

**`setup_workspace.py` 常用参数：**

| 参数 | 说明 |
| ---- | ---- |
| （无参数） | 首次初始化，已安装的组件自动跳过 |
| `--update` | 强制更新所有已安装的依赖到最新版本 |
| `--clean-cache` | 清理下载缓存目录 |
| `--cpp-algo-pr <N>` | 从指定 PR 的最新成功 CI run 下载 cpp-algo（用于快速测试尚未合并的 PR 改动） |
| `--cpp-algo-run <ID>` | 从指定 workflow run ID 下载 cpp-algo |

> `--cpp-algo-pr` 和 `--cpp-algo-run` 互斥，二选一。不指定时默认从 v2 分支的最新 push 构建下载。

### 编辑器（推荐）

推荐使用 [Visual Studio Code](https://code.visualstudio.com/)（VS Code）作为本项目的日常开发 IDE。完成上方克隆与初始化后，用 VS Code **打开仓库根目录**（需包含 `.vscode/extensions.json`），并安装工作区**推荐扩展（Workspace Recommendations）**，以便与团队环境一致（例如 Black、Prettier、**Maa Pipeline Support**、Markdownlint、Go、LLDB 等，完整列表见仓库内 `.vscode/extensions.json`）。

**安装推荐扩展：**

1. **打开工作区**：菜单 **文件 → 打开文件夹…**，选中克隆下来的仓库根目录。
2. **通知栏安装**：若右下角提示「此工作区具有扩展建议」或类似文案，选择 **安装** / **全部安装**。
3. **扩展视图**：按 `Ctrl+Shift+X`（macOS：`Cmd+Shift+X`）打开扩展侧栏，在搜索框输入 `@recommended`，展开 **工作区推荐**，对需要的扩展点击 **安装**。
4. **命令面板**：`Ctrl+Shift+P`（macOS：`Cmd+Shift+P`）→ 执行 **`Extensions: Show Recommended Extensions`**，在列表中安装。

更完整的说明见 VS Code 文档：[工作区推荐扩展](https://code.visualstudio.com/docs/editor/extension-marketplace#_workspace-recommended-extensions)。

## 0. Git 前置知识与规范

本项目依赖部分 Git 特性（特别是子模块）。在正式开始堆代码之前，请确保你已经掌握了基本的 Git 分支操作。

**如果你对 Git 还不太熟悉，请务必先通过以下外链进行互动练习，熟练后再继续往下看：**
👉 **[Learn Git Branching (Git 交互式学习与练习)](https://learngitbranching.js.org/)**

除了基础的 `add` / `commit` / `push` / `pull` 外，参与本项目你还需要了解以下两点：

### 提交规范 (Conventional Commits)

本项目的代码提交严格遵循 [约定式提交规范 (Conventional Commits)](https://www.conventionalcommits.org/zh-hans/v1.0.0/)。清晰的 Commit 历史能帮助 Reviewer 快速理解你的意图。每次提交请使用以下前缀：

- `feat:` 新增功能（例如：写了新的 Pipeline 节点）
- `fix:` 修复 Bug（例如：修正了某处 ROI 坐标错误）
- `docs:` 仅文档更改
- `style:` 不影响代码含义的更改（空白、格式、缺少分号等）
- `chore:` 日常构建过程或辅助工具的变动（不涉及生产代码）

> **示例**：`feat(SellProduct): 新增地区建设自动售卖 Pipeline`

### 关于子模块 (Submodule) 更新

本项目使用 Git Submodule 来管理一些独立的依赖库和体积较大的文件（例如用于识别的模型库）。

**🚧 新手常见踩坑点：**
在准备 `commit` 提交代码时，你可能会在 Git 状态中看到提示 `model`（或其他子模块）发生了修改，但你确信自己并没有改过任何模型文件。
这通常是因为你刚拉取了最新代码或切换了分支，主仓库记录的子模块版本指针已经更新，但你**本地的子模块文件还没有同步**，导致 Git 认为你「修改」了它。

也可能在拉取主分支更新或切换分支后，出现莫名其妙的修改，或代码报找不到模型——同样多因**主仓库的指针更新了，但你本地的子模块文件还没有同步**。

> 主仓库不存子模块的文件内容，只存一个"指针"——指向子模块仓库的某个 commit SHA。
> **💡 解决办法：**
> 遇到这种「幽灵修改」，或每次执行 `git pull` 拉取最新代码后，在仓库根目录执行：

```bash
git submodule update --init --recursive
```

## 1. 确认需求

去 [Issue](https://github.com/MaaEnd/MaaEnd/issues) 找到或创建对应需求。例如：「希望自动售卖背包中的指定物品」。

- 先确认需求是否合理、是否已有人在做。
- 不确定的话，在 Issue 里讨论，或直接发 Issue / PR 找 maintainer 沟通。

## 2. Fork 并创建 Draft PR

```bash
# Fork 后克隆你的仓库，创建功能分支
git checkout -b feat/auto-sell-items
```

尽早在 GitHub 创建 **Draft PR**，标题写清楚你在做什么。这样别人知道有人在做，避免重复劳动。

## 3. 编写 Pipeline

先看一遍[组件指南](./components-guide.md)了解项目结构，确认你该改哪里。

对于「售卖物品」，按任务名 **SellProduct** 组织 Pipeline：入口写在 `assets/resource/pipeline/SellProduct.json`，流程复杂时可在同目录下建子目录 `SellProduct/` 拆成多个 JSON（与 MaaEnd 仓库里现有「售卖产品」任务一致），然后开始写节点。

### 命名

节点名使用 PascalCase，并与任务前缀一致，例如：`SellProductOpenBag`、`SellProductSelectItem`、`SellProductConfirmSell`。

### 像写状态机/决策树一样思考

Pipeline 的核心逻辑是类似**有限状态机（FSM）/决策树（Decision Tree）**：每个节点先识别当前画面，执行操作，再由 `next` 跳到下一个状态：

```text
打开背包 → 识别物品 → 点击物品 → 识别售卖按钮 → 点击售卖 → 识别确认弹窗 → 确认 → 回到列表
```

**先识别，后操作。永远不要盲点。** 更多规则详见[编码规范](./coding-standards.md)。

## 4. 截图与模板

识别节点需要模板图。使用[开发工具](./tools-and-debug.md#开发工具)截图：

- 推荐 **Maa Pipeline Support**（VS Code 插件）——可以直接截图、框选 ROI、取色。
- 也可以使用 [MaaPipelineEditor](https://mpe.codax.site/docs) 可视化构建 Pipeline。
- 所有图片和坐标以 **1280×720** 为基准，下图中我们使用 **Maa Pipeline Support**，无需自己切换游戏分辨率，framework 会自动改变图片尺寸。

> [!NOTE]
> 截图时请注意，不要开启 HDR、黑夜模式，以及 Nvidia 或游戏++等滤镜，否则颜色会干扰识别，可以使用取色工具查看是否为标准色号。

![screenshot](https://github.com/user-attachments/assets/c9bb7157-97e4-4049-bb0a-e937456456f8)

可以看到我们的图片中有背景干扰，这会降低匹配效率，这时候我们可以用自动绿幕工具来解决这个问题。（不推荐手动来做绿幕，不仅很慢，而且不准确）

![green background](https://github.com/user-attachments/assets/4da87f61-30fe-4a94-b6ed-68672877fff3)

将截好的模板放到 `assets/resource/image/SellProduct/` 下。

当有了图片后，我们可以开始编写第一个节点。下面用 **TemplateMatch** 在主界面找到「地区建设」入口，命中后 **Click** 进入；`template` 填你放到 `assets/resource/image/` 下的相对路径，`roi` 用插件框选缩小搜索范围（需按你的模板与界面微调）；若用绿幕处理了模板，可加上 `green_mask`。

```json
{
    "SellProductMain": {
        "desc": "在主界面时，识别地区建设入口并点击进入",
        "recognition": {
            "type": "TemplateMatch",
            "param": {
                "template": "SellProduct/RegionalDevelopmentEntry.png",
                "roi": [
                    400,
                    200,
                    480,
                    320
                ],
                "threshold": 0.7,
                "green_mask": true
            }
        },
        "action": {
            "type": "Click"
        },
        "pre_delay": 0,
        "post_delay": 0,
        "rate_limit": 0,
        "post_wait_freezes": 100,
        "next": [
            "SellProductLoop"
        ]
    }
}
```

该节点会识别这张图片，当识别命中后会执行 `Click`（默认点在匹配框中心）。

编码规范：不推荐使用 `pre_delay` 或 `post_delay` 这类硬延迟，因为不同设备的性能差距很大。10 帧和 60 帧在过动画时要等待的时间完全不同，硬延迟会掩盖很多问题，开发环境能跑不代表用户环境能跑。

只在必须等画面稳定时才使用 `pre_wait_freezes` 或 `post_wait_freezes`，其他时候应该尽量避免延迟。例如上文中 `"post_wait_freezes": 100` 表示在 `roi` 区域 `[400, 200, 480, 320]` 内像素变化结束后，再等待 100 ms。

下一步 `SellProductLoop` 里应继续用识别节点确认已进入地区建设界面，而不是假设点击一定成功。FSM 最重要的是：先识别、确认当前状态，然后再进行操作。

```json
{
    "SellProductLoop": {
        "desc": "主循环，仅支持从地区建设界面开始",
        "recognition": "And",
        "all_of": [
            "InRegionalDevelopment"
        ],
        "pre_delay": 0,
        "post_delay": 0,
        "rate_limit": 0,
        "next": [
            "SellProductAuto",
            "SellProductValleyIV",
            "SellProductWuling",
            "SellProductTaskEnd"
        ]
    }
}
```

上述 `all_of` 中调用的 `InRegionalDevelopment` 是项目中已经定义好的识别节点，用于确认当前处于地区建设主界面。**你可以直接通过填写节点名称来复用已有的识别逻辑**，从而避免重复编写相同的代码。

> **💡 进阶提示：组合识别 (And / Or)**
>
> 除了传统的 `TemplateMatch` (模板匹配)、`Color` (颜色匹配) 等基础方法外，Pipeline 还支持使用逻辑条件 **`And` (与)** 和 **`Or` (或)** 来组合多个识别节点。这在处理复杂或多变的 UI 状态时非常有用。
>
> 关于组合识别的具体语法和高级用法，请参阅 [MaaFramework 官方文档 - Pipeline 协议](https://maafw.com/docs/3.1-PipelineProtocol#and)。

下方示例展示了另一个用于识别地区建设二级界面的节点 `InRegionalDevelopmentView2`，它通过 OCR 识别顶部功能名称，来精准确认当前的界面状态：

```json
{
    "InRegionalDevelopmentView2": {
        "desc": "在地区建设二级界面",
        "recognition": "OCR",
        "roi": [
            0,
            0,
            400,
            70
        ],
        "expected": [
            "据点",
            "據點",
            "Outpost",
            "拠点",
            "거점",
            "物资调度",
            "物資調度",
            "Stock Redistribution",
            "商品取引",
            "물자 관리",
            "仓储节点",
            "倉儲節點",
            "Depot Node",
            "保管ボックス",
            "저장고 노드",
            "环境监测",
            "環境監測",
            "Environment Monitoring",
            "環境観測",
            "환경 관측"
        ]
    }
}
```

文字类识别请用 OCR，以配合 i18n；不要用 TemplateMatch 做文字类识别。上面仅为演示，项目中已有更成熟的复用方案。

更推荐直接调用已有的场景跳转节点，完成后通过 JumpBack 返回，再进入下一状态，避免重复造轮子。

```json
{
    "SellProductMain": {
        "desc": "脚本入口",
        "pre_delay": 0,
        "post_delay": 0,
        "rate_limit": 0,
        "next": [
            "SellProductLoop",
            "[JumpBack]SceneEnterMenuRegionalDevelopment"
        ]
    }
}
```

常用可复用入口见下表：

| 节点         | 说明                                   | 文档                                     |
| ------------ | -------------------------------------- | ---------------------------------------- |
| 通用按钮     | 白色/黄色确认、取消、关闭、传送等      | [common-buttons.md](./common-buttons.md) |
| SceneManager | 万能跳转：从任意界面自动导航到目标场景 | [scene-manager.md](./scene-manager.md)   |

## 5. 调试与测试

在完成一套任务后需要测试。可选工具与流程见 [工具与调试](./tools-and-debug.md)。

用开发工具加载资源，连接模拟器或 PC 端，运行你的节点。

- 每改一次 Pipeline，在工具里**重新加载资源**即可，无需重编译。
- 注意不同帧率（12 fps vs 60 fps）下动画过渡速度不同，可能导致识别时机偏差。

> 如果改了 Go Service，必须先运行 `python tools/build_and_install.py`，重新编译。

当前示例使用 **Maa Pipeline Support**（VS Code 插件）：在控制面板打开管理员模式并连接窗口。

![admin](https://github.com/user-attachments/assets/9d86ae89-0985-4606-bfa6-d4ec96dbee6f)

然后点击 Pipeline 任务上的 Launch，会自动开始执行并解析任务。执行了哪些节点、哪个节点报错，可以通过日志查看。

![launch](https://github.com/user-attachments/assets/6392310c-756c-4c33-b54a-9ab5ff9f4ad2)
![debug panel](https://github.com/user-attachments/assets/653c5314-f6ba-4ffc-91a5-739ab15382dc)

接下来根据反馈调试即可。

## 6. 完善配套文件

Pipeline 跑通后，补齐配套：

### Task 定义

在 `assets/tasks/` 下新建或修改 JSON，定义任务入口节点和选项，以导入前端。例如：

```json
{
    "task": [
        {
            "name": "SellProduct",
            "label": "$task.SellProduct.label",
            "entry": "SellProductMain",
            "description": "$task.SellProduct.description",
            "option": [
                "ValleyIVSell",
                "WulingSell"
            ],
            "group": [
                "daily"
            ]
        }
    ]
}
```

### i18n 文案

在 `assets/locales/interface/` 中添加任务名称和描述的翻译键。例如：

```json
{
    "task.SellProduct.label": "🛒售卖产品",
    "task.SellProduct.description": "使用产品在各个据点兑换对应调度券\n您可以在任务选项中启用或停用特定地区的销售功能。"
}
```

最后在 `assets/interface.json` 中通过 `import` 引入任务文件，例如：

```json
{
    "import": [
        "tasks/DijiangRewards.json",
        "tasks/DailyRewards.json",
        "tasks/ClaimSimulationRewards.json",
        "tasks/SellProduct.json"
    ]
}
```

（实际文件中还会有更多条目，按项目既有顺序追加即可。）

## 7. 验证与提交

### 在 MXU 中验证

启动 `install/mxu.exe`，确认任务在 UI 里正常显示和运行。

### Push 并请求 Review

```bash
git push origin feat/auto-sell-items
```

在 GitHub 把 Draft PR 改为 **Ready for Review**，等待 maintainer review。

恭喜您完成了第一个任务！

## 接下来看什么

- 了解可复用节点，避免重复造轮子 → [组件指南](./components-guide.md)
- 掌握开发工具细节 → [工具与调试](./tools-and-debug.md)
- 查阅编码规范完整版 → [编码规范](./coding-standards.md)
- 所有文档索引 → [README.md](./README.md)
- 更具体的 Pipeline 协议说明 → [Pipeline 协议](https://maafw.com/docs/3.1-PipelineProtocol/)

---

## 手动配置指南

<details>

1. 完整克隆项目及子仓库。

2. 下载 [MaaFramework](https://github.com/MaaXYZ/MaaFramework/releases) 并解压内容到 `deps` 文件夹。

3. 下载 MaaDeps pre-built。

    ```bash
    python tools/maadeps-download.py
    ```

4. 编译 go-service、配置路径。

    ```bash
    python tools/build_and_install.py
    ```

    > 如需同时编译 cpp-algo，请加上 `--cpp-algo` 参数：
    >
    > ```bash
    > python tools/build_and_install.py --cpp-algo
    > ```

5. 将步骤 2 中解压的 `deps/bin` 内容复制到 `install/maafw/`。

6. 下载 [MXU](https://github.com/MistEO/MXU/releases) 并解压到 `install/`。

</details>
