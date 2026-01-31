# 开发手册

**MaaEnd** 基于 [MaaFramework](https://github.com/MaaXYZ/MaaFramework)，采用 [方案二](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/1.1-%E5%BF%AB%E9%80%9F%E5%BC%80%E5%A7%8B.md#%E6%96%B9%E6%A1%88%E4%BA%8Cjson--%E8%87%AA%E5%AE%9A%E4%B9%89%E9%80%BB%E8%BE%91%E6%89%A9%E5%B1%95%E6%8E%A8%E8%8D%90) 进行开发。
我们的主体流程采用 [Pipeline JSON 低代码](https://github.com/MaaEnd/MaaEnd/tree/main/assets/resource/pipeline)，复杂逻辑通过 [go-service](https://github.com/MaaEnd/MaaEnd/tree/main/agent/go-service) 编码实现。
若有意加入 MaaEnd 开发，可以先阅读 MaaFramework 相关文档，了解低代码逻辑、相关编辑调试工具的使用~

## 本地部署

### 自动设置

我们提供一个自动化的工作区初始化脚本，只需执行：

```bash
python tools/setup_workspace.py
```

即可完整设置开发所需的环境。如果出现问题，你可以参照下方的手动设置指南来分步骤操作。

### 手动设置

1. 完整 clone 项目及子仓库。

    ```bash
    git clone https://github.com/MaaEnd/MaaEnd --recursive
    ```

    **不要漏了 `--recursive`**

    如果你已经 clone 了项目，但没有使用 `--recursive` 参数，现在你可以在项目的根目录执行
    ```bash
    git submodule update --init --recursive
    ```

2. 编译 go-service 、配置路径。

    ```bash
    python tools/build_and_install.py
    ```

3. 下载 [MaaFramework](https://github.com/MaaXYZ/MaaFramework/releases) 并解压 `bin` 内容到 `install/maafw/` 。
4. 下载 [MXU](https://github.com/MistEO/MXU/releases) 并解压到 `install/` 。

## 开发技巧

- MaaFramework 有丰富的 [开发工具](https://github.com/MaaXYZ/MaaFramework/tree/main?tab=readme-ov-file#%E5%BC%80%E5%8F%91%E5%B7%A5%E5%85%B7) 可以进行低代码编辑、调试等，请善加使用。工作目录可设置为 `install` 文件夹。
- 每次修改 Pipeline 后只需要在开发工具中重新加载资源即可；但每次修改 go-service 都需要执行 `python tools/build_and_install.py` 重新进行编译。
- 可利用 vscode 等工具对 go-service 挂断点或单步运行（自行 debug 启动 go-service，或利用 vscode attach）。~~不是哥们，你靠看日志改代码啊？~~
- MXU 是面相终端用户的 GUI，不建议使用其开发调试，上述的 MaaFramework 开发工具可以极大程度提高开发效率。~~真狠啊就硬试啊~~
- MaaEnd 开发中所有图片、坐标均需要以 720p 为基准，MaaFramework 在实际运行时会根据用户设备的分辨率自动进行转换。推荐使用上述开发工具进行截图和坐标换算。
- `resource` 等文件夹是链接状态，修改 `install` 等同于修改 `assets` 中的内容，无需额外复制。**但 `interface.json` 是复制的，若有修改需手动复制回 `assets` 再进行提交。**

## 代码规范

### Pipeline 低代码规范

- 尽可能少的使用 pre_delay, post_delay, timeout, on_error 字段。增加中间节点识别流程，避免盲目 sleep 等待。
- 尽可能保证 next 第一轮即命中（即一次截图），同样通过增加中间状态识别节点来达到此目的。即尽可能扩充 next 列表，保证任何游戏画面都处于预期中。
- 所有操作通过识别进行，禁止硬编码坐标进行点击等操作。

> [!NOTE]
>
> 关于延迟，可扩展阅读 [隔壁 ALAS 的基本运作模式](https://github.com/LmeSzinc/AzurLaneAutoScript/wiki/1.-Start#%E5%9F%BA%E6%9C%AC%E8%BF%90%E4%BD%9C%E6%A8%A1%E5%BC%8F)，其推荐的实践基本等同于我们的 `next` 字段。

### Go Service 代码规范

- Go Service 仅用于处理某些特殊动作/识别，整体流程仍请使用 Pipeline 串联。请勿使用 Go Service 编写大量流程代码。

## 交流

开发 QQ 群: [1072587329](https://qm.qq.com/q/EyirQpBiW4) （干活群，欢迎加入一起开发，但不受理用户问题）
