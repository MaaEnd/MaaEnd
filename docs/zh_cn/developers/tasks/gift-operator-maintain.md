# 开发手册 - 赠送干员礼物维护文档

本文说明 `GiftOperator` 的文件分布与两条执行路线。  
该文档更新于 2026 年 6 月 6 日。

## 文件路径

| 路径                                                                | 作用                          |
| ------------------------------------------------------------------- | ----------------------------- |
| `assets/interface.json`                                             | 任务挂载（`dijiang_ship` 组） |
| `assets/tasks/GiftOperator.json`                                    | 任务入口与界面选项            |
| `assets/resource/pipeline/GiftOperator/GiftOperatorMain.json`       | 入口、帝江号定位              |
| `assets/resource/pipeline/GiftOperator/GiftOperatorNavigation.json` | 寻路与联络台接触              |
| `assets/resource/pipeline/GiftOperator/GiftOperatorContact.json`    | 联络界面选人                  |
| `assets/resource/pipeline/GiftOperator/GiftOperatorGiftFlow.json`   | 对话中的送礼 / 收礼           |
| `assets/resource/pipeline/GiftOperator/GiftOperatorBagFull.json`    | 背包已满处理                  |
| `assets/resource/pipeline/GiftOperator/Operator/Operator.json`      | 只收礼物模式的干员识别        |
| `assets/resource/image/GiftOperator/`                               | Win32 识别图片                |
| `assets/resource_adb/image/GiftOperator/`                           | ADB 识别图片                  |
| `assets/resource_adb/pipeline/GiftOperator/`                        | ADB Pipeline 镜像             |
| `tools/gift_operator/fill_gift_operator_green_box.py`               | 干员头像 green_mask 格式化    |
| `assets/locales/interface/*.json`                                   | 任务、选项与干员名称文案      |

## 路线一：默认（送礼 + 收礼）

对应选项「只收礼物」关闭。可配置赠送对象与赠送数量。

