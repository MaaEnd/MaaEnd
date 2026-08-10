# IconRecognition

`IconRecognition` 是 MaaEnd 的 C++ 物品图标识别组件。调用方提供当前界面的 `grid_type` 和 Maa ROI，组件返回物品 ID、位置与匹配分数；界面跳转和后续操作仍由 Pipeline 或业务 Service 控制。

组件只公开一个 `recognize` API，支持按物品 ID 查找、识别网格内全部物品和识别指定正方形 ROI。调用参数、参考 ROI、返回字段和错误处理统一放在开发者使用指南中；本目录其余文档只面向组件维护者。

运行时只读取公开资源：

- `assets/data/IconRecognition/recognition_items.json`
- `assets/resource/image/IconRecognition/<rarity>/*.png`
- `assets/locales/interface/*.json`

详细文档：

- [开发者使用指南：调用、参数、ROI 与返回值](/docs/zh_cn/developers/components/icon-recognition.md)
- [内部架构与维护边界](docs/architecture.md)
- [识别算法](docs/algorithm.md)
- [网格配置维护](docs/grid-profiles.md)
- [测试命令与人工审核图](docs/testing.md)
- [资源下载与发布工具](../../../../tools/icon_recognition/README.md)

截图或识别前，建议先将鼠标移动到不会遮挡物品网格的位置（例如左上角），并使用 `pre_wait_freezes`、`post_wait_freezes` 或等效机制等待画面稳定。
