# WSL（Ubuntu-24.04）上的网页端调试环境

用于在本机 Windows 上验证网页端适配，不改 homesrv 生产环境。全部装在 `~/maaend`（root）。

## 与 homesrv 的差异

| 项 | homesrv | 本机 WSL |
|---|---|---|
| 用户 | `yuhuan`（用户级，无 sudo） | `root` |
| MaaEnd 来源 | 官方 Linux 包 | 从 homesrv 打包搬运（`resource`/`interface.json`/`tasks`/`agent`/`maafw`/`MaaEnd`/`locales`） |
| 浏览器 profile | `~/.config/endfield-web` | `/root/chrome-profile`（2026-10-17 的登录态，复用至今有效） |
| 运行时目录 | `/run/user/1000` | `/run/user/0`（root 无 logind 也可用） |
| 显示 | sway headless | 同样 sway headless；MXU 窗口经 WSLg 显示到 Windows 桌面 |

## 目录

```
/root/maaend/
├── resource/            # 官方 base（v2.35.0-beta.2，5939 节点）
├── interface.json
├── tasks/               # interface.json 引用的任务定义（打包时漏过一次，务必带上）
├── agent/go-service, cpp-algo
├── maafw/               # libMaaFramework.so 等
├── MaaEnd               # Tauri GUI 二进制（627 MB），需要 libwebkit2gtk-4.1-0
├── locales/
├── config/mxu-MaaEnd.json
└── web/
    ├── resource_web/                  # 网页端覆盖层（Web* 节点）
    ├── linux/resource_web_linux/      # Linux 模板层
    ├── resource_web_homesrv/          # homesrv 模板层
    └── linux/wsl/{daily_run_wsl.sh,start_web.sh}
```

## 常用命令（Windows 侧 PowerShell 直接跑）

```powershell
# 打开 MXU（窗口经 WSLg 显示）
wsl -d Ubuntu-24.04 -u root -- bash -lc "cd /root/maaend && DISPLAY=:0 WAYLAND_DISPLAY=wayland-0 WEBKIT_DISABLE_COMPOSITING_MODE=1 ./MaaEnd"

# 关闭 MXU（GUI 关闭按钮在 WSLg 下可能无效；-9 避免它把内存里的旧配置写回）
wsl -d Ubuntu-24.04 -u root -- bash -c "pkill -9 -x MaaEnd"

# 只起网页端（sway + Chrome，不跑任务）
wsl -d Ubuntu-24.04 -u root -- bash -lc "MAAEND_PROFILE=/root/chrome-profile /root/maaend/web/linux/wsl/start_web.sh"

# 跑任务（留空=全部；5=AutoStockpile）
wsl -d Ubuntu-24.04 -u root -- bash -lc "MAAEND_TASKS=5 /root/maaend/web/linux/wsl/daily_run_wsl.sh"

# 收尾
wsl -d Ubuntu-24.04 -u root -- bash -c "pkill -f 'user-data-dir=/root/chrome-profile'; pkill -x sway"
```

## 坑（实测）

0. **MXU 要能跑，必须先补两处配置**（`fix_mxu_config.py` 已处理，重装后要重做）：
   - `interface.json` 的 `resource` 里加 `官服-网页端`，路径四层：
     `./resource`、`./web/resource_web`、`./web/linux/resource_web_linux`、`./web/resource_web_homesrv`。
     官方 `interface.json` 只有 `官服`（仅 `./resource`），MXU 里根本加载不到 `Web*` 节点。
   - 每个实例的 `savedDevice.wlrSocketPath` 改成 `wayland-1`，`resourceName` 改成 `官服-网页端`。
1. **MXU 的 Linux 控制器默认连 `wayland-0`（WSLg 的 Weston），必然失败**
   `Compositor doesn't support wlr_screencopy_unstable_v1 protocol` → `Failed to create WaylandClient` → 「自动连接失败」。
   MXU 日志原文：*「尚未手动选择过设备，已自动匹配到「wayland-0」。如需更换，请在连接设置中手动选择，下次将记住您的选择。」*
   → 在 MXU 的**连接设置**里手动选 `wayland-1`（sway 的 socket），之后会记住。
2. **`leave_game.py` 依赖 `websocket`**：`apt install python3-websocket`（1.7.0），否则收尾时优雅退出报 `ModuleNotFoundError`。
3. **GUI 需要 `libwebkit2gtk-4.1-0`**（+ `libayatana-appindicator3-1`）。
4. **preAction 「启动网页端」**：`program=/bin/bash`，`args=/root/maaend/web/linux/wsl/start_web.sh`，`waitForExit=true`。非网页端预设里默认关闭（PC 上执行 `/bin/bash` 会报错）。
5. **MaaEnd 的配置在退出时才落盘**：改 `config/mxu-MaaEnd.json` 前先 `pkill -9 -x MaaEnd`，否则内存里的旧配置会把改动覆盖掉。
6. **真·无头不可用**（homesrv 已证）：`--headless=new` 下游戏内视频流模板分从 0.9407 掉到 0.5528。sway headless 是唯一可行形态。
