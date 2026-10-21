# Windows 适配：WebView2 宿主 + CDP 控制器（设计说明）

日期：2026-10-21
状态：可运行原型（宿主可启动、控制器可截图/点击/拖动），视角转向待实测

---

## 1. 目标与约束

| 目标 | 说明 |
|---|---|
| 复用现有任务 | 不改 `assets/tasks/**`，只加薄覆盖层 |
| 不抢鼠标 | 自动化期间用户能正常用电脑 |
| 可后台 | 不要求窗口在前台/可见 |
| 跨平台同源 | Windows 与 Linux 共用同一套控制器语义（CDP + 多指触摸） |
| 可交付上游 | 代码独立在 `web/windows/`，不碰上游共享节点 |

---

## 2. 为什么是"WebView2 宿主 + CDP"

WebView2 的内核就是 Chromium/Edge，**能接受 Chromium 命令行开关**：

```csharp
new CoreWebView2EnvironmentOptions {
    AdditionalBrowserArguments = "--remote-debugging-port=9338 --remote-allow-origins=* ..."
}
```

于是"宿主"只解决四件事，其余全部走 CDP：

| 宿主负责 | 为什么必须是宿主 |
|---|---|
| 加载页面、保持进程 | 需要一个 Windows 进程 |
| 预注入脚本 | `AddScriptToExecuteOnDocumentCreatedAsync` 在页面脚本之前执行（隐藏悬浮球不闪） |
| 接管新标签页 | `NewWindowRequested` → `Handled = true` + 同窗口导航，避免 CDP target 漂移 |
| 开放 CDP 端口 | 让 Python 控制器与既有脚本原样复用 |

**截图与输入不在宿主里做**——这样 Linux 版（sway + Chrome）与 Windows 版共享同一份控制器逻辑。

---

## 3. 控制器契约（MaaFramework `CustomController`）

`maa.controller.CustomController` 要求实现 14 个方法，映射如下：

| Maa 回调 | 本实现 | 备注 |
|---|---|---|
| `connect()` | 连 CDP + 设视口 1280×720 + 开触屏仿真（+ 可选 screencast） | 失败返回 False |
| `request_uuid()` | `web-cdp:<port>` | 稳定标识 |
| `screencap()` | `Page.captureScreenshot` 或 screencast 缓存 → BGR `ndarray` | 必须 uint8、(H,W,3) BGR |
| `click(x,y)` | `touchStart` + `touchEnd`（contact 0） | 不用鼠标事件 |
| `swipe(x1,y1,x2,y2,duration)` | 分步 `touchMove`（16ms/步），contact 0 | 步数 = duration/16 |
| `touch_down(contact,x,y,pressure)` | `touchStart`，带上全部活跃触点 | 多指并存 |
| `touch_move(...)` | `touchMove` | 同上 |
| `touch_up(contact)` | `touchEnd`，移除该触点 | 空数组表示全抬 |
| `key_down/key_up/click_key` | `Input.dispatchKeyEvent` | `windowsVirtualKeyCode` |
| `input_text(text)` | `Input.insertText` | |
| `start_app/stop_app` | 仅校验 target 存活 / 空操作 | 页面已在宿主里 |

**多指语义**：`contact` 就是手指 id。视角用 `contact=1`，移动用 `contact=0`——这是 `resource_adb` 的约定，直接沿用（`__CharacterControllerDeltaSwipeAction` 的 `contact=1`）。

---

## 4. 取帧方式的选择（实测）

| 方式 | 静态页 | 动画页 | 说明 |
|---|---|---|---|
| `Page.captureScreenshot` | 244 ms | 41 ms | 页面不重绘时要等合成帧 |
| `Page.startScreencast` | 89 fps 推流 | 同左 | 持续推，本地缓存 |
| 控制器默认（screencast 缓存） | **8–11 ms** | 8–11 ms | 静态页也快 |

因此控制器 `screencast=True` 为默认。代价：多一个读取线程 + 每帧 10KB JPEG 的内存带宽；`_screencastFrameAck` 用 fire-and-forget，避免读取线程自锁。

---

## 5. 已实测通过的部分

| 项 | 结果 |
|---|---|
| 宿主启动 | ✅ WebView2 155.0.4283.62，页面 `nav-done ok=True` |
| CDP 端口 | ✅ `http://127.0.0.1:9338/json` 返回页面 target |
| 自定义控制器实例化 | ✅ `MaaCustomControllerCreate` 成功（native handle 非空） |
| `screencap()` | ✅ `(720, 1280, 3) uint8` |
| 点击 / 触摸拖动 | ✅ 无异常派发（页面为静态首页，画面变化小符合预期） |
| 截图吞吐 | ✅ screencast 8–11 ms/帧 |
| ruff | ✅ `ruff check web/windows` 全通过 |

---

## 6. 待实测（决定这条路线能否跑任务）

| # | 问题 | 判据 |
|---|---|---|
| 1 | 「触屏」模式下拖动是否产生**相对位移**（能否转视角） | 拖动前后帧差 > 8% |
| 2 | 真实云游戏画面下的取帧延迟 | screencast 缓存 < 30 ms，帧率 ≥ 20 fps |
| 3 | `--offscreen` 是否被节流 | 屏外 60 秒内帧差仍在变 |
| 4 | `--hidden` 是否可用 | 同上 |
| 5 | 摇杆/冲刺坐标（若走触屏布局） | 重标 `resource_adb` 的 9 个节点坐标 |

---

## 7. 与既有工作的关系

| 既有资产 | 关系 |
|---|---|
| `web/linux/`（PR #6696） | 同一套控制器语义的 Linux 实现（Wlroots 控制器 + CDP 注入） |
| `resource_web/pipeline/nodes.json`（32 节点） | 两条路线共用 |
| `resource_adb` 的 CharacterController（9 节点） | 触摸视角映射来源 |
| `hide_cloud_ball.py` | 本路线用 `AddScriptToExecuteOnDocumentCreatedAsync` 取代 |

---

## 8. 下一步

1. 用真实登录的云游戏会话跑 `smoke_test.py --touch-mode`，确认「触屏」按钮存在且可点；
2. 拖动测帧差，判定视角；
3. 通过后把 `WebController` 接进 `run_daily` 驱动（Windows 版），先跑 1 个任务（如 `DailyRewards`）；
4. 跑通 3 个任务后再扩到 9 项。
