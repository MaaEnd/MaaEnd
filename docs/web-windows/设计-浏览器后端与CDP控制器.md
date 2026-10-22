# Web 端适配：无头浏览器后端 + CDP 控制器（设计说明）

日期：2026-10-21（初版） / 2026-10-22（改为无头后端）
状态：可运行原型（宿主自动拉起、控制器可截图/点击/拖动），真实会话与视角转向待实测

---

## 1. 目标与约束

| 目标 | 说明 |
|---|---|
| 复用现有任务 | 不改 `assets/tasks/**`，只加薄覆盖层 |
| **完全无头** | 不创建任何平台窗口：无任务栏条目、无 Alt+Tab、无 Win+D 翻出 |
| 不抢鼠标 | 自动化期间用户能正常用电脑（输入全走 CDP） |
| 可后台 | 不要求窗口在前台或可见 |
| 零安装 | 用户机器上不需要 .NET / WebView2 Runtime / 额外浏览器 |
| 跨平台同源 | Windows 与 Linux 共用同一套控制器语义（CDP + 多指触摸） |
| 可交付上游 | 代码独立在 `web/windows/`，不碰上游共享节点 |

---

## 2. 选型记录：为什么不是 WebView2

初版用的是"C# WinForms 宿主 + WebView2"，理由是 Windows 上要拿到一个"能开 CDP 端口的 Chromium"，
WebView2 是最省事的容器（Win11 自带运行时、几十行 C#、有 `AddScriptToExecuteOnDocumentCreated`
和 `NewWindowRequested` 这类宿主级钩子）。

**这个方案被"完全无头"这条要求否定**：WebView2 的 API 面要求一个 parent HWND，官方不支持
headless（WebView2Feedback 相关 issue 被标 By Design），因此它必然是一个真窗口。

替代方案是系统 Edge 的 `--headless=new`——同一份 Chromium，只是不创建平台窗口，CDP 全功能可用。
宿主原本买到的四样东西都能在纯 CDP 里重现：

| 宿主做法 | 无头 CDP 等价物 |
|---|---|
| `AddScriptToExecuteOnDocumentCreatedAsync` | `Page.addScriptToEvaluateOnNewDocument` |
| `NewWindowRequested` → 同窗口导航 | `Target.setDiscoverTargets` / `setAutoAttach` |
| 独立 profile | `--user-data-dir`（登录态持久化） |
| 屏外/静音/固定视口/反节流 | 命令行参数（无头本身无窗口） |

### 三种"看不见"方案的实测对比

| | 现在的 C# 宿主（`--offscreen`） | 系统 Edge 屏外窗口 | **系统 Edge `--headless=new`** |
|---|---|---|---|
| 页面 `document.visibilityState` | visible | **hidden**（云游戏会据此降帧/暂停） | **visible** + `hasFocus` |
| 任务栏条目 | 无（`WS_EX_TOOLWINDOW`） | **有** | 无 |
| Alt+Tab / 任务视图 | 不出现 | 出现 | 不可能出现 |
| screencast | 8–11 ms（缓存） | 76 帧/2.5 s | 74 帧/2.5 s（同级） |
| 额外依赖 | .NET 8 + WebView2 Runtime | 无 | 无 |
| 能否进 CI | **否**（WebView2 在非交互会话下不工作） | 能 | 能 |

结论：屏外窗口"肉眼看不见但页面自己知道被隐藏"，对云游戏是硬伤；真无头在可感知的三条
（任务栏、Alt+Tab、页面可见性）上全胜，性能同级。

### 什么时候该回头用 WebView2

WebView2 的优势集中在四点，只是一旦出现下列需求，它才重新变成最优解：

1. 官网风控要求锁定 Chromium 版本（Edge 强制自动更新，无法回退）；
2. 目标用户是企业电脑，Edge 受组策略限制或禁止再起实例；
3. 安全要求不允许开远程调试端口（WebView2 可用 `CallDevToolsProtocolMethodAsync` 在进程内调）；
4. 需要长期拦截/改写网络请求（`WebResourceRequested` 比 CDP `Fetch` 好用）。

