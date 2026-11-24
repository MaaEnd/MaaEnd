"""转交委托：武陵第一个仓储节点（武陵城）装箱、竞价、快速转交的截图流程。

一天开始时的线性流程，一条路走到黑，中间不做判断：
武陵仓储节点 → 点「货物装箱」→ 选类型 → 装满 → 下一步（游戏自己完成竞价）→ 调度申请
→ 快速转交 → 确认 → 获得奖励 → 我转交的委托 → 回仓储节点。

分成两截：前面的仓储节点、装箱界面（连装满箱子）随时退得出来，两台平台都能拍；
点「下一步」才把货交出去、最后把委托转出去，那一步只能做一次，所以标了 once，
第二个平台跑到那儿直接跳过，不会因为箱子已经交出去而卡住。

跑之前游戏里得有一个还没接取的运送委托可以装箱，武陵城仓储节点也要 3 级以上：
不到 3 级，调度申请界面上只有黄底「开始运送」，没有白底「快速转交」，那一步会等不到界面而报错。

装箱、调度申请这些界面没有现成节点能一步跳进去，只能从仓储节点点进去，所以下面给这几步
写了「怎么过去」的流程函数当 route；实在回不去的地方（委托已经转出去了）会直接报错，
提示用 --resume 接着上次的运行目录跑。

这个流程会**真的消耗仓库货物、真的把委托快速转交出去**（拿定金、委托交给别人派送）。

用法见 tools/test_capture/README.md：

    uv run test-capture --flow tests/DeliveryJobs/capture_flow.py --controller Win32-Front --testset
"""

from tools.test_capture import Capture, Flow, FlowError

flow = Flow("转交委托")


# ---- 下面几个是「不在这个界面时怎么过去」，给步骤的 route 用 --------------------


def 回装箱界面(c: Capture) -> None:
    """从仓储节点点进货物、选好类型，回到「装满」的状态。"""
    c.goto(
        "DeliveryJobsInWulingLocalDepotNode",
        "SceneEnterMenuRegionalDevelopmentWulingDepotNode",
    )
    c.run("DeliveryJobsEnterWulingCityCargo")
    c.wait("DeliveryJobsInCargoPackGoods")
    c.run("DeliveryJobsSelectTypeOfGoodsToPackNextStep")
    c.wait("DeliveryJobsCargoFillToMax")
    c.run("DeliveryJobsCargoFillToMax")
    c.wait("DeliveryJobsCheckCargoFilledToMax")


def 回调度申请(c: Capture) -> None:
    """从仓储节点点「查看报价」进调度申请；货装满了、报价也还在的时候才到得了。"""
    c.goto(
        "DeliveryJobsInWulingLocalDepotNode",
        "SceneEnterMenuRegionalDevelopmentWulingDepotNode",
    )
    c.run("DeliveryJobsEnterWulingCityCargo")
    c.wait("DeliveryJobsInCargoRedistributionBid")


def 回快速转交确认(c: Capture) -> None:
    """调度申请还在的话，重新点一次「快速转交」把确认框叫回来。"""
    c.goto("DeliveryJobsInCargoRedistributionBid", 回调度申请)
    c.run("DeliveryJobsQuickTransfer")
    c.wait("DeliveryJobsCheckQuickTransferConfirmText")


def 回我转交的委托(c: Capture) -> None:
    """从仓储节点点开「我转交的委托」标签页。"""
    c.goto(
        "DeliveryJobsInWulingLocalDepotNode",
        "SceneEnterMenuRegionalDevelopmentWulingDepotNode",
    )
    c.run("ClaimDeliveryJobsClickMyTransferredTab")
    c.wait("InClaimDeliveryJobsNode")


def 回不去(c: Capture) -> None:
    """这一步只能顺着上一步走：委托已经转出去，别的界面到不了这里。"""
    raise FlowError(
        "这一步只能顺着上一步走：委托已经转交出去，换个界面进不到这里；状态不对就用 --resume 接着上次的运行目录跑"
    )


