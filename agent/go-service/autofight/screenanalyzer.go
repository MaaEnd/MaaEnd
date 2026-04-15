package autofight

import (
	"image"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	LabelCharacterComboActive     = "CharacterComboActive"
	LabelCharacterComboEmpty      = "CharacterComboEmpty"
	LabelCharacterComboFull       = "CharacterComboFull"
	LabelCharacterHealthDangerous = "CharacterHealthDangerous"
	LabelCharacterHealthNormal    = "CharacterHealthNormal"
	LabelCharacterDied            = "CharacterDied"
	LabelCharacterLevel           = "CharacterLevel"
	LabelCharacterSelect          = "CharacterSelect"
	LabelEndSkillFull             = "EndSkillFull"
	LabelEnemyAccumPower          = "EnemyAccumulatingPower"
	LabelEnemyBossHealth          = "EnemyBossHealth"
	LabelEnemyDodge               = "EnemyDodge"
	LabelEnemyTarget              = "EnemyTarget"
	LabelEnemyFacing              = "EnemyFacing"
	LabelEnergyLevelEmpty         = "EnergyLevelEmpty"
	LabelEnergyLevelFull          = "EnergyLevelFull"
	LabelMenuList                 = "MenuList"
	LabelMenuOperators            = "MenuOperators"
)

type screenDetection struct {
	Box    maa.Rect
	Label  string
	Score  float64
	IsUsed bool
}

type screenFrame struct {
	Timestamp  time.Time
	Detections []screenDetection
}

type ScreenAnalyzer struct {
	frames []screenFrame
}

// FrameCount prunes expired frames (older than 30s) and returns the current count.
func (sa *ScreenAnalyzer) FrameCount() int {
	cutoff := time.Now().Add(-30 * time.Second)
	i := 0
	for i < len(sa.frames) && sa.frames[i].Timestamp.Before(cutoff) {
		i++
	}
	sa.frames = sa.frames[i:]
	return len(sa.frames)
}

func NewScreenAnalyzer() *ScreenAnalyzer {
	return &ScreenAnalyzer{}
}

func (sa *ScreenAnalyzer) UpdateScreenDetail(ctx *maa.Context, arg image.Image) bool {
	detail_reco, err := ctx.RunRecognition("__AutoFightRecognitionScreen", arg)
	if err != nil || detail_reco == nil {
		log.Error().
			Err(err).
			Str("component", "AutoFight").
			Str("step", "run_recognition_screen").
			Msg("run recognition failed")
		return false
	}

	if !detail_reco.Hit || detail_reco.Results.All == nil {
		sa.frames = append(sa.frames, screenFrame{Timestamp: time.Now()})
		return true
	}

	frame := screenFrame{Timestamp: time.Now()}
	for _, m := range detail_reco.Results.All {
		detail, ok := m.AsNeuralNetworkDetect()
		if !ok {
			continue
		}

		frame.Detections = append(frame.Detections, screenDetection{
			Box:   detail.Box,
			Label: detail.Label,
			Score: detail.Score,
		})
	}
	sa.frames = append(sa.frames, frame)

	// labels := make([]string, 0, len(frame.Detections))
	// scores := make([]float64, 0, len(frame.Detections))
	// for _, det := range frame.Detections {
	// 	labels = append(labels, det.Label)
	// 	scores = append(scores, det.Score)
	// }
	// log.Error().
	// 	Int("frameCount", len(sa.frames)).
	// 	Int("detections", len(frame.Detections)).
	// 	Strs("labels", labels).
	// 	Floats64("scores", scores).
	// 	Msg("Screen frame updated")

	// 删除时间过久的帧
	cutoff := time.Now().Add(-30 * time.Second)
	newFrames := make([]screenFrame, 0, len(sa.frames))
	for _, f := range sa.frames {
		if f.Timestamp.After(cutoff) {
			newFrames = append(newFrames, f)
		}
	}
	sa.frames = newFrames

	return true
}

func (sa *ScreenAnalyzer) hasLabelInFrames(label string, n int, unused bool, region ...maa.Rect) bool {
	hasRegion := len(region) > 0

	total := 0
	matchedFrames := 0

	for fi := len(sa.frames) - 1; fi >= 0 && total < n; fi-- {
		total++
		for _, det := range sa.frames[fi].Detections {
			if unused && det.IsUsed {
				continue
			}
			if hasRegion && !boxIntersects(det.Box, region[0]) {
				continue
			}
			if det.Label == label {
				matchedFrames++
				break // count at most once per frame
			}
		}
	}

	if matchedFrames == 0 {
		return false
	}

	// 如果总帧数超过1且匹配帧数不超过总帧数的一半，认为不可靠，防止yolo识别异常
	if total > 1 && matchedFrames*2 <= total {
		return false
	}

	return true
}

func (sa *ScreenAnalyzer) MarkLabelUsed(label string) {
	for fi := range sa.frames {
		for di := range sa.frames[fi].Detections {
			if sa.frames[fi].Detections[di].Label == label {
				sa.frames[fi].Detections[di].IsUsed = true
			}
		}
	}
}

func (sa *ScreenAnalyzer) hasLabelInDuration(label string, duration time.Duration, region ...maa.Rect) bool {
	cutoff := time.Now().Add(-duration)
	hasRegion := len(region) > 0

	for fi := len(sa.frames) - 1; fi >= 0; fi-- {
		if sa.frames[fi].Timestamp.Before(cutoff) {
			break
		}
		for _, det := range sa.frames[fi].Detections {
			if hasRegion && !boxIntersects(det.Box, region[0]) {
				continue
			}
			if det.Label == label {
				return true
			}
		}
	}
	return false
}

