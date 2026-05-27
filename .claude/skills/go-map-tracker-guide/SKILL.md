---
name: go-map-tracker-guide
description: MaaEnd MapTracker 相关组件编写指南。为 agent/go-service/maptracker 下的 Go 代码提供说明，并提供 MapTracker 的开发文档指引。在参与开发 MapTracker 的 Go 代码实现时，或需要了解 MapTracker 详细工作原理时使用。
---

# MaaEnd MapTracker 组件编写指南

**MapTracker** 是 MaaEnd 项目中，通过计算机识别方法识别游戏内的地图信息，以提供玩家定位、寻路、导航等功能的组件。

需要注意当前项目中存在两套相似的系统，一套是使用 Go 编写的 MapTracker，另一套是使用 Cpp 编写的 MapNavigator/Locator，两套系统的实现方式完全不同且没有交集，本指南针对的是 Go 版本的 MapTracker，在开发时请注意区分。

## 参考资料

### 重要文档

当你判断确实正在进行 MapTracker 的开发工作时，*务必无条件地先读取下列文档*以快速了解详细内容：

- docs/zh_cn/developers/components/map-tracker.md 列出了 pipeline JSON 调用方视角下的 MapTracker 的使用方式；
- docs/zh_cn/developers/components/map-tracker(advanced).md 列出了更具体的有关 MapTracker 维护和开发的细节。

## 组件概览

### 核心代码

Go 代码位于 agent/go-service/maptracker 目录下，主要包含以下子包：

- default 包：主要提供小地图（游戏画面左上角的实时小地图）识别、寻路的功能，属于核心功能；
  - assert_location：MapTrackerAssetLocation 节点实现；
  - infer.go：MapTrackerInfer 节点实现；
  - move.go：MapTrackerMove 节点实现；
  - 其他辅助文件。
- bigmap 包：主要提供大地图（游戏内打开地图页面时显示的大地图）识别的功能；
  - infer.go：MapTrackerBigMapInfer 节点实现；
  - pick.go：MapTrackerBigMapPick 节点实现；
  - 其他辅助文件。
- internal 包：主要提供一些辅助功能；
  - resource.go：资源加载辅助。
- compatible 包：对 Cpp 方案的兼容层，次要，一般无需维护。

主要的依赖项是 agent/go-service/pkg/minicv 包，提供了定制化的计算机视觉功能，例如模板匹配。

### 工具代码

为了帮助使用者和维护者对地图图片、路线进行快速的操作和可视化，在 tools/map_tracker 目录下提供了一些使用 Python 写的工具代码。具体如下：

- _internal 包：
  - core_utils.py：常用工具函数；
  - gui_widget.py：提供了一些可复用的工具 GUI 组件；
  - http_utils.py：下载相关；
  - location_service.py：依赖于 maa_interface.py 提供工具内调用 MapTracker 定位的功能；
  - maa_interface.py：提供了与游戏交互的接口；
  - pipeline_handler.py：提供了 pipeline JSON 解析的功能；
  - zmdmap_schemas.py：提供了 zmdmap 数据解析的功能。
- map_fetcher.py：提供了从 zmdmap 获取最新地图图片的功能；
- map_generator.py：提供了基于最新图片来生成优化后的地图图片和数据的功能；
- map_tracker_editor：向用户提供路线编辑等可视化功能。
