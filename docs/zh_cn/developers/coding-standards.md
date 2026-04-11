# 编码规范

## Pipeline 低代码规范

### 命名：PascalCase

节点名称使用 PascalCase，同一任务内以任务名或模块名为前缀。例如 `ResellMain`、`DailyProtocolPassInMenu`、`RealTimeAutoFightEntry`。

### 禁止硬延迟

尽可能少使用 `pre_delay`、`post_delay`、`timeout`、`on_error`。通过增加中间识别节点避免盲目 sleep。

需要等画面稳定时优先使用 `pre_wait_freezes` / `post_wait_freezes`。

> [!NOTE]
>
> 关于延迟，可扩展阅读[隔壁 ALAS 的基本运作模式](https://github.com/LmeSzinc/AzurLaneAutoScript/wiki/1.-Start#%E5%9F%BA%E6%9C%AC%E8%BF%90%E4%BD%9C%E6%A8%A1%E5%BC%8F)，其推荐的实践基本等同于我们的 `next` 字段。

### `next` 第一轮即命中

尽可能扩充 `next` 列表，保证任何游戏画面都处于预期中，实现一次截图就命中目标节点。

### 识别 → 操作 → 再识别

每一步操作都基于识别。

**推荐：** 识别 A → 点击 A → 识别 B → 点击 B

**禁止：** 整体识别一次 → 点击 A → 点击 B → 点击 C

_你没法保证点完 A 之后画面是否还和之前一样。极端情况下游戏弹出新池子公告，直接点 B 可能点到抽卡里去。_

### 不要重复点击

通过 `pre_wait_freezes`、`post_wait_freezes` 等待画面静止，或增加中间节点确认按钮可点击后再执行。第二次点击可能已作用于下一界面的其他元素。详见 [Issue #816](https://github.com/MaaEnd/MaaEnd/issues/816)。

### 处理弹窗和加载

好的流程不是"主线能跑就行"，而是：正常主线能跑、弹窗能处理、加载能等过去、不在目标场景时能自动跳过去。

常见做法是在 `next` 里挂：

- `[JumpBack]SceneDialogConfirm`
- `[JumpBack]SceneWaitLoadingExit`
- `[JumpBack]SceneAnyEnterWorld`

### OCR 写完整文本

`expected` 写完整文本，不写半截。多语言处理交给 i18n 工具链。需要片段或手写正则时使用 `// @i18n-skip`。详见下文 [OCR 与 i18n](#ocr-与-i18n)。

### 颜色匹配用 HSV / 灰度

不同显卡渲染有偏差，RGB 跨设备不稳。详见资源规范中的 [颜色匹配：HSV 优先](#颜色匹配-hsv-优先)。

### 先复用，再新增

写新节点前，先查[组件指南](./components-guide.md)确认是否已有现成能力。

## Go Service 规范

Go Service 仅用于处理 Pipeline 难以实现的复杂图像算法或特殊交互逻辑。**整体流程仍由 Pipeline 串联，禁止在 Go 中编写大量流程代码。**

例如：商品购买任务中，Go Service 仅做价格比较、商品遍历等逻辑；打开商品详情、点击购买、回到列表等界面跳转由 Pipeline 完成。

一句话：**Pipeline 管流程，Go 管难点。**

## Cpp Algo 规范

Cpp Algo 支持原生 OpenCV 和 ONNX Runtime，但仅推荐用于实现单个识别算法。各类操作等业务逻辑推荐用 Go Service 编写。

其余规范参考 [MaaFramework 开发规范](https://github.com/MaaXYZ/MaaFramework/blob/main/AGENTS.md#%E5%BC%80%E5%8F%91%E8%A7%84%E8%8C%83)。

## 提交前检查

```bash
pnpm format        # JSON/YAML 格式化
pnpm format:go     # Go 格式化
pnpm check         # 资源和 schema 检查
pnpm test          # 节点测试
```

CI 也围绕这些做校验：`pnpm check`、`python tools/validate_schema.py`、`pnpm test`、`pnpm format:all`。

## 配套文件

MaaEnd 里一个功能改动常常不只改一个地方。

### 新增或修改任务

- `assets/tasks/*.json`
- `assets/resource/pipeline/**/*.json`
- `assets/locales/interface/zh_cn.json`
- `assets/interface.json`
- `tests/**/*.json`

### 新增 Go Custom 组件

- 在对应子包 `register.go` 注册
- 在 `agent/go-service/register.go` 的 `registerAll()` 中接入
- 重新执行 `python tools/build_and_install.py`

> MXU 是面向终端用户的 GUI，不建议用于日常开发调试。上述开发工具可以极大程度提高开发效率。

## 调试工作流

### 编辑 Pipeline

修改 `assets/resource/pipeline/**/*.json` 后，在开发工具中重新加载资源即可，无需重编译。

### 编辑 Go Service

修改 `agent/go-service/` 后，必须重新编译：

```bash
python tools/build_and_install.py
```

可在 VS Code 终端的运行任务中使用 `build` 任务快捷运行，也可对 go-service 挂断点或 attach 调试。

### 编辑 `interface.json`

`assets/interface.json` 是源码主文件。修改后执行：

```bash
python tools/build_and_install.py
```

若通过工具修改了 `install/interface.json`，需手动同步回 `assets/interface.json`。

### 编辑 Cpp Algo

需要 VC 生成器和 cmake，一般开发者无需更改：

```bash
python tools/build_and_install.py --cpp-algo
```

## 资源规范

### 分辨率：720p 基准

所有图片、坐标（`roi`、`target`、`box`）均以 **1280x720** 为基准。MaaFramework 在运行时会根据用户设备自动转换。推荐使用上述开发工具进行截图和坐标换算。

<a id="颜色匹配-hsv-优先"></a>

### 颜色匹配：HSV 优先

不同厂商显卡（NVIDIA、AMD、Intel）渲染存在差异，直接使用 RGB 跨设备不稳定。推荐在 HSV 空间中固定色相，仅调整饱和度和亮度。

### HDR / 颜色管理

**当被提示 "HDR" 或 "自动管理应用的颜色" 等功能已开启时，不要进行截图、取色等操作**，可能导致模板效果与用户实际显示不符。

### 资源文件夹链接

资源文件夹是链接状态，修改 `assets` 等同于修改 `install` 中的内容，无需额外复制。**但 `interface.json` 是复制的**，修改需手动同步或运行 `build_and_install.py`。

<a id="ocr-与-i18n"></a>

## OCR 与 i18n

开发者无需手动维护多语言 OCR，只需按当前语言写入预期文本，`tools/i18n` 会自动处理。

### 写法要求

- `expected` 写完整文本，不要只写片段。例如应写"这是一段示例内容"，而不是只写"示例内容"。
- 英文 `expected` 自动处理后会生成忽略大小写的正则，单词间使用 `\\s*`。例如 `Send Local Clues` → `(?i)Send\\s*Local\\s*Clues`。
- 未跳过处理的 OCR 节点，脚本会根据显示宽度差异自动补充 `roi_offset`；`only_rec: true` 的节点除外。

### 跳过自动处理

若需写片段或手写正则，在 `expected` 数组内添加 `// @i18n-skip`：

```jsonc
"expected": [
    // @i18n-skip
    "示例内容"
]
```

默认写法（推荐，会自动 i18n 处理）：

```jsonc
"expected": [
    "这是一段示例内容"
]
```

## 测试

MaaEnd 使用 maa-tools 进行节点测试，详见[节点测试文档](./node-testing.md)。编写识别节点时请尽量添加测试用例。

## 常见坑

| 坑                                  | 处理                                                                                    |
| ----------------------------------- | --------------------------------------------------------------------------------------- |
| `pnpm check` / `pnpm test` 跑不起来 | `pnpm install`                                                                          |
| 模型或 C++ 依赖目录缺失             | `git submodule update --init --recursive` 或 `python tools/setup_workspace.py --update` |
| 改了 Go 却没生效                    | 忘了 `python tools/build_and_install.py`                                                |
| 直接引用了 `__ScenePrivate*` 节点   | 应引用 `Interface` 目录暴露的场景接口节点                                               |
| 只顾主线，不处理弹窗/加载           | 把弹窗、加载、中间态视为正常情况                                                        |
| 改了任务但没补文案                  | 文案放到 `assets/locales/`                                                              |
| 本地能跑但是其他人不行              | 开滤镜了/帧数不同/GPU不同颜色有轻微偏差,RGB卡太死了                                     |
