# IconRecognition

`IconRecognition` 是 MaaEnd 的 C++ 物品图标识别组件。调用方提供当前界面的 `grid_type` 和 Maa ROI，组件返回物品 ID、位置与匹配分数；界面跳转和后续操作仍由 Pipeline 或业务 Service 控制。

组件只公开一个 `recognize` API，并支持三种请求：

1. 设置 `item_ids`，在网格内查找指定物品的位置。
2. 不设置 `item_ids`，识别网格内所有达到阈值的物品。
3. 使用 `grid_type=single_roi`，把任意正方形 ROI 构造成临时单格并识别其中物品。

运行时只读取公开资源：

- `assets/data/IconRecognition/recognition_items.json`
- `assets/resource/image/IconRecognition/<rarity>/*.png`
- `assets/locales/interface/*.json`

详细文档：

- [接口、参数、分类与出参](docs/architecture.md)
- [识别算法](docs/algorithm.md)
- [内部网格类型与参考 ROI](docs/grid-profiles.md)
- [测试命令与人工审核图](docs/testing.md)
- [资源下载与发布工具](../../../../../tools/icon_recognition/README.md)

截图或识别前，建议先将鼠标移动到不会遮挡物品网格的位置（例如左上角），并使用 `pre_wait_freezes`、`post_wait_freezes` 或等效机制等待画面稳定。