为这四种情况留的口是**后端边界**（下一节），不需要把 C# 放回仓库。

---

## 3. 后端边界

`controller/browser.py` 是唯一出入口，标识写进日志与控制器 `request_uuid()`。
**后端 = 浏览器 × 运行模式**（`MAAEND_WEB_MODE`）：

| 后端标识 | 出现条件 | 起停归属 | uuid |
|---|---|---|---|
| `edge-offscreen` | Windows 找到 Edge + `offscreen`（**默认**，屏外真窗口） | 本模块 | `web-cdp:edge-offscreen` |
| `edge-headless` | `MAAEND_WEB_MODE=headless`（纯无头） | 本模块 | `web-cdp:edge-headless` |
| `chrome-headless` / `chrome-offscreen` | 只找到 Chrome（Linux 常见） | 本模块 | `web-cdp:chrome-<mode>` |
| `external` | `MAAEND_WEB_CDP_ENDPOINT` 非空 | 用户自己（既不拉起也不关闭） | `web-cdp:external:<port>` |

两种模式的取舍点是**"要不要能随时交给人操作"**，不是性能：

| | `headless` | `offscreen` | `webview2` |
|---|---|---|---|
| 窗口对象 | 无 | 有（屏幕外 + `WS_EX_TOOLWINDOW`） | 有（屏幕外 + `WS_EX_TOOLWINDOW`） |
| 页面 `visibilityState` | `visible` | `visible`（四个反遮挡开关） | `visible` |
| 交给人操作 | ❌ 只能换进程（≈1.1 s，且必须优雅关闭否则状态不落盘） | ✅ `pop_to_screen()` 实测 **2–12 ms**，同进程不重载 | ✅ 控制口 `show`，同进程 |
| 挂着时停渲染 | ❌ | ❌ | ✅ `sleep_host()`（`Visible=false`） |
| 额外依赖 | 无 | 无 | .NET + WebView2 Runtime，需构建 |

### 3.1 `webview2` 模式（第三条线）

`MaaEndWebHost`（C# WinForms）重新引入，但**只做只有它能做的事**：进程内停车。
它启动时是一个无边框、屏幕外、`WS_EX_TOOLWINDOW` 的真窗口，ClientSize 固定 1280×720、
强制 DPR=1，因此 CSS 视口天然精确（实测 `1280×720`、截图 `(720,1280,3)`、`screencap` 20 ms）。

Python 侧通过宿主的**本地控制口**驱动（`127.0.0.1:<随机>`，按行 JSON）：

| 命令 | 语义 |
|---|---|
| `{"cmd":"show","left":120,"top":120}` | 窗口挪到屏幕内 + `Visible=true`（显式 `SW_SHOWNOACTIVATE`，不抢焦点） |
| `{"cmd":"park"}` | 挪回屏幕外、`Visible=true`：渲染继续，自动化可继续跑 |
| `{"cmd":"hide"}` | `Visible=false`：**真停车**，不渲染、省 GPU；此时自动化也跑不了 |
| `{"cmd":"resume"}` | 恢复渲染 |
| `{"cmd":"status"}` | 返回 `visible/x/y/size/cdp_port/control_port/url` |
| `{"cmd":"quit"}` | 优雅退出（让 WebView2 自己 flush profile，保住登录态） |

端口与 pid 写在 `<profile>/host-state.json`；控制器退出时先发 `quit` 再收进程树。

拉起时用 `--remote-debugging-port=0`，端口由 Chromium 写进 `<profile>/DevToolsActivePort` 后读回：
多实例不抢端口，也不给本机留一个可猜的调试入口。**同一 profile 只能有一个实例**
（第二个会静默转发 URL 后退出），所以启动失败时会把"谁占着这个 profile"列进报错。

### 3.2 运行时热切换（`switch_mode` / `promote` / `demote`）

**默认档是 `offscreen`**：启动就是一个屏幕外的真窗口，用户看不见，但 `pop_to_screen()`
2–12 ms 就能摆到屏幕上交给人操作——这是这个工具的核心价值，所以不把它藏在环境变量后面。
纯无头（不建任何窗口）仍可用：`MAAEND_WEB_MODE=headless`。

