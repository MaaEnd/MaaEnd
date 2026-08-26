package autodelivery

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/levenshtein"
)

const (
	destinationMinSimilarity = 0.70
	destinationMinMargin     = 0.05
	areaMinSimilarity        = 0.70
	areaMinMargin            = 0.05
)

type recycleBinAmbiguityError struct {
	AreaID     string
	Candidates []destination
}

func (e *recycleBinAmbiguityError) Error() string {
	ids := make([]string, 0, len(e.Candidates))
	for _, candidate := range e.Candidates {
		ids = append(ids, candidate.ID)
	}
	return fmt.Sprintf("delivery recycle bin match is ambiguous in area %q: candidates=%s", e.AreaID, strings.Join(ids, ","))
}

type destinationMatch struct {
	ObjectiveText      string
	AreaText           string
	DestinationText    string
	Distance           int
	Similarity         float64
	RunnerUpSimilarity float64
	AreaSimilarity     float64
	AreaRunnerUp       float64
}

type scoredDestination struct {
	destination destination
	match       destinationMatch
}

type scoredArea struct {
	area  area
	match destinationMatch
}

// resolveDestinationByArea 先匹配区域，再从该区域内匹配终点，避免不同区域的同名终点产生歧义。
func resolveDestinationByArea(areaOCR, destinationOCR string) (destination, destinationMatch, error) {
	areas, err := getAreas()
	if err != nil {
		return destination{}, destinationMatch{}, err
	}
	destinations, err := getDestinations()
	if err != nil {
		return destination{}, destinationMatch{}, err
	}

	area, areaMatch, err := resolveArea(areaOCR, areas)
	if err != nil {
		return destination{}, areaMatch, err
	}

	candidates := make([]destination, 0, len(destinations))
	for _, destination := range destinations {
		if destination.AreaID == area.ID {
			candidates = append(candidates, destination)
		}
	}
	if len(candidates) == 0 {
		return destination{}, areaMatch, fmt.Errorf("delivery area %q has no destination candidates", area.ID)
	}
	if recycleBins, recycleMatch, ok := resolveAmbiguousRecycleBins(destinationOCR, candidates); ok {
		recycleMatch.AreaText = areaMatch.AreaText
		recycleMatch.AreaSimilarity = areaMatch.Similarity
		recycleMatch.AreaRunnerUp = areaMatch.RunnerUpSimilarity
		return destination{}, recycleMatch, &recycleBinAmbiguityError{
			AreaID:     area.ID,
			Candidates: recycleBins,
		}
	}

	selected, match, err := resolveDestinationText(destinationOCR, candidates)
	match.AreaText = areaMatch.AreaText
	match.AreaSimilarity = areaMatch.Similarity
	match.AreaRunnerUp = areaMatch.RunnerUpSimilarity
	if err != nil {
		return destination{}, match, err
	}
	return selected, match, nil
}

func resolveAmbiguousRecycleBins(ocrText string, destinations []destination) ([]destination, destinationMatch, bool) {
	normalizedOCR := normalizeOCRText(ocrText)
	if normalizedOCR == "" {
		return nil, destinationMatch{}, false
	}

	recycleBins := make([]destination, 0, len(destinations))
	recycleBinMap := ""
	for _, candidate := range destinations {
		if candidate.Kind != destinationKindRecycleBin {
			continue
		}
		if strings.TrimSpace(candidate.Map) == "" {
			return nil, destinationMatch{}, false
		}
		if recycleBinMap == "" {
			recycleBinMap = candidate.Map
		} else if candidate.Map != recycleBinMap {
			return nil, destinationMatch{}, false
		}
		recycleBins = append(recycleBins, candidate)
	}
	if len(recycleBins) < 2 {
		return nil, destinationMatch{}, false
	}
	sort.Slice(recycleBins, func(i, j int) bool { return recycleBins[i].ID < recycleBins[j].ID })

	matches := make([]destinationMatch, 0, len(recycleBins))
	for _, recycleBin := range recycleBins {
		matches = append(matches, bestTextMatch(normalizedOCR, recycleBin.ObjectiveTexts))
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Similarity > matches[j].Similarity })

	best := matches[0]
	runnerUp := matches[1]
	if runnerUp.Similarity < destinationMinSimilarity {
		return nil, destinationMatch{}, false
	}
	best.RunnerUpSimilarity = runnerUp.Similarity
	return recycleBins, best, true
}

