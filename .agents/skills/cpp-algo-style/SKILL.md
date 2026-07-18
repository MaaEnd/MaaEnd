---
name: cpp-algo-style
description: MaaEnd cpp-algo C++ 编码风格与工程规范指南。参考 MaaFramework 的优秀实践，规范命名、头文件、公共工具、错误处理、日志、CMake 等方面。在编写、修改或审查 agent/cpp-algo/ 下的 C++ 代码时使用。
---

# cpp-algo C++ 编码风格指南

本指南以 MaaFramework 源码为标杆，针对 cpp-algo 中已知的工程缺陷制定规范。

## 1. 命名规范

cpp-algo 当前最严重的问题是 **命名风格不统一**，同一个项目中混用了多种约定。

### 必须遵循的规则

| 元素               | 风格                                       | 示例                                   |
| ------------------ | ------------------------------------------ | -------------------------------------- |
| 类 / 结构体 / 枚举 | PascalCase                                 | `MapLocator`, `MatchFeature`           |
| 成员函数           | camelCase                                  | `initialize()`, `resetTrackingState()` |
| 自由函数           | PascalCase（对外）/ snake_case（内部工具） | `CreateInputBackend()`                 |
| 成员变量           | snake*case + 尾下划线 `*`                  | `locator_`, `current_zone_id_`         |
| 局部变量           | snake_case                                 | `search_rect`, `fine_result`           |
| 常量（constexpr）  | `k` 前缀 + PascalCase                      | `kDefaultMinimapRoi`, `kMaxLostFrames` |
| 宏                 | ALL_CAPS                                   | `MAA_TRUE`, `LOG_ARGS`                 |
| 命名空间           | lowercase                                  | `maplocator`, `mapnavigator`           |
| 模板参数           | 后缀 `_t` 或 PascalCase                    | `Item_t`, `OutT`                       |
| 枚举值             | PascalCase                                 | `TrackingLost`, `ScreenBlocked`        |

### 已知违规（修改时顺手修复）

- `MapPosition` 中 `zoneId`、`sliceIndex` 等用了 camelCase 成员，应改为 `zone_id_`、`slice_index_`
- `TrackingConfig` 中 `maxNormalSpeed`、`screenBlockedThreshold` 同理
- `MatchStrategy` 中 `_isBase`（前导下划线）应改为 `is_base_`
- 常量 `MinimapROIOriginX`、`MaxLostTrackingCount`、`MinMatchScore` 缺少 `k` 前缀

## 2. 头文件规范

### Include Guard

统一使用 `#pragma once`（已做到）。

### Include 顺序

按以下分组排列，组间空行分隔：

1. 本 `.cpp` 对应的 `.h`
2. C++ 标准库 `<algorithm>`, `<string>` ...
3. 第三方库 `<opencv2/...>`, `<meojson/json.hpp>`, `<onnxruntime/...>`
4. MaaFramework `<MaaFramework/...>`, `<MaaUtils/...>`
5. 本项目头文件 `"MapTypes.h"`, `"../utils.h"`

### OpenCV 引入

**必须** 通过 `<MaaUtils/NoWarningCV.hpp>` 引入 OpenCV，禁止直接 `<opencv2/opencv.hpp>`。这是 MaaFramework 的统一做法，用于抑制编译器警告。

当前违规文件：`MatchStrategy.h`、`MotionTracker.h`、`YoloPredictor.h` 直接引入了 `<opencv2/opencv.hpp>`。

### MaaFramework 头文件引号

对外部依赖（MaaFramework、第三方库）统一使用尖括号 `<>`，对本项目内部头文件使用双引号 `""`。

## 3. 公共工具复用（消除重复代码）

cpp-algo 中存在多处 **重复实现**，必须提取到公共头文件。

### 必须提取的工具

**`ScopedImageBuffer`** — 当前在 `MapLocateAction.cpp`、`position_provider.cpp`、`adb_input_backend.cpp` 三处重复定义，应提取到公共头文件（如 `source/common/scoped_buffer.h`）：

