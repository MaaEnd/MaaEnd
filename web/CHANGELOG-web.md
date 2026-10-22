# 网页端适配 · 变更留档

规则：**每次改动都必须同时做三件事**——① git 提交（分支 `feat/web-linux-layer`）② 在本文件追加一条 ③ 把改动文件复制一份到
`D:\桌面\常用\旧机改造服务器\MaaEnd适配\archive_wsl_YYYYMMDD\`。

---

## 2026-10-23 · WSL 调试环境从 0 到「只差标题页」

### 结论先行

- 环境（sway + Chrome + MaaFW + agent）**已打通**：点「开始游戏」能到游戏标题页，`__ScenePrivateLogin` 命中。
- 卡点：`WebQueueDetected` 在标题页误命中（已修），修完尚未复跑到通过。
- 设计定案：**照安卓云（`resource_cloud_adb`）的做法**——云层只保留云 UI 专属节点，进游戏的登录/继续/月卡/维护一律复用 base 的 `__ScenePrivateLogin*` 链。

### 提交记录

- `be6b2d781` WSL 调试环境入口（start_web.sh + README）
- `5b2048c10` 补回 MAA_SAVE_DRAW 识别可视化开关（与 homesrv 部署版对齐）
- `fe81fda4e` WSL 上 MXU 能跑起来的三处修正（资源条目 / socket / Chrome 语言）
- `b7206d9c2` 关掉 profile 里的翻译气泡（压住右上角 ROI）
- `3d7ac254e` 中和旧版 resource_linux 导致的「资源加载失败」
- `64e4af4e7` 「开始游戏」接成正式任务，供 MXU 预设使用
- `63b30800c` 修 MXU 白屏两处真因 + 开始游戏任务入库
- `41d6d97fc` 语言包源在仓库 assets/locales/interface
- `4084f0641` auto_update 同步 locales 与平台层
- `391320804` 「开始游戏」pipeline 方案草案 v0.1
- `7f5de1cd3` / `f78e1151c` 照抄 PC 链路（已回退，原因见下）
- `1d3ea5851` 开始游戏改为照安卓云的做法（云层薄化）
- `f3b3f6ca2` 自动接受 Chrome 原生对话框（Leave site?）
- `5ac907956` apply_wsl_setup 顺带修 agent/maafw 符号链接
- `095e02eab` WSL 日常入口每次运行前自愈装配
- `cdc78e78a` 安卓云的云 UI 节点接进启动链
- `67bc1450f` dialog 自动接受改用脚本自身路径 + nohup+setsid
- `792bae27f` WebQueueDetected 收紧判断

### 踩过的坑（都已修，别再踩）

| 坑 | 症状 | 修法 |
|---|---|---|
| MXU 自更新覆盖 base | `resource/` 被换、`interface.json` 重写、`agent/maafw` 链接被冲 | `apply_wsl_setup.py` 幂等重放，且每次运行前自动跑 |
| 新增 resource 条目 | MXU 白屏 `Object.entries … not null` | 不新增条目，改为扩充「官服」的 path |
| 手插预设任务缺 `optionValues` | 同上白屏 | 必须带 `"optionValues": {}` |
| 旧 `resource_linux` 悬空 next | 「资源加载失败」，连坐所有层 | 删悬空引用，或随包更新平台层 |
| `locales/` 落后 | 任务名显示 `task.Xxx.label` | 用仓库 `assets/locales/interface/` 覆盖 |
| `$WEB` 未定义（`set -u`） | 浏览器起不来，`line 76: WEB: unbound variable` | 用 `${BASH_SOURCE[0]}` 定位 |
| 裸 `setsid` 起子进程 | WSL 会话结束就被收，`dialog=0` | `nohup setsid` |
| `agent/maafw` 链接丢失 | go-service 报 `cannot open shared object file`，`client.connect()` 永久阻塞 | 重建链接（脚本已带） |
| `Leave site?` 原生弹窗 | 遮住标题页，点击落不到「继续」 | `auto_dismiss_dialog.py` 常驻接受 |
| `WebQueueDetected` ROI 过大 + 泛词 | 标题页被误判成排队，空等 | 收紧 ROI + 只留中文强词 + 从 Entry 移除 |

### 待办

1. 复跑验证 `WebStartGame`：`__ScenePrivateLoginContinue` → `WebAlreadyInGame`
2. 把「打开网页端」做成任务体系的一部分（pretask `tasks/pretasks/WebLaunchBrowser.json`，用 `StartApp`），替代预设里的 preAction
3. 验证通过后接 `VisitFriends` 跑一项真实日常
---

## 2026-10-23 03:10 · 快照（MXU 直跑，rc.1）

状态：**网页端已能自己拉起（pretask 生效）**，MXU 侧 `merged import "web/tasks/pretasks/WebLaunchBrowser.json"` 确认。

这一轮跑出来的失败点（`debug_exports/MaaEnd-logs-v2.35.0-rc.1-20261023-031026`）：

| 任务 | 失败节点 | 次数 |
|---|---|---|
| 环境监测 | `GoToIndoorCropsMove` | 9 |
| 环境监测 | `IndoorCropsQuickTeleportDone` | 3 |
| 出售 | `AutoSellFriendsPricesExpected` | on_error 截图 |
| 出售 | `AutoSellExecuteValleyIVMain` | on_error 截图 |

含义：环境监测卡在「室内作物」这条子路线的**快速传送**（`IndoorCropsQuickTeleportDone` 没命中 → 后续 `GoToIndoorCropsMove` 反复走位失败）；
出售卡在**好友价格面板**（`AutoSellFriendsPricesExpected`）与四号谷地执行节点。

待办：拉这两个节点在失败时刻的识别分数与截图，区分「没找到 UI」与「找到但点不动」；室内作物路线属可选路线，可先在预设里关掉以恢复其余项。

### 当前装配基线（归档时）

- base：v2.35.0-rc.1（MXU 自更新）
- 资源层：`./resource` + `./web/resource_web` + `./web/linux/resource_web_linux` + `./web/resource_web_homesrv`（5 路径全 loaded=True）
- import：`web/tasks/WebStartGame.json`、`web/tasks/pretasks/WebLaunchBrowser.json`
- 7 个实例 socket 全部 `wayland-1`；`快速日常` 9 项全开
- 每次运行前 `apply_wsl_setup.py` 自愈（防 MXU 覆盖）
---

## 2026-10-23 03:2x · 会被覆盖的资产盘点 + 三层策略

### 规模（WSL 现网 /root/maaend）

| 位置 | 文件数 | 大小 | 覆盖方式 |
|---|---:|---:|---|
| `resource/pipeline/nodes.json` | 1 | 3.7 MB | 整文件替换（5997 节点） |
| `resource/image/` | 2284 | 34 MB | 整目录 |
| `resource/model/` | 15 | 224 MB | 整目录（ONNX） |
| `resource_linux/` | 1 | 12 KB | 整目录 |
| `resource_adb/` | 409 | 3.5 MB | 整目录 |
| `resource_cloud_adb/` `resource_macos/` `resource_playcover/` | 各 1 | 20/24/12 KB | 整目录 |
| `locales/` | 46 | 1.6 MB | 整目录 |
| `maafw/` | 30 | 67 MB | 整目录 |
| `agent/go-service` `agent/cpp-algo` | 2 | 66 MB | 整文件（`agent/maafw` 链接会被冲掉） |
| `interface.json` | 1 | 8 KB | 整文件重写 |
| `config/mxu-MaaEnd.json` | 1 | 92 KB | MXU 每次退出全量重写 |

base 侧合计 ≈ 2790 文件 / ≈400 MB；我们自己的 `web/` 只有 45 文件 / 576 KB（全在 git 里，无人覆盖）。

### 策略（三层）

1. **base 资产**：快照锁定，不试图阻止更新。`/root/maaend-pinned`（399 MB）+ `pin_base.sh` 版本比对，不一致整体还原。
2. **interface.json**：每次运行前用 `apply_wsl_setup.py` 重放（官服 4 层 path + 两条 web import）。
3. **config/mxu-MaaEnd.json**：只重放运行必需项，不当作持久化手段；真正的持久化在 `web/tasks/*.json`、`web/resource_web/pipeline/nodes.json`、`apply_wsl_setup.py`、`pin_base.sh`。

### 未做（待定）

- MXU 手动点运行不经过 `daily_run_wsl.sh`，因此不触发锁版/重放；若长期用 MXU，需加 5 分钟一次的 cron 跑 `pin_base.sh`。
