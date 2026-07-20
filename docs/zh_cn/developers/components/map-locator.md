# 开发手册 - MapLocator 小地图定位系统

MapLocator 是基于原生 C++ 实现的小地图定位系统，结合 AI 推理与传统计算机视觉算法，输出角色当前所处的地图区域（ZoneID）、全局像素坐标 `(x, y)` 与朝向角度。

它同时是 [MapNavigator](./map-navigator.md) 的位置来源——导航过程中的每一次位置判断都来自 MapLocator 的逐帧识别。

- [MapLocateRecognition](#maplocaterecognition)
- [MapLocateAssertLocation](#maplocateassertlocation)
- [定位原理](#定位原理)

MapLocator 属于 Recognition 层，只负责坐标与方位识别，不接管游戏控制。若要让角色移动到指定坐标，应使用 [MapNavigator](./map-navigator.md)——它已在 MapLocator 之上实现了寻路与移动控制；也可以基于 `out_detail` 的输出自行编写控制逻辑。

---

## MapLocateRecognition

获取角色当前所在的区域名（ZoneID）、全局像素坐标与朝向。

调用一次仅处理当前一帧画面。定位器在多次调用之间保持追踪状态：常规调用只在上一帧位置附近局部搜索，连续 `max_lost_frames` 帧无有效结果后判定追踪丢失并回退全局搜索。若需连续追踪，用 Pipeline 的循环机制（如 `next` 自连）高频调用本节点。

### 节点参数

无必填参数。可选参数（`custom_recognition_param`）：

| 参数                  | 默认值  | 说明                                                                                 |
| --------------------- | ------- | ------------------------------------------------------------------------------------ |
| `loc_threshold`       | `0.55`  | 模板匹配命中分数下限。画面干扰严重、频繁丢失定位时可适当下调（如 `0.45`）            |
| `yolo_threshold`      | `0.70`  | YOLO 判定小地图区域的最低置信度。设得过低可能将其他圆盘状 UI 误认为小地图            |
| `force_global_search` | `false` | 跨区域传送、复活、长时间切屏恢复时设为 `true`，放弃当前追踪并强制全图搜索重新锁定    |
| `max_lost_frames`     | `3`     | 连续多少帧无有效结果后判定追踪丢失。调高可容忍短暂遮挡，但错误状态的持续时间相应变长 |
| `expected_zone_id`    | 空      | 非空时仅接受该区域的定位结果，用于已知角色所在区域的场景                             |

### 返回值（out_detail）

| 字段        | 说明                                       |
| ----------- | ------------------------------------------ |
| `status`    | 状态码，见下表                             |
| `message`   | 失败原因或调试信息                         |
| `mapName`   | （成功时）定位到的区域名，如 `map01_lv001` |
| `x` / `y`   | （成功时）全局像素坐标                     |
| `rot`       | （成功时）朝向偏航角，0°–360°，北为零向    |
| `locConf`   | 本次命中的置信度得分，调参时参考           |
| `latencyMs` | 本次计算耗时（毫秒）                       |

`status` 取值：

| 值  | 枚举            | 含义                                           |
| --- | --------------- | ---------------------------------------------- |
| `0` | `Success`       | 定位成功                                       |
| `1` | `TrackingLost`  | 追踪丢失，回退全局搜索后仍无匹配               |
| `2` | `ScreenBlocked` | 画面被大面积遮挡，无法提取有效特征             |
| `3` | `Teleported`    | 两帧之间的位移超出正常移速上限，判定为强制传送 |
| `4` | `YoloFailed`    | YOLO 前置鉴别判定当前画面不含小地图区域        |

### 示例

最简调用（多数情况无需传参，节点自动通过 YOLO 确定所在区域并进入跟踪）：

```json
{
    "MyLocateTask": {
        "recognition": "Custom",
        "custom_recognition": "MapLocateRecognition",
        "action": "DoNothing"
    }
}
```

覆写参数（如长途传送后强制全局搜索）：

```json
{
    "MyLocateTask": {
        "recognition": "Custom",
        "custom_recognition": "MapLocateRecognition",
        "custom_recognition_param": {
            "loc_threshold": 0.55,
            "yolo_threshold": 0.7,
            "force_global_search": true
        },
        "action": "DoNothing"
    }
}
```

---

## MapLocateAssertLocation

判定角色当前是否位于指定 `zone_id` 的某个矩形区域内。

与 `MapLocateRecognition` 的单帧判定不同，该节点执行的是**稳定判定**：重置追踪状态并强制全局搜索，随后以 250ms 间隔轮询（最多 60 帧，约 15 秒），要求连续 3 帧定位成功、区域匹配且位置稳定在 12px 半径内，最后取质心坐标判定是否在矩形内。因此调用可能阻塞数秒，并非瞬时判定。

### 节点参数

必填参数（`custom_recognition_param`）：

| 参数      | 说明                                                |
| --------- | --------------------------------------------------- |
| `zone_id` | 目标区域名，必须与定位结果中的区域名完全一致        |
| `target`  | 4 个数字的数组 `[x, y, w, h]`，矩形左上角坐标与宽高 |

可选参数（`custom_recognition_param`）：

| 参数             | 默认值 | 说明                                                   |
| ---------------- | ------ | ------------------------------------------------------ |
| `loc_threshold`  | `0.70` | 模板匹配命中分数下限，语义同上；注意默认值高于单帧定位 |
| `yolo_threshold` | `0.70` | 语义同上                                               |

断言始终强制全局搜索且只接受 `zone_id` 内的定位结果，无需配置搜索范围。

### 返回值（out_detail）

| 字段        | 说明                                      |
| ----------- | ----------------------------------------- |
| `status`    | 定位状态码，语义同 `MapLocateRecognition` |
| `matched`   | 区域与矩形均命中时为 `true`               |
| `inTarget`  | 与 `matched` 等价                         |
| `message`   | 定位日志或失败原因                        |
| `zoneId`    | 本次判定要求的目标区域名                  |
| `x` / `y`   | （成功时）稳定窗口的质心坐标              |
| `rot`       | （成功时）朝向偏航角                      |
| `locConf`   | 本次命中的置信度得分                      |
| `latencyMs` | 本次计算耗时（毫秒）                      |
| `target`    | 回显本次判定使用的 `[x, y, w, h]`         |

### 示例

```json
{
    "WulingBaseAssert": {
        "recognition": "Custom",
        "custom_recognition": "MapLocateAssertLocation",
        "custom_recognition_param": {
            "zone_id": "Wuling_Base",
            "target": [
                605,
                878,
                60,
                20
            ]
        },
        "action": "DoNothing"
    }
}
```

> [!TIP]
>
> 该节点适合作为 `MapNavigateAction` 的入口判定：先确认角色位于预期区域，再开始整段导航。配套工具的 `断言模式` 可直接框选导出该节点，见 [MapNavigator](./map-navigator.md)。

---

## 定位原理

本节面向需要了解内部实现的读者，日常使用可跳过。

1. **原生 C++ 图像管线**：作为 `cpp-algo` 独立进程接入 Pipeline，图像处理基于 C++ / OpenCV 并针对内存拷贝优化，引入 YOLO 推理后仍保持毫秒级延迟。
2. **YOLO 前置鉴别**：按置信度判断当前画面是否存在合法的小地图区域，过滤全屏菜单、特效遮挡等异常画面。
3. **梯度域 ZNCC 匹配**：针对半透明 UI 堆叠场景提取梯度特征，配合 ZNCC（零均值归一化互相关）模板匹配；匹配主要依赖边缘与轮廓特征，在技能光效频闪或 UI 变化时保持稳定。
4. **MotionTracker 运动预测**：根据历史运动速度推断当前帧的搜索范围，而非每帧全局搜索，既提升速度，也避免匹配到远处相似但不可达的区域。

> [!IMPORTANT]
>
> 逐帧识别结果是位置的唯一来源。定位短暂丢失时，**不应**基于运动预测推算一个“虚拟位置”来填补空缺——这会让上层在没有实际观测的情况下继续动作，误差会持续累积且无法自我修正。正确做法是依靠 `max_lost_frames` 的过渡与全局搜索恢复机制。
