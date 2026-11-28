package autoessence

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const componentInventory = "AutoEssenceInventory"

// stock is the most recently loaded account inventory.
var stock *essencefilter.InventoryStock

func loadAccountStock(ctx *maa.Context, taskID int64) error {
	loaded, err := essencefilter.LoadAccountInventory(ctx, taskID)
	stock = loaded
	return err
}
