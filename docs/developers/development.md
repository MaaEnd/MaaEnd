# 开发手册

**MaaEnd** 基于 [MaaFramework](https://github.com/MaaXYZ/MaaFramework)，采用 [方案二](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/1.1-%E5%BF%AB%E9%80%9F%E5%BC%80%E5%A7%8B.md#%E6%96%B9%E6%A1%88%E4%BA%8Cjson--%E8%87%AA%E5%AE%9A%E4%B9%89%E9%80%BB%E8%BE%91%E6%89%A9%E5%B1%95%E6%8E%A8%E8%8D%90) 进行开发。
我们的主体流程采用 [Pipeline JSON 低代码](https://github.com/MaaEnd/MaaEnd/tree/main/assets/resource/pipeline)，复杂逻辑通过 [go-service](https://github.com/MaaEnd/MaaEnd/tree/main/agent/go-service) 编码实现。
若有意加入 MaaEnd 开发，可以先阅读 MaaFramework 相关文档，了解低代码逻辑、相关编辑调试工具的使用~

## 本地运行

1. 完整 clone 项目及子仓库。

    ```bash
    git clone https://github.com/MaaEnd/MaaEnd --recursive
    ```

    **不要漏了 `--recursive`**

    或者

    ```bash
    git clone https://github.com/MaaEnd/MaaEnd
    git submodule update --init --recursive
    ```

2. 编译 go-service 、配置路径。

    ```bash
    python tools/build_and_install.py
    ```

3. 下载 [MaaFramework](https://github.com/MaaXYZ/MaaFramework/releases) 并解压 `bin` 内容到 `install/maafw/` 。
4. 下载 [MXU](https://github.com/MistEO/MXU/releases) 并解压到 `install/` 。
5. 运行 `install/mxu.exe`，且后续使用相关工具编辑、调试等，都基于 `install` 文件夹。

> [!TIP]
> MaaFramework 有丰富的 [开发工具](https://github.com/MaaXYZ/MaaFramework/tree/main?tab=readme-ov-file#%E5%BC%80%E5%8F%91%E5%B7%A5%E5%85%B7) 可以进行低代码编辑、调试等，请善加使用。

6. `resource` 等文件夹是链接状态，修改 `install` 等同于修改 `assets` 中的内容，无需额外复制。  
   **但 `interface.json` 是复制的，若有修改需手动复制回 `assets` 再进行提交。**

## 代码规范

### MaaFW 说明

- MaaEnd 开发中所有图片、坐标均需要以 720p 为基准，MaaFramework 在实际运行时会根据用户设备的分辨率自动进行转换。推荐使用 [开发工具](https://github.com/MaaXYZ/MaaFramework/tree/main?tab=readme-ov-file#%E5%BC%80%E5%8F%91%E5%B7%A5%E5%85%B7) 进行截图和坐标换算。

### Pipeline 低代码规范

- 尽可能少的使用 pre_delay, post_delay, timeout, on_error 字段。增加中间节点识别流程，避免盲目 sleep 等待。
- 尽可能保证 next 第一轮即命中（即一次截图），同样通过增加中间状态识别节点来达到此目的。即尽可能扩充 next 列表，保证任何游戏画面都处于预期中。
- 所有操作通过识别进行，禁止硬编码坐标进行点击等操作。

> [!NOTE]
>
> 关于延迟，可扩展阅读 [隔壁 ALAS 的基本运作模式](https://github.com/LmeSzinc/AzurLaneAutoScript/wiki/1.-Start#%E5%9F%BA%E6%9C%AC%E8%BF%90%E4%BD%9C%E6%A8%A1%E5%BC%8F)，其推荐的实践基本等同于我们的 `next` 字段。

### Go service 代码规范

- Go service 仅用于处理某些特殊动作/识别，整体流程仍请使用 Pipeline 串联。请勿使用 Go service 编写大量流程代码。

## 交流

开发 QQ 群: [1072587329](https://qm.qq.com/q/EyirQpBiW4) （干活群，欢迎加入一起开发，但不受理用户问题）
