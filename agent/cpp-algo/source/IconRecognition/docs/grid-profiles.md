# 网格类型与参考 ROI

真实界面的网格定位是 `IconRecognition` 内部能力。调用方只需要选择与当前界面对应的 `grid_type` 并提供 Maa ROI，不应依赖内部 cell 尺寸、间距、分区或拟合过程。

| `grid_type` | 界面 |
| --- | --- |
| `trade` | 据点交易 |
| `transfer` | 背包和仓库 |
| `port_storager` | 便捷存取站 |
| `valuables` | 贵重品库 |
| `shipment` | 送货界面 |
| `credit_trade` | 信用交易所 |
| `single_roi` | 临时单格 |

默认候选过滤器和完整参数语义见[接口与数据契约](architecture.md)。`single_roi` 会直接使用正方形 ROI 构造一个临时 cell，不执行真实网格检测。各界面的图标尺寸属于对应内部类型的实现特性，不应把其中某个尺寸泛化到其它类型。

## 双侧网格 ROI

`transfer` 和 `port_storager` 都支持两种 ROI：传入覆盖左右两侧网格的整块大 ROI 时，组件会在内部拆分并识别两侧；传入只覆盖左侧或右侧的 ROI 时，只识别该侧。实际流程通常分别处理两侧，建议按当前操作目标传入单侧 ROI。

单侧 ROI 仍使用 1280x720 画面上的绝对 Maa 坐标，不是相对于大 ROI 的局部坐标，并应完整覆盖目标侧需要识别的 cell。

## 参考 ROI

下表全部使用 1280x720 基准下的 Maa `[x,y,width,height]`。参考值需要与实际界面和图标位置对应，不是跨界面通用坐标。

| 界面 | 参考 ROI |
| --- | --- |
| 据点交易 | `[170,165,935,385]` |
| 背包和仓库（完整） | `[154,202,983,291]` |
| 背包和仓库（左） | `[154,202,585,291]` |
| 背包和仓库（右） | `[739,202,398,291]` |
| 便捷存取站 | `[190,250,880,350]` |
| 便捷存取站（左） | `[190,250,318,350]` |
| 便捷存取站（右） | `[570,250,500,350]` |
| 贵重品库 | `[24,76,950,570]` |
| 送货界面 | `[34,132,386,474]` |
| 信用交易所 | `[70,95,1140,415]` |
| single ROI 示例 | `[1177,450,54,54]` |

以下示例保留完整 1280x720 截图，并在原图上标出对应 ROI：

![settlement-trade](https://github.com/user-attachments/assets/7e8623ad-61be-4415-add8-c1c2abf95390)

![inventory-transfer](https://github.com/user-attachments/assets/bc1bbea5-5aa4-4421-9ccb-b3de8314688e)

![port-storager](https://github.com/user-attachments/assets/f0fab5de-186d-40df-b212-7b8ae6714103)

![valuables](https://github.com/user-attachments/assets/4121c648-94b8-4032-8afd-3436cf31f99b)

![shipment](https://github.com/user-attachments/assets/61eb016e-13f6-4e5d-80f1-c1d1eecb57d7)

![credit-trade](https://github.com/user-attachments/assets/c4415a7c-56d0-4aa2-bf2e-230bae21211d)

![specific-roi-example](https://github.com/user-attachments/assets/76e7f9d0-ed4e-4feb-b1b4-afbc40ac6003)

除 `single_roi` 外，人工测试使用提交到仓库的 [`test/rois.json`](../test/rois.json) 作为参考 ROI 单一来源。`single_roi` 的坐标由测试目录名提供，例如 `1177-450-54` 表示 `[1177,450,54,54]`。

## 64px 双侧网格的参数职责

以下参数只用于内部实现和回归测试，调用方不应据此推导 ROI：

| 名称或来源 | 值 | 职责 |
| --- | --- | --- |
| `kTransferDiscoveryPitchRange` | `66..74px` | 只用于弱边缘和局部遮挡下的候选召回，不作为最终 pitch |
| `TransferGridProfile::preferred_pitch` | `69px` | 720p 数据的 pitch 先验中心 |
| `TransferGridProfile::pitch_min/max` | `68..70px` | 正式 x/y 浮点全局模型的合法区间；背包右侧五列按 profile 固定为 `69px` |
| `TransferGridProfile::observed_pitch_tolerance` | `1px` | 容忍粗结构峰的观测量化误差，不扩大最终模型区间 |
| `kMaximumRegularAxisResidual` | `2.25px` | 最终全局模型允许的最大观测残差 |

- 整数 cell 坐标统一由 `round(origin + index * pitch)` 投影；相邻 68/69/70px 是统一模型的舍入结果，不是逐格可变 pitch。
- 六档 rarity 分别建证据；灰色只辅助已存在的结构候选，不能独立创建晶格。
- 双侧 64px cell 的可信色带下边界锚定 `cell_top+64px`。条带必须同时满足局部背景对比、连续性、边缘和厚度约束。
- 单行或单列只有一个直接可信 index 时使用 69.0px 先验，并标记低几何置信；纵向最多允许格框证据再补一行，不能远距离补整片网格。
- 至少三行直接可信色带形成稳定晶格后，允许按结构证据补足被遮挡的末行，再以 ROI 可见覆盖率决定是否保留。
