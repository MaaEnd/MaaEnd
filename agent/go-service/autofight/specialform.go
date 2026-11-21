package autofight

import (
	"image"
	"math"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type skillFormState int

const (
	skillFormUnknown skillFormState = iota
	skillFormInactive
	skillFormActive

	skillFormCheckInterval = time.Second
	skillFormScoreMargin   = 0.03
)

// 桌面端的战技图标行，不包含下方可改键的按键提示。列顺序与终结技区域一致。
var winSkillFormRegions = [4]maa.Rect{
	{1020, 610, 62, 60},
	{1082, 610, 64, 60},
	{1146, 610, 62, 60},
	{1208, 610, 68, 60},
}

type specialFormSlot struct {
	known bool
	state skillFormState
}

// specialFormTracker 只在一次自动战斗内保存已确认的特殊形态槽位，跨帧 Miss 时不恢复盲点。
type specialFormTracker struct {
	lastCheck time.Time
	slots     [4]specialFormSlot
}

func (t *specialFormTracker) update(now time.Time, modes [4]string, characterCount int, recognize func(maa.Rect) skillFormState) {
	if !t.lastCheck.IsZero() && now.Sub(t.lastCheck) < skillFormCheckInterval {
		return
	}
	t.lastCheck = now
	n := characterCount
	if n <= 0 || n > 4 {
		n = 4
	}
	for idx := range t.slots {
		if idx >= n || modes[idx] != skillModeCycle && modes[idx] != "" {
			t.slots[idx] = specialFormSlot{}
			continue
		}
		// 少于四人时按钮向右对齐，与 AutoFight 的终结技槽位映射保持一致。
		screenSlot := idx + 4 - n
		state := recognize(winSkillFormRegions[screenSlot])
		t.slots[idx].state = state
		t.slots[idx].known = t.slots[idx].known || state != skillFormUnknown
	}
}

func (t *specialFormTracker) releaseModes(modes [4]string, energyLevel, reserveSkillLevel int) [4]string {
	for idx, slot := range t.slots {
		if modes[idx] != skillModeCycle && modes[idx] != "" {
			continue
		}
		// 特殊形态即便用于打断蓄力也不能动用保留能量；普通干员仍按原有策略释放。
		if slot.known && (slot.state != skillFormInactive || energyLevel <= reserveSkillLevel) {
			modes[idx] = skillModeNever
		}
	}
	return modes
}

func (t *specialFormTracker) markReleased(operator int) {
	if operator >= 1 && operator <= len(t.slots) && t.slots[operator-1].known {
		// 消耗本次 inactive 证据，直到新截图确认状态，防止 1s 采样间隔内再次点击关闭。
		t.slots[operator-1].state = skillFormUnknown
	}
}

func classifySkillForm(activeScore, inactiveScore float64) skillFormState {
	if activeScore > 0 && inactiveScore > 0 && math.Abs(activeScore-inactiveScore) < skillFormScoreMargin {
		return skillFormUnknown
	}
	if inactiveScore > activeScore {
		return skillFormInactive
	}
	if activeScore > 0 {
		return skillFormActive
	}
	return skillFormUnknown
}

func recognizeLiinoSkillForm(ctx *maa.Context, img image.Image, roi maa.Rect) skillFormState {
	scores := [2]float64{}
	for idx, node := range []string{"__AutoFightRecognitionLiinoActive", "__AutoFightRecognitionLiinoInactive"} {
		override := map[string]any{
			node: map[string]any{
				"recognition": map[string]any{
					"param": map[string]any{"roi": []int{roi.X(), roi.Y(), roi.Width(), roi.Height()}},
				},
			},
		}
		detail, err := ctx.RunRecognition(node, img, override)
		if err != nil {
			log.Error().Err(err).Str("component", "AutoFight").Str("node", node).Any("roi", roi).Msg("special skill form recognition failed")
			return skillFormUnknown
		}
		if detail == nil || !detail.Hit || detail.Results == nil || detail.Results.Best == nil {
			continue
		}
		if result, ok := detail.Results.Best.AsTemplateMatch(); ok {
			scores[idx] = result.Score
		}
	}
	return classifySkillForm(scores[0], scores[1])
}
