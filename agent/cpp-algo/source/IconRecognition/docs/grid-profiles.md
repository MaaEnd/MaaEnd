# 网格类型与参考 ROI

真实界面的网格定位是 `IconRecognition` 内部能力。调用方只需要选择与当前界面对应的 `grid_type` 并提供 Maa ROI，不应依赖内部 cell 尺寸、间距、分区或拟合过程。

| `grid_type` | 界面 |
| --------------- | ---------- |
| `trade` | 据点交易 |
| `transfer` | 背包和仓库 |
| `port_storager` | 便捷存取站 |
| `valuables` | 贵重品库 |
| `shipment` | 送货界面 |
| `credit_trade` | 信用交易所 |
| `single_roi` | 临时单格 |

默认候选过滤器和完整参数语义见[参数与数据契约](architecture.md)。`single_roi` 会直接使用正方形 ROI 构造一个临时 cell，不执行真实网格检测。各界面的图标尺寸属于对应内部类型的实现特性，不应把其中某个尺寸泛化到其它类型。

## 双侧网格 ROI

`transfer` 和 `port_storager` 都支持两种 ROI：传入覆盖左右两侧网格的整块大 ROI 时，组件会在内部拆分并识别两侧；传入只覆盖左侧或右侧的 ROI 时，只识别该侧。实际流程通常分别处理两侧，建议按当前操作目标传入单侧 ROI。

单侧 ROI 仍使用 1280x720 画面上的绝对 Maa 坐标，不是相对于大 ROI 的局部坐标，并应完整覆盖目标侧需要识别的 cell。

## 参考 ROI

以下坐标全部使用 1280x720 基准下的 Maa `[x,y,width,height]`。参考值需要与实际界面和图标位置对应，不是跨界面通用坐标。除 `single_roi` 外，人工测试使用提交到仓库的 [`test/rois.json`](../test/rois.json) 作为参考 ROI 单一来源。

### 据点交易

参考 ROI：`[170,165,935,385]`。

<details>
<summary>展开据点交易示例图</summary>

