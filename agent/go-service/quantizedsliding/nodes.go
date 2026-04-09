package quantizedsliding

const (
	quantizedSlidingActionName = "QuantizedSliding"

	nodeQuantizedSlidingMain           = "QuantizedSlidingMain"
	nodeQuantizedSlidingFindStart      = "QuantizedSlidingFindStart"
	nodeQuantizedSlidingGetMaxQuantity = "QuantizedSlidingGetMaxQuantity"
	nodeQuantizedSlidingFindEnd        = "QuantizedSlidingFindEnd"
	nodeQuantizedSlidingCheckQuantity  = "QuantizedSlidingCheckQuantity"
	nodeQuantizedSlidingDone           = "QuantizedSlidingDone"

	nodeQuantizedSlidingSwipeToMax       = "QuantizedSlidingSwipeToMax"
	nodeQuantizedSlidingGetQuantity      = "QuantizedSlidingGetQuantity"
	nodeQuantizedSlidingQuantityFilter   = "QuantizedSlidingQuantityFilter"
	nodeQuantizedSlidingSwipeButton      = "QuantizedSlidingSwipeButton"
	nodeQuantizedSlidingIncreaseButton   = "QuantizedSlidingIncreaseButton"
	nodeQuantizedSlidingDecreaseButton   = "QuantizedSlidingDecreaseButton"
	nodeQuantizedSlidingPreciseClick     = "QuantizedSlidingPreciseClick"
	nodeQuantizedSlidingClearMaxHit      = "QuantizedSlidingClearMaxHit"
	nodeQuantizedSlidingJumpBackNode     = "QuantizedSlidingJumpBackNode"
	nodeQuantizedSlidingFail             = "QuantizedSlidingFail"
	nodeQuantizedSlidingIncreaseQuantity = "QuantizedSlidingIncreaseQuantity"
	nodeQuantizedSlidingDecreaseQuantity = "QuantizedSlidingDecreaseQuantity"
)

var quantizedSlidingActionNodes = []string{
	nodeQuantizedSlidingMain,
	nodeQuantizedSlidingFindStart,
	nodeQuantizedSlidingGetMaxQuantity,
	nodeQuantizedSlidingFindEnd,
	nodeQuantizedSlidingCheckQuantity,
	nodeQuantizedSlidingDone,
}