var energyRegions = [3]maa.Rect{
	{540, 600, 50, 80},
	{615, 600, 50, 80},
	{690, 600, 50, 80},
}

func (sa *ScreenAnalyzer) GetEnergyLevel(unused bool) int {
	level := 0
	for _, region := range energyRegions {
		if sa.hasLabelInFrames(LabelEnergyLevelFull, 5, unused, region) {
			level++
		}
	}
	if level > 0 {
		return level
	}

	if sa.hasLabelInFrames(LabelEnergyLevelEmpty, 5, unused, energyRegions[0]) {
		return 0
	}
	return -1
}

var enemyFacingRegion = maa.Rect{250, 160, 900, 400}

func (sa *ScreenAnalyzer) GetEnemyFacing() bool {
	return sa.hasLabelInFrames(LabelEnemyFacing, 10, false, enemyFacingRegion)
}

func (sa *ScreenAnalyzer) GetEnemyTarget() bool {
	return sa.hasLabelInDuration(LabelEnemyTarget, 3*time.Second)
}

func (sa *ScreenAnalyzer) GetEnemyBossHealth() bool {
	return sa.hasLabelInDuration(LabelEnemyBossHealth, 5000*time.Millisecond)
}

var enemyTargetCenterRegion = maa.Rect{340, 0, 600, 720}

func (sa *ScreenAnalyzer) GetEnemyTargetCenter() bool {
	return sa.hasLabelInDuration(LabelEnemyTarget, 3*time.Second, enemyTargetCenterRegion)
}

func (sa *ScreenAnalyzer) GetEnemyDodge() bool {
	return sa.hasLabelInFrames(LabelEnemyDodge, 1, false)
}

func (sa *ScreenAnalyzer) GetEnemyAccumulatingPower(unused bool) bool {
	return sa.hasLabelInFrames(LabelEnemyAccumPower, 5, unused)
}

func (sa *ScreenAnalyzer) GetCharacterComboActive() bool {
	return sa.hasLabelInFrames(LabelCharacterComboActive, 1, false)
}

var characterRegions = [4]maa.Rect{
	{15, 580, 80, 100},
	{95, 580, 80, 100},
	{175, 580, 80, 100},
	{255, 580, 80, 100},
}

var endSkillRegions = [4]maa.Rect{
	{1020, 535, 65, 100},
	{1082, 535, 65, 100},
	{1146, 535, 67, 65},
	{1208, 535, 68, 65},
}

func boxIntersects(a, b maa.Rect) bool {
	return a[0] < b[0]+b[2] && b[0] < a[0]+a[2] &&
		a[1] < b[1]+b[3] && b[1] < a[1]+a[3]
}

func (sa *ScreenAnalyzer) GetEndSkillFull(unused bool) []int {
	result := make([]int, 0, 4)
	for idx := 1; idx <= 4; idx++ {
		if sa.hasLabelInFrames(LabelEndSkillFull, 5, unused, endSkillRegions[idx-1]) {
			result = append(result, idx)
		}
	}
	return result
}

func (sa *ScreenAnalyzer) GetCharacterSelect() int {
	for idx := 1; idx <= 4; idx++ {
		if sa.hasLabelInFrames(LabelCharacterSelect, 3, false, characterRegions[idx-1]) {
			return idx
		}
	}
	return 0
}

func (sa *ScreenAnalyzer) GetCharacterDied() []int {
	result := make([]int, 0, 4)
	for idx := 1; idx <= 4; idx++ {
		if sa.hasLabelInFrames(LabelCharacterDied, 5, false, characterRegions[idx-1]) {
			result = append(result, idx)
		}
	}
	return result
}

func (sa *ScreenAnalyzer) GetCharacterComboFull() []int {
	result := make([]int, 0, 4)
	for idx := 1; idx <= 4; idx++ {
		if sa.hasLabelInFrames(LabelCharacterComboFull, 3, false, characterRegions[idx-1]) {
			result = append(result, idx)
		}
	}
	return result
}

func (sa *ScreenAnalyzer) GetCharacterComboEmpty() []int {
	result := make([]int, 0, 4)
	for idx := 1; idx <= 4; idx++ {
		if sa.hasLabelInFrames(LabelCharacterComboEmpty, 3, false, characterRegions[idx-1]) {
			result = append(result, idx)
		}
	}
	return result
}

func (sa *ScreenAnalyzer) GetCharacterHealthNormal() []int {
	result := make([]int, 0, 4)
	for idx := 1; idx <= 4; idx++ {
		if sa.hasLabelInFrames(LabelCharacterHealthNormal, 3, false, characterRegions[idx-1]) {
			result = append(result, idx)
		}
	}
	return result
}

func (sa *ScreenAnalyzer) GetCharacterHealthDangerous() []int {
	result := make([]int, 0, 4)
	for idx := 1; idx <= 4; idx++ {
		if sa.hasLabelInFrames(LabelCharacterHealthDangerous, 3, false, characterRegions[idx-1]) {
			result = append(result, idx)
		}
	}
	return result
}

func (sa *ScreenAnalyzer) GetCharacterLevel() bool {
	return sa.hasLabelInFrames(LabelCharacterLevel, 5, false)
}

func (sa *ScreenAnalyzer) GetMenuList() bool {
	return sa.hasLabelInFrames(LabelMenuList, 3, false)
}

func (sa *ScreenAnalyzer) GetMenuOperators() bool {
	return sa.hasLabelInFrames(LabelMenuOperators, 3, false)
}