需求形态：**平时后台跑批，人在场时能立刻接管，接管完再回到后台**。
无头进程没有窗口对象，系统浏览器也没有"凭空造一个窗口给它"的原语，
所以从无头"切出来"只能**换进程**：优雅关闭当前后端 → 用同一 profile 以目标模式重开 → 重新装配 CDP。

```
connect()   -> offscreen（默认：屏外窗口，看不见）
pop_to_screen() / park_offscreen()          -> 2–12 ms，同进程
promote()   -> headless 起步时切到 offscreen（换进程，窗口直接摆进屏幕内）
demote()    -> offscreen 起步时切回 headless（窗口消失、hwnd 归零）
switch_mode("webview2") / switch_mode("headless")     # 任意两模式互切
```

| 项 | 实测 |
|---|---|
| headless → offscreen | **1748–1839 ms** |
| offscreen → headless | **2312–3649 ms**（随机器负载） |
| 登录态 | 两次切换后 `localStorage` 标记仍在（走优雅关闭，强杀会丢） |
| 切回无头后 | `hwnd = 0`，确实没有窗口了 |
| 用例 | `smoke_test.py --switch` 10/10（含起步就是 offscreen 的情形） |

约束：切换期间 CDP 会断开，**只能在任务之间调**；页面按配置的 `url` 重开，页内临时状态不保留。
要"同进程、零重载"的显示，只有 `webview2` 的 `show` 能做到。
顺手把 `close()` 也改成优雅关闭——之前强杀会让刚扫码登录的状态白丢一次。

---

### 3.3 窗口外观（无边框 / 任务栏）与"只有页面画面"的边界

`controller/window_style.py` 可以在**运行中**改窗口样式（跨进程也可以）：任务栏/Alt+Tab 可见 ↔
工具窗口互切、去掉/恢复标题栏，并且用 `--fit` 在去边框后把客户区重新校正到 1280×720。
控制器侧对应 `set_taskbar_visible()` / `set_borderless()`。

**为什么"只有页面画面"在 Edge 上做不到**：窗口顶部那 88 px（标签页 + 地址栏 + 书签栏）
是 Chromium 自己画在客户区里的，不是 Windows 非客户区，所以改 `WS_CAPTION` 无效。
`--app=URL` 能去掉工具栏/标签页/书签，但 Chromium 仍画约 30 px 的应用窗口标题条。
真正的纯画面只有两条路：`--kiosk`（全屏，放弃当窗口用）或 **WebView2 宿主**
（`FormBorderStyle.None`，实测窗口 1280×720 全是网页，零残留条）。

⇒ 这条差异正好落在三条线的定位上：**要"人看着舒服"的纯画面就选 webview2**；
只要"人能操作"，Edge 的 offscreen 就够。

### 3.4 窗口操作入口（关闭 / 隐藏 / 任务栏）与 MXU 的边界

窗口可以无边框、可以不在任务栏，但**用户必须还能关掉它、藏起来、找回来**。给三条入口：

| 入口 | 形态 | 适用 |
|---|---|---|
| 全局热键 | `Ctrl+Alt+H` 藏/显、`Ctrl+Alt+Q` 关闭、`Ctrl+Alt+B` 任务栏切换 | 用户在电脑前，没有终端 |
| `webctl.py` | `status / show / hide / close / taskbar / borderless`，按 `--profile` 自动发现实例 | 脚本、排错、CI |
| 宿主控制口 | 按行 JSON：`show / park / hide / resume / status / quit` | 其它宿主程序（如 MXU） |

两个实现要点：

1. **热键不能用 WinForms 的 `ShowInTaskbar`**：改它会重建窗口句柄，注册在旧句柄上的热键会一起失效
   （实测按下任务栏热键后 H/Q 就死了）。直接改 `WS_EX_TOOLWINDOW`/`WS_EX_APPWINDOW` 再
   `SW_HIDE`→`SW_SHOWNOACTIVATE` 刷新任务栏条目。
2. **热键要能降级**：`Ctrl+Alt+T` 在本机被别的程序占用（`RegisterHotKey` 返回 False），
   所以注册表按"主选 → 备选"逐个尝试，并把每条的真实结果写进日志，而不是整组静默失败。

