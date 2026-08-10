# 网格配置维护

本文档说明网格定位配置如何组织和验证，只面向 `IconRecognition` 实现维护者。调用者需要的 `grid_type`、参考 ROI 和参数写法统一见[开发者使用指南](/docs/zh_cn/developers/components/icon-recognition.md)。

## 事实来源

网格相关信息按用途维护，避免形成多个事实来源：

- 调用者可见的界面类型和参考 ROI：中英文开发者使用指南；
- C++ 枚举和字符串映射：`IconRecognitionTypes.h`；
- 通用与双侧网格 profile：`detail/GridProfiles.h` 和 `detail/GridProfiles.cpp`；
- 检测、候选选择和回退逻辑：`detail/GridDetector.cpp` 及其拆分组件；
- 标定常量的含义、调参影响和依据：对应 `constexpr` 或 profile 字段旁的注释；
- 行为边界：小算法测试、Custom 入口测试和全量截图差异测试。

内部阈值、权重、搜索范围和图像算法参数不得复制到本文件。修改这些值时，应直接修改代码旁的注释并补充或更新能锁定行为的测试。

## Profile 分层

`GridProfile` 描述常规单网格的基本几何约束，包括格子尺寸、横纵间距和最小可接受布局。`ProfileFor()` 根据 `GridType` 返回相应配置。

`TransferGridProfile` 描述背包、仓库和便捷存取站共用的双侧网格约束。`TransferProfileFor()` 再根据左右侧和具体界面选择变体。双侧 profile 除几何范围外，还约束颜色条锚点、可见范围和候选扩展边界。

两种 profile 都只提供定位策略所需的先验，不是公开协议。调用方不能根据这些字段自行计算 ROI 或格子坐标。

## 各类网格的维护入口

| 内部类型 | 定位策略 | 主要维护位置 |
| ------------------------ | ---------------------------------------------------- | ------------------------------------------------------------ |
| `GridType::Trade` | 常规格框结构定位，匹配阶段使用界面专用图标区域 | `GridProfiles.cpp`、`GridDetector.cpp`、`IconRecognizer.cpp` |
| `GridType::Transfer` | 左右区域发现，结合格框结构与稀有度颜色条拟合规则网格 | `GridProfiles.cpp`、`GridAnchors.cpp`、`GridDetector.cpp` |
| `GridType::PortStorager` | 复用双侧规则网格，但使用独立区域和边界策略 | `GridProfiles.cpp`、`GridAnchors.cpp`、`GridDetector.cpp` |
| `GridType::Valuables` | 常规格框结构定位，匹配阶段处理头像遮挡 | `GridProfiles.cpp`、`GridDetector.cpp`、`MaskPolicy.cpp` |
| `GridType::Shipment` | 常规格框结构定位，匹配阶段检查并遮罩数量条 | `GridProfiles.cpp`、`GridDetector.cpp`、`MaskPolicy.cpp` |
| `GridType::CreditTrade` | 使用信用交易卡片布局与专用区域偏移 | `GridProfiles.cpp`、`GridDetector.cpp`、`IconRecognizer.cpp` |
| `GridType::SingleRoi` | 不执行网格检测，直接构造临时格子 | `IconRecognizer.cpp` |

## 双侧网格的约束

双侧网格同时接受大 ROI 和单侧 ROI。`PartitionTransferRegions()` 负责把输入范围转换成候选区域；`DiscoverTransferGridHints()` 从结构响应中召回可能的面板；`GridAnchors` 和 `RegularLattice` 再用结构及颜色条观测确定最终横纵轴。

维护时需要保持以下边界：

- 大 ROI 可以发现两个独立面板，单侧 ROI 不能凭先验扩展到另一侧；
- 颜色条只能修正有直接证据支持的相位，不能从少量颜色像素生成整片网格；
- 结构候选与颜色条候选冲突时，选择依据必须进入网格级诊断；
- 被 ROI 裁切或被界面元素遮挡的格子只能在可见结构范围内补足；
- 最终坐标由统一规则网格投影，不能逐格累积间距。

具体步骤和术语见[识别算法](algorithm.md)。

## 修改或新增网格类型

1. 在 `GridType`、字符串解析和 `ProfileFor()` 中建立明确映射。
2. 判断新界面可复用常规结构定位、双侧定位，还是需要独立检测器；不要把界面分支堆进无关模块。
3. 将标定值定义为带说明的命名常量或 profile 字段，不在 Markdown 中抄写数值。
4. 为几何拟合、裁切边界和失败路径添加小算法测试。
5. 使用真实截图补充全量差异测试，并检查 debug 标注中的格子范围。
6. 如果调用字符串或参考 ROI 变化，同步中英文开发者使用指南；如果只调整内部实现，仅更新测试和必要的内部说明。
