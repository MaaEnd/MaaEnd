# macOS PlayCover

仅支持 Apple Silicon Mac，不支持 Intel Mac。

在下列场景中，由于 macOS 的限制，MaaEnd 将无法正常截取游戏画面，进而出现错误。

- 最小化游戏窗口
- 移动游戏窗口至其他桌面或屏幕
- 全屏化其他窗口
- 启用台前调度时切换至其他窗口

在上述场景之外，游戏窗口被其他窗口覆盖时可以正常截图。

## 安装并配置 PlayCover 与 明日方舟：终末地

1. 下载并安装 [支持 MaaTools 的 PlayCover 分支](https://github.com/hguandl/PlayCover/releases)。
2. 从 [decrypt.day](https://decrypt.day/app/id6753859465) 或你信任的来源处下载脱壳的 明日方舟：终末地 IPA 文件，将其拖入 PlayCover 主界面以安装。
3. 在 PlayCover 中修改 明日方舟：终末地 的设置。
   - 图像设置：
     - 选择 iOS 机型：`iPad Pro (13-inch) (7th gen) | M4 | 8GB`
     - 分辨率：`自定义`
       - 宽度：`1280`
       - 高度：`720`
     - 分辨率缩放：`1.0`
     - 开启 `禁用显示器息屏`
   - 绕过：
     - 开启 `启用 PlayChain (试验阶段)`
     - 开启 `MaaTools`，你可以自定义端口号
     - 开启 `启用绕过越狱检测 (试验阶段)`
     - 开启 `插入内省库`
     - 开启 `强制插入 iOS 框架`
     - 开启 `防止频繁调用 usleep`
   - 杂项：
     - 应用程序类型：`public.app-category.games`
4. 双击启动 PlayCover 中的 明日方舟：终末地，游戏标题栏应为 `明日方舟：终末地 [localhost:<端口号>]`。
5. 进入游戏内设置，修改帧率为 `60帧`。

当 明日方舟：终末地 进行客户端版本更新后，你需要重新做第 2 步，并确认第 3 步中的设置。

## 配置 MaaEnd

在右上角的连接设置中选择 PlayCover，地址填入游戏标题栏方括号中的内容。

现阶段稳定不好用，只有不涉及在大世界中移动的任务可以正常运行。
