package autoessence

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter"
	"github.com/rs/zerolog/log"
)

const componentInventory = "AutoEssenceInventory"

// stock is the essence stock of the most recently loaded EssenceInventory.json.
// Gap checks read only from it, so the file is the single source of truth.
var stock *essencefilter.InventoryStock

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