**MXU 边界**：MXU（Tauri + 前端）里没有"网页窗口操作"按钮，加按钮要改 MXU 本体，
不属于 `web/` 这条线。这边只负责把入口做干净——MXU 将来加按钮时 `exec` 一次 `webctl.py`
或往控制口发一条 JSON 即可，不需要再动控制器。

## 4. 控制器契约（MaaFramework `CustomController`）

| Maa 回调 | 本实现 | 备注 |
|---|---|---|
| `connect()` | 选后端 → 连 CDP → 修 UA → 校正视口 → 开触屏与 screencast → 探测页面 | 失败返回 False 并写日志 |
| `request_uuid()` | `web-cdp:<后端标识>` | 稳定、可辨识 |
| `screencap()` | screencast 缓存（新鲜时）否则 `Page.captureScreenshot` → BGR `ndarray` | 必须 uint8、(H,W,3) |
| `click(x,y)` | `touchStart` + `touchEnd`（contact 0） | 不用鼠标事件 |
| `swipe(...)` | 分步 `touchMove`（16 ms/步），contact 0 | 步数 = duration/16 |
| `touch_down/move/up` | `Input.dispatchTouchEvent`，携带全部活跃触点 | 多指并存 |
| `key_*`/`click_key` | `Input.dispatchKeyEvent` | `windowsVirtualKeyCode` |
| `input_text` | `Input.insertText` | |
| `start_app/stop_app` | 校验 target 存活 / 空操作 | 页面已在浏览器里 |
| `inactive()` | 只记日志 | **不关浏览器**：任务之间要复用 |

**多指语义**：`contact` 就是手指 id。视角用 `contact=1`，移动用 `contact=0`——沿用
`resource_adb` 的约定（`__CharacterControllerDeltaSwipeAction` 的 `contact=1`）。

---

## 5. 取帧方式（无头实测）

| 方式 | 静态页 | 在播视频 | 说明 |
|---|---|---|---|
| `Page.captureScreenshot` | ~55 ms | 47–52 ms | 按需，无缓存风险 |
| `Page.startScreencast` | 0 帧（不重绘就不推） | **26–30 fps** 推流 | 本地缓存最新帧 |
| 控制器默认（缓存 + 时效回落） | 8–11 ms（缓存新鲜） | 8–11 ms | 缓存超 2 s 回落截图 |

两个反直觉的实测点：

1. **`Emulation.setDeviceMetricsOverride` 会把 screencast 从 ~26 fps 砍到 ~15 fps**
   （视频确认在播的对照实验），所以精确视口不能用它，改用窗口尺寸
   （`--window-size=1310,815` 补偿 30×95，之后 `Browser.setWindowBounds` 兜底）。
2. **screencast 不是定时推帧**：纯 canvas 的 `data:` 页在无头下 0 帧。静态界面必须靠缓存 + 回落。

---

## 6. 生命周期与残留

```
WebController.connect()
  └─ browser.ensure_backend()
       ├─ MAAEND_WEB_CDP_ENDPOINT ? → external（不动它）
       └─ 否则：清残留(pidfile) → 拉起 headless → 等 DevToolsActivePort → 探活 → 写 pidfile
WebController.close() / atexit
  └─ backend.close()：taskkill /T /F（Windows）或 killpg（POSIX），删 pidfile
```

- 崩溃残留：下次启动读 pidfile，pid 还活着就整棵树杀掉，避免"占着 profile 起不来"。
- `MAAEND_WEB_KEEP_ALIVE=1` 可保留浏览器（调 UI 时有用）。
- `MAAEND_WEB_VISIBLE=1` 取消无头（排查登录/识图），这是唯一会看到窗口的模式。

---

## 7. 安全

| 措施 | 原因 |
|---|---|
| `--remote-debugging-port=0` + 随机端口 | 不给本机其它程序留可猜入口 |
| 不传 `--remote-allow-origins` | 放开它等于允许本机任意网页连上调试口；客户端用 `suppress_origin=True` 即可 |
| 独立 `--user-data-dir` | 不碰用户日常浏览器与登录态 |
| profile 权限（POSIX 0700） | 登录态等同凭证 |

