# 开发入门路线

这篇文档面向 **完全不了解 MaaFramework、但想尽快在 MaaEnd 里成功改东西并跑起来** 的开发者。

目标不是一次讲完所有细节，而是按实际开发优先级，把你带到这三个结果：

1. 能搭好本地工作区并成功运行程序。
2. 能看懂 MaaEnd 的基本结构，知道该改哪一层。
3. 能按当前仓库里开发者的常见做法，完成一次小改动并自检。

## 先记住这张脑图

MaaEnd 基本可以先理解成四层：

1. `assets/interface.json`
    - 定义项目入口、控制器、资源、任务导入列表、Agent 启动项。
2. `assets/tasks/**/*.json`
    - 定义“这个任务在 UI 里长什么样、入口节点是谁、有哪些选项”。
3. `assets/resource/pipeline/**/*.json`
    - 定义“识别什么、点哪里、下一步去哪”。这是日常开发最常改的一层。
4. `agent/go-service/**`
    - 只放 Pipeline 很难表达的复杂逻辑，例如复杂识别、计算、遍历、特殊交互。

你可以先把一条任务理解成：

`界面任务(Task) -> 进入某个 Pipeline 节点 -> 识别/操作循环 -> 必要时调用 Go 自定义逻辑`

在 MaaEnd 里，**大多数功能修改优先落在 Task + Pipeline，不是先写 Go**。

## 学习优先级

如果你是第一次接触这个项目，建议按这个顺序看：

1. 本文：先跑起来，先有整体图。
2. [`docs/zh_cn/developers/development.md`](./development.md)
3. MaaFramework 官方文档
    - Pipeline 协议：<https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/3.1-%E4%BB%BB%E5%8A%A1%E6%B5%81%E6%B0%B4%E7%BA%BF%E5%8D%8F%E8%AE%AE.md>
    - 项目接口 V2：<https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/3.3-ProjectInterfaceV2%E5%8D%8F%E8%AE%AE.md>
    - 看工作区、资源、规范、已有文档入口。
4. [`docs/zh_cn/developers/common-buttons.md`](./common-buttons.md)
    - 学会优先复用已有按钮节点，少重复造轮子。
5. [`docs/zh_cn/developers/scene-manager.md`](./scene-manager.md)
    - 学会用万能跳转进行游戏内场景切换。

## 第一步：先成功运行一次

对新开发者，推荐先区分两种“运行”：

### 1. 跑最终程序

直接运行：

- Windows: `install/mxu.exe`
- Linux/macOS: `install/mxu`

这是最终用户会接触到的 GUI 入口，适合确认“项目能不能启动”。

### 2. 做开发调试（最推荐）

