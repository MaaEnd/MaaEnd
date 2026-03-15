# MaaEndBot 分析报告 - Issue #1297

## 问题概述

基质筛选任务在第一行完成后执行下滑，随后在第二行停止不动。根据日志分析，已定位到根本原因。

---

## 日志分析

从您提供的 `maa.log` 中提取到关键信息：

```
EssenceFilterSwipeCalibrate [all_results_=[{"box":[679,90,96,96],"score":0.164086}]] 
[filtered_results_=[]] [best_result_=null] 
[param_.thresholds=[0.350000]]
```

**核心发现**：滑动后进入 `EssenceFilterSwipeCalibrate` 节点时，模板匹配**确实检测到了**基质格子（box: [679,90,96,96]），但匹配分数为 **0.164**，低于当前阈值 **0.35**，导致识别被判定为失败。

随后流程反复重试该节点，最终在约 20 秒后触发超时，任务失败：

```
Task timeout [pretask.name=EssenceFilterSwipeFirst] [duration_since(start_clock)=20025ms] [pretask.reco_timeout=20000ms]
Node.PipelineNode.Failed [name=EssenceFilterSwipeFirst]
```

---

## 根因分析

1. **流程说明**：第一行处理完成后，会执行 `EssenceFilterSwipeFirst` 下滑，然后进入 `EssenceFilterSwipeCalibrate` 做位置校准。该校准节点使用 `EssenceGeneral.png` 在 ROI `[18, 72, 956, 119]` 内做模板匹配。

2. **阈值过高**：当前 `EssenceFilterSwipeCalibrate`、`EssenceRowDetect`、`EssenceDetectFinal` 的模板匹配阈值均为 **0.35**。在您的环境下（游戏截图分辨率约 1936×1119，与基准 720p 不同），滑动后第二行的匹配分数约为 0.164，无法通过阈值。

3. **无降级路径**：`EssenceFilterSwipeFirst` 的 `next` 仅包含 `EssenceFilterSwipeCalibrate`，且该校准节点没有 `on_error`。识别失败时无法跳过校准、直接进入行检测，只能不断重试直至超时。

---

## 建议解决方案

### 方案一：降低滑动校准节点的匹配阈值（推荐）

在 `assets/resource_fast/pipeline/EssenceFilter.json` 中，将 `EssenceFilterSwipeCalibrate` 的 `threshold` 从 `0.35` 调整为 `0.16` 或 `0.2`：

```json
"EssenceFilterSwipeCalibrate": {
    "desc": "滑动后校准：按首格 Y 误差计算滑动距离，校准到基准 86",
    "recognition": {
        "type": "TemplateMatch",
        "param": {
            "roi": [18, 72, 956, 119],
            "template": "EssenceFilter/EssenceGeneral.png",
            "method": 5,
            "threshold": 0.16
        }
    },
    ...
}
```

这样在您当前环境下，分数 0.164 即可通过识别，校准逻辑可正常执行。

### 方案二：为滑动校准增加 on_error 降级

若担心降低阈值带来误匹配，可为 `EssenceFilterSwipeFirst` 增加 `on_error`，在 `EssenceFilterSwipeCalibrate` 识别失败时跳过校准，直接尝试行检测：

```json
"EssenceFilterSwipeFirst": {
    ...
    "on_error": ["EssenceRowDetect", "EssenceDetectFinal"]
}
```

注意：若 `EssenceRowDetect` 和 `EssenceDetectFinal` 仍使用 0.35 阈值，在相同环境下可能同样无法识别，因此通常需要与方案一配合使用。

### 方案三：组合使用（更稳妥）

- 将 `EssenceFilterSwipeCalibrate` 的 `threshold` 调整为 `0.16` 或 `0.2`
- 为 `EssenceFilterSwipeFirst` 增加 `on_error`: `["EssenceRowDetect", "EssenceDetectFinal"]`

这样既能在多数情况下正常完成校准，又能在极端情况下通过降级继续执行。

---

## 分辨率说明

项目规范要求以 **720p (1280×720)** 为基准。您的游戏画面为 1936×1119，与基准存在缩放差异，可能导致模板匹配分数下降。若问题在调整阈值后仍复现，可考虑：

- 检查 MaaEnd 的缩放/分辨率适配配置
- 在 720p 或更接近基准的分辨率下验证流程

---

## 总结

- **现象**：第一行筛选完成后下滑，第二行不再继续处理。
- **原因**：滑动后 `EssenceFilterSwipeCalibrate` 的模板匹配分数 (0.164) 低于阈值 (0.35)，识别失败，且无降级逻辑，最终超时。
- **建议**：将 `EssenceFilterSwipeCalibrate` 的 `threshold` 降至约 `0.16`，并视情况为 `EssenceFilterSwipeFirst` 增加 `on_error` 降级。

如需进一步协助，可提供修改后的运行日志或截图。
