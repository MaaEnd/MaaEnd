# 云终末地 PC 网页支持

Windows 网页适配使用未修改的 MXU 和 MaaFramework，改动全部位于 MaaEnd。目前仍为兼容方案，不能视为已满足直接 CDP 控制器的评审要求。

## 使用

1. 安装系统 WebView2 Runtime，在 GUI 选择云终末地网页控制器，启用启动网页的预任务。
2. 预任务只打开或复用窗口并绑定 GUI 生命周期。日常任务通过云资源中的 `CloudWebPrepare` 检查账号、启动游戏并等待排队和视频就绪。
3. 未登录时在官网自行选择扫码、手机号、密码或验证码等方式，等待人工登录不消耗启动超时。也可添加单独的登录任务，确认登录后移除；该任务不会启动游戏。
4. 停止、完成或失败均释放按键和鼠标。云控制器专用的结束游戏任务关闭云窗口，GUI 退出也会回收窗口；下次由预任务重新打开。

默认直连，不继承系统代理。仅显式设置 `MAAEND_CLOUD_PROXY=主机:端口` 时使用代理。`MAAEND_CLOUD_SOFTWARE_RENDERING=1` 禁用 GPU 合成和硬件解码，切换前须关闭已有云窗口；CPU 开销取决于视频和硬件，不能据此宣称所有无 GPU 设备均已验证。

登录目录继续使用 `agent/cloud-webview.exe.WebView2/`，与滑索导入窗口分离。不要读取、提交或随包分发此目录。账号检测只检查公开 DOM 状态，不读取 Cookie、令牌、账号字段或登录表单值。

## 职责与接口

浏览器宿主是现有 `cpp-algo` 的独立进程模式，没有新增 `main.cpp` 或独立宿主可执行文件。`CloudHost.cpp` 只处理进程、窗口及 GUI 退出回收；初始化超时针对本次创建的进程句柄清理，不依赖尚未发布的窗口标题。

登录、开始游戏、排队和提示恢复位于 `resource_cloud_web/pipeline/SceneManager/SceneCloudWeb.json`。`CloudPageState` Custom Recognition 查询公开状态和经 DOM 命中检查的按钮位置；`CloudPageReload` 只刷新仍处于已识别失败状态的同一文档。每次操作后重新识别。结束和排队超时提示分别只确认一次，未出现按钮、未关闭或重复出现时失败；上次游戏数据处理提示有有限次数的单独退避，正常输入不增加等待。

网页脚本独立位于 `cloud-webview/scripts/`，由 CMake 在构建时嵌入，测试直接读取脚本。应用仍使用现有 C++、Go 和 WebView2，不分发 Node、Python 或其他新增语言运行时。JavaScript 仅在 WebView2 内执行。

`CloudTransport` 返回独立持有、无行填充、从上到下的 BGR8 像素，输入使用固定 1280×720 网页空间；按键沿用 PC 虚拟键约定，文本单独使用 UTF-8。`ViewInput` 仅接受有符号 32 位相对增量。Windows HWND、管道、共享映射和生命周期实现留在平台适配层，其他桌面平台可替换这些实现；目前没有可运行的 Linux 网页宿主，Android 继续使用已有 `CloudADB`。

## 兼容桥与开销

原版 GUI 的 PI 没有任意 Custom Controller 入口，远程 Agent 不能替换 GUI Tasker 的控制器，因此 `Win32-CloudWeb` 仍通过透明、不激活、鼠标穿透的桥接 HWND 兼容 `PrintWindow` 与 `SendMessage`。桥接消息线程与 WebView STA 分离，输入在宿主内直接调用 CDP；外部协议保留，但不自动重放可能已生效的失败输入。

游戏取帧使用 `video → Canvas → WebView2 SharedBuffer → BGR → GDI → PrintWindow`，不依赖画质增强，也不逐帧编码 PNG。这条路径仍包含像素转换、拷贝和第二次位图读取，不是零拷贝或直接返回 BGR 的控制器。浏览器尚无解码视频时，使用页面 PNG 截图让 GUI 能先连接并执行启动 Pipeline；此路径最高每秒 2 帧，不作为游戏取帧的常规路径。

