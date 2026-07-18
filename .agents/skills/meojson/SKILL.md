---
name: meojson
description: Guide for using the meojson C++ JSON library in MaaEnd cpp-algo. Use when writing code that involves JSON parsing, serialization, struct jsonization (MEO_JSONIZATION), json::value manipulation, ext::jsonization custom type support, or custom recognition param parsing in agent/cpp-algo/.
---

# meojson in MaaEnd cpp-algo

meojson is a header-only C++ JSON library, provided via MaaFramework deps. Include via `<meojson/json.hpp>`.

## Core Types

| Type           | Description                                                 |
| -------------- | ----------------------------------------------------------- |
| `json::value`  | Universal JSON value (null/bool/number/string/array/object) |
| `json::array`  | JSON array, wraps `std::vector<json::value>`                |
| `json::object` | JSON object, wraps `std::map<std::string, json::value>`     |

## Parsing

```cpp
#include <meojson/json.hpp>

// From string — returns std::optional<json::value>
auto opt = json::parse(str);
if (!opt) { /* parse failed */ }

// From file
auto opt = json::open("/path/to/file.json");

// JSONC (with comments)
auto opt = json::parsec(str);
```

**cpp-algo 推荐模式** — 安全解析自定义识别参数：

```cpp
template <typename T>
T ParseCustomRecognitionParam(const char* custom_recognition_param)
{
    if (!custom_recognition_param || std::strlen(custom_recognition_param) == 0) {
        return T {};
    }
    auto opt = json::parse(custom_recognition_param);
    if (!opt) {
        LogError << "failed to parse custom_recognition_param" << VAR(custom_recognition_param);
        return T {};
    }
    T result {};
    if (!result.from_json(*opt)) {
        LogError << "failed to deserialize param" << VAR(custom_recognition_param);
        return T {};
    }
    return result;
}
```

> **反模式**：`json::parse(str).value_or(json::object {}).as<T>()` —— `value_or` 静默吞掉 parse 失败；空 object 调 `as<T>()` 当 `T` 有 required 字段时会抛异常。

## Constructing Values

```cpp
json::value v1 = 42;
json::value v2 = "hello";
json::value v3 = true;
json::value v4 = nullptr;    // null

json::array arr { 1, 2, "three" };
json::object obj {
    { "key1", "value1" },
    { "key2", 42 },
};

// From STL containers (implicit conversion)
std::vector<int> vec = {1, 2, 3};
json::value v5 = vec;                              // → JSON array

std::map<std::string, int> m = {{"a", 1}};
json::value v6 = m;                                // → JSON object
```

## Reading Values

### Type Checking

```cpp
v.is_null() / v.is_boolean() / v.is_number() / v.is_string()
v.is_array() / v.is_object()
v.is<int>()     // check if convertible to type
```

### Direct Access (throws on type mismatch)

```cpp
v.as_string()       // → std::string
v.as_string_view()  // → std::string_view (no copy)
v.as_integer() / v.as_double() / v.as_boolean()
v.as_array()        // → const json::array&
v.as_object()       // → const json::object&
v.as<T>()           // → T (explicit conversion)
```

### Safe Access

```cpp
// find() returns std::optional<T>
auto opt = v.find<std::string>("key");
if (opt) { std::string s = *opt; }

// get() with default value — supports chained keys
std::string s = v.get("key", "default_value");
int n = v.get("a", "b", 0);  // v["a"]["b"], default 0

// exists() / contains()
if (v.exists("key")) { ... }
```

### Subscript & Iteration

```cpp
const json::value& v2 = v["key"];      // object access
v["key"] = "new_value";                // mutable (creates key if missing)

for (const auto& item : v.as_array()) { ... }
for (const auto& [key, val] : v.as_object()) { ... }
```

## Serialization

```cpp
v.dumps()               // compact string
v.dumps(4)              // pretty print with indent=4
v.format()              // same as dumps(4)
```

**cpp-algo 常见模式** — 写回 JSON detail：

```cpp
template <typename T>
void WriteJsonDetail(MaaStringBuffer* out_detail, const T& payload)
{
    if (out_detail == nullptr) return;
    const std::string json_text = json::value(payload).dumps();
    MaaStringBufferSet(out_detail, json_text.c_str());
}
```

## Object Merge Operator

```cpp
json::value merged = obj1 | obj2;   // right side wins on conflict
obj1 |= obj2;                       // in-place merge
```

## MEO_JSONIZATION — Struct ↔ JSON

`MEO_JSONIZATION(fields...)` generates `to_json()`, `check_json()`, `from_json()` member functions.

### Basic Usage

