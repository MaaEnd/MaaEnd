# test_capture

按预先写好的步骤流程逐屏截图。每一步就是一段 Python：等界面到位、截图、点一下进下一步。

判断界面和点击位置都可以直接引用 MaaEnd 现有的 Pipeline 节点（模板匹配、OCR 都行），
这样换平台也能用同一份流程，不用给每个平台单独写坐标；
它不读 Pipeline 本身的流转，改节点顺序、改路线都不会影响写好的步骤。

开跑前先跑这个控制器自己的预任务（`assets/tasks/pretasks` 里登记的那个）：
云网页版会把外壳窗口拉起来，桌面版会把游戏拉起来，已经在跑就直接过。
接着执行一次现成的 `SceneAnyEnterWorld` 节点，把游戏带回大世界再开始截图，
免得截图里混进菜单、弹窗；安卓端游戏压根没开的话，会跑项目自己的 `OpenGame` 任务
（启动包取 `assets/tasks/AndroidOpenGame.json` 里默认客户端），等它进到大世界再继续。

整场运行期间 go-service 与 cpp-algo 都挂着，所以可以引用任意节点，
包括用自定义识别的（`MapFind` 之类）；只有 `DirectHit` 不行，它永远命中，判断不了界面。

## 流程文件放哪

放在 `tests/` 下对应任务目录里，文件名固定 `capture_flow.py`：

```text
tests/OutpostTrading/capture_flow.py
tests/DeliveryJobs/capture_flow.py
```

文件里建一个 `flow` 对象，再用 `@flow.step` 把函数登记成步骤，步骤名就是函数名：

```python
from tools.test_capture import Capture, Flow

flow = Flow("据点交易")


@flow.step
def 四号谷地据点(c: Capture) -> None:
    c.sub_task("SceneEnterMenuRegionalDevelopmentValleyIVOutpostManagement")
    c.wait("OutpostTradingInRefugeeCamp")
    c.shot("据点_难民暂居处")
    c.click("OutpostTradingCheckInfraStationTabText")
```

函数名里的字符不够用时，用 `@flow.named("据点 难民暂居处")` 指定步骤名。

已有的 `tests/OutpostTrading/capture_flow.py` 一趟跑完两个地区、截 11 张图：
四号谷地三个据点、武陵三个据点、联络干员列表的撤下与派驻两种按钮状态、选择交易货品、交易完成，
最后把据点调度券卖到见底，截一张「当前据点调度券储量不足」；
联络干员列表里有人派驻在别的据点时，还会多截一张「确认更改派驻」。
两个地区之间用 `SubTask` 转跳入口节点切换，不用手写地区选择那几步。
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
```

`SceneAnyEnterWorld` 和自定义识别都依赖 go-service、cpp-algo 注册的组件，
所以第一次跑之前要先 `uv run build-and-install`。

`--controller` 写 `assets/interface.json` 里的控制器名，脚本照着里面的定义连设备：

- `Adb` 类（`ADB`、`CloudADB`）：自动查找设备，找不到或者有多个时用 `--adb-serial` 指定地址。
- `Win32` 类（`Win32-Front`、`CloudWeb`）：按 `class_regex` / `window_regex` 找窗口，截图和输入方式取条目里写的 `screencap` / `mouse` / `keyboard`。

`CloudWeb` 是云·终末地网页版的外壳窗口（类名 `MaaEnd.CloudShell`，1280x720），
所以同一份流程换 `--controller CloudWeb` 就能跑，不用改坐标。
开跑前会跑 `LaunchCloudWeb` 预任务把外壳拉起来，外壳没装会提示先 `uv run setup-workspace`，
第一次用还要先在外壳窗口里登录一次。`--testset` 不支持它：云网页版的画面和 Win32 是同一套界面，
截图先落本地，要进测试集就用 Win32 的图。

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
| `run(node)` | 按这个节点自己的识别和动作执行一次，不跟它后面的 next |
| `hit(node)` | 看一眼现在命不命中，不等待 |
| `text(node)` | 等节点命中并返回它 OCR 认出来的文字 |
| `ocr(roi)` / `number(roi)` | 临时在一小块区域上认字 / 读数字，读不到当 0 |
| `sleep(秒)` | 硬等，能不用就不用 |

```python
c.wait("OutpostTradingInSkyKingFlatsConstructionSite")   # 界面到了，不等就开始下一步
c.shot("据点_天王坪援建点")                                # 等 2 秒画面稳定，再截一张
c.click("OutpostTradingOpenOperatorLiaisonButton")        # 点进去
```

只有截图前才等画面稳定：`wait` / `click` 这类操作命中就往下走，不截图的时候不会白等。
某个界面动画动得慢，就单独给这张图写 `c.shot("名字", settle=3)`。

`click` / `wait` 里的节点名可以填任意有识别的节点，模板匹配、OCR、颜色匹配、自定义识别都行；
`click_at` 的坐标按 1280x720 写，脚本不换算分辨率，换了平台自己核对。

### 想加判断就直接写

流程是普通 Python，读界面上的数字再决定下一步很简单。据点交易最后要把券卖光，
工具先读滑条右边「这轮能卖几份」，读出来是 0 就不再点交易：

```python
for round_index in range(1, 13):
    if c.hit("OutpostTradingZeroMoney"):   # 已经提示储量不足
        break
    c.swipe((940, 548), (1180, 548))       # 份数拉满
    tradeable = c.number((1150, 528, 100, 44))
    if tradeable < 1:
        break
    c.click("OutpostTradingSell")
    c.wait("OutpostTradingCheckHeader")    # 交易完成的提示出来了
    c.run("OutpostTradingSellCheck")       # 现成的「交易后确认」节点，自己点 ✓
```

## 顺手写进测试集

加 `--testset` 后，每张截图还会脱敏复制一份到 `tests/MaaEndTestset/<控制器>/<资源>/<任务目录>/`。
任务目录默认取流程文件所在的文件夹名（`tests/OutpostTrading/capture_flow.py` → `OutpostTrading`），用 `--testset-dir` 可以改；
资源目录默认 `官服`（对应 `Official_CN`），用 `--testset-resource` 可以改。

脱敏直接调 `.agents/skills/maaend-test-image/scripts/redact_test_image.py`，和手动导图是同一套规则：
按左下角 UID 文字判定平台，用纯绿色盖住该平台的固定 ROI，再按 OCR 兜住其它 `UID` / `#` 文字。
截图本身还是先落在 `.cache/test-capture/`（不提交），进仓库的只有脱敏后的图。

用例文件不自动生成，图导进去之后自己加到 `tests/<任务>/test_*.json`：`imageRoot` 写任务目录名，`hits` 填这一步用的节点。
每步截了哪些图、点了哪里，`manifest.json` 里都记着：每步一条，`shots` 里是图片名和导进测试集的路径，`clicked` 是点过的坐标。

一旦某个平台报错停下，剩下的步骤不会再有新图，此时别急着写用例，先看失败截图把流程修好。
