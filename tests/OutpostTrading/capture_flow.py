"""据点交易的截图流程。

用法见 tools/test_capture/README.md：

    uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller ADB --testset

跑完会把四号谷地、武陵两个地区各三个据点的界面，以及联络干员、更换货品、
交易完成、调度券见底这些界面截图存进 tests/MaaEndTestset/<控制器>/<资源>/OutpostTrading/。

每一步都用 enter 声明它要在哪个界面：不在就先按 route 转跳到对应的据点管理入口，
到了才开跑，所以中途哪一步被打断，下一步也能自己接上。
"""

from tools.test_capture import Capture, Flow, FlowError

flow = Flow("据点交易")

# 「其他据点」标记：派驻在别的据点的干员卡片上有这一行，各语言写法都列上。
OTHER_OUTPOST = ["其他据点", "其他據點", "Other outpost", "他の拠点", "다른 거점"]


@flow.step(
    enter="OutpostTradingInRefugeeCamp",
    route="SceneEnterMenuRegionalDevelopmentValleyIVOutpostManagement",
    stop=False,
)
def 四号谷地据点(c: Capture) -> None:
    c.shot("据点_难民暂居处")
    c.click("OutpostTradingCheckInfraStationTabText")

    c.shot_when("OutpostTradingInInfraStation", "据点_基建前站")
    c.click("OutpostTradingCheckReconstructionHQTabText")

    c.shot_when("OutpostTradingInReconstructionHQ", "据点_重建指挥部")
    c.click("CloseButtonType1")


@flow.step(
    enter="OutpostTradingInSkyKingFlatsConstructionSite",
    route="SceneEnterMenuRegionalDevelopmentWulingOutpostManagement",
    stop=False,
)
def 武陵据点(c: Capture) -> None:
    c.shot("据点_天王坪援建点")
    c.click("OutpostTradingCheckCardiacRemediationStationTabText")

    c.shot_when("OutpostTradingInCardiacRemediationStation", "据点_心脏修缮站")
    c.click("OutpostTradingCheckXiranflowCloudseederStationTabText")

    c.shot_when("OutpostTradingInXiranflowCloudseederStation", "据点_盈天台建设站")
    c.click("OutpostTradingCheckSkyKingFlatsConstructionSiteTabText")


@flow.step(
    enter="OutpostTradingInSkyKingFlatsConstructionSite",
    route="SceneEnterMenuRegionalDevelopmentWulingOutpostManagement",
    stop=False,
)
def 联络干员(c: Capture) -> None:
    c.click("OutpostTradingOpenOperatorLiaisonButton")

    c.shot_when("OutpostTradingInOperatorLiaison", "联络干员_当前派驻")
    c.click_at((660, 210))  # 第二张干员卡

    c.shot_when("OutpostTradingCheckAssignText", "联络干员_派驻按钮")

    # 派驻在别的据点的干员排在列表最下面：滑到底，挑一张带「其他据点」标记的卡片。
    # 只有这种干员，点派驻才会弹出「是否确认更改派驻」的确认框。
    badge = None
    for _ in range(6):
        c.run("OutpostTradingSwipeOperatorListForScan")  # 现成的「向下滑列表」动作
        c.sleep(0.5)
        badge = c.find_text(OTHER_OUTPOST, roi=(60, 110, 800, 560))
        if badge is not None:
            break
    if badge is None:
        # 谁都没派驻到别的据点时截不到这张，跳过，不影响其它界面
        print("  列表里没找到派驻在别的据点的干员，跳过「确认更改派驻」")
        c.click("CloseButtonType1")
        return
    c.click_at((badge[0] + 140, badge[1]))  # 标记在卡片左下角，往右一点才是卡片中间
    c.wait("OutpostTradingCheckAssignText")

    c.click("OutpostTradingCheckAssignText")
    for _ in range(10):
        if c.hit("OutpostTradingCheckOperatorAlreadyAssignedDialogText"):
            break
        c.sleep(0.4)
    if not c.hit("OutpostTradingCheckOperatorAlreadyAssignedDialogText"):
        # 没弹框说明选错人了，当前这张卡是没派驻过的，已经直接派过来，点撤下还原
        c.click("OutpostTradingCheckWithdrawText")
        raise FlowError("点派驻没弹确认框，可能选错了干员；已经点撤下还原")
    c.shot("联络干员_确认更改派驻")
    c.click("GrayCancelButton")  # 点取消，别真把人挪到当前据点

    c.wait("OutpostTradingInOperatorLiaison")
    c.click("CloseButtonType1")


@flow.step(
    enter="OutpostTradingInSkyKingFlatsConstructionSite",
    route="SceneEnterMenuRegionalDevelopmentWulingOutpostManagement",
    stop=False,
)
def 更换货品(c: Capture) -> None:
    c.click("OutpostTradingChangeGoods")

    c.shot_when("OutpostTradingCheckSelectGoodsText", "选择交易货品")
    c.click("CloseButtonType1")


@flow.step(
    enter="OutpostTradingInSkyKingFlatsConstructionSite",
    route="SceneEnterMenuRegionalDevelopmentWulingOutpostManagement",
    stop=False,
    once=True,
)
def 交易(c: Capture) -> None:
    if c.hit("OutpostTradingZeroMoney"):
        raise FlowError("据点调度券储量不足，一份都卖不了；等据点攒出一些券再跑")
    c.run("OutpostTradingSell")  # 现成的「交易」节点，自己会点交易按钮
    c.shot_when("OutpostTradingCheckResultHeader", "交易完成_获得物品", settle=1.0)
    c.run("OutpostTradingSellCheck")  # 现成的「交易后确认」节点，自己点 ✓


@flow.step(
    enter="OutpostTradingInSkyKingFlatsConstructionSite",
    route="SceneEnterMenuRegionalDevelopmentWulingOutpostManagement",
    stop=False,
    once=True,
)
def 清空调度券(c: Capture) -> None:
    """跑现成的 BetterSliding 流程：份数拉满，然后一路卖到券见底。"""
    try:
        c.sub_task("OutpostTradingSkyKingFlatsConstructionSiteBetterSliding")
    except FlowError:
        # 卖到券见底时这条链会以失败收尾，是不是真卖完了下面按界面确认
        print("  BetterSliding 任务以失败结束，按界面判断是否已经卖完")
    if not c.hit("OutpostTradingZeroMoney"):
        raise FlowError("跑完 BetterSliding 也没把券卖到见底，可能仓库货物不够")
    c.shot_when("OutpostTradingZeroMoney", "据点_调度券不足", settle=1.0)
    c.click("CloseButtonType1")