```cpp
class ScopedImageBuffer
{
public:
    ScopedImageBuffer() : buffer_(MaaImageBufferCreate()) {}
    ~ScopedImageBuffer() { MaaImageBufferDestroy(buffer_); }

    ScopedImageBuffer(const ScopedImageBuffer&) = delete;
    ScopedImageBuffer& operator=(const ScopedImageBuffer&) = delete;

    MaaImageBuffer* Get() const { return buffer_; }

private:
    MaaImageBuffer* buffer_;
};
```

**`DetectControllerType`** — 当前在 `MapLocateAction.cpp`、`position_provider.cpp`、`backend.cpp` 三处重复实现，应提取到 `controller_type_utils.h`。

**`MAA_TRUE` / `MAA_FALSE` 宏** — 在多个 `.cpp` 中条件定义。应在一个公共头文件中统一处理，或直接使用 `MaaBool` 的 `1` / `0`。

### 新增公共工具的原则

- 在 `source/common/` 下建立公共头文件
- 跨模块（MapLocator / MapNavigator）共用的工具放这里
- 模块内部工具放在模块自己的匿名命名空间或 `detail` 命名空间中

## 4. 命名空间

### 正确做法

- 顶层按模块分：`maplocator`、`mapnavigator`
- 后端按层级嵌套：`mapnavigator::backend::adb`
- 实现细节用匿名命名空间（`namespace { }` 在 `.cpp` 中）

### 避免的问题

`utils::SleepFor` 嵌套在 `mapnavigator` 命名空间中（`navi_math.h`），而 `source/utils.h` 是另一个全局工具头。这造成了命名空间语义冲突。应将通用工具统一放入 `source/common/`。

## 5. 类设计

### 推荐模式（参考 MaaFramework）

- **PIMPL**：对外暴露的复杂类使用 PIMPL 隐藏实现（`MapLocator` 已正确使用）
- **NonCopyable**：需要禁止拷贝的类应明确 `= delete` 拷贝构造和赋值（`ScopedImageBuffer` 已做到，但建议提取基类或用宏）
- **Strategy 模式**：`IMatchStrategy` + Factory 的设计是好的，保持
- **RAII**：资源获取即初始化，析构时释放（`ScopedImageBuffer` 是好例子）

### 需要改进的点

- `NavigationStateMachine` 持有多个裸指针（`ActionWrapper*` 等），生命周期依赖调用者保证——应添加注释说明所有权语义，或使用 `std::shared_ptr` / `std::weak_ptr`
- 全局单例 `getOrInitLocator()` 使用 `static std::shared_ptr`——可接受但应注意线程安全和测试性

## 6. 日志规范

遵循 [maa-logging skill](../maa-logging/SKILL.md) 的完整指南。此处强调 cpp-algo 特有的问题：

### 禁止高频大量日志

YOLO 推理中每帧输出完整 softmax 向量是 **严重性能问题**：

```cpp
// 错误 — 每帧打印完整分类向量
LogInfo << "YOLO Raw All:" << yoloClassNames << std::vector<float>(...);

// 正确 — 仅输出关键结果，详细信息用 LogTrace
LogDebug << "YOLO:" << VAR(predicted_name) << VAR(max_conf);
LogTrace << "YOLO all scores:" << scores;
```

### 日志级别选择

| 场景                          | 级别       |
| ----------------------------- | ---------- |
| 初始化成功/失败、关键状态变更 | `LogInfo`  |
| 定位结果、导航阶段切换        | `LogInfo`  |
| 匹配分数、中间计算            | `LogDebug` |
| 完整矩阵/向量数据             | `LogTrace` |
| 可恢复异常（追踪丢失）        | `LogWarn`  |
| 不可恢复错误                  | `LogError` |

## 7. 错误处理

### 模式