```cpp
struct LocateOutput {
    int status = 0;
    std::string message;
    std::string mapName;
    int x = 0;
    int y = 0;

    MEO_JSONIZATION(status, message, MEO_OPT mapName, MEO_OPT x, MEO_OPT y)
};

// Serialize
json::value j = data;               // implicit via to_json()

// Deserialize — 安全方式：用 from_json() 检查返回值
MyData data {};
if (!data.from_json(j)) {
    LogError << "failed to deserialize" << VAR(j);
}

// as<T>() 在类型不匹配 / required 字段缺失时会抛异常，仅在确定数据合法时使用
MyData data2 = j.as<MyData>();
```

### MEO_OPT — Optional Fields

By default all fields are **required** in `from_json()`. Prefix with `MEO_OPT` to make optional (keeps default if missing):

```cpp
struct LocateOptions {
    double loc_threshold = 0.55;
    double yolo_threshold = 0.70;
    bool force_global_search = false;

    MEO_JSONIZATION(
        MEO_OPT loc_threshold,
        MEO_OPT yolo_threshold,
        MEO_OPT force_global_search)
};
```

### MEO_KEY — Override JSON Key Name

```cpp
struct JTemplateMatch {
    std::vector<std::string> template_;   // "template" is C++ keyword
    MEO_TOJSON(MEO_KEY("template") template_);
};

// Combine with MEO_OPT:
MEO_JSONIZATION(MEO_OPT MEO_KEY("default") default_);
```

### Sub-Macros

| Macro                  | Generates           |
| ---------------------- | ------------------- |
| `MEO_TOJSON(...)`      | `to_json()` only    |
| `MEO_FROMJSON(...)`    | `from_json()` only  |
| `MEO_CHECKJSON(...)`   | `check_json()` only |
| `MEO_JSONIZATION(...)` | All three           |

### Supported Field Types

- Primitives: `int`, `double`, `bool`, `std::string`
- STL containers: `std::vector<T>`, `std::map<std::string, T>`, `std::array<T,N>`
- Nullable: `std::optional<T>`, `std::shared_ptr<T>`
- Tuple-like: `std::pair<A,B>`, `std::tuple<...>`
- Variant: `std::variant<Ts...>`
- Nested structs with `MEO_JSONIZATION` / `to_json()`
- `json::value`, `json::object`, `json::array` directly

## ext::jsonization — Custom Type Support

For types you don't own, specialize `json::ext::jsonization<T>`:

```cpp
namespace json::ext {
template <>
class jsonization<cv::Rect> {
public:
    json::value to_json(const cv::Rect& rect) const {
        return json::array { rect.x, rect.y, rect.width, rect.height };
    }
    bool check_json(const json::value& json) const {
        return json.is<std::vector<int>>() && json.as_array().size() == 4;
    }
    bool from_json(const json::value& json, cv::Rect& rect) const {
        auto arr = json.as<std::vector<int>>();
        rect = cv::Rect(arr[0], arr[1], arr[2], arr[3]);
        return true;
    }
};
}
```

MaaUtils 已提供的特化（通过 `<MaaUtils/JsonExt.hpp>` 间接可用）：

- `cv::Point` ↔ `[x, y]`、`cv::Rect` ↔ `[x, y, w, h]`、`cv::Size` ↔ `[w, h]`
- `std::filesystem::path` ↔ UTF-8 string
- `std::chrono::milliseconds` → `"123ms"` (to_json only)
- Fallback: any type with `operator<<` → string (to_json only)

## Enum Reflection

```cpp
enum class MyEnum {
    A, B, C,
    MEOJSON_ENUM_RANGE(A, C)
};

json::value j = MyEnum::B;    // → "B"
MyEnum e = j.as<MyEnum>();    // → MyEnum::B
```

## Common Pitfalls

1. **`json::parse` returns `std::optional`** — always check before use,失败路径必须 `LogError` + 早期 `return`
2. **`as_*()` / `as<T>()` throws on type mismatch** — 用 `find()` 或 `is_*()` 前置检查；对 struct 用 `from_json()` 检查返回值
3. **禁止 `.value_or(...).as<T>()`** — 静默吞错误 + 可能抛异常，应拆开检查（见上方推荐模式）
4. **`char` is deleted** — use `std::string` or `int`
5. **`ext::jsonization` lives in `json::ext` namespace**
6. **`MEO_OPT` applies to the next field only** — each optional field needs its own `MEO_OPT`
7. **`MEO_KEY` goes after `MEO_OPT`** — order is `MEO_OPT MEO_KEY("key") field`

## Quick Reference

For detailed API signatures, see [reference.md](reference.md).