func resolveArea(ocrText string, areas []area) (area, destinationMatch, error) {
	normalizedOCR := normalizeOCRText(ocrText)
	if normalizedOCR == "" {
		return area{}, destinationMatch{}, fmt.Errorf("delivery area OCR is empty after normalization")
	}

	scores := make([]scoredArea, 0, len(areas))
	for _, area := range areas {
		match := bestTextMatch(normalizedOCR, area.Texts)
		match.AreaText = match.ObjectiveText
		scores = append(scores, scoredArea{area: area, match: match})
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].match.Similarity != scores[j].match.Similarity {
			return scores[i].match.Similarity > scores[j].match.Similarity
		}
		return scores[i].area.ID < scores[j].area.ID
	})

	best := scores[0]
	runnerUp := 0.0
	if len(scores) > 1 {
		runnerUp = scores[1].match.Similarity
	}
	best.match.RunnerUpSimilarity = runnerUp
	if best.match.Similarity < areaMinSimilarity {
		return area{}, best.match, fmt.Errorf(
			"delivery area did not reach similarity threshold: best=%s similarity=%.3f threshold=%.3f",
			best.area.ID,
			best.match.Similarity,
			areaMinSimilarity,
		)
	}
	if best.match.Similarity-runnerUp < areaMinMargin {
		return area{}, best.match, fmt.Errorf(
			"delivery area match is ambiguous: best=%s similarity=%.3f runner_up=%.3f margin=%.3f",
			best.area.ID,
			best.match.Similarity,
			runnerUp,
			areaMinMargin,
		)
	}
	return best.area, best.match, nil
}

func resolveDestinationText(ocrText string, destinations []destination) (destination, destinationMatch, error) {
	normalizedOCR := normalizeOCRText(ocrText)
	if normalizedOCR == "" {
		return destination{}, destinationMatch{}, fmt.Errorf("delivery destination OCR is empty after normalization")
	}

	scores := make([]scoredDestination, 0, len(destinations))
	for _, destination := range destinations {
		var match destinationMatch
		// 普通任务目标会显示 buyerName；回收站任务只显示通用的完整 mission。
		if destination.Kind == destinationKindRecycleBin {
			match = bestTextMatch(normalizedOCR, destination.ObjectiveTexts)
		} else {
			match = bestTextMatch(normalizedOCR, destination.DestinationTexts)
			match.DestinationText = match.ObjectiveText
		}
		scores = append(scores, scoredDestination{destination: destination, match: match})
	}
	return selectDestination("delivery destination", scores)
}

func resolveDestination(ocrText string) (destination, destinationMatch, error) {
	destinations, err := getDestinations()
	if err != nil {
		return destination{}, destinationMatch{}, err
	}
	normalizedOCR := normalizeOCRText(ocrText)
	if normalizedOCR == "" {
		return destination{}, destinationMatch{}, fmt.Errorf("delivery objective OCR is empty after normalization")
	}

	scores := make([]scoredDestination, 0, len(destinations))
	for _, destination := range destinations {
		best := bestTextMatch(normalizedOCR, destination.ObjectiveTexts)
		scores = append(scores, scoredDestination{destination: destination, match: best})
	}
	selected, match, err := selectDestination("delivery objective", scores)
	if err != nil {
		return destination{}, match, err
	}

	// 兼容旧 Pipeline 直接传入完整任务目标的调用。除完整目标外还要独立匹配终点名，
	// 避免多语言任务文案中重复的长句框架掩盖短终点名缺失。
	termDestination, termMatch, err := resolveDestinationText(ocrText, destinations)
	if err != nil {
		return destination{}, match, fmt.Errorf("delivery objective destination term is not reliable: %w", err)
	}
	if termDestination.ID != selected.ID {
		return destination{}, match, fmt.Errorf(
			"delivery objective and destination term disagree: objective=%s destination=%s",
			selected.ID,
			termDestination.ID,
		)
	}
	match.DestinationText = termMatch.DestinationText
	return selected, match, nil
}

