# 覆盖层节点说明（desc 长文）

> 节点里的 `desc` 只保留一句话，完整说明放在这里。

## `CheckMenuListColor`

【网页端覆盖 · 云串流适配】检查菜单列表界面左下角颜色。上游原判据 lower=[90,35,35] upper=[191,83,85] 在云·终末地上只命中 23/17 像素（需 ≥100）⇒ InMenuList 不过 ⇒ 所有要走「进入菜单」的任务 20 秒超时失败（实测 2026-10-17：VisitFriends / DijiangRewards / DailyRewards 全卡这里）。用 MaaFramework 自己的 ColorMatch 逐维扫描（不要用自写 HSV 模拟，两者数量级会差很多：框架报的是最大连通块像素数）：只改 V 上下界几乎无效（23→24），真正的杠杆是**饱和度上界**——云端画面被做了对比度/饱和度拉伸，该区域饱和度超过上游 83 的上限。故仅把 upper 的 S 由 83 放宽到 130，其余阈值与 count 全部保持上游值。实测：正样本 168/152（过），负对照「大世界内」75、「云启动页」2（均在阈值下，区分力保留）。

## `__ScenePrivateWorldEnterMenuList`

【网页端覆盖 · 云串流适配】从大世界进入菜单总列表。上游 next 是 [__SceneImageCheck, __ScenePrivateAnyEnterMenuListSuccess]：前者会点左下角，期待弹出「登录界面/确认」登出对话框，再借那张对话框做 OSD/Mod/画面颜色/语言/抓 UID 五项 PC 渲染自检。云端实测（2026-10-17 15:31~15:33 日志）：点击后画面回到大世界、登出对话框从未出现 ⇒ __SceneImageCheckDialogIn 在 20 秒内空转 5640 次全部落空 ⇒ 任务失败。这五项自检对浏览器/云串流无意义：OSD 遮挡与 Mod 检测针对 PC 客户端；CaptureUid 在 Win32 控制器下读游戏注册表，而浏览器无该注册表项。故这里只保留「Alt+点开菜单」与「确认已在菜单列表」两步，去掉整条自检链。ImageCheckNotPassedRecognition 仅被 __SceneImageCheck 使用（已核），跳过不影响其他节点。

## `WebStartGameEntry`

【网页端 · 启动器】点一次运行就完成「开始游戏 → 排队/加载 → 进入游戏」。已经在游戏里（大世界或菜单）则直接跳过；已在排队则只等待。浏览器窗口需要先用 2-启动网页端.bat 打开并连上。timeout 给 10 分钟：游戏加载阶段是黑屏（三个判据都不命中），预算太短会误判失败。

## `WebLauncherStartButton`

【网页端 · 启动器】云游戏启动页那个黄色「开始游戏」按钮。模板取实拍帧，实测两帧得分 1.0 / 0.99999（背景有飘落粒子动画也不受影响，优于 ColorMatch）。点击后进入排队/加载，最多等 10 分钟。

## `WebTitleContinue`

【网页端 · 启动器】标题页「点击任意位置继续」。模式照上游 __ScenePrivateLoginContinue：OCR 固定 ROI → 点击 → next 自引用来循环（max_hit 10 防止无限连点）；点完直接等「进游戏」或下一次标题页，不依赖 inverse 字段（该字段只在 next 列表求值生效，离线验证不到）。云端实测命中 0.999974。

## `WebSessionBusy`

【网页端 · 启动器】「您上次游戏的数据正在火速处理中，请稍后再试」弹窗。直接 OCR 弹窗按钮「知道了」并点它（点识别框本身），比认整段提示稳。点完等 20 秒再回入口重试。

## `__ScenePrivateEndVisit`

【网页端覆盖】结束拜访。上游 OCR ROI 是右下角 260x100（[-260,-100,260,100]），但云网页端的「结束拜访」提示在画面中部偏右（实测 x≈825-895, y≈385-410，1280x720 空间），上游 ROI 完全覆盖不到 ⇒ OCR 永远 miss，拜访流程收不了尾。这里放宽到 [600,300,680,400]。

## `WebCloseRewards`

【网页端覆盖】「获得奖励」结算弹窗（帝江号/每日奖励领取后会停在它上面，此时 InWorld/InMenuList 都不命中，后续任务全部空转）。上游 CloseRewardsButton 在网页帧实测 0.9785、标题 OCR 0.9998，直接复用。

## `WebMonthlyCard`

【网页端覆盖】每日首次进游戏会弹「今日月卡奖励 / 点击确认今日月卡奖励」。此时 InWorld/InMenuList 都不命中，若不点掉，整批任务会全部超时。复用上游 OCR 期望（实测 0.9999），用普通 Click 点识别框（等于点面板确认），点完回入口重判，max_hit 防死循环。

## `__ScenePrivateAnyExit`

【网页端覆盖】退出当前场景：保留 base 的 ESC 行为（网页有键盘，ESC 在「总控中枢」这类面板里最可靠），但把 max_hit 从 100 降到 6 —— 100 次 × post_delay 3000 会在认不出的 WebView 页面上空转 300 秒（2026-10-20 实测 DailyRewards 重试 399 秒）。点击式 AnyExit（识别 X 再点）实测会把「总控中枢」留在原地、后续任务全部 21 秒超时，故不采用。

## `WebAnyExitEsc`

【网页端 · 兜底】按一次 ESC。max_hit 3：防止在没有关闭按钮的 WebView 页面上无限连按（上游 max_hit 100 × post_delay 3000 = 300 秒空转，就是被它拖的）。

## `WebLaunchBrowserStart`

【网页端 · 启动器】拉起 sway + Chrome（调 start_web.sh；已在跑会秒退）。参数名在 Linux 控制器上待实测：command / path

