# 开发手册 - 赠送干员礼物维护文档

本文说明 `GiftOperator` 的文件分布与收礼、送礼两个独立阶段。
该文档更新于 2026 年 10 月 8 日。

仅「只向信赖未满的干员送礼」（`AnyNonMaxTrust`）的送礼阶段不依赖已登记的干员头像、姓名表或选项名录。收礼、「任意干员」（`Any`）和指定干员仍使用维护名录。默认任务先收礼；如果未登记干员带有待收礼物，收礼仍可能因无法识别头像而中止，不能将整个任务视为无需维护。

## 文件路径

| 路径 | 作用 |
| ------------------------------------------------------------------- | ----------------------------- |
| `assets/interface.json` | 任务挂载（`dijiang_ship` 组） |
| `assets/tasks/GiftOperator.json` | 任务入口与界面选项 |
| `assets/resource/pipeline/GiftOperator/GiftOperatorMain.json` | 入口、帝江号定位 |
| `assets/resource/pipeline/GiftOperator/GiftOperatorNavigation.json` | 寻路与联络台接触 |
| `assets/resource/pipeline/GiftOperator/GiftOperatorContact.json` | 联络界面选人 |
| `assets/resource/pipeline/GiftOperator/GiftOperatorReceiveFlow.json` | 收礼选人、领取与每日五份计数 |
| `assets/resource/pipeline/GiftOperator/GiftOperatorGiftFlow.json` | 对话中的送礼 |
| `agent/go-service/giftoperator/` | 单人候选识别与送礼人数、成功/排除身份记录 |
| `tests/GiftOperator/test_gift_ui_status.json` | 送礼界面姓名、信赖、每日上限与预选提示识别测试 |
| `tests/GiftOperator/test_contact_candidates.json` | 联络界面信赖图标与收礼后原地恢复识别测试 |
| `assets/resource/pipeline/GiftOperator/GiftOperatorBagFull.json` | 背包已满处理 |
| `assets/resource/pipeline/GiftOperator/Operator/Operator.json` | 收礼阶段的干员识别与名称白名单 |
| `assets/resource/image/GiftOperator/` | Win32 识别图片 |
| `assets/resource_adb/image/GiftOperator/` | ADB 识别图片 |
| `assets/resource_adb/pipeline/GiftOperator/` | ADB Pipeline 镜像 |
| `tools/gift_operator/fill_gift_operator_green_box.py` | 干员头像 green_mask 格式化 |
| `assets/locales/interface/*.json` | 任务、选项与干员名称文案 |

## 新增干员时需改的路径

为收礼、「任意干员」及指定干员提供新干员支持时，至少需同步以下 7 处（`<Name>` 为干员标识，与模板文件名、option case 名保持一致）。`AnyNonMaxTrust` 的送礼阶段不读取这些干员登记；只为该模式支持新干员，无需新增头像模板或姓名条目。

| # | 路径 | 说明 |
| --- | -------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| 1 | `assets/resource/image/GiftOperator/Operators/<Name>.png` | Win32 干员头像模板；入库前须用 `tools/gift_operator/fill_gift_operator_green_box.py` 处理 |
| 2 | `assets/resource_adb/image/GiftOperator/Operators/<Name>.png` | ADB 干员头像模板；同上处理 |
| 3 | `assets/tasks/GiftOperator.json` → `SelectOperator` | 新增 case，在 UI 提供送礼对象，并配置 `GiftOperatorSendCandidate.attach.templates` 与送礼名称白名单 `GiftOperatorName` |
| 4 | `assets/resource/pipeline/GiftOperator/Operator/Operator.json` | 收礼阶段识别干员头像，并覆盖独立的名称白名单 `GiftOperatorReceiveName` |
| 5 | `assets/resource/pipeline/GiftOperator/GiftOperatorReceiveFlow.json` → `GiftOperatorSelectGiftOp.next` | 在收礼选人节点的 `next` 数组追加 `GiftOperatorSelect_<Name>`，否则该干员节点不会被触发 |
| 6 | `assets/resource/pipeline/GiftOperator/GiftOperatorGiftFlow.json` → `GiftOperatorSendCandidate.attach.operators` | 追加 `GiftOperatorSelect_<Name>`，让送礼候选器复用该干员的头像与五语言姓名 |
| 7 | `assets/locales/interface/*.json` → `operator.<Name>` | 各语言干员显示名称 |

