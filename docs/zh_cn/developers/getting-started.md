# 快速开始

这篇文档帮你在 10 分钟内搭好环境、跑起程序、完成一次改动。

## 环境要求

- Git
- Python 3.10+
- Node.js 22
- pnpm 10+
- Go 1.25.6+

## 搭起来

```bash
git clone --recursive https://github.com/MaaEnd/MaaEnd.git
cd MaaEnd
python tools/setup_workspace.py
pnpm install
```

> [!NOTE]
>
> 如果 `setup_workspace.py` 出错，参考下方[手动配置指南](#手动配置指南)。

## 跑起来

- Windows: `install/mxu.exe`
- Linux/macOS: `install/mxu`

开发调试推荐使用 [MaaFramework 开发工具](./tools-and-debug.md#开发工具)，不推荐用 MXU 做日常调试。

## 改一个东西试试

### 路线 A：只改文案

改 `assets/locales/interface/zh_cn.json`，重启程序看效果。

### 路线 B：改一个 Pipeline 节点

改 `assets/resource/pipeline/**/*.json`，在开发工具中重新加载资源即可。

### 路线 C：改 Go 逻辑

改 `agent/go-service/` 下的代码，然后执行：

```bash
python tools/build_and_install.py
```

## 提交前

```bash
pnpm format        # JSON/YAML 格式化
pnpm format:go     # Go 格式化
pnpm check         # 资源和 schema 检查
pnpm test          # 节点测试
```

## 接下来看什么

- 了解项目架构和可复用节点 → [组件指南](./components-guide.md)
- 掌握开发工具和调试流程 → [工具与调试](./tools-and-debug.md)
- 查阅编码规范 → [编码规范](./coding-standards.md)
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
