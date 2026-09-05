package autoessence

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const maxBaseAttributeSelections = 3

var (
	errTooManyBaseCheckboxSelections  = fmt.Errorf("more than %d base attribute checkboxes selected", maxBaseAttributeSelections)
	errTooFewBaseCheckboxSelections   = fmt.Errorf("fewer than %d base attribute checkboxes selected", maxBaseAttributeSelections)
	errTooManyBonusCheckboxSelections = fmt.Errorf("more than 1 bonus attribute checkbox selected")
)

type setupLocationAttach struct {
	MenuMode   string `json:"menu_mode"`
	LocationID string `json:"location_id"`
	Slot2ID    int    `json:"slot2_id"`
}

func getSetupLocationAttach(ctx *maa.Context, nodeName string) (*setupLocationAttach, map[string]any, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", nodeName).
			Msg("failed to get setup location node json")
		return nil, nil, err
	}

	var wrapper struct {
		Attach map[string]any `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", nodeName).
			Msg("failed to unmarshal setup location attach")
		return nil, nil, err
	}

	attachRaw := wrapper.Attach
	if attachRaw == nil {
		attachRaw = map[string]any{}
	}

	attachBytes, err := json.Marshal(attachRaw)
	if err != nil {
		return nil, nil, err
	}

	var attach setupLocationAttach
	if err := json.Unmarshal(attachBytes, &attach); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", nodeName).
			Msg("failed to unmarshal setup location attach fields")
		return nil, nil, err
	}

	return &attach, attachRaw, nil
}

func (a *setupLocationAttach) isLocationMode() bool {
	return strings.TrimSpace(a.MenuMode) == menuModeLocation
}

func (a *setupLocationAttach) validateForEngraveOverride(raw map[string]any) error {
	baseIDs, err := collectSelectedBaseAttributeIDs(raw)
	if err != nil {
		return err
	}
	if len(baseIDs) != maxBaseAttributeSelections {
		return fmt.Errorf("expected %d base attributes, got %d", maxBaseAttributeSelections, len(baseIDs))
	}
	if a.Slot2ID <= 0 {
		return fmt.Errorf("slot2 bonus attribute id is missing")
	}
	return nil
}

func validateLocationModeAttach(raw map[string]any, attach *setupLocationAttach) error {
	if raw == nil {
		raw = map[string]any{}
	}

	baseCheckboxCount := countTrueAttachWithPrefix(raw, "s1_")
	if baseCheckboxCount > maxBaseAttributeSelections {
		return errTooManyBaseCheckboxSelections
	}
	if baseCheckboxCount < maxBaseAttributeSelections {
		return errTooFewBaseCheckboxSelections
	}

	bonusCheckboxCount := countTrueAttachWithPrefix(raw, "s2_")
	if bonusCheckboxCount > 1 {
		return errTooManyBonusCheckboxSelections
	}

	return nil
}

func countTrueAttachWithPrefix(raw map[string]any, prefix string) int {
	count := 0
	for key, value := range raw {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if attachBoolTrue(value) {
			count++
		}
	}
	return count
}

func collectSelectedBaseAttributeIDs(raw map[string]any) ([]int, error) {
	if raw == nil {
		return nil, errTooFewBaseCheckboxSelections
	}

	selected := make([]int, 0, maxBaseAttributeSelections)
	for key, value := range raw {
		if !strings.HasPrefix(key, "s1_") || !attachBoolTrue(value) {
			continue
		}
		idText := strings.TrimPrefix(key, "s1_")
		id, err := strconv.Atoi(idText)
		if err != nil || id <= 0 {
			continue
		}
		selected = append(selected, id)
	}

	sort.Ints(selected)
	return selected, nil
}

func attachBoolTrue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case json.Number:
		return v.String() == "1"
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	default:
		return false
	}
}

func stopTaskWithInvalidOptions(ctx *maa.Context, err error) bool {
	log.Error().
		Err(err).
		Str("component", componentName).
		Msg("stopping task due to invalid location mode options")

	maafocus.Print(ctx, i18n.T("autoessence.invalid_location_options"))
	return false
}
