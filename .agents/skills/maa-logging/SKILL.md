---
name: maa-logging
description: MaaFramework 日志宏用法指南，适用于 MaaEnd cpp-algo。Use when writing logging code, using LogInfo/LogError/LogWarn/LogDebug/LogTrace, outputting containers or custom types to logs, or when the user asks about logging best practices in agent/cpp-algo/.
---

# MaaFramework 日志宏用法

头文件：`<MaaUtils/Logger.h>`

## 可用宏

| 宏                                  | 级别                                            |
| ----------------------------------- | ----------------------------------------------- |
| `LogFatal` / `LogError` / `LogWarn` | 错误与警告                                      |
| `LogInfo` / `LogDebug` / `LogTrace` | 信息与调试                                      |
| `LogFunc`                           | 函数作用域（进入打 enter，离开打 leave + 耗时） |
| `VAR(x)`                            | 格式化为 `[x=value]`                            |
| `VAR_VOIDP(x)`                      | 同上，但将指针转为 `void*` 输出                 |

## 核心原则：不要手动拼字符串

用 `<<` 流式输出即可，各片段间自动加空格分隔：

```cpp
// Good
LogInfo << "OK " << VAR(result.position->zoneId) << VAR(result.position->x)
        << VAR(result.position->y) << VAR(result.position->score);

LogError << "MapLocateAction: Locator init failed";

// Bad - 日志中不要手动拼接（std::format 用于一般字符串构建是好的，但日志应直接用 << 和 VAR）
LogInfo << "OK " + std::to_string(x) + ", " + std::to_string(y);
LogInfo << std::format("position: ({}, {})", x, y);
```

## cpp-algo 典型用法

### 自定义识别回调中的日志

```cpp
#include <MaaUtils/Logger.h>

MaaBool MyRecognitionRun(MaaContext* context, MaaTaskId task_id,
    const char* node_name, ...)
{
    LogInfo << VAR(context) << VAR(task_id) << VAR(node_name);

    if (!locator) {
        LogError << "Locator init failed";
        return MAA_FALSE;
    }

    LogWarn << "Screen Blocked";
    LogInfo << "matched" << VAR(zone_id) << VAR(x) << VAR(y);
    return MAA_TRUE;
}
```

### 多变量输出

```cpp
LogInfo << "Phase transition." << VAR(from_phase_name) << VAR(to_phase_name)
        << VAR(reason) << VAR(current_node_idx);

LogDebug << "Startup motion gate." << VAR(state->startup_anchor_initialized)
         << VAR(state->startup_motion_confirmed)
         << VAR(position.x) << VAR(position.y);
```

## 容器直接输出

`vector`、`set`、`map<string, T>` 等 STL 容器可以直接 `<<`，会自动序列化为 JSON：

```cpp
std::vector<std::string> names = { "a", "b", "c" };
LogInfo << "names:" << names;       // 输出: names: ["a","b","c"]

std::map<std::string, int> config;
LogInfo << "config:" << config;     // 输出: config: {"key":value,...}
```

不需要手动写 for 循环来打印容器内容。

## 自定义类型输出

给结构体加 `MEO_TOJSON(...)` 或 `MEO_JSONIZATION(...)`，即可直接输出：

```cpp
struct LocateOutput {
    int status = 0;
    std::string message;
    int x = 0;
    int y = 0;

    MEO_JSONIZATION(status, message, MEO_OPT x, MEO_OPT y)
};

// 直接输出
LogInfo << VAR(output);
// 输出: [output={"status":0,"message":"...","x":123,"y":456}]

// 容器嵌套也能工作
std::vector<LocateOutput> results;
LogInfo << VAR(results);
```

## 原理简述

`LogStream::stream` 按优先级尝试：

1. 可构造 `json::value` → `dumps()` 输出（含 `MEO_TOJSON` 类型、基本类型）
2. 可构造 `json::array` → `dumps()` 输出（含序列容器）
3. 可构造 `json::object` → `dumps()` 输出（含 `map<string, T>`）
4. 有 `operator<<` → 直接流输出
5. 以上都不满足 → `static_assert` 编译失败

如果遇到编译报错 "Unsupported type"，给类型加 `MEO_TOJSON` 或特化 `json::ext::jsonization<T>` 即可。
