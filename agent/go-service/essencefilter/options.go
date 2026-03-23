package essencefilter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// matchOptsFromPipeline maps pipeline attach options to the match engine subset.
func matchOptsFromPipeline(opts *EssenceFilterOptions) matchapi.EssenceFilterOptions {
	if opts == nil {
		return matchapi.EssenceFilterOptions{}
	}
	return matchapi.EssenceFilterOptions{
		Rarity6Weapon:            opts.Rarity6Weapon,
		Rarity5Weapon:            opts.Rarity5Weapon,
		Rarity4Weapon:            opts.Rarity4Weapon,
		KeepFuturePromising:      opts.KeepFuturePromising,
		FuturePromisingMinTotal:  opts.FuturePromisingMinTotal,
		LockFuturePromising:      opts.LockFuturePromising,
		KeepSlot3Level3Practical: opts.KeepSlot3Level3Practical,
		Slot3MinLevel:            opts.Slot3MinLevel,
		LockSlot3Practical:       opts.LockSlot3Practical,
		DiscardUnmatched:         opts.DiscardUnmatched,
	}
}

func getOptionsFromAttach(ctx *maa.Context, nodeName string) (*EssenceFilterOptions, error) {
	raw, err := ctx.GetNodeJSON(nodeName)

	if err != nil {
		log.Error().Err(err).Str("node", nodeName).Msg("failed to get options from node")
		return nil, err
	}

	// unmarshal into wrapper struct to extract Attach field
	var wrapper struct {
		Attach EssenceFilterOptions `json:"attach"`
	}

	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		log.Error().Err(err).Str("node", nodeName).Msg("failed to unmarshal options")
		return nil, err
	}

	return &wrapper.Attach, nil
}

func rarityListToString(rarities []int) string {
	switch len(rarities) {
	case 1:
		return strconv.Itoa(rarities[0])
	case 2:
		return fmt.Sprintf("%d 和 %d", rarities[0], rarities[1])
	case 3:
		return fmt.Sprintf("%d， %d 和 %d", rarities[0], rarities[1], rarities[2])
	case 4:
		return fmt.Sprintf("%d， %d， %d 和 %d", rarities[0], rarities[1], rarities[2], rarities[3])
	default:
		return fmt.Sprintf("%d+", len(rarities))
	}
}

func essenceListToString(EssenceTypes []EssenceMeta) string {
	names := make([]string, len(EssenceTypes))
	for i, e := range EssenceTypes {
		names[i] = e.Name
	}
	return strings.Join(names, "、")
}
