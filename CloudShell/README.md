# CloudShell

《云·终末地》网页版的 Windows PC 版（bushi）

## 与 MaaFramework 的约定

| 项目 | 取值 |
| --- | --- |
| 窗口类名 | `MaaEnd.CloudShell` |
| 客户区 | 1280×720 物理像素，网页视口与之一一对应（缩放比例固定为 1） |
| 截图方式 | `PrintWindow` |
| 鼠标 / 键盘 | `PostMessage` |

## 构建

需要 Visual Studio（MSVC）与 CMake 3.24+，在 VS 开发者命令行中执行：

```bash
cmake --preset "NinjaMulti Win32"
cmake --build --preset Release
```

WebView2 SDK 由 `uv run -m tools.setup.dep_3rdparty --webview2` 准备固定版本到 `agent/cpp-algo/3rdparty/webview2`；CMake 也可通过 `WEBVIEW2_SDK_DIR` 或同名环境变量指定 SDK 路径。`build_and_install.py --cloud-shell` 会自动准备该依赖，无需重复运行准备命令。产物位于 `build/bin/RelWithDebInfo/cloud-shell.exe`。

不想本地编译时，可以在 Windows 上运行 `uv run setup-workspace`，让脚本从 CI artifact 自动下载预编译好的 `install/cloud-shell/cloud-shell.exe`；也可以用 CI 页面上的 `cloud-shell-win-<arch>` artifact 手动下载。

## 运行

```bash
cloud-shell [--url <url>] [--width 1280] [--height 720] [--title <title>]
            [--user-data-dir <dir>] [--inject <script.js>]... [--devtools] [--debug-port <port>]
```

- 日志写到 `<工作目录>/debug/cloud-shell.log`。
- 登录数据默认放在 `<工作目录>/cache/cloud-shell/WebView2`。
- `--inject` 追加注入调试脚本，`--devtools` 打开 DevTools，`--debug-port` 打开只监听本机的远程调试端口。
