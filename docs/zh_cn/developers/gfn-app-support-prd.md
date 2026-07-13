# PRD：GeForce NOW 原生客户端（GFN-App）支持

> [!NOTE]
> 本文档为产品需求文档（PRD），描述需求与设计约束，不包含最终实现。
> 状态：**草案**。涉及"待实测验证"的条目需在实现后于真实 Windows + GeForce NOW 环境确认。

## 1. 背景与目标

### 1.1 背景

MaaEnd 目前支持的控制器形态：

- **本地 Windows 客户端**（`Win32-Front`）：Unity 窗口类 `UnityWndClass`，标题匹配 `Endfield`，`ScreenDC` 截图 + `Seize` 输入；
- **安卓端**（`ADB`）、**PlayCover**、**Wlroots**。

部分用户通过 **GeForce NOW（GFN）云游戏原生桌面客户端** 游玩终末地：本地进程为 `GeForceNOW.exe`（基于 CEF），游戏本体运行在云端，本机不存在游戏进程与游戏安装目录。GFN 窗口的类名与标题均不匹配 `Win32-Front` 的正则，控制器列表中永远找不到 GFN 会话，MaaEnd 完全不可用。

姊妹项目 MaaNTE 已完成同类支持并实测确认关键结论（见其 `docs/zh_cn/develop/geforce-now-support-prd.md`）：

- GFN 原生客户端流窗口类名为 **`CEFCLIENT`**（EnumWindows 运行时取证，pid 级验证；社区旧文档的 `CEF-OSC-WIDGET` 为老版本客户端，不适用）；
- 窗口标题模式为 **`<游戏英文名> on GeForce NOW`**；
- `PrintWindow` 截图 + `Seize` 鼠标/键盘组合可用，键盘按键（含 WASD 长按）可透传到云端；
- 流窗口无边框、无页面头部，接受标准 `MoveWindow`/`SetWindowPos` 外部缩放，客户区调至 1280x720 可获得干净 720p 帧；
- **串流渲染分辨率在会话建立时锁定**：本地缩放窗口不会改变云端渲染分辨率，已开始串流的会话仍按原分辨率云端渲染后缩放显示，固定 ROI 识别会系统性失败。

### 1.2 目标

- 用户使用 GFN 原生客户端游玩终末地时，MaaEnd 能自动找到游戏窗口并正常连接控制器。
- Go agent 在 GFN 场景下自动把游戏窗口客户区调整为 1280x720 基准分辨率，调整失败时优雅降级并给出明确的用户引导。
- 对 GFN 场景的能力边界（仅前台、串流分辨率锁定、依赖本地安装的任务不可用）给出明确的用户提示与文档说明。

### 1.3 非目标

- **不支持 GFN Chrome 网页版**：MaaNTE 实测其窗口模式下页面自绘头部占据客户区顶部约 26px，游戏画面下移且按比例缩放，所有固定 ROI 识别失败；即使 F11 全屏可用，本期不纳入。
- 不支持 GFN 移动端 / TV 端。
- 不自动化 GFN 自身的登录、排队、会话续期流程（属流程外状态，见 §6 风险）。

## 2. 现状分析

### 2.1 主路径：MaaFramework Win32 控制器

`assets/interface.json` 中 `Win32-Front` 控制器由 GUI 壳（MXU）传给 MaaFramework 的窗口枚举接口完成匹配：

```jsonc
"win32": {
    "class_regex": "UnityWndClass",
    "window_regex": "Endfield",
    "screencap": "ScreenDC",
    "mouse": "Seize",
    "keyboard": "Seize"
}
```

GFN 窗口（类 `CEFCLIENT`，标题含 `on GeForce NOW`）两个正则均不命中。

### 2.2 辅助路径：Go agent 任务预检链

`agent/go-service` 通过 taskersink 在任务启动事件（`EventStatusStarting`）上挂载预检：

