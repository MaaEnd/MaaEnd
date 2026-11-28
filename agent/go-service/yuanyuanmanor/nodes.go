package yuanyuanmanor

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	component = "yuanyuanmanor"

	recognizeMenuName = "InnkeepingMenuRecognition"
	decideMenuName    = "InnkeepingMenuDecide"
	checkMenuCardName = "InnkeepingMenuCheckCard"
)

const (
	nodeInnkeepingMenuSwitchTabEntreeSuccess  = "InnkeepingMenuSwitchTabEntreeSuccess"
	nodeInnkeepingMenuSwitchTabSideSuccess    = "InnkeepingMenuSwitchTabSideSuccess"
	nodeInnkeepingMenuSwitchTabDessertSuccess = "InnkeepingMenuSwitchTabDessertSuccess"
	nodeInnkeepingMenuSwitchTabSoupSuccess    = "InnkeepingMenuSwitchTabSoupSuccess"
	nodeInnkeepingMenuSwitchTabDrinksSuccess  = "InnkeepingMenuSwitchTabDrinksSuccess"

	nodeInnkeepingMenuDecide          = "InnkeepingMenuDecide"
	nodeInnkeepingMenuCheckCard       = "InnkeepingMenuCheckCard"
	nodeInnkeepingMenuPickCard        = "InnkeepingMenuPickCard"
	nodeInnkeepingMenuPickCardSuccess = "InnkeepingMenuPickCardSuccess"

	nodeInnkeepingMenuDishCount   = "InnkeepingMenuDishCount"
	nodeInnkeepingMenuDrinksCount = "InnkeepingMenuDrinksCount"

	nodeInnkeepingMenuCardPositionAnchor  = "InnkeepingMenuCardPositionAnchor"
	nodeInnkeepingMenuCardIconRecognition = "InnkeepingMenuCardIconRecognition"
	nodeInnkeepingMenuCardQuantity        = "InnkeepingMenuCardQuantity"

	nodeInnkeepingMenuCard1Position = "InnkeepingMenuCard1Position"
	nodeInnkeepingMenuCard2Position = "InnkeepingMenuCard2Position"
	nodeInnkeepingMenuCard3Position = "InnkeepingMenuCard3Position"
	nodeInnkeepingMenuCard4Position = "InnkeepingMenuCard4Position"
	nodeInnkeepingMenuCard5Position = "InnkeepingMenuCard5Position"
	nodeInnkeepingMenuCard6Position = "InnkeepingMenuCard6Position"
	nodeInnkeepingMenuCard7Position = "InnkeepingMenuCard7Position"
	nodeInnkeepingMenuCard8Position = "InnkeepingMenuCard8Position"
)

var nodeInnkeepingMenuCardPositions = [8]string{
	nodeInnkeepingMenuCard1Position,
	nodeInnkeepingMenuCard2Position,
	nodeInnkeepingMenuCard3Position,
	nodeInnkeepingMenuCard4Position,
	nodeInnkeepingMenuCard5Position,
	nodeInnkeepingMenuCard6Position,
	nodeInnkeepingMenuCard7Position,
	nodeInnkeepingMenuCard8Position,
}

