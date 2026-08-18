package seizedeliveryjobs

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/levenshtein"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	deliveryDestinationMinSimilarity = 0.70
	deliveryDestinationMinMargin     = 0.05
	deliveryAreaMinSimilarity        = 0.70
	deliveryAreaMinMargin            = 0.05

	deliveryDestinationCatalogResourcePath = "data/SeizeDeliveryJobs/delivery_destinations.json"
	deliveryNavigationConfigResourcePath   = "data/SeizeDeliveryJobs/destinations.json"
)

type deliveryArea struct {
	ID    string
	Texts []string
}

type deliveryDestination struct {
	ID                      string
	AreaID                  string
	MapNavigatorSourceID    string
	MapNavigatorZone        string
	MapNavigatorTarget      [2]float64
	MapNavigatorTargetDeckY *float64
	InitialPathPrefix       []any
	InitialPathSuffix       []any
	DestinationTexts        []string
	ObjectiveTexts          []string
}

type deliveryNavigatorCatalog struct {
	Destinations []deliveryNavigatorDestination `json:"destinations"`
}

type deliveryNavigatorDestination struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	Map     string            `json:"map"`
	U       float64           `json:"u"`
	V       float64           `json:"v"`
	Name    map[string]string `json:"name"`
	Mission map[string]string `json:"mission"`
	Area    map[string]string `json:"area"`
}

type deliveryNavigationConfig struct {
	Description string                        `json:"description"`
	Maps        []deliveryNavigationMapConfig `json:"maps"`
}

type deliveryNavigationMapConfig struct {
	ID            string                         `json:"id"`
	NavigatorZone string                         `json:"navigator_zone"`
	Description   string                         `json:"description"`
	Areas         []deliveryNavigationAreaConfig `json:"areas"`
}

type deliveryNavigationAreaConfig struct {
	ID                string                                `json:"id"`
	Description       string                                `json:"description"`
	InitialPathPrefix []any                                 `json:"initial_path_prefix"`
	Destinations      []deliveryNavigationDestinationConfig `json:"destinations"`
}

type deliveryNavigationDestinationConfig struct {
	SourceID          string      `json:"source_id"`
	ID                string      `json:"id"`
	Description       string      `json:"description"`
	TargetOverride    *[2]float64 `json:"target_override"`
	TargetDeckY       *float64    `json:"target_deck_y"`
	InitialPathSuffix []any       `json:"initial_path_suffix"`
}

type deliveryDestinationMatch struct {
	ObjectiveText      string
	AreaText           string
	DestinationText    string
	Distance           int
	Similarity         float64
	RunnerUpSimilarity float64
	AreaSimilarity     float64
	AreaRunnerUp       float64
}

var deliveryAreaAliases = map[string][]string{
	"WulingCity": {"武陵城"},
}

var deliveryRecycleDestinationTexts = []string{
	"资源回收站",
	"資源回收站",
	"Recycling Station",
	"資源回収所",
	"재활용센터",
}

type deliveryDestinationCache struct {
	sync.Once
	areas        []deliveryArea
	destinations []deliveryDestination
	err          error
}

var (
	deliveryDestinationCatalogPathFunc = func() string { return deliveryDestinationCatalogResourcePath }
	deliveryNavigationConfigPathFunc   = func() string { return deliveryNavigationConfigResourcePath }
	deliveryDestinationCatalog         deliveryDestinationCache
)

type scoredDeliveryDestination struct {
	destination deliveryDestination
	match       deliveryDestinationMatch
}

type scoredDeliveryArea struct {
	area  deliveryArea
	match deliveryDestinationMatch
}

