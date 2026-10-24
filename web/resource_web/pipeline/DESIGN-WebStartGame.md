# 「开始游戏（云·终末地网页版）」pipeline 方案（草案 v0.1）

目标：**纯 pipeline 实现**，不依赖 `run_daily_linux.py` 的任何外部编排。
任务 `WebStartGame` 的 `entry = WebStartGameEntry`，在 MXU 里点运行即可自洽跑完：
启动页 → 排队/加载 → 标题页 → 进入大世界/菜单；中途弹窗自愈。

---

## 1. 现状（已有节点，32 个 web 覆盖节点里的 6 个）

```
WebStartGameEntry   DirectHit / 无动作
  next: [WebSessionBusy, WebMonthlyCard, WebAlreadyInGame, WebTitleContinue, WebQueueDetected, WebLauncherStartButton]
      │
      ├─ WebSessionBusy   OCR「知道了/确定/OK」→ Click → next [WebStartGameEntry]（+post_delay 20s）
      ├─ WebMonthlyCard   OCR「今日月卡奖励…」→ Click → next [WebStartGameEntry]（max_hit 5）
      ├─ WebAlreadyInGame Or(InWorld, InMenuList) → DoNothing → next []      ← 成功终点
      ├─ WebQueueDetected OCR「排队中/预计等待…」→ DoNothing
      │     next: [WebSessionBusy, WebMonthlyCard, WebWaitInGame, WebTitleContinue]
      ├─ WebTitleContinue OCR「点击任意位置继续」→ Click（max_hit 10）
      │     next: [WebSessionBusy, WebMonthlyCard, WebWaitInGame, WebTitleContinue]
      └─ WebLauncherStartButton  TemplateMatch StartButton.png 0.95 ROI[700,450,560,200] → Click
            next: [WebSessionBusy, WebMonthlyCard, WebWaitInGame, WebTitleContinue]
                  │
                  └─ WebWaitInGame  Or(InWorld, InMenuList) → DoNothing → next []   ← 成功终点
```

## 2. 这套现在的问题（要在这轮一起修）

| # | 问题 | 影响 |
|---|---|---|
| 1 | `WebCloseRewards.next = null`（断链） | 领奖弹窗关掉后整条链结束，后面的节点不再判 |
| 2 | `WebWaitInGame.timeout = 600s` 且 `next: []` | 排队久了要干等 10 分钟才失败，失败后无重试 |
| 3 | `WebLauncherStartButton` 无 `max_hit` | 转场期间可能连点，把「开始游戏」点成二次开服/取消 |
| 4 | 所有节点都没有 `pre_wait_freezes` | 转场中判据不稳，容易误命中（尤其 0.95 模板） |
| 5 | 没有「登录过期 / 云玩时长用尽 / 服务繁忙页」节点 | 出现这类页面只能等超时，报错信息也看不出原因 |
| 6 | `focus` 文案只覆盖一半节点 | MXU 的运行日志里看不出卡在哪一步 |
| 7 | 阈值 0.95 是单平台帧抠的 | Linux 上若掉到 0.94 就会整条链卡住（历史上踩过） |

## 3. 目标流转（草案）

```
WebStartGameEntry  (DirectHit)
├─ WebLoginExpired        OCR「登录已过期/请重新登录/扫码登录」 → 停 + 报错文案（不自动扫码）
├─ WebQuotaExhausted      OCR「时长已用完/畅玩卡已过期」       → 停 + 报错文案
├─ WebSessionBusy         OCR「知道了」                        → Click → 回 Entry
├─ WebMonthlyCard         OCR「今日月卡奖励」                   → Click → 回 Entry
├─ WebAlreadyInGame       Or(InWorld, InMenuList)              → 成功终点
├─ WebQueueDetected       OCR「排队中/预计等待」                 → DoNothing（等）
├─ WebTitleContinue       OCR「点击任意位置继续」                → Click（max_hit 10）
└─ WebLauncherStartButton TemplateMatch「开始游戏」0.90          → Click(max_hit 2)
        ↓
   WebWaitInGame  Or(InWorld, InMenuList)  timeout 180s
        ├─ 命中 → WebCloseRewards → 回 Entry（Entry 再判 AlreadyInGame 即结束）
        └─ 超时 → WebStartGameRetry（max_hit 3）→ 回 Entry
```