## 路线一：默认（先收礼，再送礼）

对应选项「只收礼物」关闭。任务入口 `GiftOperatorMain` 通过 `SubTask` 顺序执行 `StashBackpackSubTask`、`GiftOperatorReceiveMain`、`GiftOperatorSendMain`。收礼完成后，才按配置的赠送对象、送礼人数与赠礼目标执行送礼。

送礼对象选项 `SelectOperator` 提供「任意干员」、各指定干员和「只向信赖未满的干员送礼」（`AnyNonMaxTrust`）。「送礼人数」（`GiftOperatorCount`）为独立的正整数输入，默认 `1`；「赠礼目标」（`GiftRingCount`）选择送满一环、两环或三环，默认 `ThreeRings`（送满三环）。目标是该干员当天累计已满环数，包含先前已经完成的进度，不是本次额外赠送的环数或固定礼物件数。这些配置只影响送礼阶段，不影响前面的每日五份收礼。

1. 任务开始前存放背包。
2. 执行独立收礼阶段，逐一领取每日五份礼物；若当天已领取过部分礼物，扫描完剩余礼物后结束该阶段。具体流程见[路线二](#路线二只收礼物)。
3. 收礼结束并确认返回帝江号大世界后，启动送礼阶段，再打开干员联络界面。
4. 每轮只选择一名送礼对象（点击后须经[选中态校验](#选中态校验)确认，实现见 `GiftOperatorContact.json`）：
    - **任意干员**：切换为信赖度升序，候选器以已有的 31 名干员头像识别当前列表中的目标，在每张卡片内单独读取信赖，按行、列顺序选择未满信赖且尚未成功或排除的身份；信赖已为 `200%` 或无法确认时跳过该卡片。
    - **只向信赖未满的干员送礼**：设置 `GiftOperatorSendCandidate.attach.generic` 为 `true`，从通用信赖图标定位卡片，读取卡片信赖，并截取不含信息按钮、边框和信赖文字的头像核心作为运行时模板。只按本次任务的临时头像身份筛选和排除，不读取 `attach.operators`、固定头像或五语言姓名表。
    - **指定干员**：`GiftOperatorSendCandidate.attach.templates` 仅包含该干员头像，候选器只选择该身份。成功送礼或排除后不会重复选择；指定目标耗尽时结束并报告未完成人数，不为填满人数而重复送礼。
5. 确认呼唤；若干员未到位，按[朝向修正与单次传送恢复](#召唤干员后找不到对话按钮怎么办)（实现见 `GiftOperatorNavigation.json`）。
6. 等待干员出现，进入对话并打开送礼界面。所有送礼对象选项均初次读取当天累计已满环数、当前环的高/中/低档进度、信赖与固定每日上限文字；这些累计进度在重新打开送礼界面后仍保留。当天已达到所选环数目标、信赖已为 `200%` 或今日已满时，排除该身份，不减少待送礼人数。
7. 可以赠送时，每次只点击一件礼物，随后走[选中态校验](#选中态校验)并重新识别预览进度；未达到目标时继续选择。预览达到目标，或三环已满、今日上限/`200%` 信赖进度封顶时，结束选择并确认赠送、跳过对话、离开。回到大世界后，再与同一干员对话并重开送礼界面，核验实际累计环数及封顶状态；预览不作为成功依据。
8. 只有 `observe_after` 核对并提交成功，才把 `remaining` 减一、将身份加入 `completed` 并从后续候选中排除。剩余人数不为零时继续下一位；扫描后没有可用目标时结束并报告未完成人数，包括人数大于可用目标数或指定单人已耗尽的情况。`finish` 汇总成功名单、排除名单与剩余人数；剩余人数大于零时返回未完成错误。

Go 的 `GiftOperatorCandidateRecognition` 按 `attach.generic` 区分两条识别路径。`false` 用于「任意干员」及指定干员：按头像模板文件名生成固定身份标识，复用 `Operator/Operator.json` 的头像与五语言姓名；`attach.templates` 为空表示使用已登记干员，指定列表则限制候选。`true` 仅用于 `AnyNonMaxTrust`：使用通用信赖图标、运行时头像和现场姓名。所有其他选项须显式设置 `generic: false`，避免选项覆盖残留。`GiftOperatorSessionAction` 只维护赠礼目标、待完成人数、身份、观察与成功/排除记录；所有界面操作和阶段切换仍由 Pipeline 负责。

### 信赖未满模式：运行时身份校验

1. 通过通用 `GiftOperatorTrustIcon` 定位卡片，保存本轮头像核心；点击前重新匹配同一运行时模板，再校验只选中一人。已成功或今日已满的临时头像不会再次入选；信赖排序可能变化，不能用卡片序号排除。
2. `GiftOperatorSendSetGenericDialoguePhase` 命中后，将呼唤确认使用的对话锚点切换到 `GiftOperatorSendFindDialogue`。`GiftOperatorDialogueRecognition` 现场读取对话提示姓名，避开已处理对象和本轮误遇姓名，再限制 `GiftOperatorName` 后进入对话。
3. 进入赠礼界面后，先在右上头像区域匹配本轮运行时头像，再通过 `GiftOperatorGiftName` 读取实际姓名。头像通过才绑定赠礼姓名和世界对话姓名、记录初始累计环进度、信赖及每日上限状态。两处姓名可有不同 OCR 拼写，分别保存，不依赖五语言名录。
4. 如果遇到旁边残留的旧干员，`GiftOperatorSendWrongRecipient` 只记录当前遭遇姓名供本轮避开，然后关闭界面、离开对话并继续寻找本轮目标；保留临时头像和剩余人数，不将待召集对象标为成功或排除。
5. 提交后再次进入同一干员的赠礼界面，必须重新通过运行时头像及已绑定赠礼姓名校验，再确认实际累计环进度达到目标或实际封顶状态，才能提交成功计数。头像、姓名或成功证据无法确认时保留未完成计数；不得将识别失败当作送礼成功。

新干员通常无需为此送礼模式登记；联络卡片布局、赠礼头像缩放或姓名位置变化仍需维护通用识别。各控制器的布局须分别验证，不能仅按信赖图标大小同比推算头像尺寸。既有今日上限截图用于姓名、信赖和状态识别，不是成功送礼的正例。模拟未知干员时应使用隔离资源副本移除其登记，并单独验证送礼入口，避免前面的名录收礼阶段先行阻断。

成功依据使用提交后重新打开的稳定送礼界面，核验实际累计环进度达到目标；实际三环已满、固定文字「[今日赠礼可提升的信赖]已达上限，请明天再来吧」或 `200%` 信赖进度封顶也可结束本轮送礼。预览环数和选礼物时的提示「[今日赠礼可提升的信赖]已达上限，无法选择更多」只用于停止继续预选，不能单独作为送礼成功或人数扣减的证据。

成功计数不再要求信赖整数百分比增长，而以提交后确认的实际累计环进度和封顶状态为准。若实际进度未达到目标且没有确认封顶，或进度无法识别，则保留未完成人数并停止，不把预览或点击成功当作实际送礼完成。

两个阶段共用联络台导航与呼唤确认，但使用独立的选人、对话与名称白名单。`GiftOperatorCheckContact.next` 经 `[Anchor]GiftOperatorSelectPhase` 分派到收礼或送礼选人；`GiftOperatorConfirmSelect.next` 经 `[Anchor]GiftOperatorWaitChatPhase` 分派到收礼的 `GiftOperatorReceiveWaitChat`、固定身份送礼的 `GiftOperatorWaitChat` 或通用送礼的 `GiftOperatorSendFindDialogue`。收礼只覆盖 `GiftOperatorReceiveName`，不会改变送礼选项配置的 `GiftOperatorName`。

## 路线二：只收礼物

对应选项「只收礼物」开启，通过禁用 `GiftOperatorSendMain`，在收礼阶段结束后直接结束任务。收礼流程与默认路线的第一阶段完全相同，不受赠送对象、送礼人数和赠礼目标影响；不再提供「接受全部礼物」选项。

1. 同样先存放背包，再寻路至干员联络台。
2. 每轮先通过 `GiftOperatorReceiveListToTop` 反向滑动，并用 `ListCompleteRecognition` 确认联络列表回到顶部，避免保留的滚动位置漏掉前面的礼物；随后在列表中[识别带礼物图标的干员](#收礼模式如何正确选中目标干员)（实现见 `GiftOperatorReceiveFlow.json` 与 `Operator/Operator.json`），而非按信赖排序或指定干员选人。
3. 确认呼唤，进入对话，只点击「收下礼物」，不进入送礼分支。
4. 领取后跳过对话并离开，`GiftOperatorReceiveBackInWorld` 确认 `InDijiangWorld` 后，才通过 `GiftOperatorReceiveContinue` 开始下一轮收礼。
5. `GiftOperatorReceiveContinue.max_hit` 为 `4`，初次领取加后续四轮，共最多领取五份。计数只在收礼阶段入口清空，寻找下一份礼物时不会清空；该限制对应每日五份礼物，不是失败重试。
6. 若当天剩余不足五份，当前页无礼物时继续滑动，通过 `ListCompleteRecognition` 确认列表滑动前后不再变化，再关闭联络界面；确认返回大世界后结束收礼阶段。
7. 若背包已满，提示后结束任务。

## 特殊处理

预选数量只表示本次尚未提交的礼物，收益预览已包含当天此前的赠礼。例如此前已送一件时，本次预选 29 件对应当天累计 30 件、第一环填满；29、59、74 不是固定停选阈值。游戏环从 0 编号，选项中的「一环」表示第 0 环填满，「两环」表示第 0、1 环填满。三环通过持续逐件选礼，直到游戏提示不能再选择，然后提交并重新打开核验；此前赠礼无需重新从零计算。

### 选中态校验

本任务里「点一下」不等于「选中了」。联络台先校验选中颜色、文字底色与序号；送礼界面则读取黄色标签中的完整数量，并要求每次点击后总选数增加。

```text
选中高亮颜色 → 高亮区域内的文字底色 → OCR 读取关键文字
```

#### 联络台选干员

实现位于 `GiftOperatorContact.json`。每次点击列表行后，用 `And` 同时满足：

1. **标签高亮颜色**：识别该行选中态的 HSV 色块（青绿色标签底）。
2. **序号文字底色**：以上一步命中区域为锚，再识别序号数字所在的文字底色。
3. **序号 OCR**：当前收礼和送礼每轮都只选一人，读取序号 `1` 确认目标已入列。

送礼候选器先记录单个干员身份，再重新识别该身份的头像并点击，避免沿用上一帧的点击框。所有送礼对象与收礼路线在点中目标后，校验序号 `1` 无误再点确认呼唤。

#### 送礼界面选礼物

实现位于 `GiftOperatorGiftFlow.json`：

1. 先用颜色匹配在底部礼物栏定位可点击项并点击。
2. 定位每个已选礼物格的黄色数量标签。
3. 排除标签内的图标和下方边框，OCR 读取完整正整数并汇总所有标签，再读取本次预选后的累计环进度。每次点击后都重新识别；达到目标或预览封顶后才点击「确认赠送」，提交后的成功仍须重新进入界面核验。

维护时若选中态识别漂移，优先检查这三层的颜色阈值与 OCR 区域偏移，干员与礼物两处应对照排查。

### 收礼模式：如何正确选中目标干员

收礼不能靠干员名字 OCR 直接点列表，而是**先找礼物、再认头像、最后校验名字**，逻辑分布在 `GiftOperatorReceiveFlow.json` 与 `Operator/Operator.json`。

1. **第一步：定位「有礼物的行」**  
   在联络列表区域用 `Gift.png` / `Gift_2.png` 模板匹配礼物图标（`green_mask`）。命中后偏移到相邻的点击区域，选中该行干员。

2. **第二步：确认是哪位干员**  
   以礼物图标命中位置为锚点，在相邻区域二次匹配该干员头像（`Operators/<Name>.png`，同样 `green_mask`）。  
   匹配成功后，把收礼对话使用的独立名称 OCR 白名单 `GiftOperatorReceiveName` 改成这名干员的多语言名字，不修改送礼白名单 `GiftOperatorName`。
   这一步写在 `Operator/Operator.json`，每名干员各一条；新增干员时必须同步维护。

    > **举例**：联络列表里礼物行旁二次匹配到 `Operators/Gilberta.png`，白名单即收窄为「洁尔佩塔 / Gilberta / …」仅这名干员。呼唤后在大世界等待对话时，须同时看到对话图标且名称 OCR 命中该白名单才会点击；场上出现佩丽卡、伊冯等其他干员时，名称对不上，**不会误点**。

3. **第三步：确认选中态**  
   复用上方[选中态校验](#选中态校验)逻辑，确认列表行高亮且序号为 `1`，再点击黄色确认按钮呼唤。

4. **第四步：对话阶段二次校验**  
   干员到场后，同时识别「对话图标」和「干员名称 OCR」，两者都命中才发起交互。  
   这样即使列表里点中了礼物行，也能在对话前再挡一次「叫错人」的情况。

头像模板必须经过 `fill_gift_operator_green_box.py` 处理（绿色描边 + 右上角遮罩），否则 `green_mask` 匹配不稳定。Win32 与 ADB 各有一套图片，需分别处理。

每轮查找先由 `GiftOperatorReceiveListToTop` 回到顶部，再由 `GiftOperatorReceiveSelect` 向下扫描。当前屏找不到带礼物的干员时，`GiftOperatorReceiveSwipe` 滑动列表并等待画面稳定后继续扫描；`GiftOperatorReceiveListComplete` 使用 `ListCompleteRecognition` 比较滑动前后的列表，确认列表不再变化且当前页没有礼物后，结束收礼阶段。`GiftOperatorReceiveRoundEntry` 每轮同时重置 `GiftOperatorReceiveListTopComplete` 与 `GiftOperatorReceiveListComplete` 的 `attach.ready`，收礼份数保持不变。

### 召唤干员后：找不到对话按钮怎么办

呼唤前后，任务先识别联络台或目标干员的对话入口；未命中时按已接线的朝向进行站位修正，再重新识别。逻辑在 `GiftOperatorNavigation.json`，每步遵循「识别对话 → 修正朝向 → 再识别对话」，不沿用旧的三组坐标移动兜底。

当前修正顺序与每轮 `max_hit` 如下：

| 次序 | 节点 | 朝向 | `max_hit` |
| ---- | -------------------------------- | ---------------- | --------- |
| 1 | `GiftOperatorTurnEast` | 正东（90°） | 2 |
| 2 | `GiftOperatorTurnNorthwest` | 西北（315°） | 1 |
| 3 | `GiftOperatorTurnNorthByWest` | 北偏西（345°） | 1 |
| 4 | `GiftOperatorTurnWest` | 正西（270°） | 2 |
| 5 | `GiftOperatorTurnSoutheast` | 东南（135°） | 2 |

仅使用当前流程中启用的朝向节点；未接线的坐标移动节点不参与修正，不应据此补造新的移动坐标。

若本轮从原地或非传送点出发，寻路或呼唤前后的站位修正失败后，经 `SceneEnterWorldDijiang0` 传送，再从传送点完整走一遍路线。若本轮已经从传送点出发，或这次恢复后再次失败，则通过 `ERROR` 结束，不再额外传送。

每轮初始化导航恢复锚点及朝向修正次数，避免上一轮的恢复状态残留；恢复不清空已收礼份数，也不重置送礼 session。送礼恢复时重新召集 `Pending` 中的同一名干员，保留身份、剩余人数和已处理记录，不能重新 `reserve` 新候选。

传送恢复仅覆盖赠礼提交前的寻路及呼唤阶段。礼物提交后重新进入界面的身份、实际环进度或成功验证失败，不接入该恢复路径；保留未完成人数并结束，避免重复赠送。

另外两处同类重试，用于应对干员走过来导致点击偏移：

- 找到对话按钮但点完没进对话 → 原地再试一次点击。
- 进了对话但右侧动作按钮还没出来 → 跳过按钮也会自我重试一次，再等赠送 / 收礼按钮出现。

### 送礼阶段选人的差异（对比收礼）

送礼每轮选择一人，不需要先找礼物图标。`AnyNonMaxTrust` 使用通用信赖图标、运行时头像和现场姓名；「任意干员」与指定干员仍使用固定头像身份，指定干员同时限制 `attach.templates`。收礼仍先找礼物图标，再通过维护名录确认头像和姓名。成功计数只在送礼验证后提交，排除依据为对应模式的身份。
