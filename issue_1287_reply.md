# MaaEndBot 分析报告 - Issue #1287

## 一、问题理解

根据您的描述和日志分析，**自动倒卖**任务在**进入好友飞船的物资调度终端后，点击商品进行出售时直接失败**。

## 二、日志分析结论

我已下载并分析了您提供的日志文件 `MaaEnd-logs-v2.0.2-20260315-082402.zip`，定位到失败原因：

### 关键错误信息

```
[2026-03-15 08:23:29.524][ERR][Actuator.cpp][L178] failed to get target rect [name=ResellShipPage1Click]
```

### 失败流程简述

1. ✅ 任务成功完成：进入商店 → 弹性需求物资 → 扫描价格 → 决策购买 → 购买成功 → 进入好友飞船
2. ✅ 成功走到终端：A+S 撞墙 → A+W 走向终端 → 按 F 交互 → **识别到终端界面** (`ResellShipInTerminal` 成功)
3. ✅ 成功识别商品：`ResellShipPage1Click` 的模板匹配成功，识别到 `storeItem.png`，置信度 0.978，位置 `[29, 359, 62, 33]`
4. ❌ **点击动作失败**：执行 Click 时出现 `failed to get target rect`，导致任务直接失败

## 三、根因分析

失败发生在 `assets/resource/pipeline/Resell/ShipSell.json` 中的 **ResellShipPage1Click** 和 **ResellShipPage2Click** 节点。

当前配置：

```json
"action": {
    "type": "Click",
    "param": {
        "target_offset": [0, 0, 40, -100]
    }
}
```

根据 MaaFramework Pipeline 协议，`target_offset` 的四个值会**分别与识别框的 [x, y, w, h] 相加**：

- 识别框：`[29, 359, 62, 33]`
- 计算后：`[29+0, 359+0, 62+40, 33+(-100)]` = `[29, 359, 102, -67]`

**高度为 -67，得到无效矩形**，导致 Actuator 无法计算点击目标，从而报错 `failed to get target rect`。

## 四、解决方案

### 方案一：修正 target_offset（推荐）

若本意是「在识别框基础上向右、向上偏移再点击」，应使用**位置偏移**，即前两个分量表示偏移，后两个为 0：

```json
"target_offset": [40, -100, 0, 0]
```

这样得到：`[69, 259, 62, 33]`，矩形有效。

### 方案二：直接点击识别框

若只需点击识别到的商品区域，可去掉 `target_offset` 或设为 `[0, 0, 0, 0]`：

```json
"action": {
    "type": "Click"
}
```

或显式指定：

```json
"action": {
    "type": "Click",
    "param": {
        "target_offset": [0, 0, 0, 0]
    }
}
```

### 需要修改的节点

- `ResellShipPage1Click`（约第 255–261 行）
- `ResellShipPage2Click`（约第 411–417 行）

两处配置相同，需一并修改。

## 五、建议

1. **优先尝试方案一**：若之前设计是点击商品名称区域，使用 `[40, -100, 0, 0]` 更符合原意。
2. **若方案一仍有问题**：可改用方案二，直接点击识别框中心。
3. **验证步骤**：修改后重新运行自动倒卖，确认能正常进入好友飞船终端并完成出售。

## 六、其他说明

- 日志中 `ResellShipInTerminal` 首次识别失败（score 0.407 < 0.8），约 1 秒后重试成功（score 0.997），说明终端界面加载存在延迟，当前逻辑可接受。
- 您使用的 MaaEnd 版本为 v2.0.2，游戏版本为 1.0.0，上述修改与版本兼容。

如需进一步协助，可提供修改后的运行日志或截图。
