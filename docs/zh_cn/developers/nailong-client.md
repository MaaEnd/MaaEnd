# 奶龙定制客户端

奶龙主题由 MaaEnd 仓库维护，界面以固定提交的 MXU 为基础构建。无需创建或保留 MXU fork，也不会向 MXU 官方仓库写入代码。

## 源码与构建入口

- `tools/client/nailong/client.json`：MXU 官方源码地址、固定提交和定制版本号。
- `tools/client/nailong/theme.patch`：主题、鼠标追视、互动文案和开发版演示的源码改动。
- `tools/client/nailong/assets/`：背景、贴图、动图及图像处理提示词。
- `tools/client/nailong/client.mjs`：准备源码、测试、开发和构建入口。
- `.cache/nailong-client/`：从官方源码生成的本地工作目录，不提交。

依赖：Node.js 22 及以上、pnpm、Rust stable 和平台编译工具。Windows 需要 Visual Studio C++ 桌面开发工具与 Windows SDK；Linux 需要 WebKitGTK 4.1、AppIndicator、librsvg 和 patchelf；macOS 需要 Xcode 命令行工具。

在 MaaEnd 仓库根目录执行：

```bash
pnpm install --frozen-lockfile
pnpm client:prepare
pnpm client:test
pnpm client:build
```

`client:prepare` 从 `client.json` 指定的官方提交准备源码，检查并应用补丁、复制素材，统一写入定制版本号。重复执行会校验文件摘要；缓存被手动改动或补丁变化时会停止，要求先保存改动并重建缓存，不会自动清理用户文件。

`client:build` 将可执行文件安装为 `install/MaaEnd.exe`（Windows）或 `install/MaaEnd`（macOS/Linux），同时输出 `nailong-client.json` 版本记录；Windows 有 PDB 时一并安装。它只构建界面，运行真实任务仍需安装 MaaFramework、Go/C++ Agent 与 MaaEnd 资源。

`setup-workspace` 的客户端安装步骤也调用这个入口，不再下载 MXU 官方通用界面成品。现有 Python 环境准备与 Agent 构建方式不变。

## 开发与演示

```bash
pnpm client:dev
```

开发入口把 MaaEnd 的 `assets/interface.json` 等文件复制到隔离的调试目录，并链接资源目录；`install/agent`、`install/maafw` 存在时一并链接。开发配置保存在 `.cache/nailong-client/src-tauri/target/debug/config/`，不影响原有配置。

在设置中选择第四项“奶龙”。背景不透明度可调，桌面端眼睛在鼠标移出窗口后继续追视。点击右下角奶龙会切换四句互动文案；连接等待显示 `Thinking...`。

开发版任务按钮旁有“主题演示”开关。勾选后可测试“进入战斗模式”“退出战斗模式”和“模拟完成”，无需游戏、管理员权限或运行库。演示状态独立保存在内存中，不修改真实连接或任务状态；演示期间阻止快捷键、定时入口和中控台启动真实任务。生产构建不显示演示入口。

## 发布集成

`.github/workflows/install.yml` 从 MaaEnd 的补丁构建客户端，并在原有安装步骤复制到发布目录。支持 Windows/macOS 的 x86_64、aarch64，以及 Linux x86_64；Linux aarch64 保持原有不附带桌面客户端的行为。

可单独指定构建目标与输出目录：

```bash
node tools/client/nailong/client.mjs build --target win-x86_64 --output .cache/client-dist
```

跨架构编译需先通过 `rustup target add` 安装对应目标。发布版本取自 `client.json`，不跟随 MXU 最新发布自动漂移。升级基础提交前先检查补丁是否适用；缓存中的源码不是维护源，主题修改必须回写到补丁及素材目录。

## 提交前验证

本功能不修改游戏 Pipeline 或节点测试。提交时运行整个定制客户端测试集与构建：

```bash
pnpm client:test
pnpm client:build
pnpm exec prettier --check tools/client/nailong/client.json package.json
pnpm exec markdownlint docs/zh_cn/developers/nailong-client.md docs/en_us/developers/nailong-client.md
```

`client:test` 包含准备脚本的幂等性与缓存保护测试、全部前端单元测试、TypeScript 检查、前端生产构建和全部 Rust 测试。Windows 还需在管理员 PowerShell 7 中，用本次构建的可执行文件执行固定 MXU 源码自带的四项启动回归和六组重复启动：

```powershell
$source = '.cache/nailong-client/tests/windows'
$exe = (Resolve-Path 'install/MaaEnd.exe').Path
$evidence = Join-Path $PWD '.cache/nailong-validation'
foreach ($scenario in @('creation-failure', 'normal-close', 'hidden-window', 'minimize-to-tray')) {
    pwsh -NoProfile -File "$source/startup.ps1" -Executable $exe -Scenario $scenario -EvidenceDirectory $evidence
    if ($LASTEXITCODE -ne 0) { throw "Startup regression failed: $scenario" }
}
pwsh -NoProfile -File "$source/repeated-startup.ps1" -Executable $exe -EvidenceDirectory "$evidence/repeated" -Pairs 6
if ($LASTEXITCODE -ne 0) { throw 'Repeated startup regression failed' }
```

测试只操作自己启动的测试进程。日志和诊断文件保留在忽略的缓存目录，不能随代码推送。若要取消定制，回退本次 MaaEnd 改动即可；本地缓存和原始配置无需删除。