@flow.step(
    enter="DeliveryJobsInValleyIVLocalDepotNode",
    route="SceneEnterMenuRegionalDevelopmentValleyIVDepotNode",
    stop=False,
)
def 四号谷地仓储节点(c: Capture) -> None:
    c.shot("四号谷地_仓储节点_货物装箱")


@flow.step(
    enter="DeliveryJobsInWulingLocalDepotNode",
    route="SceneEnterMenuRegionalDevelopmentWulingDepotNode",
    stop=False,
)
def 武陵仓储节点(c: Capture) -> None:
    c.shot("武陵_仓储节点_货物装箱")


@flow.step(
    enter="DeliveryJobsInWulingLocalDepotNode",
    route="SceneEnterMenuRegionalDevelopmentWulingDepotNode",
)
def 武陵城货物装箱(c: Capture) -> None:
    """进装箱界面、选类型、把箱子装满。到这儿都还没交出去，退出去也不亏。"""
    c.run("DeliveryJobsEnterWulingCityCargo")
    c.shot_when("DeliveryJobsInCargoPackGoods", "选择货物类型")

    c.run("DeliveryJobsSelectTypeOfGoodsToPackNextStep")
    c.shot_when("DeliveryJobsCargoFillToMax", "货物装箱")  # 填充界面出来了，右上有「装满」按钮

    c.run("DeliveryJobsCargoFillToMax")  # 装满了只是货进了箱子，还没交出去
    c.shot_when("DeliveryJobsCheckCargoFilledToMax", "货物装箱完成")


@flow.step(
    enter="DeliveryJobsInCargoPackGoods",
    route=回装箱界面,
    stop=False,
    once=True,
)
def 武陵城装满货物(c: Capture) -> None:
    # 点「下一步」才算提交：货交出去、竞价游戏自己完成，到这儿就回不了头了
    c.run("DeliveryJobsFillToMaxNextStep")
    c.shot_when("DeliveryJobsCargoBid", "货物竞价")
    c.run("DeliveryJobsCargoBid")


@flow.step(
    enter="DeliveryJobsInCargoRedistributionBid",
    route=回调度申请,
)
def 武陵城调度申请(c: Capture) -> None:
    """只看不确认：点开快速转交截一张确认框，然后点取消，别的平台还能再来一次。"""
    c.shot("调度申请")

    c.run("DeliveryJobsQuickTransfer")
    c.shot_when("DeliveryJobsCheckQuickTransferConfirmText", "快速转交确认")


@flow.step(
    enter="DeliveryJobsCheckQuickTransferConfirmText",
    route=回快速转交确认,
    stop=False,
    once=True,
)
def 武陵城确认转交(c: Capture) -> None:
    """真确认：到这一步委托才转出去，从这儿起都只能做一次。"""
    c.run("DeliveryJobsQuickTransferConfirm")
    c.shot_when("DeliveryJobsCheckRewardHeader", "获得奖励")


@flow.step(
    enter="DeliveryJobsCheckRewardHeader",
    route=回不去,
    stop=False,
    once=True,
)
def 武陵城关掉奖励(c: Capture) -> None:
    """关掉获得奖励界面，落到「我转交的委托」页。"""
    c.run("DeliveryJobsQuickTransferReward")
    c.wait("InClaimDeliveryJobsNode")


@flow.step(
    enter="InClaimDeliveryJobsNode",
    route=回我转交的委托,
    stop=False,
    once=True,
)
def 武陵城我转交的委托(c: Capture) -> None:
    c.shot("我转交的委托")


@flow.step(
    enter="InClaimDeliveryJobsNode",
    route=回我转交的委托,
    once=True,
)
def 武陵城回仓储节点(c: Capture) -> None:
    """点回「本地仓储节点」标签页，回到仓储节点界面再截一张转交后的样子。"""
    c.run("DeliveryJobsQuickTransferBackToLocalDepotNode")
    c.shot_when("DeliveryJobsInWulingLocalDepotNode", "转交后仓储节点")
