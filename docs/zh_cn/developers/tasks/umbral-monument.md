# 影拓丰碑

## 范围

- UI：其他菜单 → 影拓丰碑，默认「仅普通」，可选「普通＋苦难」。
- 复用行动手册导航，从常驻页进入；已在活动内可直接返回赛季页。
- 横向遍历赛季，纵向遍历关卡；跳过已完成难度和本次已尝试难度。
- 使用当前队伍和现有 AutoFight 设置，不自动换队。
- 普通失败时也跳过对应苦难；未知图标或选关标题不符时停止，不猜测点击。
- 当前仅适配 PC、简体中文、16:9。界面文案翻译不代表外服 OCR 已适配。

## 实现分工

`assets/resource/pipeline/UmbralMonument/UmbralMonument.json` 管理导航、滚动、点击、难度切换、编队确认、战斗与结算。
特别注意：选关页和编队页都有「挑战影拓丰碑」，编队页通过「快捷编队」区分，需要再点击一次。

`agent/go-service/umbralmonument` 只负责动态名称、完成图标和本轮去重状态。
状态保存在当前任务的 `UmbralMonumentMain.attach` 中，不跨账号或任务共享。
列表到头复用 `ListCompleteRecognition`，每次滑动后等待列表区域静止。

关卡列表图标按提供的截图判断：白/灰色是普通未通关，红色是普通已通关但苦难未完成，黄色是苦难已通关。
不要把赛季卡片下方的汇总徽记配色与关卡列表图标混用。
图标取样列为 720p 的 x=46..67；UI 布局改变时需重新提供截图校准。

## 验证

```powershell
pnpm format
pnpm format:go
pnpm check
pnpm test
cd agent/go-service
go test ./umbralmonument
```

可选的真实截图识别和模拟点击测试：将原截图放在安装目录 `temp/`，文件名为
`1.png`、`2.png`、`3-普通.png`、`3-苦难.png`、`3-通关.png`、`4.png`、`5.png`、`6.png`。
设置 `MAAEND_SCREENSHOT_ROOT` 为含 `maafw/`、`resource/`、`temp/` 的安装目录，然后运行：

```powershell
go test ./umbralmonument -run TestScreenshots -v
```

这些测试只使用截图控制器，不连接游戏，不会向真实窗口发送点击。
原始截图含玩家 UID，未纳入源码；正式节点截图应按项目惯例提交到 MaaEndTestset。

## 尚待实机验证

- 赛季横向滚动、关卡纵向滚动、切换苦难后标题变化、完整连续战斗。
- 已使用真实成功、失败截图验证结算识别，并在截图控制器上验证「编队确认 → 结算 → 离开」。
  失败页复用协议空间的「行动失败」识别，仅点击「离开」，不点击「重新挑战」。
  离线回归不代表已经完成真实游戏连续作战验证。
- 锁定赛季、网络弹窗及不同 UI 缩放未验证；不会对未知界面进行盲目点击。