1. 任务开始前存放背包，再进入帝江号舰桥大世界。
2. 寻路至干员联络台并打开联络界面。
3. 选人（点击后须经[选中态校验](#选中态校验)确认，实现见 `GiftOperatorContact.json`）：
    - **任意**：切换为信赖度升序，连选三名信赖未满的干员；每点一人校验序号 1 → 2 → 3，三人齐备后才确认呼唤。
    - **指定干员**：在列表中用头像模板匹配目标干员，命中后同样校验选中态再呼唤。
4. 确认呼唤；若干员未到位，按[预设朝向与坐标移动兜底](#召唤干员后找不到对话按钮怎么办)（实现见 `GiftOperatorNavigation.json`）。
5. 等待干员出现，进入对话。
6. 对话中按优先级处理：
    - 能赠送 → 选礼物（选中后同样走[选中态校验](#选中态校验)，实现见 `GiftOperatorGiftFlow.json`）、确认赠送、跳过对话、离开。
    - 能收礼 → 领取礼物、跳过对话、离开。
    - 好感已满 → 直接离开。
7. 赠送次数由「赠送数量」选项控制，默认可连送多件。

## 路线二：只收礼物

对应选项「只收礼物」开启。不再主动送礼，只领取干员送来的礼物。

1. 同样先存放背包，再寻路至干员联络台。
2. 在联络列表中[识别带礼物图标的干员](#收礼模式如何正确选中目标干员)（实现见 `GiftOperatorContact.json` 与 `Operator/Operator.json`），而非按信赖排序或指定干员选人。
3. 确认呼唤，进入对话，优先点击「收下礼物」。
4. 领取后：
    - **接受全部礼物**关闭 → 跳过对话后离开，任务结束。
    - **接受全部礼物**开启 → 回到任务开头，继续找下一名有礼物的干员；直到联络界面无可选干员才离开。
5. 若背包已满，提示后结束任务。

## 特殊处理

### 选中态校验

本任务里「点一下」不等于「选中了」，联络台选干员与送礼界面选礼物共用同一套**三层串联**判断，避免空点或点偏：

```text
选中高亮颜色 → 高亮区域内的文字底色 → OCR 读取关键文字
```

#### 联络台选干员

实现位于 `GiftOperatorContact.json`。每次点击列表行后，用 `And` 同时满足：

1. **标签高亮颜色**：识别该行选中态的 HSV 色块（青绿色标签底）。
2. **序号文字底色**：以上一步命中区域为锚，再识别序号数字所在的文字底色。
3. **序号 OCR**：在文字区域内读取 `1` / `2` / `3`，与当前应选的第几人对应。

「任意」路线据此逐步确认第一、二、三名干员均已入列；「指定干员」与收礼路线在点中目标后，校验序号 `1` 无误再点确认呼唤。

#### 送礼界面选礼物

实现位于 `GiftOperatorGiftFlow.json`，链路相同，仅锚点与 OCR 目标不同：

1. 先用颜色匹配在底部礼物栏定位可点击项并点击。
2. 再校验礼物格的选中高亮颜色 + 文字底色。
3. 最后 OCR 读取该格内的数量数字（`\d+`），确认礼物确实处于选中态，才继续点「确认赠送」。

维护时若选中态识别漂移，优先检查这三层的颜色阈值与 OCR 区域偏移，干员与礼物两处应对照排查。

### 收礼模式：如何正确选中目标干员

收礼不能靠干员名字 OCR 直接点列表，而是**先找礼物、再认头像、最后校验名字**，逻辑分布在 `GiftOperatorContact.json` 与 `Operator/Operator.json`。

1. **第一步：定位「有礼物的行」**  
   在联络列表区域用 `Gift.png` 模板匹配礼物图标（`green_mask`）。命中后向左侧偏移点击，选中该行干员。

2. **第二步：确认是哪位干员**  
   以礼物图标命中位置为锚点，在相邻区域二次匹配该干员头像（`Operators/<Name>.png`，同样 `green_mask`）。  
   匹配成功后，临时把后续对话阶段使用的干员名称 OCR 白名单改成这名干员的多语言名字。  
   这一步写在 `Operator/Operator.json`，每名干员各一条；新增干员时必须同步维护。

    > **举例**：联络列表里礼物行旁二次匹配到 `Operators/Gilberta.png`，白名单即收窄为「洁尔佩塔 / Gilberta / …」仅这名干员。呼唤后在大世界等待对话时，须同时看到对话图标且名称 OCR 命中该白名单才会点击；场上出现佩丽卡、伊冯等其他干员时，名称对不上，**不会误点**。

3. **第三步：确认选中态**  
   复用上方[选中态校验](#选中态校验)逻辑，确认列表行高亮且序号为 `1`，再点击黄色确认按钮呼唤。

4. **第四步：对话阶段二次校验**  
   干员到场后，同时识别「对话图标」和「干员名称 OCR」，两者都命中才发起交互。  
   这样即使列表里点中了礼物行，也能在对话前再挡一次「叫错人」的情况。

头像模板必须经过 `fill_gift_operator_green_box.py` 处理（绿色描边 + 右上角遮罩），否则 `green_mask` 匹配不稳定。Win32 与 ADB 各有一套图片，需分别处理。

当前屏找不到带礼物的干员时，列表最多滑动 2 次；仍找不到则落入「无可选干员」，在开启「接受全部礼物」时可作为整轮结束条件。

### 召唤干员后：找不到对话按钮怎么办

呼唤确认后，任务会先等干员出现并尝试点击对话入口。若此时画面上还看不到可交互的对话按钮，不会一直傻等，而是进入**站位修正兜底**，逻辑在 `GiftOperatorNavigation.json`。

修正顺序固定为三组预设，每组各尝试一次（任务开头会清零计数，避免上轮残留）：

| 次序 | 朝向         | 移动目标       |
| ---- | ------------ | -------------- |
| 1    | 正西（270°） | (186.6, 175.0) |
| 2    | 正北（0°）   | (188.0, 175.3) |
| 3    | 正东（90°）  | (188.6, 176.2) |

每组都是「先转向 → 再短距离移动 → 等待角色停稳」，然后重新尝试寻找对话按钮。

若三组都试过仍找不到，任务报错结束并提示「寻找干员识别失败」，同时保留截图供排查。常见原因是干员刷在了预设区域外，或呼唤后站位与模板 ROI 偏差过大。

另外两处同类重试，用于应对干员走过来导致点击偏移：

- 找到对话按钮但点完没进对话 → 原地再试一次点击。
- 进了对话但右侧动作按钮还没出来 → 跳过按钮也会自我重试一次，再等赠送 / 收礼 / 好感已满按钮出现。

### 默认模式选人的差异（对比收礼）

「任意」路线不靠头像，而是：切信赖度升序 → 从上到下找**信赖未满且未被选中**的行，连点三名。  
「指定干员」路线与收礼第二步类似，直接在列表里用头像模板匹配，但不需要先找礼物图标。

## 新增干员时需改的路径

1. `assets/resource/image/GiftOperator/Operators/<Name>.png`
2. `assets/resource_adb/image/GiftOperator/Operators/<Name>.png`（跑 `fill_gift_operator_green_box.py`）
3. `assets/tasks/GiftOperator.json`（`SelectOperator` 新增 case）
4. `assets/resource/pipeline/GiftOperator/Operator/Operator.json`（收礼模式干员识别）
5. `assets/locales/interface/*.json`（`operator.<Name>` 与多语言名称）
