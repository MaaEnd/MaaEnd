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

### 走到没回头路的步骤就分段

有些界面点下一步就回不来了（货卖光了、一次性弹窗关掉了），所以流程要分段跑。
给那一步加 `stop=True`，它就是本段的结尾，后面属于下一段：

```python
@flow.step(enter="OutpostTradingInSkyKingFlatsConstructionSite", route="...", stop=True)
def 交易(c: Capture) -> None:
    c.run("OutpostTradingSell")
```

分段之后按「段在外、平台在内」跑：**一段在所有平台上各跑一遍，才进下一段**。
两个平台 A、B，6 步里第 3 步标了 stop，实际顺序是：

```text
A 跑 1-3  →  B 跑 1-3  →  B 跑 4-6  →  A 跑 4-6
```

平台顺序一段正、一段反（A B → B A → A B……），这样上一段的最后一台可以直接接着跑下一段，
不用重连也不用重新登录；只有真正换平台那一次，才靠下一个客户端登录把上一个挤下线（顶号）。
工具不会主动关游戏。

某台平台中途失败时，它后面的段会跳过，别的平台照常跑完，最后汇总哪些平台失败了。
全流程没有 `stop` 时就是一段，一台跑完全部再换下一台，和以前一样。

已有的 `tests/OutpostTrading/capture_flow.py` 一趟跑完两个地区、截 11 张图：
四号谷地三个据点、武陵三个据点、联络干员列表的撤下与派驻两种按钮状态、选择交易货品、交易完成，
最后把据点调度券卖到见底，截一张「当前据点调度券储量不足」；
联络干员列表里有人派驻在别的据点时，还会多截一张「确认更改派驻」。
两个地区之间不用手写地区选择那几步：每步的 `route` 指向对应地区的据点管理入口，不在那个界面时自己转过去。
**它会真的把当轮能卖的都卖掉**，跑之前留意当前据点的券和仓库货物。

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
```

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

### 想加判断就直接写

流程是普通 Python，读界面上的数字再决定下一步很简单。据点交易最后要把券卖光，
工具先读滑条右边「这轮能卖几份」，读出来是 0 就不再点交易：

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
