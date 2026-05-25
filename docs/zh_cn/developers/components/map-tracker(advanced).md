# 开发手册 - MapTracker 高级参考文档

## 简介

此文档介绍了 **MapTracker** 相关组件的**进阶性内容**。适用于以下类型的读者：

- 您想要在代码级别来调用 MapTracker 库，以实现更复杂的功能；
- 您是 MapTracker 的维护者，希望学习 MapTracker 的日常维护方法。

> [!WARNING]
>
> 如果您只希望在 pipeline 中，低代码地调用 MapTracker 的相关节点，您无需阅读此进阶性文档。请您直接阅读[这一份文档](./map-tracker.md)。

## 编程节点说明

下面将详细介绍 MapTracker 中不能用于低代码调用的编程节点。这些节点只适合进行代码级别的调用，不宜在 pipeline 中使用。

### Recognition: MapTrackerInfer

📍获取玩家当前所处的地图名称、位置坐标和朝向。

> [!TIP]
>
> MapTracker 使用一个介于 $[0, 360)$ 的整数来表示玩家的**朝向**，单位是度。0° 表示朝向正北方向，以顺时针旋转为递增方向。

#### 节点参数

必填参数：无

可选参数：

- `map_name_regex`: 用于筛选地图名称的[正则表达式](https://regexr.com/)。仅匹配该正则表达式的地图会参与识别。例如：
    - `^map\\d+_lv\\d+$`: 默认值。匹配所有常规地图。
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: 匹配所有常规地图和分层地图（Tier）。
    - `^map01_lv001$`: 仅匹配 "map01_lv001"（四号谷地-枢纽区）。
    - `^map01_lv\\d+$`: 匹配 "map01"（四号谷地）的所有子区域。

- `precision`: 介于 $(0, 1]$ 的实数，默认 `0.5`。控制匹配的精确度。较大的值会更严格地匹配地图特征，但可能导致匹配速度缓慢；较小的值会极大提升匹配速度，但可能导致结果错误。在需要匹配的地图数量较少时（例如只匹配一张地图），推荐使用较大的值以获得更准确的结果。

- `threshold`: 介于 $(0, 1]$ 的实数，默认 `0.4`。控制匹配的置信度阈值。低于此值的匹配结果将不命中识别。

### Recognition: MapTrackerBigMapInfer

🗺️ 在大地图界面中推断当前视野区域在地图中的坐标与地图缩放。

> [!TIP]
>
> “当前视野区域”的裁切规则请参见具体代码中的定义。

#### 节点参数

请参见具体代码中 `MapTrackerBigMapInferParam` 的类型定义。

## 维护办法

MapTracker 的日常维护主要涉及的是**地图图片的更新**。当游戏开放了新版本时，需要将最新的地图同步到 MapTracker 的地图图片库中。

目前，地图数据和地图图片的来源是 zmdmap。您可以通过运行**地图获取与生成脚本**来轻松地完成地图图片的更新。

### 操作步骤

> [!TIP]
>
> 运行脚本要求安装 Python 及 `opencv-python`、`PyMaxflow` 依赖库。
>
> ```bash
> pip install opencv-python PyMaxflow
> ```

该工具脚本的完整操作步骤如下：

1. 从 zmdmap 拉取最新的地图数据：

    ```bash
    python tools/map_tracker/map_fetcher.py json -o tools/map_tracker/data
    ```

2. 从 zmdmap 拉取最新的 Region 地图的原始图片（并将其切割为若干 Level 地图图片），同时拉取最新的 Tier 地图的原始图片：

    ```bash
    python tools/map_tracker/map_fetcher.py image -i tools/map_tracker/data -o tools/map_tracker/images
    ```

3. 对所有 Level 地图图片进行重叠区域再分配：

    ```bash
    python tools/map_tracker/map_generator.py distinguish_levels -i tools/map_tracker/images -o tools/map_tracker/final --layout-dir tools/map_tracker/data
    ```

4. 对所有 Tier 地图图片进行画布扩展和背景叠加：

    ```bash
    python tools/map_tracker/map_generator.py tidy_tiers -i tools/map_tracker/images -o tools/map_tracker/final
    ```

5. 生成最终地图图片的 BBox 数据：

    ```bash
    python tools/map_tracker/map_generator.py bbox -i tools/map_tracker/final -o tools/map_tracker/final
    ```

6. 得到的 `tools/map_tracker/final` 目录下的图片和 BBox 数据即为最新的地图图片库。

### 名词解释

- Region 地图：指的是游戏中一个地区的大地图（多个 Level 合并后的地图）；

- Level 地图：指的是游戏中一个地区的子区域地图；

- Tier 地图：指的是游戏中的分层地图；

- 重叠区域再分配：为了保证同一地点不会在两个 Level 地图中同时出现，采用了一种基于最大流切割的算法，将多个 Level 的重叠区域划分到合适的 Level 中。

- 画布扩展：为了方便计算坐标，会把 Tier 地图的画布扩展到与对应 Level 地图相同的尺寸。

- 背景叠加：由于游戏内的 Tier 地图会在对应 Level 地图的基础上进行叠加显示，因此在生成 Tier 地图时也会把对应 Level 地图的图片内容叠加到 Tier 地图上作为背景，以提高识别的精度。

- BBox 数据：记录的是每一张地图图片的边界框坐标数据，用于减少匹配时的运算量。

### 备选方案

若因不可抗力原因导致 zmdmap 停止提供服务，只要能有以下数据就能实现地图图片的更新：

1. 地图数据：所有 Region 和 Level 的名称、几何坐标数据。

2. Region 地图的解包图片：游戏内事实上采用了 600\*600 的图网来存储地图图片（原始尺寸），可能需要自行拼接这些图片来得到完整的 Region 地图图片。

    > [!TIP]
    >
    > 720P PC 游戏中，小地图的缩放倍率是原始地图尺寸的 0.1625 倍。

3. Tier 地图的解包图片及 Tier 归属信息。
