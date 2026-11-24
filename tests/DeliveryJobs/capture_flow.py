"""转交委托：武陵第一个仓储节点（武陵城）装箱、竞价、快速转交的截图流程。

一天开始时的线性流程，一条路走到黑，中间不做判断：
武陵仓储节点 → 点「货物装箱」→ 选类型 → 装满 → 下一步（游戏自己完成竞价）→ 调度申请
→ 快速转交 → 确认 → 获得奖励 → 我转交的委托 → 回仓储节点。

分成两截：前面的仓储节点、装箱界面随时退得出来，两台平台都能拍；
从「装满货物」开始会消耗仓库货物、最后把委托转出去，只能做一次，所以标了 once，
第二个平台跑到这儿直接跳过，不会因为没货可装而卡住。

跑之前游戏里得有一个还没接取的运送委托可以装箱，武陵城仓储节点也要 3 级以上：
不到 3 级，调度申请界面上只有黄底「开始运送」，没有白底「快速转交」，那一步会等不到界面而报错。

这个流程会**真的消耗仓库货物、真的把委托快速转交出去**（拿定金、委托交给别人派送）。

用法见 tools/test_capture/README.md：

    uv run test-capture --flow tests/DeliveryJobs/capture_flow.py --controller Win32-Front --testset
"""

from tools.test_capture import Capture, Flow

flow = Flow("转交委托")


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
    """进装箱界面、选货物类型。到这儿还没消耗东西，退出去也不亏。"""
    c.run("DeliveryJobsEnterWulingCityCargo")
    c.wait("DeliveryJobsInCargoPackGoods")
    c.shot("选择货物类型")

    c.run("DeliveryJobsSelectTypeOfGoodsToPackNextStep")
    c.wait("DeliveryJobsCargoFillToMax")  # 填充界面出来了，右上有「装满」按钮
    c.shot("货物装箱")


@flow.step(
    enter="DeliveryJobsInCargoPackGoods",
    route="SceneEnterMenuRegionalDevelopmentWulingDepotNode",
    stop=False,
    once=True,
)
def 武陵城装满货物(c: Capture) -> None:
    c.run("DeliveryJobsCargoFillToMax")  # 货进箱子了，从这儿开始消耗仓库货物
    c.wait("DeliveryJobsCheckCargoFilledToMax")
    c.shot("货物装箱完成")

    # 这一步之后就没法重来了：点完直接到调度申请，竞价游戏自己完成
    c.run("DeliveryJobsFillToMaxNextStep")
    c.wait("DeliveryJobsInCargoRedistributionBid")


@flow.step(
    enter="DeliveryJobsInCargoRedistributionBid",
    route="SceneEnterMenuRegionalDevelopmentWulingDepotNode",
    stop=False,
    once=True,
)
def 武陵城调度申请(c: Capture) -> None:
    c.shot("调度申请")

    c.run("DeliveryJobsQuickTransfer")
    c.wait("DeliveryJobsCheckQuickTransferConfirmText")
    c.shot("快速转交确认")


@flow.step(
    enter="DeliveryJobsCheckQuickTransferConfirmText",
    route="SceneEnterMenuRegionalDevelopmentWulingDepotNode",
    stop=False,
    once=True,
)
def 武陵城确认转交(c: Capture) -> None:
    # 确认之后委托就交给别人了，后面几步都只能做一次
    c.run("DeliveryJobsQuickTransferConfirm")
    c.wait("DeliveryJobsCheckRewardHeader")
    c.shot("获得奖励")


@flow.step(
    enter="DeliveryJobsCheckRewardHeader",
    route="SceneEnterMenuRegionalDevelopmentWulingDepotNode",
    stop=False,
    once=True,
)
def 武陵城关掉奖励(c: Capture) -> None:
    """关掉获得奖励界面，落到「我转交的委托」页。"""
    c.run("DeliveryJobsQuickTransferReward")
    c.wait("InClaimDeliveryJobsNode")


@flow.step(
    enter="InClaimDeliveryJobsNode",
    route="SceneEnterMenuRegionalDevelopmentWulingDepotNode",
    stop=False,
    once=True,
)
def 武陵城我转交的委托(c: Capture) -> None:
    c.shot("我转交的委托")


@flow.step(
    enter="InClaimDeliveryJobsNode",
    route="SceneEnterMenuRegionalDevelopmentWulingDepotNode",
    once=True,
)
def 武陵城回仓储节点(c: Capture) -> None:
    """点回「本地仓储节点」标签页，回到仓储节点界面再截一张转交后的样子。"""
    c.run("DeliveryJobsQuickTransferBackToLocalDepotNode")
    c.wait("DeliveryJobsInWulingLocalDepotNode")
    c.shot("转交后仓储节点")
