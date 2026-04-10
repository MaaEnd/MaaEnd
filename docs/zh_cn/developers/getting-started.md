# 快速开始

以「自动售卖物品」为例，走一遍从需求到合并的完整开发流程。

## 环境准备

- Git
- Python 3.10+
- Node.js 22
- pnpm 10+
- Go 1.25.6+

```bash
git clone --recursive https://github.com/MaaEnd/MaaEnd.git
cd MaaEnd
python tools/setup_workspace.py
pnpm install
```

> [!NOTE]
>
> 如果 `setup_workspace.py` 出错，参考下方[手动配置指南](#手动配置指南)。

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

对于「售卖物品」，在 `assets/resource/pipeline/` 下新建目录 `SellItems/`，然后开始写节点。

### 命名

节点名使用 PascalCase，以任务名为前缀：`SellItemsOpenBag`、`SellItemsSelectItem`、`SellItemsConfirmSell`。

### 像写状态机一样思考

Pipeline 的核心逻辑是**有限状态机（FSM）**——每个节点先识别当前画面，执行操作，再由 `next` 跳到下一个状态：

```text
打开背包 → 识别物品 → 点击物品 → 识别售卖按钮 → 点击售卖 → 识别确认弹窗 → 确认 → 回到列表
```

**先识别，后操作。永远不要盲点。** 更多规则详见[编码规范](./coding-standards.md)。

## 4. 截图与模板

识别节点需要模板图。使用[开发工具](./tools-and-debug.md#开发工具)截图：

- 推荐 **Maa Pipeline Support**（VS Code 插件）——可以直接截图、框选 ROI、取色。
- 也可以使用 [MaaPipelineEditor](https://mpe.codax.site/docs) 可视化构建 Pipeline。
- 所有图片和坐标以 **1280×720** 为基准，当使用**Maa Pipeline Support** 无需自己切换游戏的分辨率，framework会自动resize。

将截好的模板放到 `assets/resource/image/SellItems/` 下。

## 5. 调试与测试

用开发工具加载资源，连接模拟器，运行你的节点。

- 每改一次 Pipeline，在工具里**重新加载资源**即可，无需重编译。
- 注意不同帧率（12fps vs 60fps）下动画过渡速度不同，可能导致识别时机偏差。

> 如果改了 Go Service，必须先运行 `python tools/build_and_install.py`，重新编译。

## 6. 完善配套文件

Pipeline 跑通后，补齐配套：

### Task 定义

在 `assets/tasks/` 下新建或修改 JSON，定义任务入口节点和选项，来导入前端。

### i18n 文案

在 `assets/locales/interface/` 中添加任务名称和描述的翻译键。

### interface.json

如果新增了任务，需要在 `assets/interface.json` 中导入。修改后执行 `python tools/build_and_install.py` 同步到 `install/`。

## 7. 验证与提交

### 在 MXU 中验证

启动 `install/mxu.exe`，确认任务在 UI 里正常显示和运行。

### Push 并请求 Review

```bash
git push origin feat/auto-sell-items
```

在 GitHub 把 Draft PR 改为 **Ready for Review**，等待 maintainer review。

## 接下来看什么

- 了解可复用节点，避免重复造轮子 → [组件指南](./components-guide.md)
- 掌握开发工具细节 → [工具与调试](./tools-and-debug.md)
- 查阅编码规范完整版 → [编码规范](./coding-standards.md)
- 所有文档索引 → [README.md](./README.md)

---

<details>
<summary>手动配置指南</summary>
<br>

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