- `taskersink/aspectratio` — 分辨率/宽高比检查，不达标时警告并强停任务；全屏时尝试 Alt+Enter 切窗口化后复检（依赖 `pretask/gamesetting` 读取**本地游戏配置文件**判断全屏状态）；
- `taskersink/hdrcheck`、`taskersink/processcheck`、`taskersink/cursormove` — 其他环境预检。

对 GFN 的失效点：

| 失效点                     | 说明                                                                                                               |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| 控制器正则不命中           | `class_regex` / `window_regex` 均针对本地 Unity 窗口                                                               |
| 无窗口缩放能力             | GFN 窗口非 720p 时，aspectratio 只会强停任务，无自动修正手段                                                       |
| Alt+Enter 复检依赖本地文件 | GFN 场景本机无游戏安装，`gamesetting.GetVideoFullScreen()` 读取失败 → Alt+Enter 路径自然跳过，属预期降级，无需修改 |

## 3. 目标窗口特征

| 目标               | 进程名                             | 窗口类                                                               | 标题模式                                                                   | 缩放行为                                                   |
| ------------------ | ---------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------------- | ---------------------------------------------------------- |
| 本地客户端（现状） | 游戏本体进程                       | `UnityWndClass`                                                      | `Endfield`                                                                 | 游戏内设置分辨率                                           |
| GFN 原生客户端     | `GeForceNOW.exe`（本地无游戏进程） | `CEFCLIENT`（MaaNTE 实测；GFN 客户端与游戏无关，同客户端结论可复用） | `Arknights: Endfield on GeForce NOW`（**已实测确认**，正则取语言无关片段） | 接受标准 `SetWindowPos` 外部缩放；流窗口无边框、无页面头部 |

## 4. 功能需求

### FR1 — 新增 GFN-App 控制器条目（`assets/interface.json`）

```jsonc
{
    "name": "GFN-App",
    "label": "$controller.GFN-App.label",
    "description": "$controller.GFN-App.description",
    "type": "Win32",
    "win32": {
        "class_regex": "CEFCLIENT",
        "window_regex": "Endfield.*on GeForce NOW",
        "screencap": "PrintWindow",
        "mouse": "Seize",
        "keyboard": "Seize",
    },
}
```

- 前台模式（`PrintWindow` + `Seize`）：`SendMessage`/`PostMessage` 后台注入对 CEF 的 GPU 合成窗口普遍不可靠（MaaNTE 结论）；若实测 `PrintWindow` 出现黑屏帧，回退 `ScreenDC`。
- 不加 `permission_required`：GFN 客户端为普通用户权限进程，无需提权注入。
- 键盘输入必须能透传到 GFN 串流（客户端把按键转发到云端主机），WASD 等长按键位为验证重点。
- 配套新增 i18n 键 `controller.GFN-App.label` / `controller.GFN-App.description`，同步 5 个 locale 文件（`assets/locales/interface/{zh_cn,zh_tw,en_us,ja_jp,ko_kr}.json`）。

### FR2 — Go agent GFN 模式检测与窗口自动缩放（`agent/go-service/taskersink/gfnwindow`）

新增 taskersink 包 `gfnwindow`，在任务启动事件上执行：

