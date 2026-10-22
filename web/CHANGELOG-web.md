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
