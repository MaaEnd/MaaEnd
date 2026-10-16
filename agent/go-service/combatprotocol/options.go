package combatprotocol

import (
	"encoding/json"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// attachOptions mirrors the attach block of the container entry node. The
// block carries defaults from the node JSON; the client (MXU) writes the
// assembled slot list (one entry per included child, in list order) and each
// slot's threshold into it when assembling a run.
type attachOptions struct {
	Mode           string   `json:"mode"`
	Slots          []string `json:"slots"`
	SlotThresholds []int    `json:"slot_thresholds"`
}

// readAttachOptions reads the merged attach values from the container entry
// node via Context.GetNodeJSON, following the ScheduleRecognition pattern.
func readAttachOptions(ctx *maa.Context) (attachOptions, error) {
	if ctx == nil {
		return attachOptions{}, fmt.Errorf("context is nil")
	}
	raw, err := ctx.GetNodeJSON(attachNodeName)
	if err != nil {
		return attachOptions{}, err
	}
	var wrapper struct {
		Attach attachOptions `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return attachOptions{}, err
	}
	return wrapper.Attach, nil
}

// slotList returns the assembled rotation slots. Thresholds fall back to the
// per-task defaults when the client sends none for a slot. An empty slot table
// (a client without container assembly support) falls back to the default
// order so the task still works out of the box.
func (o attachOptions) slotList() []Slot {
	if len(o.Slots) == 0 {
		log.Warn().
			Str("component", "CombatProtocol").
			Msg("no slots assembled, falling back to the default order (AutoEssence -> ProtocolSpace)")
		return []Slot{
			{Task: TaskAutoEssence, Threshold: defaultThresholdAutoEssence},
			{Task: TaskProtocolSpace, Threshold: defaultThresholdProtocolSpace},
		}
	}
	slots := make([]Slot, 0, len(o.Slots))
	for i, task := range o.Slots {
		if task != TaskAutoEssence && task != TaskProtocolSpace {
			continue
		}
		threshold := defaultThreshold(task)
		if i < len(o.SlotThresholds) && o.SlotThresholds[i] > 0 {
			threshold = o.SlotThresholds[i]
		}
		slots = append(slots, Slot{Task: task, Threshold: threshold})
	}
	return slots
}

// normalizedMode returns the configured rotation mode, falling back to the
// sanity-accumulation mode.
func (o attachOptions) normalizedMode() string {
	if o.Mode == ModeDate {
		return ModeDate
	}
	return ModeSanity
}

func defaultThreshold(task string) int {
	if task == TaskProtocolSpace {
		return defaultThresholdProtocolSpace
	}
	return defaultThresholdAutoEssence
}
