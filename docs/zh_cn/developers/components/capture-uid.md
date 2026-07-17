# 开发手册 - CaptureUid 参考文档

`CaptureUid` 是一个通用 UID 获取与缓存模块。它通过截屏 OCR 读取玩家 UID，经随机盐 SHA-256 哈希后缓存，供其他子系统以伪匿名标识符引用。

> [!important]
> 原始 UID 不会被存储或记录。仅保留哈希后的伪匿名标识符，用于跨会话关联数据。

## 实现文件

当前实现位于 `agent/go-service/captureuid/`：

| 文件          | 职责                                                            |
| ------------- | --------------------------------------------------------------- |
| `action.go`   | CustomAction 入口，反序列化参数，调用 `Capture` 或 `ClearCache` |
| `capture.go`  | 核心逻辑：截屏、OCR、哈希、缓存                                 |
| `register.go` | 向 MaaFramework 注册 `CaptureUid` 自定义动作                    |

## 在 Pipeline 中调用

### 获取 UID

> [!note]
> Pipeline中无法对捕获的uid进行处理，所以实际用途为在Scene导航过程中的某个稳定界面进行截图，并缓存此时的捕获结果，从而提高识别准确度

使用默认参数获取 UID（缓存优先、当前画面 OCR、允许降级为 `"unknown"`）：

```json
"AutoStockpileGetUid": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "CaptureUid",
            "custom_action_param": {}
        }
    },
    "next": [
        "AutoStockpileStart"
    ]
}
```

### 清空缓存

切换账号后需清空缓存，避免旧 UID 被复用：

```json
"__AccountSwitchClearUidCache": {
    "desc": "清空UID缓存",
    "recognition": "DirectHit",
    "action": "Custom",
    "custom_action": "CaptureUid",
    "custom_action_param": {
        "clear_cache": true
    }
}
```

## 参数说明

| 字段                     | 类型   | 默认值  | 说明                                                                                     |
| ------------------------ | ------ | ------- | ---------------------------------------------------------------------------------------- |
| `use_cache`              | `bool` | `true`  | 缓存中有 UID 时直接返回，不再截屏 OCR。                                                  |
| `stay_on_current_screen` | `bool` | `true`  | 是否在当前画面截屏 OCR。为 `false` 时先导航至 `SceneEnterMenuOperationalManual` 再截屏。 |
| `allow_unknown`          | `bool` | `true`  | OCR 失败时返回 `"unknown"` 而非报错。为 `false` 时 OCR 失败将导致动作失败。              |
| `clear_cache`            | `bool` | `false` | 清空 UID 缓存并立即返回，不执行截屏 OCR。                                                |

> [!note]
> `clear_cache` 为 `true` 时，其余参数均不生效——动作仅清空缓存后直接返回成功。

## 在 Go 代码中直接调用

除 Pipeline 外，其他 Go 模块也可直接调用 `captureuid` 包的导出函数：

### 获取 UID（带缓存）

```go
uid, err := captureuid.Capture(ctx, ctrl, true, true, true)
// useCache=true, stayOnCurrentScreen=true, allowUnknown=true
```

### 读取缓存 UID

```go
uid := captureuid.GetCachedUID()
// 无缓存时返回空字符串 ""
```

### 清空缓存

```go
captureuid.ClearCache()
```

## 工作原理

动作按以下顺序执行：

1. **缓存检查** — 若 `use_cache` 为 `true` 且缓存已有 UID，直接返回。
2. **导航**（可选）— 若 `stay_on_current_screen` 为 `false`，先执行 `SceneEnterMenuOperationalManual` 导航到可读取 UID 的界面。
3. **截屏** — 通过 `ctrl.PostScreencap()` 获取当前画面。
4. **OCR** — 在 ROI 区域 `{60, 690, 155, 25}` 内识别文字，提取所有数字字符。
5. **数字校验** — 验证提取的数字位数为 8–12 位。不在此范围则按 `allow_unknown` 决定返回 `"unknown"` 或报错。
6. **哈希** — 读取（或首次生成）随机盐 `debug/record/random_salt.txt`，计算 `SHA-256(数字UID + 盐)` 取前 16 位十六进制作为伪匿名标识符。
7. **缓存** — 将哈希结果存入内存缓存，供后续调用直接使用。

## 隐私设计

- 原始 UID 数字**不被存储或记录**，仅存在于当次计算的内存中。
- 每次安装随机生成 16 字节盐，保存至 `debug/record/random_salt.txt`。
- 最终标识符为 `SHA-256(UID数字 + 盐)[:16]` — 16 位十六进制字符串，足以跨会话标识同一玩家但无法反推原始 UID。

## 现有集成

| 使用方                 | 文件                                                                                 | 方式                      | 用途                     |
| ---------------------- | ------------------------------------------------------------------------------------ | ------------------------- | ------------------------ |
| AutoStockpile          | `assets/resource/pipeline/AutoStockpile/Main.json`（`AutoStockpileGetUid` 节点）     | Pipeline                  | 获取并缓存 UID           |
| AutoStockpile selector | `agent/go-service/autostockpile/selector.go`                                         | Go API（`GetCachedUID`）  | 关联物价数据与伪匿名身份 |
| CreditShopping         | `agent/go-service/creditshopping/action_record.go`                                   | Go API（`Capture`）       | 记录货架快照时关联 UID   |
| AccountSwitch          | `assets/resource/pipeline/AccountSwitch.json`（`__AccountSwitchClearUidCache` 节点） | Pipeline（`clear_cache`） | 切换账号后清空缓存       |
