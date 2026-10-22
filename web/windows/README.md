# web/windows —— 浏览器后端 + CDP 控制器

云·终末地网页端的**桌面适配层**：拉起一个**无头** Chromium（默认系统 Edge），
用 CDP 截图/多指触摸，交给 MaaFramework 的自定义控制器驱动。

**零窗口、零任务栏条目、零 Alt+Tab、不抢鼠标。** 用户不需要安装任何额外运行时
（Win10/11 自带 Edge；Linux 建议装 google-chrome）。

```
┌──────────────────────────────┐
│ MaaEnd 任务 / Tasker          │
│   └─ WebController           │  ← Python，CustomController 子类
│        │ CDP (WebSocket)     │
├────────┼─────────────────────┤
│ browser.py 选出的后端         │
│   └─ Edge --headless=new      │  ← 或外部端点（MAAEND_WEB_CDP_ENDPOINT）
│        └─ 云·终末地网页        │
└──────────────────────────────┘
```

## 后端边界

`controller/browser.py` 是唯一的后端出入口。后端 = 浏览器 × 运行模式，两者都写进日志和控制器 uuid：

| 后端标识 | 出现条件 | 起停归属 | uuid |
|---|---|---|---|
| `edge-headless` | Windows 找到 Edge + `MAAEND_WEB_MODE=headless`（默认） | 本模块 | `web-cdp:edge-headless` |
| `edge-offscreen` | 同上但 `MAAEND_WEB_MODE=offscreen` | 本模块 | `web-cdp:edge-offscreen` |
| `webview2` | `MAAEND_WEB_MODE=webview2`（需先构建宿主） | 本模块拉起宿主 | `web-cdp:webview2` |
| `chrome-headless` / `chrome-offscreen` | 只找到 Chrome（Linux 服务器常见） | 本模块 | `web-cdp:chrome-<mode>` |
| `external` | 设了 `MAAEND_WEB_CDP_ENDPOINT` | 用户自己 | `web-cdp:external:<port>` |

**三种模式怎么选**：

| | `headless`（默认） | `offscreen` | `webview2` |
|---|---|---|---|
| 窗口对象 | 无 | 有，屏幕外 + `WS_EX_TOOLWINDOW` | 有，屏幕外 + `WS_EX_TOOLWINDOW` |
| 页面 `visibilityState` | `visible` | `visible`（四个反遮挡开关） | `visible` |
| 随时拉出来给人操作 | ❌ 只能换进程（≈1.1 s） | ✅ `pop_to_screen()` **2–12 ms** | ✅ 控制口 `show`，同进程 |
| **挂着时能不能停渲染** | ❌ 渲染常驻 | ❌ 渲染常驻 | ✅ `sleep_host()`（`Visible=false`，省 GPU） |
| 额外依赖 | 无 | 无 | **.NET 8/10 + WebView2 Runtime**，需构建 |
| 适合 | 无人值守跑批 | 要人能随时接手 | 挂着不想占 GPU / 企业环境 |

`webview2` 这条线的唯一不可替代能力是**进程内停车**（`Visible=false` 时真的不渲染）；
代价是要构建一个 C# 宿主，且仅 Windows。

**构建并运行 WebView2 宿主**：

```bash
dotnet build -c Release web/windows/MaaEndWebHost        # 只需 .NET 8 或 10 的 SDK
MAAEND_WEB_MODE=webview2 python smoke_test.py            # 控制器自动找 bin/Release/net*/MaaEndWebHost.exe
```

宿主自带一个本地控制口（`127.0.0.1:<随机>`，端口写在 `<profile>/host-state.json`），
按行 JSON 收发：`{"cmd":"show","left":120,"top":120}` / `park` / `hide` / `resume` / `status` / `quit`。

`external` 是为"自带宿主"留的口：企业锁版本的 Chrome、自己编的 WebView2 宿主、公司内网调试实例，
都只需要给出 `host:port`，仓库里不必多一门语言、CI 不必多一条构建链。

## 结构

| 路径 | 作用 |
|---|---|
| `controller/browser.py` | 后端边界：定位/拉起无头浏览器、随机调试端口、pidfile、残留清理、收尸 |
| `controller/cdp_client.py` | 最小 CDP 客户端（多指触摸、截图、screencast 帧缓存） |
| `controller/web_controller.py` | `CustomController` 实现，Maa 的回调 → CDP；UA 修正、视口校正、登录提示、收尸 |
| `controller/smoke_test.py` | 不跑任务，直接验证"控制器 → 后端 → CDP"链路 |
| `controller/bench_capture.py` | 截图吞吐基准（静态页 / 动画页 / screencast） |

## 快速开始

不需要构建、不需要先开任何东西：

```bash
cd web/windows/controller
uv run --with maafw --with websocket-client --with pillow python smoke_test.py
```

连外部端点（不拉起浏览器，也不关闭它）：

```bash
MAAEND_WEB_CDP_ENDPOINT=127.0.0.1:9338 python smoke_test.py
```

## 环境变量

