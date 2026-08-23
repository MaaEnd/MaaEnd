package autodelivery

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
)

const (
	catalogResourcePath       = "data/AutoDelivery/delivery_destinations.json"
	overridesResourcePath     = "data/AutoDelivery/overrides.json"
	destinationKindNPC        = "npc"
	destinationKindRecycleBin = "recycle_bin"
)

type area struct {
	ID      string
	DepotID string
	Texts   []string
}

type destination struct {
	ID               string
	Kind             string
	AreaID           string
	DepotID          string
	Path             []any
	DestinationTexts []string
	ObjectiveTexts   []string
}

type generatedCatalog struct {
	Depots       []generatedDepot       `json:"depots"`
	Destinations []generatedDestination `json:"destinations"`
}

type generatedDepot struct {
	ID  string  `json:"id"`
	Map string  `json:"map"`
	U   float64 `json:"u"`
	V   float64 `json:"v"`
}

type generatedDestination struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	DepotID string            `json:"depot_id"`
	U       float64           `json:"u"`
	V       float64           `json:"v"`
	Name    map[string]string `json:"name"`
	Mission map[string]string `json:"mission"`
	Area    map[string]string `json:"area"`
}

type navigationOverrides struct {
	Depots       []depotOverride       `json:"depots"`
	Destinations []destinationOverride `json:"destinations"`
}

type depotOverride struct {
	SourceID      string `json:"source_id"`
	Path          []any  `json:"path"`
	RetryPath     []any  `json:"retry_path"`
	DeparturePath []any  `json:"departure_path"`
}

type destinationOverride struct {
	SourceID string `json:"source_id"`
	Path     []any  `json:"path"`
}

type depot struct {
	ID            string
	Map           string
	Path          []any
	RetryPath     []any
	DeparturePath []any
}

type catalogCache struct {
	sync.Once
	areas        []area
	destinations []destination
	depots       map[string]depot
	err          error
}

var catalog catalogCache

func getAreas() ([]area, error) {
	_, err := getDestinations()
	return catalog.areas, err
}

func getDestinations() ([]destination, error) {
	catalog.Do(func() {
		areas, destinations, depots, err := loadCatalog()
		if err != nil {
			catalog.err = err
			return
		}
		catalog.areas = areas
		catalog.destinations = destinations
		catalog.depots = depots
	})
	return catalog.destinations, catalog.err
}

func getDepot(depotID string) (depot, error) {
	_, err := getDestinations()
	if err != nil {
		return depot{}, err
	}
	route, exists := catalog.depots[depotID]
	if !exists {
		return depot{}, fmt.Errorf("delivery depot %q is not present in the AutoDelivery catalog", depotID)
	}
	return route, nil
}

func loadCatalog() ([]area, []destination, map[string]depot, error) {
	var generated generatedCatalog
	generatedPath := catalogResourcePath
	if err := resource.ReadJsonResource(generatedPath, &generated); err != nil {
		return nil, nil, nil, fmt.Errorf("load MapNavigator destination catalog %q: %w", generatedPath, err)
	}
	if len(generated.Destinations) == 0 {
		return nil, nil, nil, fmt.Errorf("MapNavigator destination catalog %q is empty", generatedPath)
	}

	overridePath := overridesResourcePath
	var overrides navigationOverrides
	if err := resource.ReadJsonResource(overridePath, &overrides); err != nil {
		return nil, nil, nil, fmt.Errorf("load delivery navigation config %q: %w", overridePath, err)
	}
	depots, err := buildDepots(generated, overrides)
	if err != nil {
		return nil, nil, nil, err
	}
	areas, destinations, err := buildDestinations(generated, overrides, depots)
	if err != nil {
		return nil, nil, nil, err
	}
	return areas, destinations, depots, nil
}

func buildDepots(
	generated generatedCatalog,
	overrides navigationOverrides,
) (map[string]depot, error) {
	depots := make(map[string]depot, len(generated.Depots))
	for index, source := range generated.Depots {
		if strings.TrimSpace(source.ID) == "" || strings.TrimSpace(source.Map) == "" {
			return nil, fmt.Errorf("AutoDelivery depot at index %d has an empty id or map", index)
		}
		if _, exists := depots[source.ID]; exists {
			return nil, fmt.Errorf("duplicate AutoDelivery depot id %q", source.ID)
		}
		if source.U < 0 || source.V < 0 {
			return nil, fmt.Errorf("AutoDelivery depot %q has a negative target", source.ID)
		}
		depots[source.ID] = depot{
			ID:  source.ID,
			Map: source.Map,
			Path: []any{
				map[string]any{
					"action": "NAVMESH",
					"target": [2]float64{source.U, source.V},
				},
			},
		}
	}
	if len(depots) == 0 {
		return nil, fmt.Errorf("AutoDelivery depot catalog is empty")
	}

	seenOverrides := make(map[string]struct{}, len(overrides.Depots))
	for index, config := range overrides.Depots {
		if strings.TrimSpace(config.SourceID) == "" {
			return nil, fmt.Errorf("AutoDelivery depot override at index %d has an empty source_id", index)
		}
		if _, exists := seenOverrides[config.SourceID]; exists {
			return nil, fmt.Errorf("duplicate AutoDelivery depot override source_id %q", config.SourceID)
		}
		seenOverrides[config.SourceID] = struct{}{}
		route, exists := depots[config.SourceID]
		if !exists {
			return nil, fmt.Errorf("AutoDelivery depot override %q is not present in the generated catalog", config.SourceID)
		}
		if len(config.Path) == 0 && len(config.RetryPath) == 0 && len(config.DeparturePath) == 0 {
			return nil, fmt.Errorf(
				"AutoDelivery depot override %q has neither path, retry_path nor departure_path",
				config.SourceID,
			)
		}
		if len(config.Path) != 0 {
			route.Path = config.Path
		}
		route.RetryPath = config.RetryPath
		route.DeparturePath = config.DeparturePath
		depots[config.SourceID] = route
	}
	return depots, nil
}

