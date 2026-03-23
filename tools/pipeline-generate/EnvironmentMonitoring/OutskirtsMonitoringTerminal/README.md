# 城郊监测终端 (OutskirtsMonitoringTerminal) — 通用任务节点模板

使用 `MAA-pipeline-generate` 工具与 `data.json` 批量生成对应的 Pipeline 文件。

## 运行方式（从 MaaEnd 仓库根目录）

```bash
npx @joebao/maa-pipeline-generate \
 tools/pipeline-generate/EnvironmentMonitoring/OutskirtsMonitoringTerminal/template.jsonc \
 tools/pipeline-generate/EnvironmentMonitoring/OutskirtsMonitoringTerminal/data.json \
 --output-dir assets/resource/pipeline/EnvironmentMonitoring/OutskirtsMonitoringTerminal
```

## 变量说明

- `Id` — 节点 ID 标识符（英文，无空格）
- `Name` — 任务中文名称（用于 desc 文本）
- `ExpectedText` — 任务列表项 OCR 识别文本（多语言数组）
- `InExpectedText` — 任务详情界面 OCR 识别文本（多数情况与 ExpectedText 相同）
- `EnterMap` — 传送节点名称
- `MapName` — 地图名称
- `MapTarget` — 位置断言目标坐标 `[x, y, w, h]`
- `MapPath` — 寻路路径点数组 `[[x, y], ...]`
- `CameraSwipeDirection` — 调整镜头朝向的滑动节点名称
