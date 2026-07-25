---
name: go-map-tracker-guide
description: MaaEnd MapTracker 相关组件编写指南。为 agent/go-service/maptracker 下的 Go 代码提供说明，并提供 MapTracker 的开发文档指引。在参与开发 MapTracker 的 Go 代码实现时，或需要了解 MapTracker 详细工作原理时使用。
---

# MaaEnd MapTracker 组件编写指南

**MapTracker** 是 MaaEnd 项目中，通过计算机识别方法识别游戏内的地图信息，以提供玩家定位、寻路、导航等功能的组件。

需要注意当前项目中存在两套相似的系统，一套是使用 Go 编写的 MapTracker，另一套是使用 Cpp 编写的 MapNavigator/Locator，两套系统的实现方式完全不同且没有交集，本指南针对的是 Go 版本的 MapTracker，在开发时要区分。

## 参考资料

### 重要文档

当你判断确实正在进行 MapTracker 的开发工作时，*务必无条件地先读取下列文档*以快速了解详细内容：

- docs/zh_cn/developers/components/map-tracker.md 列出了 pipeline JSON 调用方视角下的 MapTracker 的使用方式；
- docs/zh_cn/developers/components/map-tracker(advanced).md 列出了更具体的有关 MapTracker 维护、开发和测试的细节。

## 组件概览

### 核心代码

**Go** 代码位于 agent/go-service/maptracker 目录下，主要包含以下子包：

- default 包：主要提供**小地图**（游戏画面左上角的实时小地图）识别、寻路的功能，属于核心功能；
    - assert_location：MapTrackerAssetLocation 位置条件判断 节点实现；
    - infer.go：MapTrackerInfer 玩家位置和朝向识别 节点实现；
    - move.go：MapTrackerMove 依照指定路径移动 节点实现；
    - goal.go：MapTrackerGoal 依照 NavMesh 移动 节点实现；
    - 其他节点文件和辅助文件。
- bigmap 包：主要提供**大地图**（游戏内打开地图页面时显示的大地图）识别的功能；
    - infer.go：MapTrackerBigMapInfer 视窗位置识别 节点实现；
    - find_image.go：MapTrackerBigMapFindImage 大地图中的图标识别 节点实现；
    - zoom.go：MapTrackerBigMapZoom 调节大地图缩放 节点实现；
    - pick.go：MapTrackerBigMapPick 点击指定大地图点 节点实现；
    - 其他辅助文件。
- internal 包：主要提供一些辅助和公共功能；
    - algo.go：二维几何点及相关算法实现；
    - nav_mesh.go：NavMesh 数据解析实现；
    - resource.go：资源加载辅助；
    - 其他辅助文件和测试文件。
- compatible 包：对 Cpp 方案的兼容层，次要，一般无需维护。

主要的依赖项是 agent/go-service/pkg/minicv 包，提供了定制化的计算机视觉功能，例如模板匹配。

### 工具代码

为了帮助使用者和维护者对地图图片、路线进行快速的操作和可视化，在 tools/map_tracker 目录下提供了一些使用 **Python** 写的工具代码。具体如下：

- \_internal 包：
    - core_utils.py：常用工具函数；
    - http_utils.py：下载相关；
    - location_service.py：依赖于 maa_interface.py 提供工具内调用 MapTracker 定位的功能；
    - maa_interface.py：提供了与游戏交互的接口；
    - nav_mesh.py：提供了 NavMesh 数据解析的功能；
    - pipeline_handler.py：提供了 pipeline JSON 解析的功能；
    - sample_collector.py：提供 Web 工具的测试样本采集服务；
    - sample_files.py：提供测试样本文件名和坐标索引；
    - zmdmap_schemas.py：提供了 zmdmap 数据解析的功能。
- map_fetcher.py：提供了从 zmdmap 获取最新地图图片的功能（常通过 CI 运行）；
- map_generator.py：提供了基于最新图片来生成优化后的地图图片和数据的功能（常通过 CI 运行）；
- map_tracker_tester.py：提供 MapTrackerInfer 节点小地图推理功能的集成测试（常通过 CI 运行）；
- map_tracker_master.py：提供节点编辑、日志分析、NavMesh 编辑和测试数据采集的全能 Web 工具（专给人类用户使用）。

### 其他牵涉的文件

除了上述两种主要的代码类型外，MapTracker 还会涉及到一些其他文件：

- .github/workflows/sync-zmdmap.yml：会调起 map_fetcher.py 和 map_generator.py 来同步最新的地图图片和数据；
- .github/workflows/test.yml：会调起 map_tracker_tester.py 运行测试；
- tools/schema/：其中包含有 MapTracker pipeline 节点的 JSON Schema 定义文件以方便 IDE 解析，仅在修改了节点参数定义后需要编辑；
- 以及我们之前提到的 docs/ 内的两个文档及其英文副本。

## 风格习惯

### 测试

首先，不要滥用测试。主仓库的 pnpm test/check 是给整个项目的 MaaFW 相关功能的测试，与 MapTracker 无关。

MapTracker 的开发过程中一共可能涉及三种测试：

1. Go 组件的编码测试：主要是通过 go build 编译构建和 go 单元测试完成的。
2. Python 脚本工具和 Web 前端工具的编码测试：主要是通过 python 编译和 pnpm build 构建完成的，工具性代码一般不设单元测试以免增加维护成本。
3. MapTrackerInfer 节点的集成测试：主要是通过 map_tracker_tester.py 脚本完成的，仅在修改了 CV 或识别算法时需要运行。
