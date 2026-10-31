# 环境监测（EnvironmentMonitoring）失败归因

> 结论：**不是视角拖动的问题**。失败发生在「跑到拍照点」之前的路线阶段，
> 最后由 `EnvironmentMonitoringFinish`（错误汇总节点）统一报出。

## 1. 失败是怎么被"报"出来的

上游 `assets/resource/pipeline/EnvironmentMonitoring.json` 里：

```json
"EnvironmentMonitoringFinish": {
  "action": "Custom",
  "custom_action": "FailureCollectorFinish",
  "custom_action_param": { "key": "EnvironmentMonitoring" },
  // 这个节点是收集错误的节点，如果之前有节点报错，这个节点一定报错
  // 所以完全没有必要重复上报
}
```

也就是说 `EnvironmentMonitoringFinish Action.Failed` **不是自身故障**，
它只是把前面任意一条路线记录过的错误汇总上报。看到它失败，必须往前找真正的失败点。

## 2. 真正的失败位置：拍照阶段一次都没进入

homesrv 全部 `maafw*.log` 里各节点出现次数：

| 节点 | 次数 | 含义 |
|---|---|---|
| `AcceptHangingVines` | 90 | 接任务：执行过 |
| `GoToHangingVinesMission` | 210 | 前往任务点：执行过 |
| `EnvironmentMonitoringEnterCameraMode` | **0** | 进入拍照模式：**从未执行** |
| `EnvironmentMonitoringWaitShutterButton` | **0** | 等快门按钮：从未执行 |
| `EnvironmentMonitoringCheckPhotographyTarget` | **0** | 检查拍照目标：从未执行 |
| `EnvironmentMonitoringCameraScan` / `__CameraScanMove*` | **0** | 相机扫描/拖动：从未执行 |

路线链（以垂藤为例）：

```
HangingVinesExecute → AcceptHangingVines → GoToHangingVinesMission
  → TrackOrGoTo → Track/AlreadyTracked → GoToHangingVines
  → StartPos/NotAtStartPos → GoToHangingVinesMove
  → HangingVinesTakePhoto → EnvironmentMonitoringTakePhoto（→ 进入拍照模式）
```

断点在 `GoToHangingVinesMove` 之后、`EnvironmentMonitoringTakePhoto` 之前，
即**跑到拍照点/到达判定**这一段，还没轮到相机扫描。

## 3. 排除项

- **相机拖动可用**：实测把 1280×720 坐标 (520,360)→(360,360) 拖动、以及无按键移动 600px，
  四格对比显示相机水平朝向确实改变。拖动本身没有坏。
- **`relative_move` 不是原因**：全库 5997 节点里只有 `MapNavigatorZiplinePitchReset` 用到相对移动，
  环境监测的 42 个节点动作只有 `Click 9 / Custom 5 / Swipe 5`。
- **`FailureCollectorFinish` 不是原因**：它按设计上报。

## 4. 复现与定位方法

```bash
MAA_SAVE_DRAW=1 python3 run_daily_linux.py --tasks EnvironmentMonitoring --instance hmuddz8
```

然后看：
1. `<log_dir>/vision/` 里失败节点的识别框；
2. 日志里 `failure_task` 触发了哪条路线（`HangingVinesFailed` / `XiraniteNexusFailed` /
   `MiniShellbeastFailed` / `TreeOfTheOldCourtyardFailed` / `Outskirts…` / `MarkerStone…`）；
3. 断点节点的 on_error 截图，判断是"没走到位"还是"到达判定不过"。

## 5. 现状

2026-10-23 00:10 的 homesrv 全量 9 项 `9/9 rc=0`，环境监测当天通过；
历史上那批 `EnvironmentMonitoringFinish` 失败集中在 10-21 的日志里。

## 6. A/B 对照：与 CDP 抓帧无关（2026-10-23 17:5x）

同样预设、同一台机器，仅把控制器从 `cdp` 换回 `wlroots`（合成器帧），环境监测**照样失败**：

| 控制器 | 结果 | `XiraniteNexusInQuickTeleportMap` | `__CameraScanMove*` |
|---|---|---|---|
| cdp | FAIL（163.5s，含重试） | 832 次 / 412 失败（49.5%） | 0 |
| wlroots | FAIL（177.2s + 83.1s） | 516 次 / 250 失败（48.4%） | 0 |

同一屏背靠背抓帧对比：两帧均 1280x720，平均像素差 3.26；10 个模板的灰度 NCC 平均差
（cdp − wlroots）= −0.0085（噪声量级）。即 CDP 帧在灰度匹配上没有系统性劣势。

> 启动页那条 0.90 的坑是另一回事：无 `<video>` 时走 `Page.captureScreenshot`，
> 已用独立模板层 `resource_web_cdp` 修掉，与游戏内画面无关。

结论：环境监测的失败属于**任务流程/状态**问题，不是换画面引起；
同类问题的当期清单：① WebStartGame 单项超时 60s 偏短；② 任务间未归位（上一项留在菜单）；
③ 环境监测「追踪→任务地图」未成功；④ AutoCollect 恢复链回不去大世界。修复延后。
