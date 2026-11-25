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
节点名统一写成 `Nodes.节点名`，编辑器会补全并显示节点说明，见下面「编辑器里补全节点名」。

```python
from tools.test_capture import Capture, Flow, Nodes

flow = Flow("据点交易")


@flow.step(
    enter=Nodes.OutpostTradingInRefugeeCamp,
    route=Nodes.SceneEnterMenuRegionalDevelopmentValleyIVOutpostManagement,
)
def 四号谷地据点(c: Capture) -> None:
    c.shot("据点_难民暂居处")
    c.click(Nodes.OutpostTradingCheckInfraStationTabText)
```

每一步开跑前先看一眼 `enter` 命不命中：命中就直接往下做；不在就先 `sub_task(route)` 转过去
（`route` 不写时退回 `SceneAnyEnterWorld` 回大世界），等 `enter` 命中才开始。
这样中途被弹窗打断、上一步没走完，下一步会自己先回到该在的界面。

有些界面没有现成节点能一步跳进去（比如装箱界面只能从仓储节点点进去、再选一次货物类型），
这时 `route` 可以写一段流程函数，自己一步步点过去，最常用的是「先回仓储节点、再点进去」：

```python
def 回装箱界面(c: Capture) -> None:
    c.goto(Nodes.DeliveryJobsInWulingLocalDepotNode, Nodes.SceneEnterMenuRegionalDevelopmentWulingDepotNode)
    c.run(Nodes.DeliveryJobsEnterWulingCityCargo)
    c.wait(Nodes.DeliveryJobsInCargoPackGoods)


@flow.step(enter=Nodes.DeliveryJobsInCargoPackGoods, route=回装箱界面, once=True)
def 装满货物(c: Capture) -> None:
    c.run(Nodes.DeliveryJobsCargoFillToMax)
```

实在回不去的界面（比如委托已经转出去、只在某一瞬间存在的奖励页），`route` 写个直接抛错的函数，
让人一眼看出是状态不对，而不是傻等超时。

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

任何一台平台中途失败，整场就停下：后面的段、别的平台都不再跑，免得在状态不对的游戏上接着点；最后汇总是哪台平台、哪一步失败的。

### 只能做一次的步骤标 once

有些步骤做完之后别的平台再做就没东西可截（比如把调度券一次卖光、把委托转交出去）。
这种写 `once=True`：

```python
@flow.step(enter=Nodes.OutpostTradingInSkyKingFlatsConstructionSite, route=..., once=True)
def 清空调度券(c: Capture) -> None:
    c.sub_task(Nodes.OutpostTradingSkyKingFlatsConstructionSiteBetterSliding)
```

第一个跑到的平台做完，后面到的平台直接跳过这一步，记一笔跳过、不算失败；
跑完汇总里会写哪些平台跳过几段，那几段它们本来就没有图。

## 怎么跑

命令分成四个子命令：`run` 跑流程、`diagram` 画流程图、`export` 补导测试图、`nodes` 刷新节点名补全。`uv run test-capture -h` 看子命令列表，`uv run test-capture run -h` 看某个子命令自己的参数；只写 `uv run test-capture run` 不给参数时，click 会报缺哪个参数，并列出可选的流程文件；给了 `--flow` 却没给 `--controller` 时列出可选的控制器名。

```bash
# 单个平台
uv run test-capture run --flow tests/OutpostTrading/capture_flow.py --controller ADB

# 多个平台依次跑
uv run test-capture run --flow tests/OutpostTrading/capture_flow.py --controller Win32-Front --controller ADB

# 只等界面、不点击不滑动，先把流程对一遍
uv run test-capture run --flow tests/OutpostTrading/capture_flow.py --controller ADB --dry-run

# 游戏已经停在要截的界面上，别动它
uv run test-capture run --flow tests/OutpostTrading/capture_flow.py --controller ADB --no-open-game

# 截图前不等画面稳定，截到什么就存什么
uv run test-capture run --flow tests/OutpostTrading/capture_flow.py --controller ADB --wait 0

# 顺手把截图脱敏后导进测试集
uv run test-capture run --flow tests/OutpostTrading/capture_flow.py --controller ADB --testset

# 忘了加 --testset：直接把那次运行目录里的截图脱敏补导一遍，不用重跑
uv run test-capture export .cache/test-capture/20261122-075434

# 上次跑一半停了：接着那个运行目录继续，跑过的步骤不再跑
uv run test-capture run --resume .cache/test-capture/20261124-090000
```

### 先画一张流程图再跑

流程文件本身就能画成一张 HTML，看一眼就知道顺序对不对：

- 最上面是「运行顺序」表格：一行一台平台、一列一段，格子里的数字是第几个上场，斜线格是整段跳过；下面一条箭头串标出在哪里换平台（顶号）。
- 往下每一步只画一次：要在的界面、不在时怎么过去（写的是流程函数就能点开看它一步步做了什么）、按代码顺序排的调用、步骤的文档字符串和代码注释；截图单独一行橙色高亮，粗体是图片名。
- 每步右上角是这一步在哪些平台上跑，划掉的是「只做一次」被别的平台做过、这台跳过。
- 顶部按钮可以只看某一台平台跑的步骤，或只看「只做一次」的步骤；亮色、暗色跟着系统走。

```bash
# 所有流程各画一张，存到 .cache/test-capture/diagrams/<任务目录>.html
uv run test-capture diagram
# 带上 --controller 还能把平台顺序和跳过关系标出来
uv run test-capture diagram --controller ADB --controller Win32-Front
# 只画一个流程；后面跟路径就存到那里，不跟就存默认目录
uv run test-capture diagram --flow tests/DeliveryJobs/capture_flow.py 流程.html
```