- 将工作目录设置为**项目根目录**的文件夹
- 用 **MaaFramework [开发工具](https://github.com/MaaXYZ/MaaFramework/tree/main?tab=readme-ov-file#%E5%BC%80%E5%8F%91%E5%B7%A5%E5%85%B7)** 调试 Pipeline。（推荐使用 [MaaPipelineEditor](https://github.com/kqcoxn/MaaPipelineEditor) 进行可视化开发和 VS Code 的 [MaaPipelineEditor 插件](https://github.com/neko-para/maa-support-extension) 进行 Pipeline 调试）
- 用 VS Code 调试 `agent/go-service`。

不推荐把 MXU 当主要调试工具。当前项目要求：

- Pipeline 调试看 MaaFramework 开发工具。
- Go 逻辑调试直接断点或 attach。
- MXU 主要用于最终运行验证。

## 第二步：先学会判断“这次改动该改哪”

这是最重要的入门能力。

### 只改界面文案、任务名、选项文案

先看：

- `assets/interface.json`（由于使用 i18n 键国际化，建议使用开发工具获得更好的阅读体验，下文同理）
- `assets/locales/interface/zh_cn.json`

例如任务标题、描述、选项标签，通常都在这里。

### 改任务编排、入口节点、UI 选项

先看：

- `assets/tasks/**/*.json`

例如：

- 任务名 `name`
- 任务入口节点 `entry`
- 任务支持的资源 `resource`
- 任务支持的控制器 `controller`
- 流水线覆盖参数 `pipeline_override`
- 任务可选项 `option`
- 任务归属分组 `group`
- 预设任务组合 `assets/tasks/preset/*.json`

示例：

```jsonc
"task": [
    {
        "name": "活动任务",
        "label": "$活动任务",
        "entry": "ActivityTask",
        "resource": ["Official"],
        "description": "仅在官服资源包中可用的活动任务"
    },
    {
        "name": "通用任务",
        "label": "$通用任务",
        "entry": "CommonTask"
    }
]
```

### 改识别、点击、跳转、等待、流程细节

先看：

- `assets/resource/pipeline/**/*.json`

这通常是 MaaEnd 的主战场。多数功能调整都在这一层完成。

### 确实需要复杂逻辑时

再看：

- `agent/go-service/**`

当前项目的要求是：**只有当 Pipeline 不够表达时，才上 Go Service**。

## 第三步：按仓库现在的常见做法来改

下面这些就是当前项目里最值得先学会的“默认做法”。

### 1. 优先改 Pipeline，不要先写 Go

开发者普遍遵循的是：

- 任务流程尽量放在 `assets/resource/pipeline/`。
- Go Service 只做复杂识别、算法、数据计算、特殊交互。
- 不要把整段业务流程塞进 Go。

一句话：**Pipeline 管流程，Go 管难点。**

### 2. 先复用已有节点，再新增节点

开始写新节点前，优先查：

- [`docs/zh_cn/developers/common-buttons.md`](./common-buttons.md)
- [`docs/zh_cn/developers/scene-manager.md`](./scene-manager.md)
- [`docs/zh_cn/developers/custom.md`](./custom.md)

仓库里大量场景都已经有现成能力，比如：

- 通用确认/取消/关闭按钮
- `SceneManager` 的场景跳转接口
- `SubTask`、`ClearHitCount`、`ExpressionRecognition` 等公共 Custom 节点

### 3. 所有图片和坐标都按 720p 来

当前项目统一基准分辨率是 **1280x720**。

这意味着：

- 模板图按 720p 裁。
- `roi`、`target`、`box` 都按 720p 写。
- 不要直接按自己当前设备的原始分辨率填坐标。

### 4. 流程要写成“识别 -> 操作 -> 再识别”

这是 MaaEnd 里最核心的开发习惯之一。

推荐：

- 识别按钮 A
- 点击 A
- 再识别按钮 B
- 点击 B

不推荐：

- 识别一次画面后连点好几步

原因很简单：点完之后画面可能已经变了，弹窗、加载、错误状态都可能插进来。

### 5. 少写硬延迟，多写状态节点

当前项目明显偏向这些做法：

- 少用 `pre_delay`、`post_delay`、`timeout`
- 多补中间识别节点
- 多扩充 `next`
- 需要等画面稳定时优先用 `pre_wait_freezes` / `post_wait_freezes`

目标是：**尽量让 `next` 在第一轮截图就命中。**

### 6. 弹窗、加载、场景切换要作为常规情况处理

在 MaaEnd 里，好的流程不是“主线能跑就行”，而是：

- 正常主线能跑
- 弹窗来了能处理
- 加载来了能等过去
- 不在目标场景时能自动跳过去

这也是为什么很多任务会在 `next` 里挂：

- `[JumpBack]SceneDialogConfirm`
- `[JumpBack]SceneWaitLoadingExit`
- `[JumpBack]SceneAnyEnterWorld`

### 7. OCR 先写完整文本

当前项目的默认习惯：

- `expected` 写完整文本，不写半截。
- OCR 的多语言处理优先交给现有 i18n 工具链。

如果你确实要写片段或手写正则，再使用 `// @i18n-skip`。

### 8. 颜色匹配优先用 HSV/灰度

原因是不同显卡渲染会有偏差，直接写 RGB 更容易跨设备不稳。

## 第四步：学会一次最小改动闭环

最推荐的新手路径，不是“直接做一个全新大任务”，而是：

1. 找一个已有任务。
2. 做一个很小但完整的改动。
3. 运行验证。
4. 做格式化和检查。

最典型的改动类型有三种：

### 路线 A：只改文案

改：

- `assets/locales/interface/zh_cn.json`

然后启动 `install/mxu.exe` 看 UI 是否变化。

### 路线 B：只改任务入口或选项

改：

- `assets/tasks/*.json`

如果改的是任务定义或入口，建议重新运行一次：

```bash
python tools/build_and_install.py
```

### 路线 C：只改一个 Pipeline 节点

改：

- `assets/resource/pipeline/**/*.json`

然后：

- 在 MaaFramework 开发工具里重新加载资源，或
- 重新运行程序进行验证

这是最接近仓库里真实开发节奏的上手方式。

## 第五步：知道什么时候必须重建

这是新手最容易踩的坑之一。

### 改了 Pipeline / Task / Locale

通常先重新加载资源或重新启动程序即可。

### 改了 Go Service

必须重新编译并同步到 `install/`：

```bash
python tools/build_and_install.py
```

如果你还改了 C++ 算法侧，则用：

> **注意：这需要 VC 作为生成器，采用 cmake 编译，一般开发者无需更改 C++ 侧。**

```bash
python tools/build_and_install.py --cpp-algo
```

### 改了 `assets/interface.json`

建议把 **源码里的 `assets/interface.json` 视为主文件**。

如果你是直接改源码，重新执行一次：

```bash
python tools/build_and_install.py
```

如果你是通过工具改了 `install/interface.json`，要记得把修改同步回 `assets/interface.json`，不要只留在安装目录里。

## 第六步：提交流程前至少做这些检查

### 格式化

```bash
pnpm format
pnpm format:go
```

### 资源和 schema 检查

```bash
pnpm check
```

### 节点测试

```bash
pnpm test
```

当前仓库 CI 也主要围绕这些内容做校验：

- `pnpm check`
- `python tools/validate_schema.py`
- `pnpm test`
- `pnpm format:all`

## 第七步：学会补“配套文件”

MaaEnd 里一个功能改动，常常不只改一个地方。

### 新增或修改任务时，常见联动文件

- `assets/tasks/*.json`
- `assets/resource/pipeline/**/*.json`
- `assets/locales/interface/zh_cn.json`
- `assets/interface.json`
- `tests/**/*.json`

### 如果新增了 Go Custom 组件

除了实现文件本身，还要记得：

- 在对应子包 `register.go` 注册
- 在 `agent/go-service/register.go` 的 `registerAll()` 中接入
- 重新执行 `python tools/build_and_install.py`

## 第八步：常见坑位

### 1. 忘了装 Node 依赖

症状：`pnpm check` / `pnpm test` / `pnpm format` 跑不起来。

处理：

```bash
pnpm install
```

### 2. 忘了更新子模块

症状：模型或 C++ 依赖目录缺失。

处理：

```bash
git submodule update --init --recursive
```

或者重新跑：

```bash
python tools/setup_workspace.py --update
```

### 3. 改了 Go 代码却没有生效

大概率是忘了重新执行：

```bash
python tools/build_and_install.py
```

### 4. 直接引用了 `__ScenePrivate*` 节点

这不是推荐用法。任务里应优先引用 `Interface` 目录暴露出来的场景接口节点。

### 5. 只顾主线，不处理弹窗/加载

这会让流程在真实用户环境下非常脆。请把弹窗、加载、中间态视为正常情况。

### 6. 改了任务但没补文案

- 文案放到 `assets/locales/`

## 一条最短可行开发路线（总结）

如果你只想先快速入门，请按这个顺序做：

1. 跑 `python tools/setup_workspace.py`
2. 跑 `pnpm install`
3. 启动 `install/mxu.exe` 或 `install/mxu`
4. 读一遍 [`docs/zh_cn/developers/development.md`](./development.md)
5. 选一个已有任务，先只改 `assets/locales/interface/zh_cn.json` 或一个简单 Pipeline 节点

做到这里，你就已经不是“完全不了解 MaaFW”的状态了，至少已经能在 MaaEnd 里完成一次真实可运行的修改。

## 接下来该看什么

- 想写更稳的 Pipeline：看 [`docs/zh_cn/developers/development.md`](./development.md)
- 想复用通用按钮：看 [`docs/zh_cn/developers/common-buttons.md`](./common-buttons.md)
- 想做场景跳转：看 [`docs/zh_cn/developers/scene-manager.md`](./scene-manager.md)
- 想补测试：看 [`docs/zh_cn/developers/node-testing.md`](./node-testing.md)
- 想扩展 Go 自定义逻辑：看 [`docs/zh_cn/developers/custom.md`](./custom.md)
