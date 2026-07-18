# cpp-algo 命名重构参考

本文件提供具体的重构示例，供修改代码时对照。

## 成员变量命名：camelCase → snake*case*

### MapTypes.h — `MapPosition`

```cpp
// 当前（错误）
struct MapPosition
{
    std::string zoneId;
    double x = 0.0;
    double y = 0.0;
    double score = 0.0;
    int sliceIndex = 0;
    double scale = 1.0;
    double angle = 0.0;
    long long latencyMs = 0;
    bool isHeld = false;
};

// 修正
struct MapPosition
{
    std::string zone_id;
    double x = 0.0;
    double y = 0.0;
    double score = 0.0;
    int slice_index = 0;
    double scale = 1.0;
    double angle = 0.0;
    long long latency_ms = 0;
    bool is_held = false;
};
```

### MapTypes.h — `TrackingConfig`

```cpp
// 当前（错误）
struct TrackingConfig
{
    double maxNormalSpeed = 40.0;
    double screenBlockedThreshold = 0.4;
    int edgeSnapMargin = 1;
    double velocitySmoothingAlpha = 0.5;
    double maxDtForPrediction = 5.0;
};

// 修正
struct TrackingConfig
{
    double max_normal_speed = 40.0;
    double screen_blocked_threshold = 0.4;
    int edge_snap_margin = 1;
    double velocity_smoothing_alpha = 0.5;
    double max_dt_for_prediction = 5.0;
};
```

### MatchStrategy.h — `TrackingValidation`

```cpp
// 当前（错误）
struct TrackingValidation
{
    bool isValid;
    bool isEdgeSnapped;
    bool isTeleported;
    bool isScreenBlocked;
    double absX, absY;
};

// 修正
struct TrackingValidation
{
    bool is_valid;
    bool is_edge_snapped;
    bool is_teleported;
    bool is_screen_blocked;
    double abs_x, abs_y;
};
```

## 常量命名：添加 k 前缀

### MapTypes.h

```cpp
// 当前（错误）
constexpr int MinimapROIOriginX = kDefaultMinimapRoi.x;
constexpr int MinimapROIOriginY = kDefaultMinimapRoi.y;
constexpr int MinimapROIWidth = kDefaultMinimapRoi.width;
constexpr int MinimapROIHeight = kDefaultMinimapRoi.height;
constexpr int MaxLostTrackingCount = 3;
constexpr double MinMatchScore = 0.7;
constexpr double MobileSearchRadius = 50.0;

// 修正
constexpr int kMinimapRoiOriginX = kDefaultMinimapRoi.x;
constexpr int kMinimapRoiOriginY = kDefaultMinimapRoi.y;
constexpr int kMinimapRoiWidth = kDefaultMinimapRoi.width;
constexpr int kMinimapRoiHeight = kDefaultMinimapRoi.height;
constexpr int kMaxLostTrackingCount = 3;
constexpr double kMinMatchScore = 0.7;
constexpr double kMobileSearchRadius = 50.0;
```

## Include 修正示例

### MatchStrategy.h

```cpp
// 当前（错误）— 直接引入 opencv
#include <opencv2/opencv.hpp>

// 修正 — 通过 MaaUtils 抑制警告
#include <MaaUtils/NoWarningCV.hpp>
```

## 重复代码提取示例

### source/common/scoped_buffer.h（新建）

```cpp
#pragma once

#include <MaaFramework/Utility/MaaBuffer.h>

namespace maaend
{

class ScopedImageBuffer
{
public:
    ScopedImageBuffer() : buffer_(MaaImageBufferCreate()) {}
    ~ScopedImageBuffer() { MaaImageBufferDestroy(buffer_); }

    ScopedImageBuffer(const ScopedImageBuffer&) = delete;
    ScopedImageBuffer& operator=(const ScopedImageBuffer&) = delete;
    ScopedImageBuffer(ScopedImageBuffer&&) = delete;
    ScopedImageBuffer& operator=(ScopedImageBuffer&&) = delete;

    MaaImageBuffer* Get() const { return buffer_; }
    operator MaaImageBuffer*() const { return buffer_; }

private:
    MaaImageBuffer* buffer_;
};

class ScopedStringBuffer
{
public:
    ScopedStringBuffer() : buffer_(MaaStringBufferCreate()) {}
    ~ScopedStringBuffer() { MaaStringBufferDestroy(buffer_); }

    ScopedStringBuffer(const ScopedStringBuffer&) = delete;
    ScopedStringBuffer& operator=(const ScopedStringBuffer&) = delete;
    ScopedStringBuffer(ScopedStringBuffer&&) = delete;
    ScopedStringBuffer& operator=(ScopedStringBuffer&&) = delete;

    MaaStringBuffer* Get() const { return buffer_; }
    operator MaaStringBuffer*() const { return buffer_; }

    const char* CStr() const
    {
        const char* raw = MaaStringBufferGet(buffer_);
        return raw ? raw : "";
    }

    bool IsEmpty() const { return MaaStringBufferIsEmpty(buffer_); }

private:
    MaaStringBuffer* buffer_;
};

} // namespace maaend
```

### DetectControllerType 提取到 controller_type_utils.h

```cpp
// 在 controller_type_utils.h 中添加（当前该文件只有 IsAdbLike / IsWlroots 判断）

#include <MaaFramework/MaaAPI.h>

#include "common/scoped_buffer.h"

inline std::string DetectControllerType(MaaController* ctrl)
{
    if (ctrl == nullptr) {
        return {};
    }

    maaend::ScopedStringBuffer buffer;
    if (!MaaControllerGetInfo(ctrl, buffer.Get()) || buffer.IsEmpty()) {
        return {};
    }

    const auto info = json::parse(buffer.CStr()).value_or(json::object {});
    if (info.contains("type") && info.at("type").is_string()) {
        return info.at("type").as_string();
    }
    return {};
}
```

## MaaFramework 值得借鉴的模式

### NonCopyable 基类

MaaFramework 提供 `NonCopyable` 和 `NonCopyButMovable` 基类，避免每个类重复写 `= delete`：

```cpp
class NonCopyable
{
public:
    NonCopyable(const NonCopyable&) = delete;
    NonCopyable(NonCopyable&&) = delete;
    NonCopyable& operator=(const NonCopyable&) = delete;
    NonCopyable& operator=(NonCopyable&&) = delete;

protected:
    NonCopyable() = default;
};
```

cpp-algo 中的 `ScopedImageBuffer` 等类可直接继承此基类。

### VAR() 宏用于调试日志

```cpp
LogDebug << VAR(score) << VAR(zone_id) << VAR(search_rect);
// 输出: score=0.73, zone_id="OMV01", search_rect=[100, 200, 300, 400]
```

### 成员变量尾下划线一致性

MaaFramework 中所有类成员变量无例外地使用 `trailing_underscore_`：

- `tasker_`, `entry_`, `stdout_level_`, `trace_mutex_`

cpp-algo 中 `MapLocator::Impl` 的 `motionTracker`、`currentZoneId` 等应统一改为 `motion_tracker_`、`current_zone_id_`。