它只读流程文件，不连设备、不动游戏，随便画。

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
| `shot_when(node, name, settle=2)` | 等这个节点命中再截一张存成 `name`，比 `wait` 加 `shot` 少写一行 |
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
c.click(Nodes.OutpostTradingOpenOperatorLiaisonButton)  # 点进去
```

界面已经在 `enter` 里声明过，函数里不用再写一遍 `wait`。

大部分图都是「某个界面出来了才拍」，这种直接写 `shot_when`，等和拍一步到位：

```python
c.shot_when(Nodes.DeliveryJobsCheckCargoFilledToMax, "货物装箱完成")  # 装满的图标出来了才拍
```

只有截图前才等画面稳定：`wait` / `click` 这类操作命中就往下走，不截图的时候不会白等。
某个界面动画动得慢，就单独给这张图写 `c.shot("名字", settle=3)`，`shot_when` 一样能加。

`click` / `wait` 里的节点名可以填任意有识别的节点，模板匹配、OCR、颜色匹配、自定义识别都行；
`click_at` 的坐标按 1280x720 写，脚本不换算分辨率，换了平台自己核对。

### 编辑器里补全节点名

节点名统一写成 `Nodes.节点名`（`from tools.test_capture import Nodes`）。先生成一次节点名类型存根：

```bash
uv run test-capture nodes
```

它把所有资源包（`assets/resource*/pipeline`）里的节点写进 `tools/test_capture/node_names.pyi`，每个节点是 `Nodes` 的一个属性。VS Code（Pylance）或 pyright 读到它之后，敲 `Nodes.` 就会弹出节点名补全，选中某一项、或者鼠标停在写好的 `Nodes.XXX` 上，能看到这个节点的说明：

- 节点的 `desc` 字段；
- 节点名上方紧挨着的 `//` 注释；
- 识别和动作类型（比如「识别 TemplateMatch，动作 Click」）；
- 节点定义在哪个文件（`assets/` 下的路径），几个资源包都有同名节点时都列上。

写错的名字当场标红，`enter` / `route` 也一样。这个文件不进仓库；之后每次跑 `test-capture`（包括画流程图）都会按当时的 pipeline 顺手刷新，改了节点也不用手动再生成。没生成时 `Nodes.XXX` 就是普通字符串 `"XXX"`，不补全也不报错。

为什么不直接在引号里补全：Pylance 给字符串补全时只列字符串本身，带不出任何说明，所以换成属性写法。

有两类节点写不成属性，还是用字符串，字符串在引号里照样能补全节点名（只是没有说明）、写错照样标红：

- 下划线开头的节点（pipeline 内部用的，比如 `__ScenePrivate...`）：写成属性 pyright 会当成私有成员报错；
- 名字里带 `-` 这类字符、不是合法 Python 名字的节点（比如 `__BS-1`）。

节点名先放进变量再传，变量写 `Nodes.XXX` 就行；要写字符串的话把变量标成 `NodeName`，不然编辑器会说普通字符串对不上：

```python
from tools.test_capture import Nodes

tabs = [Nodes.OutpostTradingCheckInfraStationTabText, Nodes.OutpostTradingCheckReconstructionHQTabText]
```

### 节点名写错会当场停下

编辑器标红只是提醒，开跑前工具还会再核对一遍：扫流程文件，把 `hit` / `wait` / `text` / `click` /
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
    if c.hit(Nodes.OutpostTradingZeroMoney):  # 已经提示储量不足
        break
    c.swipe((940, 548), (1180, 548))  # 份数拉满
    tradeable = c.number((1150, 528, 100, 44))
    if tradeable < 1:
        break
    c.click(Nodes.OutpostTradingSell)
    c.wait(Nodes.OutpostTradingCheckResultHeader)  # 交易完成的提示出来了
    c.run(Nodes.OutpostTradingSellCheck)  # 现成的「交易后确认」节点，自己点 ✓
```

## 顺手写进测试集

加 `--testset` 后，每张截图还会脱敏复制一份到 `tests/MaaEndTestset/<控制器>/<资源>/<任务目录>/`。
任务目录默认取流程文件所在的文件夹名（`tests/OutpostTrading/capture_flow.py` → `OutpostTrading`），用 `--testset-dir` 可以改；
资源目录默认 `官服`（对应 `Official_CN`），用 `--testset-resource` 可以改。

脱敏直接调 `.agents/skills/maaend-test-image/scripts/redact_test_image.py`，和手动导图是同一套规则：
按左下角 UID 文字判定平台，用纯绿色盖住该平台的固定 ROI，再按 OCR 兜住其它 `UID` / `#` 文字。
截图本身还是先落在 `.cache/test-capture/`（不提交），进仓库的只有脱敏后的图。

忘了加 `--testset` 也不用重跑：`uv run test-capture export <运行目录>` 会读里面的 `manifest.json`，
把那次截的图逐张脱敏补导到测试集，和当场导出走的是同一套脚本；任务目录默认取 `manifest.json` 里记的流程文件所在文件夹，也可以用 `--testset-dir` 改。

用例文件不自动生成，图导进去之后自己加到 `tests/<任务>/test_*.json`：`imageRoot` 写任务目录名，`hits` 填这一步用的节点。
每步截了哪些图、点了哪里，`manifest.json` 里都记着：每步一条，`shots` 里是图片名和导进测试集的路径，`clicked` 是点过的坐标。

一旦某个平台报错停下，剩下的步骤不会再有新图，此时别急着写用例，先看失败截图把流程修好。
