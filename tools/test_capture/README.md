# test_capture

按预先写好的步骤流程逐屏截图。每一步就是一段 Python：等界面到位、截图、点一下进下一步。

判断界面和点击位置都可以直接引用 MaaEnd 现有的 Pipeline 节点（模板匹配、OCR 都行），
这样换平台也能用同一份流程，不用给每个平台单独写坐标；
它不读 Pipeline 本身的流转，改节点顺序、改路线都不会影响写好的步骤。

开跑前先跑这个控制器自己的预任务（`assets/tasks/pretasks` 里登记的那个）：
云网页版会把外壳窗口拉起来，桌面版会把游戏拉起来，已经在跑就直接过。

安卓端（`ADB` / `CloudADB`）连上之后先跑项目自己的 `OpenGame` 任务：游戏没开就按客户端包启动
（启动包取 `assets/tasks/AndroidOpenGame.json` 里的默认客户端），顺手过掉登录页、排队和公告，
已经在游戏里就直接过。桌面端和云网页版不需要这一步，窗口由预任务准备好。

最后所有平台都跑一次现成的 `SceneAnyEnterWorld`，把游戏带回大世界再开始截图，免得截图里混进菜单、弹窗。

整场运行期间 go-service 与 cpp-algo 都挂着，所以可以引用任意节点，
包括用自定义识别的（`MapFind` 之类）；只有 `DirectHit` 不行，它永远命中，判断不了界面。

## 流程文件放哪

放在 `tests/` 下对应任务目录里，文件名固定 `capture_flow.py`：

```text
tests/OutpostTrading/capture_flow.py
tests/DeliveryJobs/capture_flow.py
```

文件里建一个 `flow` 对象，再用 `@flow.step` 把函数登记成步骤，步骤名就是函数名。
每一步都要写清楚它要在哪个界面：`enter` 是界面节点，`route` 是不在这个界面时用哪个现成节点转过去。

```python
from tools.test_capture import Capture, Flow

flow = Flow("据点交易")


@flow.step(
    enter="OutpostTradingInRefugeeCamp",
    route="SceneEnterMenuRegionalDevelopmentValleyIVOutpostManagement",
)
def 四号谷地据点(c: Capture) -> None:
    c.shot("据点_难民暂居处")
    c.click("OutpostTradingCheckInfraStationTabText")
```

每一步开跑前先看一眼 `enter` 命不命中：命中就直接往下做；不在就先 `sub_task(route)` 转过去
（`route` 不写时退回 `SceneAnyEnterWorld` 回大世界），等 `enter` 命中才开始。
这样中途被弹窗打断、上一步没走完，下一步会自己先回到该在的界面。

函数名里的字符不够用时，用 `@flow.named("据点 难民暂居处", enter=..., route=...)` 指定步骤名。

### 每一步默认都是一段

步骤一旦走到没回头路的地方（货卖光了、一次性弹窗关掉了），另一个平台就没法重来，
所以不用写什么，**每一步默认就是本段的结尾**：这一步在所有平台上各跑一遍，才进下一步。
两个平台 A、B 跑 4 步，实际顺序是：

```text
A1  →  B1  →  B2  →  A2  →  A3  →  B3  →  B4  →  A4
```

平台顺序一段正、一段反（A B → B A → A B……），这样上一段的最后一台可以直接接着跑下一段。
代价是每换一段就要换一次平台，也就是顶一次号；工具不会主动关游戏。

哪几步连在一起跑不会出问题，就给其中不切段的那一步写 `stop=False`，它和下一步并成一段，
这样能少切几次平台、少顶几次号。

某台平台中途失败时，它后面的段会跳过，别的平台照常跑完，最后汇总哪些平台失败了。

### 只能做一次的步骤标 once

有些步骤做完之后别的平台再做就没东西可截（比如把调度券一次卖光、把委托转交出去）。
这种写 `once=True`：

```python
@flow.step(enter="OutpostTradingInSkyKingFlatsConstructionSite", route="...", once=True)
def 清空调度券(c: Capture) -> None:
    c.sub_task("OutpostTradingSkyKingFlatsConstructionSiteBetterSliding")
```