// resolveDeliveryDestinationByArea resolves a destination from two independently recognized UI fields.
// The area is resolved first so identical destination names in different areas remain unambiguous.
func resolveDeliveryDestinationByArea(areaOCR, destinationOCR string) (deliveryDestination, deliveryDestinationMatch, error) {
	areas, err := getDeliveryAreas()
	if err != nil {
		return deliveryDestination{}, deliveryDestinationMatch{}, err
	}
	destinations, err := getDeliveryDestinations()
	if err != nil {
		return deliveryDestination{}, deliveryDestinationMatch{}, err
	}

	area, areaMatch, err := resolveDeliveryArea(areaOCR, areas)
	if err != nil {
		return deliveryDestination{}, areaMatch, err
	}

	candidates := make([]deliveryDestination, 0, len(destinations))
	for _, destination := range destinations {
		if destination.AreaID == area.ID {
			candidates = append(candidates, destination)
		}
	}
	if len(candidates) == 0 {
		return deliveryDestination{}, areaMatch, fmt.Errorf("delivery area %q has no destination candidates", area.ID)
	}

	destination, match, err := resolveDeliveryDestinationText(destinationOCR, candidates)
	match.AreaText = areaMatch.AreaText
	match.AreaSimilarity = areaMatch.Similarity
	match.AreaRunnerUp = areaMatch.RunnerUpSimilarity
	if err != nil {
		return deliveryDestination{}, match, err
	}
	return destination, match, nil
}