// nodeROI 读取节点（JSON 中定义）的识别 ROI。
func nodeROI(ctx *maa.Context, nodeName string) (maa.Rect, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return maa.Rect{}, err
	}
	var node struct {
		Recognition struct {
			Param struct {
				ROI maa.Rect `json:"roi"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return maa.Rect{}, err
	}
	return node.Recognition.Param.ROI, nil
}

const (
	nodeRestaurantState = "RestaurantState"

	nodeKitchenPanelReady    = "RestaurantKitchenPanelReady"
	nodeObservationEmpty     = "RestaurantObservationEmpty"
	nodeRuntimeTargetKitchen = "RestaurantRuntimeTargetKitchen"
	nodeRuntimeNextKitchen   = "RestaurantRuntimeNextKitchen"
	nodeRuntimeIdle          = "RestaurantRuntimeIdle"

	nodeRuntimeSwitchKitchen1 = "RestaurantRuntimeSwitchKitchen1"
	nodeRuntimeSwitchKitchen2 = "RestaurantRuntimeSwitchKitchen2"
	nodeRuntimeSwitchKitchen3 = "RestaurantRuntimeSwitchKitchen3"

	nodeRuntimeClickDish1 = "RestaurantRuntimeClickDish1"
	nodeRuntimeClickDish2 = "RestaurantRuntimeClickDish2"
	nodeRuntimeClickDish3 = "RestaurantRuntimeClickDish3"
	nodeRuntimeClickDish4 = "RestaurantRuntimeClickDish4"
	nodeRuntimeClickDish5 = "RestaurantRuntimeClickDish5"
	nodeRuntimeClickDish6 = "RestaurantRuntimeClickDish6"

	nodeKitchen1Selected = "RestaurantKitchen1Selected"
	nodeKitchen2Selected = "RestaurantKitchen2Selected"
	nodeKitchen3Selected = "RestaurantKitchen3Selected"

	nodeKitchen1Count0 = "RestaurantKitchen1Count0"
	nodeKitchen1Count1 = "RestaurantKitchen1Count1"
	nodeKitchen1Count2 = "RestaurantKitchen1Count2"
	nodeKitchen1Count3 = "RestaurantKitchen1Count3"
	nodeKitchen2Count0 = "RestaurantKitchen2Count0"
	nodeKitchen2Count1 = "RestaurantKitchen2Count1"
	nodeKitchen2Count2 = "RestaurantKitchen2Count2"
	nodeKitchen2Count3 = "RestaurantKitchen2Count3"
	nodeKitchen3Count0 = "RestaurantKitchen3Count0"
	nodeKitchen3Count1 = "RestaurantKitchen3Count1"
	nodeKitchen3Count2 = "RestaurantKitchen3Count2"
	nodeKitchen3Count3 = "RestaurantKitchen3Count3"

	nodeRecipe1RestockMarker = "RestaurantRecipe1RestockMarker"
	nodeRecipe2RestockMarker = "RestaurantRecipe2RestockMarker"
	nodeRecipe3RestockMarker = "RestaurantRecipe3RestockMarker"
	nodeRecipe4RestockMarker = "RestaurantRecipe4RestockMarker"
	nodeRecipe5RestockMarker = "RestaurantRecipe5RestockMarker"
	nodeRecipe6RestockMarker = "RestaurantRecipe6RestockMarker"

	nodeTable1Observation      = "RestaurantTable1Observation"
	nodeTable1Dining           = "RestaurantTable1Dining"
	nodeTable1SingleOrder      = "RestaurantTable1SingleOrder"
	nodeTable1DoubleOrder      = "RestaurantTable1DoubleOrder"
	nodeTable1SinglePoint1Icon = "RestaurantTable1SinglePoint1Icon"
	nodeTable1DoublePoint1Icon = "RestaurantTable1DoublePoint1Icon"
	nodeTable1DoublePoint2Icon = "RestaurantTable1DoublePoint2Icon"

	nodeTable2Observation      = "RestaurantTable2Observation"
	nodeTable2Dining           = "RestaurantTable2Dining"
	nodeTable2SingleOrder      = "RestaurantTable2SingleOrder"
	nodeTable2DoubleOrder      = "RestaurantTable2DoubleOrder"
	nodeTable2SinglePoint1Icon = "RestaurantTable2SinglePoint1Icon"
	nodeTable2DoublePoint1Icon = "RestaurantTable2DoublePoint1Icon"
	nodeTable2DoublePoint2Icon = "RestaurantTable2DoublePoint2Icon"

	nodeTable3Observation      = "RestaurantTable3Observation"
	nodeTable3Dining           = "RestaurantTable3Dining"
	nodeTable3SingleOrder      = "RestaurantTable3SingleOrder"
	nodeTable3DoubleOrder      = "RestaurantTable3DoubleOrder"
	nodeTable3SinglePoint1Icon = "RestaurantTable3SinglePoint1Icon"
	nodeTable3DoublePoint1Icon = "RestaurantTable3DoublePoint1Icon"
	nodeTable3DoublePoint2Icon = "RestaurantTable3DoublePoint2Icon"

	nodeTable4Observation      = "RestaurantTable4Observation"
	nodeTable4Dining           = "RestaurantTable4Dining"
	nodeTable4SingleOrder      = "RestaurantTable4SingleOrder"
	nodeTable4DoubleOrder      = "RestaurantTable4DoubleOrder"
	nodeTable4SinglePoint1Icon = "RestaurantTable4SinglePoint1Icon"
	nodeTable4DoublePoint1Icon = "RestaurantTable4DoublePoint1Icon"
	nodeTable4DoublePoint2Icon = "RestaurantTable4DoublePoint2Icon"

	nodeTable5Observation      = "RestaurantTable5Observation"
	nodeTable5Dining           = "RestaurantTable5Dining"
	nodeTable5SingleOrder      = "RestaurantTable5SingleOrder"
	nodeTable5DoubleOrder      = "RestaurantTable5DoubleOrder"
	nodeTable5SinglePoint1Icon = "RestaurantTable5SinglePoint1Icon"
	nodeTable5DoublePoint1Icon = "RestaurantTable5DoublePoint1Icon"
	nodeTable5DoublePoint2Icon = "RestaurantTable5DoublePoint2Icon"

	nodeTable6Observation      = "RestaurantTable6Observation"
	nodeTable6Dining           = "RestaurantTable6Dining"
	nodeTable6SingleOrder      = "RestaurantTable6SingleOrder"
	nodeTable6DoubleOrder      = "RestaurantTable6DoubleOrder"
	nodeTable6SinglePoint1Icon = "RestaurantTable6SinglePoint1Icon"
	nodeTable6DoublePoint1Icon = "RestaurantTable6DoublePoint1Icon"
	nodeTable6DoublePoint2Icon = "RestaurantTable6DoublePoint2Icon"
)

type tableNodeGroup struct {
	observation      string
	dining           string
	singleOrder      string
	doubleOrder      string
	singlePoint1Icon string
	doublePoint1Icon string
	doublePoint2Icon string
}

func (g tableNodeGroup) iconNodes() []string {
	return []string{g.singlePoint1Icon, g.doublePoint1Icon, g.doublePoint2Icon}
}

var (
	runtimeRoutes = []string{
		nodeRuntimeSwitchKitchen1,
		nodeRuntimeSwitchKitchen2,
		nodeRuntimeSwitchKitchen3,
		nodeRuntimeNextKitchen,
		nodeRuntimeClickDish1,
		nodeRuntimeClickDish2,
		nodeRuntimeClickDish3,
		nodeRuntimeClickDish4,
		nodeRuntimeClickDish5,
		nodeRuntimeClickDish6,
		nodeRuntimeIdle,
	}

	nodeRuntimeSwitchKitchens = [3]string{
		nodeRuntimeSwitchKitchen1,
		nodeRuntimeSwitchKitchen2,
		nodeRuntimeSwitchKitchen3,
	}

	nodeRuntimeClickDishes = [6]string{
		nodeRuntimeClickDish1,
		nodeRuntimeClickDish2,
		nodeRuntimeClickDish3,
		nodeRuntimeClickDish4,
		nodeRuntimeClickDish5,
		nodeRuntimeClickDish6,
	}

	nodeKitchenSelected = [3]string{
		nodeKitchen1Selected,
		nodeKitchen2Selected,
		nodeKitchen3Selected,
	}

	nodeKitchenCounts = [3][4]string{
		{nodeKitchen1Count0, nodeKitchen1Count1, nodeKitchen1Count2, nodeKitchen1Count3},
		{nodeKitchen2Count0, nodeKitchen2Count1, nodeKitchen2Count2, nodeKitchen2Count3},
		{nodeKitchen3Count0, nodeKitchen3Count1, nodeKitchen3Count2, nodeKitchen3Count3},
	}

	nodeRecipeRestockMarkers = [6]string{
		nodeRecipe1RestockMarker,
		nodeRecipe2RestockMarker,
		nodeRecipe3RestockMarker,
		nodeRecipe4RestockMarker,
		nodeRecipe5RestockMarker,
		nodeRecipe6RestockMarker,
	}

	tableNodes = [6]tableNodeGroup{
		{
			observation:      nodeTable1Observation,
			dining:           nodeTable1Dining,
			singleOrder:      nodeTable1SingleOrder,
			doubleOrder:      nodeTable1DoubleOrder,
			singlePoint1Icon: nodeTable1SinglePoint1Icon,
			doublePoint1Icon: nodeTable1DoublePoint1Icon,
			doublePoint2Icon: nodeTable1DoublePoint2Icon,
		},
		{
			observation:      nodeTable2Observation,
			dining:           nodeTable2Dining,
			singleOrder:      nodeTable2SingleOrder,
			doubleOrder:      nodeTable2DoubleOrder,
			singlePoint1Icon: nodeTable2SinglePoint1Icon,
			doublePoint1Icon: nodeTable2DoublePoint1Icon,
			doublePoint2Icon: nodeTable2DoublePoint2Icon,
		},
		{
			observation:      nodeTable3Observation,
			dining:           nodeTable3Dining,
			singleOrder:      nodeTable3SingleOrder,
			doubleOrder:      nodeTable3DoubleOrder,
			singlePoint1Icon: nodeTable3SinglePoint1Icon,
			doublePoint1Icon: nodeTable3DoublePoint1Icon,
			doublePoint2Icon: nodeTable3DoublePoint2Icon,
		},
		{
			observation:      nodeTable4Observation,
			dining:           nodeTable4Dining,
			singleOrder:      nodeTable4SingleOrder,
			doubleOrder:      nodeTable4DoubleOrder,
			singlePoint1Icon: nodeTable4SinglePoint1Icon,
			doublePoint1Icon: nodeTable4DoublePoint1Icon,
			doublePoint2Icon: nodeTable4DoublePoint2Icon,
		},
		{
			observation:      nodeTable5Observation,
			dining:           nodeTable5Dining,
			singleOrder:      nodeTable5SingleOrder,
			doubleOrder:      nodeTable5DoubleOrder,
			singlePoint1Icon: nodeTable5SinglePoint1Icon,
			doublePoint1Icon: nodeTable5DoublePoint1Icon,
			doublePoint2Icon: nodeTable5DoublePoint2Icon,
		},
		{
			observation:      nodeTable6Observation,
			dining:           nodeTable6Dining,
			singleOrder:      nodeTable6SingleOrder,
			doubleOrder:      nodeTable6DoubleOrder,
			singlePoint1Icon: nodeTable6SinglePoint1Icon,
			doublePoint1Icon: nodeTable6DoublePoint1Icon,
			doublePoint2Icon: nodeTable6DoublePoint2Icon,
		},
	}
)