- **检测**：`pienv.ControllerName() == "GFN-App"` 即 GFN 模式，无需进程/窗口枚举 —— 控制器连接时 MaaFramework 已完成选窗，HWND 从 controller info（`hwnd` 字段）直接获取。检测与缩放结果输出 INFO 级结构化日志（hwnd、窗口类、before/after 尺寸）。
- **缩放**：客户区非 1280x720（±2px 容差）时，按实测窗口/客户区矩形差值计算边框尺寸（无边框窗口差值为 0，等价于 `AdjustWindowRectEx` 且无需读取样式位），经 `SetWindowPos` 把客户区调整为 1280x720 并贴到工作区（`rcWork`，排除任务栏）右下角。**不强制 `WS_CAPTION`**，保持 GFN 流窗口无边框形态（MaaNTE 实测 GFN 窗口接受标准缩放）。
- **贴靠右下角**：即使客户区已是 1280x720（本次未触发缩放），也会单独执行一次仅移动（`SWP_NOSIZE`）的贴靠，确保窗口停留在右下角，与 MaaNTE `97cc62e` 的 `bottom_right` 锚点行为一致；贴靠失败仅记录 DEBUG 日志，不影响任务继续执行。
- **降级**：缩放失败时任务**不中断**，输出 WARNING 日志与用户可见提示（maafocus），引导用户在 GFN 客户端设置中将串流分辨率固定为 1280x720。
- **串流分辨率锁定提示**：缩放成功后仍提示 —— 若游戏会话在调整前已开始串流，云端仍按原分辨率渲染，识别可能失败；需在 GFN 设置固定 720p 串流后重启会话，或保持窗口 1280x720 时再启动游戏。
- **注册顺序**：在 `registerAll()` 中先于 `aspectratio.Register()` 注册，保证缩放先于分辨率强校验执行。
- **缓存刷新**：缩放成功后必须触发一次 `PostScreencap` 刷新控制器缓存分辨率，否则后续 `aspectratio` 读到缩放前的旧值会误停任务（实测踩坑）。
- **平台守卫**：Win32 逻辑走 `//go:build windows` 构建标签，非 Windows 平台空实现（参照 `taskersink/hdrcheck` 的 `hdr_windows.go` / `hdr_other.go` 结构）。
- **公共化**：controller info → HWND 解析逻辑提炼为 `pkg/control` 公共函数，`taskersink/aspectratio` 改为复用，消除与 `altenter_windows.go` 的重复。

### FR3 — 任务控制器解锁（`assets/tasks/**/*.json`）

GFN-App 与 `Win32-Front` 模式一致（前台 + Seize），原则上所有支持 `Win32-Front` 的任务均可运行。在所有 `controller` 数组含 `"Win32-Front"` 的任务里追加 `"GFN-App"`，**排除以下依赖本地游戏安装/进程的任务**（GFN 场景游戏运行在云端，原理上不可用）：

| 任务                                    | 排除原因                          |
| --------------------------------------- | --------------------------------- |
| `tasks/pretasks/GameSetting.json`       | pretask 读取/修改本地游戏配置文件 |
| `tasks/AccountSwitch.json`              | 依赖本地客户端的账号切换流程      |
| `tasks/CloseGame.json` 等 ADB-only 任务 | 本就不含 `Win32-Front`，不受影响  |

新增任务时按本表原则声明控制器限制。

### FR4 — 与现有预检链的交互

| 预检                        | GFN 场景行为                                                                                                                                      |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `gfnwindow`（本期新增）     | 检测 GFN 模式 → 自动缩放 → 提示；非 GFN 控制器下零开销直接返回                                                                                    |
| `aspectratio`               | 在 `gfnwindow` 之后执行；缩放成功则直接通过；缩放失败则按现状警告并强停（兜底）。Alt+Enter 路径因读不到本地配置文件自然跳过，属预期降级，无需修改 |
| `hdrcheck` / `processcheck` | 行为不变（检查的是本机环境，与 GFN 无冲突）                                                                                                       |

## 5. 非功能需求

- **零新增 Go 依赖**：Win32 调用基于现有 `golang.org/x/sys/windows` 封装扩展。
- **向后兼容**：本地客户端 / ADB / PlayCover / Wlroots 用户的行为与性能不受任何影响。
- **日志规范**：遵循 zerolog 结构化日志约定 —— DEBUG 记探测细节、WARNING 记降级路径；用户可见提示走 maafocus + i18n（5 语言同步），禁止裸文本。
- **格式规范**：JSON 改动通过 `pnpm format` / `pnpm check`；Go 改动通过 `pnpm format:go` 与 `go vet`。

## 6. 风险与开放问题

