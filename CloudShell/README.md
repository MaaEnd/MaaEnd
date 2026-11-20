# CloudShell

《云·终末地》网页版的 Windows PC 版（bushi）

## 与 MaaFramework 的约定

| 项目 | 取值 |
| --- | --- |
| 窗口类名 | `MaaEnd.CloudShell` |
| 窗口标题 | `MaaEnd CloudShell`，固定不变（不跟随页面标题，MXU 会按标题记住已连接的程序） |
| 客户区 | 1280×720 物理像素，网页视口与之一一对应（缩放比例固定为 1） |
| 截图方式 | `PrintWindow` |
| 鼠标 / 键盘 | `PostMessage` |

## 在 MaaEnd 里的启动流程

`LaunchCloudWeb` 预任务只负责把外壳窗口准备好，进入游戏交给连接后的任务：

1. 预任务启动外壳（已经在运行就直接成功），等外壳窗口出现后返回；
2. MXU 按窗口类名与标题连接 `CloudWeb` 控制器；
3. 「启动游戏（网页）」任务（`CloudWebOpenGame`，入口 `OpenGame`）点官网页的「开始游戏」，等串流准备完成（启动中、排队、重试都能等），直到出现游戏画面，后续任务从游戏画面接管。

页面状态判断都在 Pipeline 的 `OpenGame`（`assets/resource/pipeline/OpenGame.json`），和 ADB 端「打开游戏」共用一套。登录状态由外壳保存在 `cache/cloud-shell`，第一次使用需先在外壳窗口里手动登录。

想收工时用「关闭游戏（网页）」任务：它结束外壳进程，云端串流随之断开（会话由云端回收），登录状态留在 `cache/cloud-shell` 里不受影响，下次启动游戏会重新打开外壳。

## 构建

需要 Visual Studio（MSVC）与 CMake 3.24+，在 VS 开发者命令行中执行：

```bash
cmake --preset "NinjaMulti Win32"
cmake --build --preset Release
```

WebView2 SDK 按固定版本与 SHA256 自动下载到构建目录，下载会读取 `HTTPS_PROXY` 等代理环境变量；离线时可以用 `-DWEBVIEW2_SDK_DIR=<已解压的 NuGet 包>` 指定。产物位于 `build/bin/RelWithDebInfo/maaend-cloud.exe`。

不想本地编译时，可以在 Windows 上运行 `uv run setup-workspace`，让脚本从 CI artifact 自动下载预编译好的 `install/cloud-shell/maaend-cloud.exe`；也可以用 CI 页面上的 `cloud-shell-win-<arch>` artifact 手动下载。

## 运行

```bash
maaend-cloud [--url <url>] [--width 1280] [--height 720] [--title <title>]
            [--user-data-dir <dir>] [--inject <script.js>]... [--devtools] [--debug-port <port>]
```

- 日志写到 `<工作目录>/debug/cloud-shell.log`。
- 登录数据默认放在 `<工作目录>/cache/cloud-shell/WebView2`。
- `--inject` 追加注入调试脚本，`--devtools` 打开 DevTools，`--debug-port` 打开只监听本机的远程调试端口。
