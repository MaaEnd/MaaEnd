# 云终末地网页端 · 启动器与传输契约（提案 v0.1）

> 背景：维护者建议「把启动 webview 做成一个接口，win/linux/macos 各挂一个实现，其余通用；不应该为 win 和 linux 写两套」。
> 现状：#6746 是 Windows 专用（WebView2 宿主 + 命名管道 + 页面内 JS 取帧），本 PR 是 Linux 专用（headless chrome + CDP + 自建驱动）。两边重复。
> 语言约束（维护者）：**agent 只能用 Go / C++；tools 可以用 py / js；不要 Python、不要 C#。**

---

## 0. 一句话

**平台相关的只有「怎么把浏览器跑起来、怎么连上它」；取帧、覆盖层、preset 展开、重试全部平台无关。**
所以契约只需要定：启动/停止 1 个接口 + 1 套消息 + 1 段取帧 JS。

---

## 1. 分层

```
assets/tasks/pretasks/*.json          (PI v2.7.0，按 controller 过滤)
        │ exec
        ▼
启动器（唯一平台相关）                 ← Windows: WebView2 宿主 / Linux: chrome+CDP 桥 / macOS: 待定
        │ 起浏览器，暴露控制通道
        ▼
core（平台无关）
  ├─ 消息层：eval / screenshot / input_* / close
  ├─ 帧策略：querySelector('video') → canvas 1280x720
  ├─ 覆盖层加载 + preset → pipeline_override
  └─ 重试 / 失败截图 / 日志
```

---

## 2. 接口（唯一必须平台化的部分）

```text
cloud-web-launch --prepare --profile <dir> --url <url> --viewport 1280x720 [--mode headless|visible]
    stdout 单行 JSON：
      {"ready":true,
       "control":"unix:/run/user/1000/maaend-cloud-web-1234.sock",   # Linux
       "cdp":"http://127.0.0.1:PORT",                                 # 可选：外部排查用
       "pid":1234}

cloud-web-launch --status
      {"running":true,"control":"...","pid":1234}

cloud-web-launch --stop
      优雅关闭：先 flush profile（localStorage / IndexedDB 落盘），再退进程
```

- Windows 侧 `control` 用现有的命名管道：`pipe:\\.\pipe\MaaEndCloudWebView_<pid>`；
- Linux 侧 `control` 用 unix domain socket；
- macOS 待定（`unix:` 或平台原生）。
- 失败：非零退出 + stderr 一行原因（不要吞异常）。

---

## 3. 控制通道消息集（两端完全一致）

请求：

```json
{"id": 12, "method": "eval",       "params": {"js": "...", "await_promise": false}}
{"id": 13, "method": "screenshot", "params": {"format": "png"}}
{"id": 14, "method": "input",      "params": {"kind": "mouse", "type": "click", "x": 640, "y": 360}}
{"id": 15, "method": "status",     "params": {}}
{"id": 16, "method": "close",      "params": {}}
```

响应：

```json
{"id": 12, "result": {"value": "..."}}
{"id": 13, "result": {"data_b64": "...", "w": 1280, "h": 720}}
{"id": 12, "error": {"message": "..."}}
```

**帧策略固定为一段 JS**（两边已经同构，直接抽出来共用）：

```js
[...document.querySelectorAll('video')]
  .find(v => v.isConnected && v.readyState >= 2 && !v.paused && !v.ended && v.videoWidth > 0)
// → canvas 1280x720 → toDataURL
```

- Windows 现在走 SharedBuffer 传帧、Linux 走 CDP 回传，**像素来源是同一个 `<video>` 元素**；
  差别只在承载，不在算法。契约里写死这条，macOS 以后照抄即可。

---

## 4. 平台实现清单

| 平台 | 启动器 | 控制通道 | 帧 |
|---|---|---|---|
| Windows | WebView2 宿主（#6746 已有） | 命名管道（已有） | 页面内 JS → SharedBuffer（已有） |
| Linux | `chrome --headless=new --remote-debugging-port=0` + 读 `DevToolsActivePort` | unix socket ⇄ CDP 桥 | 同一段 JS，经 CDP |
| macOS | 未认领 | — | — |

---

## 5. Linux 实现（本 PR 认领）

- **语言**：Go（`agent/go-service --tool cloud-web-launch`）或 C++（`agent/cpp-algo/cloud-webview/linux/`）——按维护者偏好二选一，不引入 Python。
- **组成**：
  1. `prepare`：起 chrome、等 `DevToolsActivePort`、建 unix socket、输出一行 JSON；
  2. `bridge`：把 §3 的消息翻译成 CDP（`Runtime.evaluate` / `Page.captureScreenshot` / `Input.dispatch*`）；
  3. `stop`：SIGTERM + 等 profile 落盘。
- **规模**：400–600 行 + 一个 smoke（起 → connect → screenshot → stop）。
- **可复用**：本 PR 的 `web/linux/cdp_controller.py` 已经把「JS 取帧 + CDP 输入 + 视口校正」跑通过，Go/C++ 版是翻译，不是重新设计。

---

## 6. 与 PI 的接线

```jsonc
// assets/interface.json
{ "name": "Linux-CloudWeb", "type": "Linux",
  "attach_resource_path": ["./resource_linux", "./resource_cloud_web"],
  "linux": { "input": "Wlr", "screencap": "Wlr", "use_win32_vk_code": true } }
```

```jsonc
// assets/tasks/pretasks/LaunchCloudWebLinux.json
{ "pretask": { "controller": ["Linux-CloudWeb"],
               "exec": "agent/cloud-web-launch", "args": ["--prepare"],
               "name": "LaunchCloudWebLinux" } }
```

- 覆盖层统一放 `assets/resource_cloud_web/`（并入 #6746 的目录，不另起 `resource_web`）；
- 不需要新的 `controller.type`，不需要改 MXU。

---

## 7. 待确认（@SENyiAi / 维护者）

1. 消息形状：沿用 #6746 的 `cloud_protocol`（长度前缀 JSON），还是套 CDP 的 JSON-RPC 外形？
2. `window_handle` 是否必须？Linux 无头模式没有窗口，`Win32` 控制器那条路用不上它。
3. 帧格式：PNG（无损、体积大）还是 JPEG（实测单帧 ~190ms）？
4. 是否允许给 `go-service` 加一个 websocket 依赖（Go 版桥需要；C++ 版则要自己写 WS 客户端）。