## 4. 节点表（草案，字段待定）

| 节点 | 识别 | 动作 | 关键字段 | next |
|---|---|---|---|---|
| `WebStartGameEntry` | DirectHit | 无 | `timeout 900000` | 见上图 8 个候选 |
| `WebLauncherStartButton` | TemplateMatch `WebStartGame/StartButton.png` | Click | `threshold 0.90`，`max_hit 2`，`pre_wait_freezes 300`，`post_delay 800` | `WebSessionBusy / WebMonthlyCard / WebWaitInGame / WebTitleContinue` |
| `WebQueueDetected` | OCR 排队词表 | DoNothing | `pre_wait_freezes 500` | 同上 |
| `WebTitleContinue` | OCR 继续词表 | Click | `max_hit 10`，`post_delay 800` | 同上 |
| `WebWaitInGame` | Or(InWorld, InMenuList) | DoNothing | **`timeout 180000`**，`pre_wait_freezes 500` | `WebStartGameRetry`（超时链） |
| `WebCloseRewards` | And(CloseRewardsButtonText, CloseRewardsButton) | Click | `post_delay 1200` | **`WebStartGameEntry`**（修断链） |
| `WebSessionBusy` | OCR 确认按钮 | Click | `post_delay 20000` | `WebStartGameEntry` |
| `WebMonthlyCard` | OCR 月卡词表 | Click | `max_hit 5`，`post_delay 1500` | `WebStartGameEntry` |
| `WebLoginExpired`（新） | OCR 登录过期词表 | DoNothing | `focus` 写明「需要人工重新登录」 | `[]`（终止） |
| `WebQuotaExhausted`（新） | OCR 时长词表 | DoNothing | 同上 | `[]` |
| `WebStartGameRetry`（新） | DirectHit | 无 | `max_hit 3` | `WebStartGameEntry` |

## 5. 失败语义（要在草案里定死）

- **能在 pipeline 内自愈的**：弹窗（忙碌/月卡/领奖）、转场误判、排队等待 → 都在 `next` 里兜，不报错。
- **必须上报失败的**：登录过期、时长用尽、重试 3 次仍进不去 → 任务返回失败，`focus` 文案要能直接读出来是哪一类。
- 不引入 python 侧恢复链（`run_daily_linux.py` 的 `recover()` 保留给日常任务之间的兜底，这条链路自己闭环）。

## 6. 与 interface 任务的衔接

`web/tasks/WebStartGame.json` 保持瘦身，只写 `entry` 与 `controller`：

```json
{"name":"WebStartGame","entry":"WebStartGameEntry",
 "controller":["Linux-Wlroots","Linux-ScreenCast","Linux-Gamescope"],
 "group":["open_world"]}
```

## 7. 待你拍板的 5 件事

1. **排队等待上限**：`WebWaitInGame` 180s 还是 300s？（云端排队实测有 3–5 分钟的情况）
2. **重试次数** `WebStartGameRetry.max_hit`：3 次够不够？超时后是否要「换页面重进」（`reset_page.py` 那种 hard reset）？
3. **阈值**：`StartButton` 定 0.90 还是分平台两套（Linux 重抠一张）？
4. **要不要自动扫码/重新登录**：我倾向**不自动**，只报错停下（扫码必须人工）。
5. **节点命名前缀**：继续用 `Web*`，还是按上游风格改成 `__Web*Private*` 以区分「对上游节点的覆盖」与「我们新增的入口节点」？