第一个跑到的平台做完，后面到的平台直接跳过这一步，记一笔跳过、不算失败；
跑完汇总里会写哪些平台跳过几段，那几段它们本来就没有图。

## 怎么跑

```bash
# 单个平台
uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller ADB

# 多个平台依次跑
uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller Win32-Front --controller ADB

# 只等界面、不点击不滑动，先把流程对一遍
uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller ADB --dry-run

# 游戏已经停在要截的界面上，别动它
uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller ADB --no-open-game

# 截图前不等画面稳定，截到什么就存什么
uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller ADB --wait 0

# 顺手把截图脱敏后导进测试集
uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller ADB --testset

# 忘了加 --testset：直接把那次运行目录里的截图脱敏补导一遍，不用重跑
uv run test-capture --from-run .cache/test-capture/20261122-075434

# 上次跑一半停了：接着那个运行目录继续，跑过的步骤不再跑
uv run test-capture --resume .cache/test-capture/20261124-090000
```

### 中途停下与接着跑

按 `Ctrl+C` 会马上停：当前那一步的 maa 任务会被打断，不会干等它自己跑完；连按两次直接硬退。

进度随时写在运行目录的 `manifest.json` 里，每跑完一步就更新一次，所以停在哪一步都记得住。
用 `--resume <运行目录>` 接着跑：已经跑过的步骤不再跑，`once` 的步骤也照旧只做一次，
流程文件和平台都从那份 manifest 里取，不用再给 `--flow` / `--controller`。
因为 manifest 是边跑边写的，中断的运行目录也能用 `--from-run` 补导测试图。

`SceneAnyEnterWorld` 和自定义识别都依赖 go-service、cpp-algo 注册的组件，
所以第一次跑之前要先 `uv run build-and-install`。

`--controller` 写 `assets/interface.json` 里的控制器名，脚本照着里面的定义连设备：

- `Adb` 类（`ADB`、`CloudADB`）：自动查找设备，找不到或者有多个时用 `--adb-serial` 指定地址。
- `Win32` 类（`Win32-Front`、`CloudWeb`）：按 `class_regex` / `window_regex` 找窗口，截图和输入方式取条目里写的 `screencap` / `mouse` / `keyboard`。

`CloudWeb` 是云·终末地网页版的外壳窗口（类名 `MaaEnd.CloudShell`，1280x720），
所以同一份流程换 `--controller CloudWeb` 就能跑，不用改坐标。
开跑前会跑 `LaunchCloudWeb` 预任务把外壳拉起来，外壳没装会提示先 `uv run setup-workspace`，
第一次用还要先在外壳窗口里登录一次。截图和 Win32 几乎一样（都走桌面版界面），
但左下角是云 HUD，所以测试集里单独占一个 `CloudWeb` 平台目录。

`PlayCover`、`Linux-*`、`MacOS-*` 暂时没接，脚本会直接说明不支持。

## 流程里能写什么

流程函数拿到一个 `Capture` 对象，可用的方法：

| 方法 | 做什么 |
| --- | --- |
| `wait(node)` | 等节点命中，返回它的识别框；不额外等待 |
| `shot(name, settle=2)` | 先等画面稳定（默认 `--wait` 秒）再截当前画面，文件名用 `name` |
| `click(node)` | 等节点命中，点它的识别框中心 |
| `click_at((x, y), offset=(0, 0))` | 点固定坐标（1280x720 基准），`offset` 微调 |
| `swipe((x1, y1), (x2, y2), duration=500)` | 划过一段距离，用来拖滑条、翻列表 |
| `sub_task(node)` | 跑现成节点，让它自己把界面转跳到目标 |
| `goto(enter, route=None)` | 先看在这个界面没有，不在就转跳过去；每个步骤开跑前自动调一次 |
| `run(node)` | 按这个节点自己的识别和动作执行一次，不跟它后面的 next |
| `hit(node)` | 看一眼现在命不命中，不等待 |
| `text(node)` | 等节点命中并返回它 OCR 认出来的文字 |
| `ocr(roi)` / `number(roi)` | 临时在一小块区域上认字 / 读数字，读不到当 0 |
| `sleep(秒)` | 硬等，能不用就不用 |

