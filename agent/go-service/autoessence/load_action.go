package autoessence

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &LoadInventoryAction{}

// LoadInventoryAction loads EssenceInventory.json into the AutoEssence inventory.
type LoadInventoryAction struct{}

func (a *LoadInventoryAction) Run(_ *maa.Context, _ *maa.CustomActionArg) bool {
	path := fsutil.OutputPath(inventoryFilePath)
	if err := LoadFile(path); err != nil {
		log.Error().
			Err(err).
			Str("component", componentInventory).
			Str("action", "LoadInventory").
			Str("path", path).
			Msg("failed to load inventory")
		return false
	}
	return true
}
