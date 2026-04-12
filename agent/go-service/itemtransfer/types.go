package itemtransfer

import (
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

type itemOrderData struct {
	Items         map[string]itemInfo `json:"items"`
	CategoryOrder map[string][]string `json:"category_order"`
}

type itemInfo struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

type fallbackParams struct {
	TargetClass int    `json:"target_class"`
	Descending  bool   `json:"descending"`
	Side        string `json:"side"`
}

type gridItem struct {
	Box     [4]int
	ClassID uint64
	Score   float64
	CenterX int
	CenterY int
}

const (
	componentName = "itemtransfer"

	repoNNDNode    = "ItemTransferDetectAllItems"
	bagNNDNode     = "ItemTransferDetectAllItemsBag"
	tooltipOCRNode = "ItemTransferTooltipOCR"

	tooltipOffsetX = 31
	tooltipOffsetY = 6
	tooltipWidth   = 117
	tooltipHeight  = 58
)

var (
	cachedData     *itemOrderData
	cachedDataOnce sync.Once
	cachedDataErr  error
)

func loadItemOrderData() (*itemOrderData, error) {
	cachedDataOnce.Do(func() {
		var data itemOrderData
		err := resource.ReadJsonResource("data/ItemTransfer/item_order.json", &data)
		if err != nil {
			cachedDataErr = err
			return
		}

		cachedData = &data
		log.Info().
			Str("component", componentName).
			Int("item_count", len(data.Items)).
			Int("category_count", len(data.CategoryOrder)).
			Msg("item order data loaded")
	})
	return cachedData, cachedDataErr
}

// inferSide returns the side from params if set, otherwise infers from the
// pipeline node name: nodes containing "Bag" operate on the bag area.
func inferSide(paramSide, taskName string) string {
	if paramSide != "" {
		return paramSide
	}
	if strings.Contains(taskName, "Bag") {
		return "bag"
	}
	return "repo"
}