```python
c.shot("据点_天王坪援建点")  # 等 2 秒画面稳定，再截一张
c.click("OutpostTradingOpenOperatorLiaisonButton")  # 点进去
```

界面已经在 `enter` 里声明过，函数里不用再写一遍 `wait`。

只有截图前才等画面稳定：`wait` / `click` 这类操作命中就往下走，不截图的时候不会白等。
某个界面动画动得慢，就单独给这张图写 `c.shot("名字", settle=3)`。

`click` / `wait` 里的节点名可以填任意有识别的节点，模板匹配、OCR、颜色匹配、自定义识别都行；
`click_at` 的坐标按 1280x720 写，脚本不换算分辨率，换了平台自己核对。

### 节点名写错会当场停下

节点名是直接写的字符串，容易打错。开跑前工具会扫一遍流程文件，把 `hit` / `wait` / `text` / `click` /
`run` / `sub_task` 的参数、`goto` 的两个参数、以及每个步骤的 `enter` / `route`，拿去跟资源里的节点对；
对不上就一次全列出来，还给出像的候选，然后直接退出：

```text
流程里的节点名对不上资源，先改好再跑：
  tests/DeliveryJobs/capture_flow.py:40「DeliveryJobsInCargoRedistributionBidd」资源里没有，是不是「DeliveryJobsInCargoRedistributionBid、…」
```

核对发生在装好资源之后、拉起游戏和连设备之前，写错了不会真的动游戏。
节点名传成变量时没法静态检查，工具会提示是哪几行，那几行只能跑到才知道。

### 想加判断就直接写

流程是普通 Python，读界面上的数字再决定下一步很简单。比如读滑条右边「这轮能卖几份」，
读出来是 0 就不再点：

```python
for round_index in range(1, 13):
    if c.hit("OutpostTradingZeroMoney"):  # 已经提示储量不足
        break
    c.swipe((940, 548), (1180, 548))  # 份数拉满
    tradeable = c.number((1150, 528, 100, 44))
    if tradeable < 1:
        break
    c.click("OutpostTradingSell")
    c.wait("OutpostTradingCheckResultHeader")  # 交易完成的提示出来了
    c.run("OutpostTradingSellCheck")  # 现成的「交易后确认」节点，自己点 ✓
```

## 顺手写进测试集

加 `--testset` 后，每张截图还会脱敏复制一份到 `tests/MaaEndTestset/<控制器>/<资源>/<任务目录>/`。
任务目录默认取流程文件所在的文件夹名（`tests/OutpostTrading/capture_flow.py` → `OutpostTrading`），用 `--testset-dir` 可以改；
资源目录默认 `官服`（对应 `Official_CN`），用 `--testset-resource` 可以改。

脱敏直接调 `.agents/skills/maaend-test-image/scripts/redact_test_image.py`，和手动导图是同一套规则：
按左下角 UID 文字判定平台，用纯绿色盖住该平台的固定 ROI，再按 OCR 兜住其它 `UID` / `#` 文字。
截图本身还是先落在 `.cache/test-capture/`（不提交），进仓库的只有脱敏后的图。

忘了加 `--testset` 也不用重跑：`--from-run <运行目录>` 会读里面的 `manifest.json`，
把那次截的图逐张脱敏补导到测试集，和当场导出走的是同一套脚本；任务目录默认取 `manifest.json` 里记的流程文件所在文件夹，也可以用 `--testset-dir` 改。

用例文件不自动生成，图导进去之后自己加到 `tests/<任务>/test_*.json`：`imageRoot` 写任务目录名，`hits` 填这一步用的节点。
每步截了哪些图、点了哪里，`manifest.json` 里都记着：每步一条，`shots` 里是图片名和导进测试集的路径，`clicked` 是点过的坐标。

一旦某个平台报错停下，剩下的步骤不会再有新图，此时别急着写用例，先看失败截图把流程修好。
