# Linux 侧结论留档（覆盖层 · 模板 · 未解）— 2026-10-23

> 用途：本 PR 后续会收敛成「Linux 启动器 + Linux 覆盖」，这里把**不依赖任何实现**的结论单独留一份，避免随代码一起丢失。
> 基线：本分支 HEAD `11500b9a5`，对比上游 `v2` @ `247e301e1`（416 个 pipeline JSON，5,997 个节点）。

---

## 1. 覆盖层现状：0 冗余

| 指标 | 数量 |
|---|---|
| 覆盖层节点 | **31**（14 个新增 + 17 个覆盖） |
| 叶子字段总数 | 156 |
| └ 新增节点的字段 | 121 |
| └ 覆盖节点的字段 | 35 |
| └ 与 base 完全相同（冗余） | **0** |
| └ 真实改动 | 29（另有 5 条 desc 改写） |
| └ base 里没有的新键 | 1（`__ScenePrivateAnyExit.recognition = "DirectHit"`） |

对照：2026-10-22 的 `2d86221` 版本里有 41 个字段与 base 逐字相同（占覆盖字段的 61%），
本次已全部收敛——**覆盖层现在只写差异，可以按「深合并 + 数组整段替换」的语义读**。

---

## 2. 17 个覆盖节点的 29 处真实改动

| 节点 | 字段 | base | 覆盖 |
|---|---|---|---|
| `CheckMenuListColor` | `upper` | `[191, 83, 85]` | `[191, 130, 85]` |
| `__ScenePrivateWorldEnterMenuList` | `next` | `["__SceneImageCheck", "...AnyEnterMenuListSuccess"]` | `["...AnyEnterMenuListSuccess"]` |
| `__ScenePrivateEndVisit` | `recognition.param.roi` | `[-260, -100, 260, 100]` | `[600, 300, 680, 400]` |
| `__ScenePrivateAnyExit` | `post_delay` | `3000` | `600` |
| `__ScenePrivateAnyExit` | `max_hit` | `100` | `6` |
| `__ScenePrivateAnyExit` | `recognition` | <无> | `"DirectHit"` |
| `AutoStockInStapleItem` | `recognition.param.lower` | 3 组颜色 | 5 组（补 `[72,148,223]`、`[155,…]`） |
| `AutoStockInStapleItem` | `recognition.param.upper` | 3 组 | 5 组（补 `[77,255,255]`、`[160,…]`） |
| `CanAfford` | `upper` | `[0, 2, 150]` | `[180, 25, 150]` |
| `DailyEventRecognitionRedDot` | `recognition.param.lower` | `[14, 245, 245]` | `[13, 217, 222]` |
| `DailyEventRecognitionRedDot` | `recognition.param.roi` | `[171, 100, 61, 490]` | `[190, 102, 104, 530]` |
| `DailyEventRecognitionRedDot` | `recognition.param.upper` | `[14, 255, 255]` | `[14, 255, 247]` |
| 7 个 `DailyEventUnread*Color` | `lower` | `[230, 230, 0]` | `[230, 235, 0]` |
| 7 个 `DailyEventUnread*Color` | `upper` | `[255, 255, 13]` | `[255, 255, 123]` |
| `__AutoUseSpMedication…SanityBoosterDeadline` | `lower` | 2 组 | 1 组 `[65, 120, 89]` |
| `__AutoUseSpMedication…SanityBoosterDeadline` | `upper` | 2 组 | 1 组 `[75, 255, 247]` |
| `SceneAnyEnterWorld` | `next` | 15 项 | 16 项：在 `…ScreenshotStableAbnormal` 与 `[JumpBack]__ScenePrivateAnyExit` 之间插入 `WebCloseProtocolPass` |
| `AutoStockpileGotoElasticGoods` | `post_delay` | `0` | `800` |

要点：
- `__ScenePrivateAnyExit` 从「识别式点击 ×100 + 3s 间隔」改成 `DirectHit` + `max_hit=6` + `post_delay=600`，
  这是网页端有键盘的直接收益（ESC 退出），也是覆盖层里唯一改「动作语义」的一处。
- `SceneAnyEnterWorld.next` 插的是**通行证全屏页兜底**：网页端会先弹通行证全屏页，base 链路会卡住。
- `AutoStockpileGotoElasticGoods.post_delay=800` 是修时序抖动（10-21 10:41 那次 8/9 的根因）。

## 3. 14 个新增节点（web 启动链）

```
WebStartGameEntry → WebAlreadyInGame / WebSessionBusy / WebMonthlyCard / WebQueueDetected
                  → WebLauncherStartButton → WebTitleClickToContinue / WebTitleContinue
WebCloseRewards、WebWaitInGame、WebAnyExitEsc
__ScenePrivateCloudBackPill、__ScenePrivateCloudClickToContinue、WebCloseProtocolPass
```

