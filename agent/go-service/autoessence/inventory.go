package autoessence

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentInventory = "AutoEssenceInventory"

// stock is the most recently loaded account inventory or explicitly imported file.
var stock *essencefilter.InventoryStock

func loadAccountStock(ctx *maa.Context, taskID int64) error {
	loaded, err := essencefilter.LoadAccountInventory(ctx, taskID)
	stock = loaded
	return err
}

// LoadFile loads EssenceInventory.json into memory; a missing file yields an empty stock.
func LoadFile(path string) error {
	if path == "" {
		path = essencefilter.InventoryPath()
	}
	loaded, err := essencefilter.LoadInventoryStock(path)
	if err != nil {
		return err
	}
	stock = loaded
	log.Info().
		Str("component", componentInventory).
		Str("path", path).
		Msg("inventory loaded")
	return nil
}

// Count returns how many held essences match the skill combo identified by weaponIDs.
// weaponIDs must be non-empty; only groups that contain all listed IDs are summed.
func Count(weaponIDs []string) int {
	return stock.Count(weaponIDs)
}