func buildDestinations(
	generated generatedCatalog,
	overrides navigationOverrides,
	depots map[string]depot,
) ([]area, []destination, error) {
	sources := make(map[string]generatedDestination, len(generated.Destinations))
	for index, source := range generated.Destinations {
		if strings.TrimSpace(source.ID) == "" {
			return nil, nil, fmt.Errorf("MapNavigator destination at index %d has an empty id", index)
		}
		if _, exists := sources[source.ID]; exists {
			return nil, nil, fmt.Errorf("duplicate MapNavigator destination id %q", source.ID)
		}
		sources[source.ID] = source
	}

	seenSourceIDs := make(map[string]struct{}, len(overrides.Destinations))
	destinationOverrides := make(map[string]destinationOverride, len(overrides.Destinations))
	for index, config := range overrides.Destinations {
		if strings.TrimSpace(config.SourceID) == "" {
			return nil, nil, fmt.Errorf("delivery destination override at index %d has an empty source_id", index)
		}
		if _, exists := seenSourceIDs[config.SourceID]; exists {
			return nil, nil, fmt.Errorf("duplicate delivery destination source_id %q", config.SourceID)
		}
		seenSourceIDs[config.SourceID] = struct{}{}
		if _, exists := sources[config.SourceID]; !exists {
			return nil, nil, fmt.Errorf(
				"delivery destination override source_id %q is not present in the generated catalog",
				config.SourceID,
			)
		}
		if len(config.Path) == 0 {
			return nil, nil, fmt.Errorf("delivery destination override %q has an empty path", config.SourceID)
		}
		destinationOverrides[config.SourceID] = config
	}

	areaTextsByID := make(map[string][]string)
	areaDepotIDs := make(map[string]string)
	destinations := make([]destination, 0, len(sources))
	sourceIDs := make([]string, 0, len(sources))
	for sourceID := range sources {
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Strings(sourceIDs)

	for _, sourceID := range sourceIDs {
		source := sources[sourceID]
		kind := strings.TrimSpace(source.Kind)
		switch kind {
		case destinationKindNPC, destinationKindRecycleBin:
		default:
			return nil, nil, fmt.Errorf(
				"MapNavigator destination %q has unknown kind %q",
				source.ID,
				source.Kind,
			)
		}
		if source.DepotID != "" {
			if _, exists := depots[source.DepotID]; !exists {
				return nil, nil, fmt.Errorf(
					"MapNavigator destination %q references unknown depot %q",
					source.ID,
					source.DepotID,
				)
			}
		}
		areaID, err := localizedAreaID(source.Area)
		if err != nil {
			return nil, nil, fmt.Errorf("MapNavigator destination %q: %w", source.ID, err)
		}
		var path []any
		if route, exists := depots[source.DepotID]; exists {
			path = append(path, route.DeparturePath...)
		}
		if override, exists := destinationOverrides[source.ID]; exists {
			path = append(path, override.Path...)
		} else {
			path = append(path, map[string]any{
				"action": "NAVMESH",
				"target": [2]float64{source.U, source.V},
			})
		}
		if depotID, exists := areaDepotIDs[areaID]; exists && depotID != source.DepotID {
			return nil, nil, fmt.Errorf(
				"delivery area %q mixes depots %q and %q",
				areaID,
				depotID,
				source.DepotID,
			)
		}
		areaDepotIDs[areaID] = source.DepotID

		destinationTexts := localizedTexts(source.Name)
		objectiveTexts := localizedTexts(source.Mission)
		sourceAreaTexts := localizedTexts(source.Area)
		if len(destinationTexts) == 0 || len(objectiveTexts) == 0 || len(sourceAreaTexts) == 0 {
			return nil, nil, fmt.Errorf("MapNavigator destination %q has incomplete localized text", source.ID)
		}

		destinations = append(destinations, destination{
			ID:               source.ID,
			Kind:             kind,
			AreaID:           areaID,
			DepotID:          source.DepotID,
			Path:             path,
			DestinationTexts: destinationTexts,
			ObjectiveTexts:   objectiveTexts,
		})
		areaTextsByID[areaID] = appendUniqueTexts(areaTextsByID[areaID], sourceAreaTexts...)
	}

	areas := make([]area, 0, len(areaTextsByID))
	for areaID, texts := range areaTextsByID {
		areas = append(areas, area{
			ID:      areaID,
			DepotID: areaDepotIDs[areaID],
			Texts:   texts,
		})
	}
	sort.Slice(areas, func(i, j int) bool {
		return areas[i].ID < areas[j].ID
	})
	return areas, destinations, nil
}

func localizedAreaID(localized map[string]string) (string, error) {
	english := strings.TrimSpace(localized["en_us"])
	if english == "" {
		return "", fmt.Errorf("area has no en_us name")
	}

	var id strings.Builder
	for _, r := range english {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			id.WriteRune(r)
		}
	}
	if id.Len() == 0 {
		return "", fmt.Errorf("area en_us name %q cannot form an id", english)
	}
	return id.String(), nil
}

func localizedTexts(localized map[string]string) []string {
	languages := make([]string, 0, len(localized))
	for language := range localized {
		languages = append(languages, language)
	}
	sort.Strings(languages)

	texts := make([]string, 0, len(languages))
	for _, language := range languages {
		texts = appendUniqueTexts(texts, localized[language])
	}
	return texts
}

func appendUniqueTexts(texts []string, candidates ...string) []string {
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
