# 云终末地网页端（headless Linux）运行层

在**无桌面的 Linux 服务器**上跑「云·终末地」网页版日常：headless sway 提供合成器，Chrome 跑网页，
MaaFramework 的 **Linux-Wlroots 控制单元**负责截图与虚拟键鼠，任务层完全复用上游 PC 资源。

```
sway (headless, 1280x720)
└─ Chrome --ozone-platform=wayland  →  endfield.hypergryph.com/cloud
   ▲ wlr-screencopy                  ▲ 虚拟键盘/指针
   └──────── MaaFramework (WlRoots) ───────┘
              ▲
       run_daily_linux.py  ←  resolve_preset.py（选项树 → pipeline_override）
```

## 目录

| 路径 | 作用 |
|---|---|
| `linux/run_daily_linux.py` | 日常驱动：读预设 → 解析覆盖 → 逐任务 → 重试 → 失败截图 → 汇总 |
| `linux/resolve_preset.py` | 任务选项树递归展开成 `pipeline_override`（`input` 型 / `switch` / `select` / 任务级 override） |
| `linux/homesrv/chrome_homesrv.sh` | 起 headless sway + Chrome |
| `linux/homesrv/daily_run_homesrv.sh` | 一键日常：起环境 → 跑 → 收尾 |
| `linux/homesrv/hide_cloud_ball.py` | 隐藏云端悬浮球（DOM 覆盖层；点「开始游戏」后云端会新开标签页，需常驻守护） |
| `linux/homesrv/auto_update.sh` | 跟进上游：API → `releases.atom` 回退 → 同步资源/任务/覆盖层基线 |
| `linux/homesrv/option_audit.py` | 覆盖项基线审计（升级后选项漂移检测） |
| `linux/homesrv/post_upgrade_check.py` | 升级后自检（关键节点/资源是否还在） |
| `linux/homesrv/map_probe.py` | 地图界面节点级探针（诊断用） |
| `resource_web/pipeline/nodes.json` | **覆盖层 32 个节点** |
| `resource_web/image/` | 网页端模板（如 `EnvironmentMonitoring/PhotoIcon.png`） |
| `resource_web_homesrv/` | 单机特有覆盖（示例，1 个节点） |

## 覆盖层放什么

只放「网页端与 PC 客户端行为不同」的节点：

- 退出：`__ScenePrivateAnyExit` 用 ESC×6 替代上游的识别式点击 ×100 + 3s 间隔（后者在网页端会空转 300 秒）
- 启动链：启动页 / 排队 / 月卡 / 标题页分支；`SceneAnyEnterWorld` 只插入一个 `WebCloseProtocolPass`（终末地通行证全屏页兜底），其余 next 与上游逐字一致
- 稳定物资购买：13 个颜色节点（`AutoStockInStapleItem` 等 HSV 分段，网页端取色与 PC 不同）
- `AutoStockpileGotoElasticGoods`：点击后 `post_delay: 800`，避免「弹性物资」还没切过去就进下一节点

> 已撤回：活动中心相关的 `InEventCenter` / `__ScenePrivateAnyEnterMenuSuccess`。
> 悬浮球隐藏后（`hide_cloud_ball.py --watch`，实测 `visible:0`），上游精确匹配「活动中心」可直接命中，
> DailyRewards 单跑 53.1s 通过、日志命中 382 次，不再需要放宽 `expected`。

## 实测

Ubuntu 24.04 / headless sway / Chrome，默认 1280×720：

- 定时全量 **9/9，12 分 24 秒**（WebStartGame 55s / VisitFriends 84s / DijiangRewards 55s / CreditShoppingN2 50s / AutoStockpile 40s / AutoStockStaple 20s / SellProduct 143s / DailyRewards 83s / DeliveryJobs 63s）
- 串流画面上模板命中：`MapMind` 0.976、`MapMissionReminder` 0.996、`MapOverviewEnter` 0.992（**必须套 `green_mask`**，否则只有 0.645）、`MapTeleportAnchor` 0.925
- 小图标会掉分：上游 `EnvironmentMonitoring/PhotoIcon.png` 在串流画面上只有 0.623（阈值 0.8），用网页截图裁片后可到 1.000 / 0.991 / 0.901

## 用法

```bash
export MAAEND_ROOT=$HOME/maaend
export MAAEND_RES_DIRS="$MAAEND_ROOT/resource:$MAAEND_ROOT/web/resource_web:$MAAEND_ROOT/web/linux/resource_web_linux:$MAAEND_ROOT/web/resource_web_homesrv"

bash web/linux/homesrv/daily_run_homesrv.sh                      # 全量
MAAEND_TASKS=AutoStockStaple,DailyRewards bash web/linux/homesrv/daily_run_homesrv.sh
python3 web/linux/run_daily_linux.py --instance <实例名> --dry-run
```

## 未解决

- **AutoEssence**：能连续跑到雪松林 / 北武陵禁区 / 首墩，但都卡在 `__ScenePrivateMap*EnterWorldAnchorWithPick`。模板侧正常（地图/任务提示/地区总览/锚点图标均命中），怀疑 go-service `MapFind`（世界坐标 → 屏幕坐标）在网页端地图上换算不对。
- **EnvironmentMonitoring**：换掉 `PhotoIcon` 后能从终端页走到地图，同样卡在地图锚点。
- **`SceneMapLayerSwitchToMain`**：`MapLayersOther.png` 带黄色选中高亮条，单层地图本来没有该控件；分层地图（如「首墩内部」）缩小后的状态还没取到图。

## 注意

- 截图长边会被 MaaFramework 缩到 1280，模板必须按 1280×720 标；`set_screenshot_target_long_side()` 可改但要重标模板。
- `linux/homesrv/*.sh` 里带本机路径（`~/maaend`），按需改。