- 返回 `bool` / `std::optional` 表示成功/失败
- 失败路径 `LogError` + 早期 `return`
- OpenCV 操作用 `try/catch` 保护（`CoreMatch` 中已有，应推广到其他 cv 调用密集处）
- MaaFramework C API 返回值必须检查

### 禁止

- 静默忽略错误
- 假设指针非空而不检查

## 8. 现代 C++ 用法

项目目标 **C++20**，应积极使用现代特性：

| 推荐              | 示例                                              |
| ----------------- | ------------------------------------------------- |
| `std::optional`   | 返回可能失败的结果                                |
| 指定初始化器      | `LocateResult { .status = ..., .position = ... }` |
| `std::filesystem` | 路径操作                                          |
| `std::format`     | 字符串格式化（替代 `std::stringstream`）          |
| `std::ranges`     | 容器算法链（`controller_type_utils.h` 中已用）    |
| `constexpr`       | 编译期常量                                        |
| 结构化绑定        | `auto [x, y] = getPosition();`                    |
| smart pointers    | `std::unique_ptr` / `std::shared_ptr` 管理资源    |

### X-Macro 的使用

`NAVI_ACTION_TYPES(X)` 宏用于生成枚举和字符串映射。这种模式可以接受，但应：

- 在宏定义处添加注释解释用途
- 确保使用 `#undef` 清理临时宏

## 9. 魔法数字

cpp-algo 中散布大量硬编码阈值（`0.43`、`4.5`、`0.85`、`0.55` 等）。

### 规则

- **所有阈值** 必须定义为 `constexpr` 命名常量，带 `k` 前缀
- 常量定义集中放在对应模块的 config 结构体或头文件顶部
- 必须附带注释说明物理含义和调优依据

```cpp
// 错误
if (score < 0.55) { return false; }

// 正确
constexpr double kGlobalSearchPassThreshold = 0.55;  // 全局搜索及格线，容忍 UI 遮挡 + 光影
if (score < kGlobalSearchPassThreshold) { return false; }
```

## 10. CMake 规范

### 禁止 `file(GLOB_RECURSE)`

当前 `source/CMakeLists.txt` 使用 `file(GLOB_RECURSE)` 自动收集源文件。CMake 官方文档明确不推荐此做法（新增/删除文件不会触发重新配置）。

应改为显式列出源文件：

```cmake
target_sources(cpp-algo PRIVATE
    main.cpp
    MapLocator/MapLocator.cpp
    MapLocator/MapLocateAction.cpp
    # ...
)
```

### 清理未使用变量

`${cpp_algo_header}` 从未定义却被引用，应清除。

## 11. 文件命名

| 元素          | 风格                                   | 示例                                     |
| ------------- | -------------------------------------- | ---------------------------------------- |
| 类对应的文件  | PascalCase                             | `MapLocator.h`, `MapLocator.cpp`         |
| 工具/非类文件 | snake_case                             | `controller_type_utils.h`, `navi_math.h` |
| 目录          | PascalCase（模块）/ snake_case（工具） | `MapLocator/`, `Backend/Adb/`            |

当前 `my_reco_1/` 是示例模板目录，如果保留应重命名为有意义的名称。

## 12. 注释语言

- 代码注释使用 **中文** 或 **英文** 均可，但单个文件内保持一致
- 对外接口（`.h` 中的 public 方法）建议英文注释
- 算法实现细节（`.cpp` 中）用中文注释解释"为什么"是可以的（当前做得好的部分）

## 审查清单

修改 cpp-algo 代码时，对照检查：

- [ ] 命名风格是否符合上表
- [ ] 是否引入了重复代码（检查是否已有公共工具）
- [ ] OpenCV 是否通过 `NoWarningCV.hpp` 引入
- [ ] 日志级别是否合理，是否避免了高频大量输出
- [ ] 新常量是否有 `k` 前缀和注释
- [ ] 错误路径是否有日志和合理返回值
- [ ] 新文件是否加入了 CMakeLists.txt 的显式列表（如已迁移）

详细的命名对照和重构示例见 [reference.md](reference.md)。
