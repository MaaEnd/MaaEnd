package bettersliding

const (
	betterSlidingActionName = "BetterSliding"

	nodeBetterSlidingMain                 = "BetterSlidingMain"
	nodeBetterSlidingFindStart            = "BetterSlidingFindStart"
	nodeBetterSlidingFindSwipeForReset    = "BetterSlidingFindSwipeForReset"
	nodeBetterSlidingGetSliderMaxQuantity = "BetterSlidingGetSliderMaxQuantity"
	nodeBetterSlidingGetAvailableQuantity = "BetterSlidingGetAvailableQuantity"
	nodeBetterSlidingFindEnd              = "BetterSlidingFindEnd"
	nodeBetterSlidingCheckQuantity        = "BetterSlidingCheckQuantity"
	nodeBetterSlidingDone                 = "BetterSlidingDone"

	nodeBetterSlidingSwipeToMax              = "BetterSlidingSwipeToMax"
	nodeBetterSlidingGetSliderQuantity       = "BetterSlidingGetSliderQuantity"
	nodeBetterSlidingSliderQuantityFilter    = "BetterSlidingSliderQuantityFilter"
	nodeBetterSlidingAvailableQuantityFilter = "BetterSlidingAvailableQuantityFilter"
	nodeBetterSlidingSwipeButton             = "BetterSlidingSwipeButton"
	nodeBetterSlidingIncreaseButton          = "BetterSlidingIncreaseButton"
	nodeBetterSlidingDecreaseButton          = "BetterSlidingDecreaseButton"
	nodeBetterSlidingPreciseClick            = "BetterSlidingPreciseClick"
	nodeBetterSlidingClearMaxHit             = "BetterSlidingClearMaxHit"
	nodeBetterSlidingJumpBackNode            = "BetterSlidingJumpBackNode"
	nodeBetterSlidingJumpBackMoveMouse       = "[JumpBack]BetterSlidingMoveMouse"
	nodeBetterSlidingFail                    = "BetterSlidingFail"
	nodeBetterSlidingIncreaseQuantity        = "BetterSlidingIncreaseQuantity"
	nodeBetterSlidingDecreaseQuantity        = "BetterSlidingDecreaseQuantity"
	nodeBetterSlidingReset                   = "BetterSlidingReset"
)

var betterSlidingActionNodes = []string{
	nodeBetterSlidingMain,
	nodeBetterSlidingFindStart,
	nodeBetterSlidingGetSliderMaxQuantity,
	nodeBetterSlidingGetAvailableQuantity,
	nodeBetterSlidingFindEnd,
	nodeBetterSlidingCheckQuantity,
	nodeBetterSlidingDone,
}