---

## 8. 已实测通过

| 项 | 结果 |
|---|---|
| 后端自举 | ✅ 无任何前置进程，0.6–1.0 s 就绪（`Edg/155.0.4283.62`） |
| 无头能力 | ✅ 真 GPU（ANGLE/NVIDIA D3D11）、MSE H.264、Widevine、WebRTC |
| 精确视口 | ✅ CSS `1280×720`（截图 `(720,1280,3)` uint8） |
| 页面状态 | ✅ `visibilityState=visible`、`hasFocus=true` |
| offscreen 模式 | ✅ 窗口在 `(-21845,-21845)`、`WS_EX_TOOLWINDOW` 已挂（`0x200180`）、页面仍 `visible` |
| offscreen 交接 | ✅ `setWindowBounds` 拉回屏幕 **2–12 ms**、不夺焦点；再挪回屏幕外仍 `visible` |
| 启动不打扰 | ✅ 启动瞬间会抢前台，随后自动还原（`SetForegroundWindow` + `AttachThreadInput` 兜底） |
| 输入不打扰 | ✅ 触摸点击/触摸拖动/鼠标拖动全程 122 次采样：前台窗口不变、系统光标未被搬动 |
| 护栏 | ✅ 注入后 `requestFullscreen` / `requestPointerLock` 被废（误点全屏不会顶掉桌面） |
| webview2 后台 | ✅ 无边框屏幕外窗口 + `WS_EX_TOOLWINDOW`；CSS 视口 `1280×720`、截图 `(720,1280,3)`、`screencap` 20 ms |
| webview2 控制口 | ✅ `show`/`park`/`hide`/`resume`/`status`/`quit` 全部生效；`quit` 后端口不可连、进程退出 |
| UA 修正 | ✅ 抹掉 `HeadlessChrome` 后官网"浏览器可能影响体验"警告消失（截图对照） |
| 点击 / 拖动 | ✅ 派发无异常，画面差异符合预期（点开登录弹窗 81% 像素变化） |
| 收尸 | ✅ 退出后 0 残留进程、pidfile 已删 |
| **热切换** | ✅ headless→offscreen 1748 ms / offscreen→headless 2312–3649 ms；两次切换后 `localStorage` 标记仍在；切回后 `hwnd=0`（真零窗口）；10/10 |
| 优雅关闭 | ✅ 正常 `close()` 也走 `Browser.close`（0.4 s），避免刚登录的状态被强杀丢掉 |
| ruff | ✅ `ruff check web/windows` 通过 |

## 9. 待实测（决定这条路线能否真正跑任务）

| # | 问题 | 判据 |
|---|---|---|
| 1 | 真实登录后的取帧延迟 | screencast 缓存 < 30 ms，帧率 ≥ 20 fps |
| 2 | 「触屏」模式下拖动是否产生相对位移（能否转视角） | 拖动前后帧差 > 8% |
| 3 | 登录态与风控：扫码登录后 profile 复用是否稳定 | 二次启动无需重新登录 |
| 4 | 摇杆/冲刺坐标（若走触屏布局） | 重标 `resource_adb` 的 9 个节点坐标 |

---

## 10. 与既有工作的关系

| 既有资产 | 关系 |
|---|---|
| `web/linux/`（PR #6696） | 同一套控制器语义的 Linux 实现；无头后端可两边共用 |
| `resource_web/pipeline/nodes.json`（32 节点） | 两条路线共用 |
| `resource_adb` 的 CharacterController（9 节点） | 触摸视角映射来源 |
| `assets/tasks/pretasks/LaunchGamePC.json` | 用户入口的既有范式（`pretask` 起外部程序），web 版可照此接入 |

## 11. 下一步

1. 用真实登录的云游戏会话跑 `smoke_test.py --touch-mode`，确认「触屏」按钮存在且可点；
2. 拖动测帧差，判定视角；
3. 通过后把 `WebController` 接进 `run_daily` 驱动，先跑 1 个任务（如 `DailyRewards`）；
4. 跑通 3 个任务后再扩到 9 项。