它们只做三件事：**认启动页状态**（排队 / 月卡 / 已进游戏）、**点开始按钮**（黄/青两版模板）、
**关掉网页特有的挡屏**（奖励弹窗、通行证全屏页、云端悬浮球）。

---

## 4. 模板实测（1280×720，网页串流画面）

| 节点 | 上游模板分数 | 处理 | 处理后 |
|---|---|---|---|
| `InMapAny` → MapMind | **0.959** | 无需处理 | 命中（阈值 0.9） |
| `InMapAny` → MapMissionReminder | **0.998** | 无需处理 | 命中 |
| `MapOverviewEnter` | **0.645** | **必须套 `green_mask`**（模板中间 25.4% 是绿幕遮罩） | **0.992** |
| `SceneMapLayerSwitchToMain` → MapLayersMain | **0.953** | 无需处理 | 命中 |
| `SceneMapLayerSwitchToMain` → MapLayersOther | **0.382** | 单层地图本来没有该控件 | 不适用 |
| `EnvironmentMonitoring` → PhotoIcon | **0.623**（阈值 0.8） | 用网页截图重裁模板 | **0.991 / 1.000 / 0.901**（三张卡） |

另一轮（PR 正文）的实测：MapMind 0.976 / MapMissionReminder 0.996 / MapOverviewEnter 0.992 / MapTeleportAnchor 0.925 —— 与上表同结论，分数随帧波动 ±0.02。

**结论：网页端不需要动阈值，需要的是「绿幕遮罩」和「按 1280×720 重裁小图标」。**

---

## 5. 全量实测

| 时间 | 结果 | 备注 |
|---|---|---|
| 2026-10-21 04:30 | **9/9，12 分 24 秒** | WebStartGame 55s / VisitFriends 84s / DijiangRewards 55s / CreditShoppingN2 50s / AutoStockpile 40s / AutoStockStaple 20s / SellProduct 143s / DailyRewards 83s / DeliveryJobs 63s |
| 2026-10-21 10:41 | 8/9，8 分 11 秒 | AutoStockpile 时序抖动 → 已用 `post_delay=800` 修掉，单跑 31s 通过 |

---

## 6. 三项未解（交接给后续实现）

1. **AutoEssence**：能连续跑到雪松林 / 北武陵禁区 / 首墩，但都卡在
   `__ScenePrivateMap*EnterWorldAnchorWithPick`。模板侧正常，怀疑 go-service 的 `MapFind`
   （世界坐标 → 屏幕坐标）在网页端地图上换算不对，尚未验证。
2. **EnvironmentMonitoring**：换掉 `PhotoIcon` 后能从终端页走到地图，同样卡地图锚点；
   08:00 那次归因结论是「断点在拍照点到达判定，不是视角拖动」。
3. **`SceneMapLayerSwitchToMain`**：`MapLayersOther.png` 带黄色选中高亮条，单层地图没有该控件；
   分层地图（如「首墩内部」）缩小后的状态还没取到图。

---

## 7. 一个待确认的风险

`web/resource_web_homesrv/pipeline/nodes.json` 只含 **1 个节点** `WebLauncherStartButton`，
且是**残缺定义**（只有 `recognition`，没有 `action` / `next` / `timeout` / 延迟字段）。

- 如果资源加载是**字段级深合并**（和 `pipeline_override` 一致）：安全，只是多认一版青色模板；
- 如果是**整节点替换**：这个变体会让启动按钮「只认不点」。

建议：把双模板直接合并进主份（`template: ["WebStartGame/StartButton.png", "WebStartGame/StartButtonCyan.png"]`），
删掉 homesrv 变体——一步消掉这个不确定性。

---

## 8. 后续走向

- 以 **#6746** 为主线（C++/WebView2 宿主 + `type: Win32` + `pretask` + `assets/resource_cloud_web/`）；
- 本 PR 收敛为「Linux 启动器 + CDP transport + Linux 覆盖 + 文档」，见
  [`cloud-web-launcher-contract-20261023.md`](./cloud-web-launcher-contract-20261023.md)；
- **保留**：`web/linux/homesrv/chrome_homesrv.sh`（启动器雏形）、`web/linux/cdp_controller.py`（取帧 JS + CDP 输入）、
  `web/resource_web/**`（31 节点）、`docs/web-linux/**`（9 篇实测）、`web/linux/wsl/**`（WSL 环境搭建，独立价值）；
- **移出**（core 已覆盖）：`run_daily_linux.py`、`resolve_preset.py`、`daily_run_homesrv.sh`、
  `auto_update.sh`、`option_audit.py`、`post_upgrade_check.py`、`map_probe.py`、`web/tools/**`。