![settlement-trade](https://github.com/user-attachments/assets/7e8623ad-61be-4415-add8-c1c2abf95390)

</details>

### 背包和仓库

- 完整区域：`[154,202,983,291]`；
- 左侧：`[154,202,585,291]`；
- 右侧：`[739,202,398,291]`。

<details>
<summary>展开背包和仓库示例图</summary>

![inventory-transfer](https://github.com/user-attachments/assets/bc1bbea5-5aa4-4421-9ccb-b3de8314688e)

</details>

### 便捷存取站

- 完整区域：`[190,250,880,350]`；
- 左侧：`[190,250,318,350]`；
- 右侧：`[570,250,500,350]`。

<details>
<summary>展开便捷存取站示例图</summary>

![port-storager](https://github.com/user-attachments/assets/f0fab5de-186d-40df-b212-7b8ae6714103)

</details>

### 贵重品库

参考 ROI：`[24,76,950,570]`。

<details>
<summary>展开贵重品库示例图</summary>

![valuables](https://github.com/user-attachments/assets/4121c648-94b8-4032-8afd-3436cf31f99b)

</details>

### 送货界面

参考 ROI：`[34,132,386,474]`。

<details>
<summary>展开送货界面示例图</summary>

![shipment](https://github.com/user-attachments/assets/61eb016e-13f6-4e5d-80f1-c1d1eecb57d7)

</details>

### 信用交易所

参考 ROI：`[70,95,1140,415]`。

<details>
<summary>展开信用交易所示例图</summary>

![credit-trade](https://github.com/user-attachments/assets/c4415a7c-56d0-4aa2-bf2e-230bae21211d)

</details>

### `single_roi`

`single_roi` 用于识别调用方指定正方形 ROI 内的物品。它不绑定某个固定界面，也不执行真实网格检测；ROI 宽高必须相等且完全位于图片内。

#### 典型示例

##### 据点交易入口

示例 ROI：`[1177,450,54,54]`。人工测试目录名使用 `1177-450-54` 表示该区域。

<details>
<summary>展开据点交易入口示例图</summary>

![specific-roi-example](https://github.com/user-attachments/assets/76e7f9d0-ed4e-4feb-b1b4-afbc40ac6003)

</details>

## 64px 双侧网格的参数职责

以下参数只用于内部实现和回归测试，调用方不应据此推导 ROI：

| 名称或来源 | 值 | 职责 |
| ----------------------------------------------- | ---------- | --------------------------------------------------------------------- |
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

## 主要标定常量

以下参数以 1280x720 回归样本标定。表格用于说明调参职责，代码中的命名常量仍是数值的单一事实来源。

### 格框结构评分

| 常量 | 值 | 含义 | 适用界面 |
| -------------------------- | ------ | ------------------ | ----------------------------- |
| `kPairedBorderWeight` | `0.45` | 成对格框边缘权重 | 常规单网格、信用交易 fallback |
| `kDirectionalBorderWeight` | `0.35` | 单向连续边缘权重 | 常规单网格、信用交易 fallback |
| `kContrastWeight` | `0.20` | 格内外亮度对比权重 | 常规单网格、信用交易 fallback |
| `kDiagonalPenaltyWeight` | `0.20` | 斜向纹理惩罚权重 | 常规单网格、信用交易 fallback |

### 双侧网格候选峰值

| 常量 | 值 | 含义 | 适用界面 |
| ----------------------------- | ------ | -------------------------------- | ---------------------- |
| `kLocalPeakMaximumRatio` | `0.22` | 候选峰相对全图最大响应的最低比例 | 背包和仓库、便捷存取站 |
| `kLocalPeakPercentile` | `92.0` | 正响应分布的最低百分位 | 背包和仓库、便捷存取站 |
| `kLocalPeakNeighborhoodSize` | `5` | 局部极大值膨胀核边长 | 背包和仓库、便捷存取站 |
| `kLocalPeakEqualityTolerance` | `1e-7` | 峰值相等判断的浮点容差 | 背包和仓库、便捷存取站 |

### 双侧网格候选组合

| 常量 | 值 | 含义与调参影响 | 适用界面 |
| ---------------------------------------- | ------- | ---------------------------------------------------------------------------- | ---------------------- |
| `kTransferHypothesisMinimumOccupancy` | `0.42` | 候选覆盖槽位比例的最低值；调高减少破碎误检，调低召回遮挡网格 | 背包和仓库、便捷存取站 |
| `kTransferLocalizedMaximumWidthRatio` | `0.70` | 宽 ROI 中单侧候选的最大宽度比例；调大可能合并左右面板，调小可能截断面板 | 背包和仓库、便捷存取站 |
| `kIndependentCandidateMinimumScoreRatio` | `0.15` | 独立局部候选相对最强候选的最低分；调高只保留强面板，调低保留弱面板但增加冲突 | 背包和仓库、便捷存取站 |
| `kGridPairMinimumRelativeScore` | `0.15` | 左右配对中较弱一侧相对较强一侧的最低分；调高抑制伪配对，调低提高弱侧召回 | 背包和仓库、便捷存取站 |
| `kTransferStructureBlurSigma` | `0.8px` | 结构响应图的高斯平滑强度；调大抑制噪声，调小保留窄边界但更易受噪声影响 | 背包和仓库、便捷存取站 |

### 模板匹配融合

| 常量 | 值 | 含义与调参影响 | 适用界面 |
| ---------------------------- | ------ | ---------------------------------------------------------------- | ------------ |
| `kTemplateScoreWeight` | `0.85` | 模板结构分数权重；调高更重视轮廓，调低增强颜色判据占比 | 全部图标识别 |
| `kColorScoreWeight` | `0.15` | Lab 颜色分数权重；调高更能区分同轮廓物品，但更易受光照影响 | 全部图标识别 |
| `kCompositeContentSizeRatio` | `7/16` | 复合图标内容层相对底图边长比例；调大提高内容可见性，调小减少覆盖 | 复合图标 |

### 界面专用门控

| 常量 | 值 | 含义 | 适用界面 |
| ----------------------------------------------------- | -------------- | ---------------------------- | -------- |
| `kShipmentQuantityBarHeight` | `20px` | 顶部数量条检查高度 | 送货界面 |
| `kShipmentQuantityBarMinPixels` | `500` | 判定存在数量条的最少颜色像素 | 送货界面 |
| `kValuablesPortraitDetectionRect` | `[60,0,36,42]` | 武器头像圆检测区域 | 贵重品库 |
| `kPortraitHoughDp` | `1.0` | Hough 累加器分辨率比例 | 贵重品库 |
| `kPortraitHoughMinDistance` | `16px` | 圆心最小间距 | 贵重品库 |
| `kPortraitHoughCannyThreshold` | `100.0` | Hough 内部 Canny 高阈值 | 贵重品库 |
| `kPortraitHoughAccumulatorThreshold` | `16.0` | 圆检测累加器阈值 | 贵重品库 |
| `kPortraitHoughMinRadius` / `kPortraitHoughMaxRadius` | `14..22px` | 可接受圆半径 | 贵重品库 |
| `kPortraitCenterMinX` / `kPortraitCenterMaxX` | `70..96px` | 可接受圆心 x 范围 | 贵重品库 |
| `kPortraitCenterMinY` / `kPortraitCenterMaxY` | `0..30px` | 可接受圆心 y 范围 | 贵重品库 |
