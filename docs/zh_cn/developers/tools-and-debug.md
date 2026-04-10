# 工具与调试

## 开发工具

MaaFramework 提供了丰富的[开发工具](https://github.com/MaaXYZ/MaaFramework/tree/main?tab=readme-ov-file#%E5%BC%80%E5%8F%91%E5%B7%A5%E5%85%B7)，调试时工作目录设为**项目根目录**。

| 工具                                                                       | 简介                                                        |
| -------------------------------------------------------------------------- | ----------------------------------------------------------- |
| [MaaDebugger](https://github.com/MaaXYZ/MaaDebugger)                       | 独立调试工具                                                |
| [Maa Pipeline Support](https://github.com/neko-para/maa-support-extension) | VSCode 插件，提供调试、截图、获取 ROI、取色等功能           |
| [MFAToolsPlus](https://github.com/SweetSmellFox/MFAToolsPlus)              | 跨平台开发工具箱，提供便捷的数据获取和模拟测试方法          |
| [MaaPipelineEditor](https://mpe.codax.site/docs)                           | 可视化阅读与构建 Pipeline，功能完备，提供渐进式本地功能扩展 |
| [MaaLogAnalyzer](https://github.com/MaaXYZ/MaaLogAnalyzer)                 | 可视化分析基于 MaaFramework 开发应用的日志                  |
| [MAA-pipeline-generate](https://github.com/Joe-Bao/MAA-pipeline-generate)  | 批量生成仅有细微差异的 Pipeline 模板                        |

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

### 颜色匹配：HSV 优先

不同厂商显卡（NVIDIA、AMD、Intel）渲染存在差异，直接使用 RGB 跨设备不稳定。推荐在 HSV 空间中固定色相，仅调整饱和度和亮度。

### HDR / 颜色管理

**当被提示 "HDR" 或 "自动管理应用的颜色" 等功能已开启时，不要进行截图、取色等操作**，可能导致模板效果与用户实际显示不符。

### 资源文件夹链接

资源文件夹是链接状态，修改 `assets` 等同于修改 `install` 中的内容，无需额外复制。**但 `interface.json` 是复制的**，修改需手动同步或运行 `build_and_install.py`。

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

## 交流

开发 QQ 群: [1072587329](https://qm.qq.com/q/EyirQpBiW4) （干活群，欢迎加入一起开发，但不受理用户问题）
