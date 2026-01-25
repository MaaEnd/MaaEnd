# 开发手册

**MaaEnd** 基于 [MaaFramework](https://github.com/MaaXYZ/MaaFramework)，采用 [方案二](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/1.1-%E5%BF%AB%E9%80%9F%E5%BC%80%E5%A7%8B.md#%E6%96%B9%E6%A1%88%E4%BA%8Cjson--%E8%87%AA%E5%AE%9A%E4%B9%89%E9%80%BB%E8%BE%91%E6%89%A9%E5%B1%95%E6%8E%A8%E8%8D%90) 进行开发。
我们的主体流程采用 [Pipeline JSON 低代码](https://github.com/MaaEnd/MaaEnd/tree/main/assets/resource/pipeline)，复杂逻辑通过 [go-service](https://github.com/MaaEnd/MaaEnd/tree/main/agent/go-service) 编码实现。
若有意加入 MaaEnd 开发，可以先阅读 MaaFramework 相关文档，了解低代码逻辑、相关编辑调试工具的使用~

## 本地运行

1. 完整 clone 项目及子仓库

    ```bash
    git clone https://github.com/MaaEnd/MaaEnd --recursive
    ```

    **不要漏了 `--recursive`**

    或者

    ```bash
    git clone https://github.com/MaaEnd/MaaEnd
    git submodule update --init --recursive
    ```

2. 编译 go-service 、配置路径

    ```bash
    python tools/build_and_install.py
    ```

3. 下载 [MaaFramework](https://github.com/MaaXYZ/MaaFramework/releases) 并解压 `bin` 内容到 `install/maafw/`
4. 下载 [MXU](https://github.com/MistEO/MXU/releases) 并解压到 `install/`
5. 运行 `install/mxu.exe`，且后续使用相关工具编辑、调试等，都基于 `install` 文件夹
6.  `resource` 等文件夹是链接状态，修改 `install` 等同于修改 `assets` 中的内容，无需额外复制。但 `interface.json` 是复制的，若有修改需手动复制回 `assets` 再进行提交。

## 代码规范

### MaaFramework 说明

- MaaFramework 有丰富的 [开发工具](https://github.com/MaaXYZ/MaaFramework/tree/main?tab=readme-ov-file#%E5%BC%80%E5%8F%91%E5%B7%A5%E5%85%B7) 可以进行低代码编辑、调试等，请善加使用
- MaaEnd 开发中所有图片、坐标均需要以 720p 为基准，MaaFramework 在实际运行时会根据用户设备的分辨率自动进行转换。推荐使用开发工具进行截图和坐标换算。

### Pipeline 低代码规范

**MaaEnd** 对 Pipeline 低代码提出额外实践要求：

- 尽可能少的使用 pre_delay, post_delay, timeout, on_error 字段。增加中间节点识别流程，避免盲目 sleep 等待。
- 尽可能保证 next 第一轮即命中（即一次截图），同样通过增加中间状态识别节点来达到此目的。即尽可能扩充 next 列表，保证任何游戏画面都处于预期中。
- 所有操作通过识别进行，禁止硬编码坐标进行点击等操作

### Go service 代码规范

想到再写.jpg

### UI 说明

[MXU](https://github.com/MistEO/MXU) 是一个基于 [PI](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/3.3-ProjectInterfaceV2%E5%8D%8F%E8%AE%AE.md) 协议的通用 GUI。短期内暂无为 MaaEnd 定制 GUI 的计划。  
若想新增字段请考虑相关修改是否足够通用，并向 PI 协议先行提交 issue，等协议确定后再行修改 MXU 相关代码。  
大部分自动化脚本可能涉及的功能通过 PI 协议都可以实现，可以先加群 / issue 讨论是否有必要新增字段。

## 交流

开发 QQ 群: 1072587329 （干活群，不受理用户问题）