func bestTextMatch(normalizedOCR string, expectedTexts []string) destinationMatch {
	best := destinationMatch{Distance: -1}
	for _, expectedText := range expectedTexts {
		normalizedExpected := normalizeOCRText(expectedText)
		distance := objectiveEditDistance(normalizedOCR, normalizedExpected)
		similarity := normalizedEditSimilarity(normalizedExpected, distance)
		if similarity > best.Similarity || similarity == best.Similarity && (best.Distance < 0 || distance < best.Distance) {
			best = destinationMatch{
				ObjectiveText: expectedText,
				Distance:      distance,
				Similarity:    similarity,
			}
		}
	}
	return best
}

func selectDestination(label string, scores []scoredDestination) (destination, destinationMatch, error) {
	if len(scores) == 0 {
		return destination{}, destinationMatch{}, fmt.Errorf("delivery destination candidate list is empty")
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].match.Similarity != scores[j].match.Similarity {
			return scores[i].match.Similarity > scores[j].match.Similarity
		}
		return scores[i].destination.ID < scores[j].destination.ID
	})

	best := scores[0]
	runnerUpSimilarity := 0.0
	if len(scores) > 1 {
		runnerUpSimilarity = scores[1].match.Similarity
	}
	best.match.RunnerUpSimilarity = runnerUpSimilarity

	if best.match.Similarity < destinationMinSimilarity {
		return destination{}, best.match, fmt.Errorf(
			"%s did not reach similarity threshold: best=%s similarity=%.3f threshold=%.3f",
			label,
			best.destination.ID,
			best.match.Similarity,
			destinationMinSimilarity,
		)
	}
	if best.match.Similarity-runnerUpSimilarity < destinationMinMargin {
		return destination{}, best.match, fmt.Errorf(
			"%s match is ambiguous: best=%s similarity=%.3f runner_up=%.3f margin=%.3f",
			label,
			best.destination.ID,
			best.match.Similarity,
			runnerUpSimilarity,
			destinationMinMargin,
		)
	}

	return best.destination, best.match, nil
}

func normalizeOCRText(text string) string {
	var normalized strings.Builder
	for _, r := range text {
		if r >= '！' && r <= '～' {
			r -= '！' - '!'
		}
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}

// objectiveEditDistance 计算预期目标与 OCR 文本中最佳子串的编辑距离。
// ROI 可能同时包含换行后的英文、日文目标和下一条任务，因此忽略最佳子串之外的文本。
func objectiveEditDistance(ocrText, objectiveText string) int {
	ocrRunes := []rune(ocrText)
	objectiveRunes := []rune(objectiveText)
	if len(ocrRunes) <= len(objectiveRunes) {
		return levenshtein.Distance(ocrText, objectiveText)
	}

	previous := make([]int, len(ocrRunes)+1)
	current := make([]int, len(ocrRunes)+1)
	for objectiveIndex, objectiveRune := range objectiveRunes {
		current[0] = objectiveIndex + 1
		for ocrIndex, ocrRune := range ocrRunes {
			cost := 0
			if objectiveRune != ocrRune {
				cost = 1
			}
			current[ocrIndex+1] = min(
				previous[ocrIndex+1]+1,
				current[ocrIndex]+1,
				previous[ocrIndex]+cost,
			)
		}
		previous, current = current, previous
	}

	best := previous[0]
	for _, distance := range previous[1:] {
		best = min(best, distance)
	}
	return best
}

func normalizedEditSimilarity(objectiveText string, distance int) float64 {
	length := len([]rune(objectiveText))
	if length == 0 {
		return 1
	}
	return 1 - float64(distance)/float64(length)
}
