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

`expected` 写完整文本，不写半截。多语言处理交给 i18n 工具链。需要片段或手写正则时使用 `// @i18n-skip`。详见[工具与调试 - OCR 与 i18n](./tools-and-debug.md#ocr-与-i18n)。

### 颜色匹配用 HSV / 灰度

不同显卡渲染有偏差，RGB 跨设备不稳。详见[工具与调试 - 颜色匹配](./tools-and-debug.md#颜色匹配hsv-优先)。

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

## 常见坑

| 坑 | 处理 |
| --- | --- |
| `pnpm check` / `pnpm test` 跑不起来 | `pnpm install` |
| 模型或 C++ 依赖目录缺失 | `git submodule update --init --recursive` 或 `python tools/setup_workspace.py --update` |
| 改了 Go 却没生效 | 忘了 `python tools/build_and_install.py` |
| 直接引用了 `__ScenePrivate*` 节点 | 应引用 `Interface` 目录暴露的场景接口节点 |
| 只顾主线，不处理弹窗/加载 | 把弹窗、加载、中间态视为正常情况 |
| 改了任务但没补文案 | 文案放到 `assets/locales/` |