| 变量 | 作用 |
|---|---|
| `MAAEND_WEB_MODE` | `headless`（默认）/ `offscreen`；非法值直接报错，不静默降级 |
| `MAAEND_WEB_CDP_ENDPOINT` | 直连外部端点，如 `127.0.0.1:9338`；设了就不拉起、也不关闭 |
| `MAAEND_WEB_BROWSER` | 浏览器可执行文件路径，覆盖自动探测 |
| `MAAEND_WEB_PROFILE` | 用户数据目录（登录态 + 诊断截图 + browser.log 都在这里） |
| `MAAEND_WEB_URL` | 起始地址，默认云·终末地网页端 |
| `MAAEND_WEB_VISIBLE=1` | 调试：窗口真的显示在屏幕上（等价 offscreen + 放在屏幕内） |
| `MAAEND_WEB_KEEP_ALIVE=1` | 控制器退出时保留本模块拉起的浏览器 |
| `MAAEND_WEB_KEEP_UA=1` | 保留原始无头 UA（默认会把 `HeadlessChrome` 抹成 `Chrome`） |
| `MAAEND_WEB_ALLOW_FULLSCREEN=1` | 不注入「禁全屏/禁指针锁」护栏 |
| `MAAEND_WEB_LOG_DIR` | 诊断截图目录（默认放在 profile 下的 `diagnostics/`） |
| `MAAEND_WEB_STRICT=1` | 识别到登录/风控页时让 `connect()` 直接失败，而不是只告警 |

## 实测数据（Edge 155 / 1280×720 CSS 视口 / 本机）

| 项目 | headless | offscreen |
|---|---|---|
| 后端就绪（`/json/version` 可连） | **0.55–0.85 s** | 0.55–0.85 s |
| CSS 视口 | **精确 1280×720** | 精确 1280×720（`--force-device-scale-factor=1` + `setWindowBounds` 校正） |
| `Page.captureScreenshot`（在播视频） | 47–52 ms | 47–55 ms |
| `Page.startScreencast`（在播视频） | ~26–30 fps | 同左（屏外时也推帧） |
| 控制器 `screencap()`（screencast 缓存） | 8–11 ms | 8–11 ms |
| 整棵进程树占用 | 15 进程 / 空闲 ~850 MB | 同左 |
| 空闲 CPU | ~0.3 核（≈2% 整机） | 同左 |
| 拉回屏幕给用户 | ❌ 需换进程 ≈1.1 s | **2–12 ms**，不夺焦点 |
| 任务栏 / Alt+Tab | 无 | 挂 `WS_EX_TOOLWINDOW` 后无 |

三点结论：

1. **默认开 screencast**，但**必须带缓存时效回落**：无头下 screencast 只在"有新合成帧"时推，
   静态界面会拿到旧帧，所以超过 2 s 的缓存要走 `captureScreenshot`。
2. **不要用 `Emulation.setDeviceMetricsOverride` 强制视口**：实测 screencast 从 ~26 fps 掉到 ~15 fps。
   精确视口靠窗口尺寸，不是靠 emulation。
3. **屏外窗口不如真无头**：`--window-position=-32000,-32000` 虽然肉眼看不见，但页面会
   `document.visibilityState == "hidden"`（云游戏客户端普遍据此降帧），而且任务栏会多一个条目、
   Alt+Tab 能切过去。真无头报 `visible` + `hasFocus`。

## 三个坑（都已处理，别改回去）

- **UA**：无头 UA 带 `HeadlessChrome`，官网点开登录弹窗后会弹
  "当前浏览器可能影响游戏体验 / 建议使用最新版本 Chrome"。控制器在 `connect()` 里把
  `HeadlessChrome` 换成 `Chrome` 并同步 client hints，然后 reload 一次——实测警告消失。
- **CDP 握手 403**：`websocket-client` 默认带 `Origin` 头，Chromium 会拒。客户端用
  `suppress_origin=True`，因此**不需要** `--remote-allow-origins`（那等于向本机任意网页开放调试口）。
- **视口对不齐（两个独立原因）**：① `--window-size` 会被 profile 里记住的窗口尺寸覆盖 → 启动后必须
  用 `Browser.setWindowBounds` 校正一次；② 真窗口会继承系统缩放（150% 屏上实测 `devicePixelRatio=1.5`，
  视口只能凑到 1281×721、截图 1279×720）→ 必须带 `--force-device-scale-factor=1`。

## 依赖

- 浏览器：Microsoft Edge（Win10/11 自带）或 Google Chrome；Linux 上别用 Chromium——
  它不含 H.264 专有编解码，云游戏画面放不出来。
- 控制器：`maafw`、`numpy`、`pillow`、`websocket-client`

## 已知限制 / 待实测

1. **登录**：无头没有可见窗口，登录靠截图（登录页会把 PNG 落到 `diagnostics/`）让用户扫码；
   登录态存在 profile 里，之后不需要重复登录。
2. **真实会话未验**：本仓库只验到"能起、能截、能点"。真实登录后的取帧延迟、视角转向、
   `document.visibilityState` 对串流的影响，要用真实账号跑 `smoke_test.py` 才作数。
3. **仅 Windows 已验证**：`--headless=new` + CDP 本身跨平台，Linux 侧理论上同一套代码可用，
   但没在真机上跑过。
4. **触屏模式待验证**：客户端工具栏有「触屏 / 鼠标 / 手柄」，视角转向依赖触屏模式下的相对位移
   （见 `docs/web-windows/`）。