| #   | 风险 / 开放问题                                                                                                                                                                                                                       | 影响                                     | 应对                                                                                                                                                    |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| R1  | ~~终末地在 GFN 的实际窗口标题未确认~~ **已关闭**：实测确认标题为 `Arknights: Endfield on GeForce NOW`，正则 `Endfield.*on GeForce NOW` 命中                                                                                           | —                                        | 保持匹配语言无关片段（游戏英文名 + `GeForce NOW`）；如遇多语言客户端标题差异再收集样本                                                                  |
| R2  | CEF GPU 合成窗口对后台注入不可靠，GFN 用户只能前台运行                                                                                                                                                                                | 无法使用后台任务                         | 控制器即声明为前台模式；locale 描述与文档中声明该限制                                                                                                   |
| R3  | ~~压缩伪影可能拉低识别匹配分~~ **已实测定性**：720p 串流下 TemplateMatch 正常（实测 0.95+），但 1~2 单位宽的精确 ColorMatch 全部失配（品牌黄 hue 本地 28-29 → 串流实测 26-31，连通像素 47/3000），触发 SceneManager"颜色识别失败"误报 | ColorMatch 校准门禁在 GFN 下必然中止任务 | 已新增 `resource_gfn` 覆盖资源（随 GFN-App 控制器 `attach_resource_path` 加载），放宽按钮底色与加载校准色块的颜色容差；后续发现同类节点按此模式补充覆盖 |
| R4  | 串流渲染分辨率在会话建立时锁定，本地缩放窗口不改变云端渲染                                                                                                                                                                            | 会话中途缩放后识别仍失败                 | FR2 的串流分辨率锁定提示；引导用户在 GFN 设置固定 720p 串流并重启会话                                                                                   |
| R5  | `CEFCLIENT` 类名可能随 GFN 客户端版本变化（历史上出现过 `CEF-OSC-WIDGET`）                                                                                                                                                            | 客户端更新后控制器失效                   | 探测日志保留窗口类字段，便于未来核对；PRD 记录取证方法                                                                                                  |
| R6  | GFN 自身的排队、闲置踢出、会话到期画面                                                                                                                                                                                                | 任务流程外状态，Pipeline 无法恢复        | 非目标（§1.3）；提示用户保持会话活跃，长任务失败时日志可定位                                                                                            |
| R7  | `PrintWindow` 对 GFN 流窗口截图可用性未在终末地场景复测                                                                                                                                                                               | 黑屏帧则识别全部失败                     | MaaNTE 同客户端实测可用；若失效回退 `ScreenDC`（要求窗口前台不遮挡，与 Seize 前提一致）                                                                 |

## 7. 验收标准

- [ ] `GFN-App` 控制器在 MXU 控制器列表中可见、可选、可连接。（待 Windows 实测）
- [ ] 控制器连接成功，截图分辨率为 1280x720，鼠标/键盘输入在串流中生效（含 WASD 长按）。（待 Windows 实测）
- [ ] GFN 窗口客户区非 720p 时，任务启动自动缩放到 1280x720；缩放失败时任务不中断且输出引导提示。（待 Windows 实测）
- [ ] 非 GFN 控制器（Win32-Front / ADB / PlayCover / Wlroots）行为回归一致，无新增日志噪音。
- [ ] 代表性任务（如 `DailyRewards`、`PuzzleSolver`）在 GFN 下端到端跑通。（待 Windows 实测）
- [ ] 5 个 interface locale 与 5 个 go-service locale 的新增键完整同步，`pnpm format:check` / `pnpm check` / `pnpm test` 通过。
- [ ] `agent/go-service` 在 `GOOS=windows` 与本机双目标下 `go build ./...` / `go vet ./...` 通过。

## 8. 后续工作

1. Windows + GFN 实机回填 R5/R7 实测数据（窗口类复核、`PrintWindow` 截图可用性），必要时修正 `class_regex` / `screencap`（R1 窗口标题已确认）；
2. 视用户反馈评估 GFN Chrome 网页版（F11 全屏前提）与其他 Chromium 系浏览器支持；
3. 视实测识别命中率评估 GFN 专用模板或阈值调整。