Windows 的 `PrintWindow` 可直接读取 DWM 缓存而不触发 `WM_PRINT`，现有接口不能保证每次请求都同步触发浏览器取帧。任务期间按视频最高约 30 fps 刷新；停止或完成后降为每秒 2 帧，避免预览永久停在最后一帧。这保留少量空闲开销，空闲截图可能落后约一个刷新周期；严格按需取帧和移除 GDI 中间层仍受当前 GUI 接口限制。

桥接复用两块帧缓冲，同时最多保留一个帧通知。任务 ID 和进程监视防止迟到的结束事件降低下一任务的刷新频率。停止释放输入不依赖截图刷新。

## 输入和显示

可见窗口可主动聚焦、失焦、最小化、最大化及调整尺寸，自动操作不恢复窗口或抢占系统光标。固定 CSS 视口和 DPR，显示缩放不参与输入坐标或转向系数。失焦时屏蔽物理悬停和滚轮，主动点击后允许手动操作。云窗口拒绝指针锁定、主动 `window.focus()` 和读取剪贴板权限，不影响滑索导入窗口。

绝对定位抑制多余镜头增量，按住按钮的拖动保留浏览器运动量；相对输入使用明确增量。仅在自动输入确被官网浮球遮挡时，临时屏蔽浮球的 DOM 命中，发送后恢复，手动操作不受影响。滚轮保留当前目标坐标；小键盘及左右修饰键有独立映射，不支持的键记录警告，不关闭宿主。

云网页导航使用每度 4 单位的初始水平换算，仅对云控制器生效，保留上游灵敏度观察器。测量依据上游小地图模型的镜头方位 `camRot`：120 单位的一次样本转过约 29.27°，PC 默认每度约 7.927778 单位应转约 15.14°，比值约 1.93；16 次双向样本的中位比值约 1.94。这是图像测量结果，不是 CDP 固定系数；保留模型残差，不改公共阈值或用户 DPI。

网页使用 PC 资源，仅追加 `resource_cloud_web`。公共 PC 业务流程、路线和阈值不变；黄色修正限于已有证据的云资源节点。

## 构建和验证

按[开发环境](./tools-and-debug.md)准备依赖，构建现有 Agent：

```powershell
uv run build-and-install --cpp-algo
```

C++ Agent 与随附 DLL 按整组替换，任何文件被占用或复制失败时恢复旧组；回滚失败保留备份并报告路径。

```sh
node --test agent/cpp-algo/cloud-webview/tests/*.test.mjs
uv run agent/cpp-algo/cloud-webview/tests/test_startup_pipeline.py
uv run python -m unittest tools.setup.test_setup_workspace
cmake -S agent/cpp-algo/cloud-webview/tests -B build/cloud-policy
cmake --build build/cloud-policy --config Release
ctest --test-dir build/cloud-policy -C Release -L '^cloud-policy$' --no-tests=error --output-on-failure
```

完整 Agent 工程配置 `MAAEND_BUILD_CLOUD_WEBVIEW_TESTS=ON` 后，还可运行 `ctest -C Release -L '^cloud-desktop$' --no-tests=error --output-on-failure`。该测试使用原版 Framework 和 Toolkit，需要启用 DWM 的空闲交互桌面，覆盖连续截图、空闲低频刷新、滚轮、按键、拖动、停止释放及重开。启动 Pipeline 测试在真实 Framework 中运行实际节点，浏览器状态由隔离的测试控制器提供，不能替代官网实测。

持续 125%/150% DPI、多显示器、第三方截图、低端 CPU 及当前架构的完整日常仍需分别核验。既有路线样本和旧独立 GUI 测试不能替代重构后的验证。测试集来自 `tests/MaaEndTestset` 子模块；不得提交含其他窗口、UID、账号信息的截图或运行日志。