func resolveDeliveryArea(ocrText string, areas []deliveryArea) (deliveryArea, deliveryDestinationMatch, error) {
	normalizedOCR := normalizeDeliveryOCRText(ocrText)
	if normalizedOCR == "" {
		return deliveryArea{}, deliveryDestinationMatch{}, fmt.Errorf("delivery area OCR is empty after normalization")
	}

	scores := make([]scoredDeliveryArea, 0, len(areas))
	for _, area := range areas {
		match := bestDeliveryTextMatch(normalizedOCR, area.Texts)
		match.AreaText = match.ObjectiveText
		scores = append(scores, scoredDeliveryArea{area: area, match: match})
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
	if best.match.Similarity < deliveryAreaMinSimilarity {
		return deliveryArea{}, best.match, fmt.Errorf(
			"delivery area did not reach similarity threshold: best=%s similarity=%.3f threshold=%.3f",
			best.area.ID,
			best.match.Similarity,
			deliveryAreaMinSimilarity,
		)
	}
	if best.match.Similarity-runnerUp < deliveryAreaMinMargin {
		return deliveryArea{}, best.match, fmt.Errorf(
			"delivery area match is ambiguous: best=%s similarity=%.3f runner_up=%.3f margin=%.3f",
			best.area.ID,
			best.match.Similarity,
			runnerUp,
			deliveryAreaMinMargin,
		)
	}
	return best.area, best.match, nil
}

func resolveDeliveryDestinationText(ocrText string, destinations []deliveryDestination) (deliveryDestination, deliveryDestinationMatch, error) {
	normalizedOCR := normalizeDeliveryOCRText(ocrText)
	if normalizedOCR == "" {
		return deliveryDestination{}, deliveryDestinationMatch{}, fmt.Errorf("delivery destination OCR is empty after normalization")
	}

	scores := make([]scoredDeliveryDestination, 0, len(destinations))
	for _, destination := range destinations {
		match := bestDeliveryTextMatch(normalizedOCR, destination.DestinationTexts)
		match.DestinationText = match.ObjectiveText
		scores = append(scores, scoredDeliveryDestination{destination: destination, match: match})
	}
	return selectDeliveryDestination("delivery destination", scores)
}

func resolveDeliveryDestination(ocrText string) (deliveryDestination, deliveryDestinationMatch, error) {
	destinations, err := getDeliveryDestinations()
	if err != nil {
		return deliveryDestination{}, deliveryDestinationMatch{}, err
	}
	normalizedOCR := normalizeDeliveryOCRText(ocrText)
	if normalizedOCR == "" {
		return deliveryDestination{}, deliveryDestinationMatch{}, fmt.Errorf("delivery objective OCR is empty after normalization")
	}

	scores := make([]scoredDeliveryDestination, 0, len(destinations))
	for _, destination := range destinations {
		best := bestDeliveryTextMatch(normalizedOCR, destination.ObjectiveTexts)
		scores = append(scores, scoredDeliveryDestination{destination: destination, match: best})
	}
	destination, match, err := selectDeliveryDestination("delivery objective", scores)
	if err != nil {
		return deliveryDestination{}, match, err
	}

	// During Pipeline migration, the legacy input still contains the complete objective. Require an independent
	// destination-name match as well, otherwise a long common sentence scaffold could hide a missing short name.
	termDestination, termMatch, err := resolveDeliveryDestinationText(ocrText, destinations)
	if err != nil {
		return deliveryDestination{}, match, fmt.Errorf("delivery objective destination term is not reliable: %w", err)
	}
	if termDestination.ID != destination.ID {
		return deliveryDestination{}, match, fmt.Errorf(
			"delivery objective and destination term disagree: objective=%s destination=%s",
			destination.ID,
			termDestination.ID,
		)
	}
	match.DestinationText = termMatch.DestinationText
	return destination, match, nil
}

func bestDeliveryTextMatch(normalizedOCR string, expectedTexts []string) deliveryDestinationMatch {
	best := deliveryDestinationMatch{Distance: -1}
	for _, expectedText := range expectedTexts {
		normalizedExpected := normalizeDeliveryOCRText(expectedText)
		distance := deliveryObjectiveEditDistance(normalizedOCR, normalizedExpected)
		similarity := normalizedEditSimilarity(normalizedExpected, distance)
		if similarity > best.Similarity || similarity == best.Similarity && (best.Distance < 0 || distance < best.Distance) {
			best = deliveryDestinationMatch{
				ObjectiveText: expectedText,
				Distance:      distance,
				Similarity:    similarity,
			}
		}
	}
	return best
}

func selectDeliveryDestination(label string, scores []scoredDeliveryDestination) (deliveryDestination, deliveryDestinationMatch, error) {
	if len(scores) == 0 {
		return deliveryDestination{}, deliveryDestinationMatch{}, fmt.Errorf("delivery destination candidate list is empty")
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

	if best.match.Similarity < deliveryDestinationMinSimilarity {
		return deliveryDestination{}, best.match, fmt.Errorf(
			"%s did not reach similarity threshold: best=%s similarity=%.3f threshold=%.3f",
			label,
			best.destination.ID,
			best.match.Similarity,
			deliveryDestinationMinSimilarity,
		)
	}
	if best.match.Similarity-runnerUpSimilarity < deliveryDestinationMinMargin {
		return deliveryDestination{}, best.match, fmt.Errorf(
			"%s match is ambiguous: best=%s similarity=%.3f runner_up=%.3f margin=%.3f",
			label,
			best.destination.ID,
			best.match.Similarity,
			runnerUpSimilarity,
			deliveryDestinationMinMargin,
		)
	}

	return best.destination, best.match, nil
}

func getDeliveryAreas() ([]deliveryArea, error) {
	_, err := getDeliveryDestinations()
	return deliveryDestinationCatalog.areas, err
}

func getDeliveryDestinations() ([]deliveryDestination, error) {
	deliveryDestinationCatalog.Do(func() {
		areas, destinations, err := loadDeliveryDestinationData()
		if err != nil {
			deliveryDestinationCatalog.err = err
			return
		}
		deliveryDestinationCatalog.areas = areas
		deliveryDestinationCatalog.destinations = destinations
	})
	return deliveryDestinationCatalog.destinations, deliveryDestinationCatalog.err
}

func loadDeliveryDestinationData() ([]deliveryArea, []deliveryDestination, error) {
	var navigatorCatalog deliveryNavigatorCatalog
	navigatorCatalogPath := deliveryDestinationCatalogPathFunc()
	if err := resource.ReadJsonResource(navigatorCatalogPath, &navigatorCatalog); err != nil {
		return nil, nil, fmt.Errorf("load MapNavigator destination catalog %q: %w", navigatorCatalogPath, err)
	}
	if len(navigatorCatalog.Destinations) == 0 {
		return nil, nil, fmt.Errorf("MapNavigator destination catalog %q is empty", navigatorCatalogPath)
	}

	navigationConfigPath := deliveryNavigationConfigPathFunc()
	var navigationConfig deliveryNavigationConfig
	if err := resource.ReadJsonResource(navigationConfigPath, &navigationConfig); err != nil {
		return nil, nil, fmt.Errorf("load delivery navigation config %q: %w", navigationConfigPath, err)
	}
	return buildDeliveryDestinationData(navigatorCatalog, navigationConfig)
}

func buildDeliveryDestinationData(
	navigatorCatalog deliveryNavigatorCatalog,
	navigationConfig deliveryNavigationConfig,
) ([]deliveryArea, []deliveryDestination, error) {
	sources := make(map[string]deliveryNavigatorDestination, len(navigatorCatalog.Destinations))
	for index, source := range navigatorCatalog.Destinations {
		if strings.TrimSpace(source.ID) == "" {
			return nil, nil, fmt.Errorf("MapNavigator destination at index %d has an empty id", index)
		}
		if _, exists := sources[source.ID]; exists {
			return nil, nil, fmt.Errorf("duplicate MapNavigator destination id %q", source.ID)
		}
		sources[source.ID] = source
	}

	if len(navigationConfig.Maps) == 0 {
		return nil, nil, fmt.Errorf("delivery navigation config has no maps")
	}
	seenMapIDs := make(map[string]struct{}, len(navigationConfig.Maps))
	seenAreaIDs := make(map[string]struct{})
	seenSourceIDs := make(map[string]struct{})
	seenBusinessIDs := make(map[string]struct{})
	areas := make([]deliveryArea, 0)
	destinations := make([]deliveryDestination, 0)
	for mapIndex, mapConfig := range navigationConfig.Maps {
		if strings.TrimSpace(mapConfig.ID) == "" || strings.TrimSpace(mapConfig.NavigatorZone) == "" {
			return nil, nil, fmt.Errorf("delivery map at index %d has an empty id or navigator_zone", mapIndex)
		}
		if _, exists := seenMapIDs[mapConfig.ID]; exists {
			return nil, nil, fmt.Errorf("duplicate delivery map id %q", mapConfig.ID)
		}
		seenMapIDs[mapConfig.ID] = struct{}{}
		if len(mapConfig.Areas) == 0 {
			return nil, nil, fmt.Errorf("delivery map %q has no areas", mapConfig.ID)
		}

		for areaIndex, areaConfig := range mapConfig.Areas {
			if strings.TrimSpace(areaConfig.ID) == "" {
				return nil, nil, fmt.Errorf("delivery map %q area at index %d has an empty id", mapConfig.ID, areaIndex)
			}
			if _, exists := seenAreaIDs[areaConfig.ID]; exists {
				return nil, nil, fmt.Errorf("duplicate delivery area id %q", areaConfig.ID)
			}
			seenAreaIDs[areaConfig.ID] = struct{}{}
			if len(areaConfig.Destinations) == 0 {
				return nil, nil, fmt.Errorf("delivery area %q has no destinations", areaConfig.ID)
			}

			areaTexts := make([]string, 0)
			for destinationIndex, destinationConfig := range areaConfig.Destinations {
				owner := fmt.Sprintf("delivery area %q destination at index %d", areaConfig.ID, destinationIndex)
				if strings.TrimSpace(destinationConfig.SourceID) == "" || strings.TrimSpace(destinationConfig.ID) == "" {
					return nil, nil, fmt.Errorf("%s has an empty source_id or id", owner)
				}
				if _, exists := seenSourceIDs[destinationConfig.SourceID]; exists {
					return nil, nil, fmt.Errorf("duplicate delivery destination source_id %q", destinationConfig.SourceID)
				}
				seenSourceIDs[destinationConfig.SourceID] = struct{}{}
				if _, exists := seenBusinessIDs[destinationConfig.ID]; exists {
					return nil, nil, fmt.Errorf("duplicate delivery destination id %q", destinationConfig.ID)
				}
				seenBusinessIDs[destinationConfig.ID] = struct{}{}

				source, exists := sources[destinationConfig.SourceID]
				if !exists {
					return nil, nil, fmt.Errorf("%s source_id %q is not present in MapNavigator destination catalog", owner, destinationConfig.SourceID)
				}
				if source.Map != mapConfig.ID {
					return nil, nil, fmt.Errorf("%s catalog map %q does not match delivery map %q", owner, source.Map, mapConfig.ID)
				}

				mapNavigatorTarget := [2]float64{source.U, source.V}
				if destinationConfig.TargetOverride != nil {
					mapNavigatorTarget = *destinationConfig.TargetOverride
				}
				destinationTexts := localizedDeliveryTexts(source.Name)
				if source.Kind == "recycle_bin" {
					destinationTexts = append([]string(nil), deliveryRecycleDestinationTexts...)
				}
				objectiveTexts := localizedDeliveryTexts(source.Mission)
				sourceAreaTexts := localizedDeliveryTexts(source.Area)
				if len(destinationTexts) == 0 || len(objectiveTexts) == 0 || len(sourceAreaTexts) == 0 {
					return nil, nil, fmt.Errorf("%s source_id %q has incomplete localized text", owner, destinationConfig.SourceID)
				}

				destinations = append(destinations, deliveryDestination{
					ID:                      destinationConfig.ID,
					AreaID:                  areaConfig.ID,
					MapNavigatorSourceID:    source.ID,
					MapNavigatorZone:        mapConfig.NavigatorZone,
					MapNavigatorTarget:      mapNavigatorTarget,
					MapNavigatorTargetDeckY: destinationConfig.TargetDeckY,
					InitialPathPrefix:       areaConfig.InitialPathPrefix,
					InitialPathSuffix:       destinationConfig.InitialPathSuffix,
					DestinationTexts:        destinationTexts,
					ObjectiveTexts:          objectiveTexts,
				})
				areaTexts = appendUniqueDeliveryTexts(areaTexts, sourceAreaTexts...)
			}
			areas = append(areas, deliveryArea{
				ID:    areaConfig.ID,
				Texts: appendUniqueDeliveryTexts(areaTexts, deliveryAreaAliases[areaConfig.ID]...),
			})
		}
	}

	sort.Slice(areas, func(i, j int) bool {
		return areas[i].ID < areas[j].ID
	})
	return areas, destinations, nil
}

func resetDeliveryDestinationCatalogForTest() {
	deliveryDestinationCatalog = deliveryDestinationCache{}
}

func localizedDeliveryTexts(localized map[string]string) []string {
	languages := make([]string, 0, len(localized))
	for language := range localized {
		languages = append(languages, language)
	}
	sort.Strings(languages)

	texts := make([]string, 0, len(languages))
	for _, language := range languages {
		texts = appendUniqueDeliveryTexts(texts, localized[language])
	}
	return texts
}

func appendUniqueDeliveryTexts(texts []string, candidates ...string) []string {
	seen := make(map[string]struct{}, len(texts)+len(candidates))
	for _, text := range texts {
		seen[text] = struct{}{}
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		texts = append(texts, candidate)
	}
	return texts
}

func normalizeDeliveryOCRText(text string) string {
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

// deliveryObjectiveEditDistance returns the edit distance between the expected objective and its best matching
// substring in the OCR text. Text outside the matched substring is ignored because the ROI can contain another
// objective below a wrapped English or Japanese delivery objective.
func deliveryObjectiveEditDistance(ocrText, objectiveText string) int {
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

func deliveryObjectiveText(detail *maa.RecognitionDetail) (string, error) {
	if detail == nil || detail.Results == nil {
		return "", fmt.Errorf("delivery objective OCR returned no recognition results")
	}

	results := detail.Results.Filtered
	if len(results) == 0 {
		results = detail.Results.All
	}
	if len(results) == 0 && detail.Results.Best != nil {
		results = []*maa.RecognitionResult{detail.Results.Best}
	}

	var text strings.Builder
	for _, result := range results {
		if result == nil {
			continue
		}
		ocr, ok := result.AsOCR()
		if !ok || ocr == nil {
			continue
		}
		text.WriteString(strings.TrimSpace(ocr.Text))
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("delivery objective OCR returned no text")
	}
	return text.String(), nil
}
