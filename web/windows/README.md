# web/windows —— WebView2 宿主 + CDP 控制器

云·终末地网页端的 **Windows 适配层**：用 WebView2 承载网页，用 CDP 截图/触摸，
交给 MaaFramework 的自定义控制器驱动。**不抢鼠标、不需要窗口在前台。**

```
┌──────────────────────────────┐
│ MaaEnd 任务 / Tasker          │
│   └─ WebController           │  ← Python，CustomController 子类
│        │ CDP (WebSocket)     │
├────────┼─────────────────────┤
│ MaaEndWebHost.exe (C#)       │
│   └─ WebView2 (Edge 内核)     │
│        └─ 云·终末地网页        │
└──────────────────────────────┘
```

## 结构

| 路径 | 作用 |
|---|---|
| `MaaEndWebHost/` | C# WinForms 宿主：加载页面、预注入脚本、接管新窗口、开放 CDP 端口 |
| `controller/cdp_client.py` | 最小 CDP 客户端（多指触摸、截图、screencast 帧缓存） |
| `controller/web_controller.py` | `CustomController` 实现，Maa 的 14 个回调 → CDP |
| `controller/smoke_test.py` | 不跑任务，直接验证"控制器→CDP→宿主"链路 |
| `controller/bench_capture.py` | 截图吞吐基准（静态页 / 动画页 / screencast） |

## 快速开始

```bash
# 1) 构建宿主（需要 .NET 8 SDK + WebView2 Runtime）
cd web/windows/MaaEndWebHost
dotnet build -c Release

# 2) 起宿主（屏外 + CDP 9338）
./bin/Release/net8.0-windows/MaaEndWebHost.exe --offscreen --cdp-port 9338 --console

# 3) 验证链路
cd ../controller
uv run --with maafw --with websocket-client --with pillow python smoke_test.py --port 9338
```

## 宿主参数

| 参数 | 说明 |
|---|---|
| `--url <URL>` | 起始地址（默认云终末地官网） |
| `--cdp-port <N>` | CDP 端口（默认 9338；0 = 不开放） |
| `--profile <DIR>` | WebView2 用户数据目录（默认 `%LOCALAPPDATA%\MaaEndWebHost\udf`） |
| `--size <WxH>` | 视口（默认 1280x720） |
| `--offscreen` | 窗口移到屏幕外（推荐的后台方式） |
| `--hidden` | 隐藏窗口（实验性，可能被 Chromium 节流） |
| `--block-pointer-lock` | 预注入禁掉 `requestPointerLock` |
| `--keep-ball` | 不隐藏云端悬浮球 |
| `--console` | 附加控制台看日志 |

## 实测数据（WebView2 155 / 1280×720 / 本机）

| 取帧方式 | 耗时 | 说明 |
|---|---|---|
| `Page.captureScreenshot`（静态页） | **244 ms/帧** | 页面不重绘时要等帧，慢 |
| `Page.captureScreenshot`（动画页） | **41 ms/帧** | 有持续重绘就快 |
| `Page.startScreencast` | **89 fps 推流**，10 KB/帧 | 持续推，本地缓存最新帧 |
| 控制器 `screencap()`（screencast 缓存） | **8–11 ms** | 静态页也一样快 |

结论：**默认开 screencast**（`WebController(screencast=True)`），否则静态菜单页每帧要 244ms。

## 依赖

- 宿主：.NET 8 SDK（构建）、WebView2 Runtime（运行，Win11 自带）
- 控制器：`maafw`、`numpy`、`pillow`、`websocket-client`

## 已知限制

1. **不是真无头**：WebView2 必须有窗口；`--offscreen` 是推荐做法，`--hidden` 待实测帧率。
2. **仅 Windows**：Linux 继续用 `web/linux`（sway + Chrome + CDP），控制器逻辑相同。
3. **触屏模式待验证**：客户端工具栏有「触屏 / 鼠标 / 手柄」，视角转向依赖触屏模式下的相对位移（见 `docs/web-windows/`）。
