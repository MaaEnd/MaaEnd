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

WebView2 SDK 按固定版本与 SHA256 自动下载到构建目录，下载会读取 `HTTPS_PROXY` 等代理环境变量；离线时可以用 `-DWEBVIEW2_SDK_DIR=<已解压的 NuGet 包>` 指定。产物位于 `build/bin/RelWithDebInfo/cloud-shell.exe`。

## 运行

```bash
cloud-shell [--url <url>] [--width 1280] [--height 720] [--title <title>]
            [--user-data-dir <dir>] [--inject <script.js>]... [--devtools] [--debug-port <port>]
```

- 日志写到 `<工作目录>/debug/cloud-shell.log`。
- 登录数据默认放在 `<工作目录>/cache/cloud-shell/WebView2`。
- `--inject` 追加注入调试脚本，`--devtools` 打开 DevTools，`--debug-port` 打开只监听本机的远程调试端口。
