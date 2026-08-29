package autodelivery

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
)

const (
	catalogResourcePath       = "data/AutoDelivery/catalog.json"
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
	Map              string
	AreaID           string
	DepotID          string
	RouteNode        string
	ZipRouteNode     string
	RetryRouteNode   string
	SerialID         int
	Names            map[string]string
	AreaNames        map[string]string
	DestinationTexts []string
	ObjectiveTexts   []string
}

type generatedCatalog struct {
	Depots       []generatedDepot       `json:"depots"`
	Destinations []generatedDestination `json:"destinations"`
}

type generatedDepot struct {
	ID             string            `json:"id"`
	Name           map[string]string `json:"name"`
	Map            string            `json:"map"`
	RouteNode      string            `json:"route_node"`
	ZipRouteNode   string            `json:"zip_route_node"`
	RetryRouteNode string            `json:"retry_route_node"`
}

type generatedDestination struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	SerialID       int               `json:"serial_id"`
	DepotID        string            `json:"depot_id"`
	Name           map[string]string `json:"name"`
	Mission        map[string]string `json:"mission"`
	Area           map[string]string `json:"area"`
	RouteNode      string            `json:"route_node"`
	ZipRouteNode   string            `json:"zip_route_node"`
	RetryRouteNode string            `json:"retry_route_node"`
}

type depot struct {
	ID             string
	Names          map[string]string
	Map            string
	RouteNode      string
	ZipRouteNode   string
	RetryRouteNode string
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

func getDestination(destinationID string) (destination, error) {
	destinations, err := getDestinations()
	if err != nil {
		return destination{}, err
	}
	for _, candidate := range destinations {
		if candidate.ID == destinationID {
			return candidate, nil
		}
	}
	return destination{}, fmt.Errorf("delivery destination %q is not present in the AutoDelivery catalog", destinationID)
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
	if err := resource.ReadJsonResource(catalogResourcePath, &generated); err != nil {
		return nil, nil, nil, fmt.Errorf("load AutoDelivery runtime catalog %q: %w", catalogResourcePath, err)
	}
	depots, err := buildDepots(generated)
	if err != nil {
		return nil, nil, nil, err
	}
	areas, destinations, err := buildDestinations(generated, depots)
	if err != nil {
		return nil, nil, nil, err
	}
	return areas, destinations, depots, nil
}

func buildDepots(generated generatedCatalog) (map[string]depot, error) {
	depots := make(map[string]depot, len(generated.Depots))
	for index, source := range generated.Depots {
		if strings.TrimSpace(source.ID) == "" || strings.TrimSpace(source.Map) == "" {
			return nil, fmt.Errorf("AutoDelivery depot at index %d has an empty id or map", index)
		}
		if strings.TrimSpace(source.RouteNode) == "" || strings.TrimSpace(source.ZipRouteNode) == "" {
			return nil, fmt.Errorf("AutoDelivery depot %q has incomplete route nodes", source.ID)
		}
		if len(localizedTexts(source.Name)) == 0 {
			return nil, fmt.Errorf("AutoDelivery depot %q has no localized name", source.ID)
		}
		if _, exists := depots[source.ID]; exists {
			return nil, fmt.Errorf("duplicate AutoDelivery depot id %q", source.ID)
		}
		depots[source.ID] = depot{
			ID:             source.ID,
			Names:          source.Name,
			Map:            source.Map,
			RouteNode:      source.RouteNode,
			ZipRouteNode:   source.ZipRouteNode,
			RetryRouteNode: source.RetryRouteNode,
		}
	}
	if len(depots) == 0 {
		return nil, fmt.Errorf("AutoDelivery depot catalog is empty")
	}
	return depots, nil
}

func buildDestinations(generated generatedCatalog, depots map[string]depot) ([]area, []destination, error) {
	areaTextsByID := make(map[string][]string)
	areaDepotIDs := make(map[string]string)
	destinations := make([]destination, 0, len(generated.Destinations))
	seen := make(map[string]struct{}, len(generated.Destinations))

	for index, source := range generated.Destinations {
		if strings.TrimSpace(source.ID) == "" {
			return nil, nil, fmt.Errorf("AutoDelivery destination at index %d has an empty id", index)
		}
		if _, exists := seen[source.ID]; exists {
			return nil, nil, fmt.Errorf("duplicate AutoDelivery destination id %q", source.ID)
		}
		seen[source.ID] = struct{}{}
		switch source.Kind {
		case destinationKindNPC, destinationKindRecycleBin:
		default:
			return nil, nil, fmt.Errorf("AutoDelivery destination %q has unknown kind %q", source.ID, source.Kind)
		}
		if source.Kind == destinationKindRecycleBin && source.SerialID <= 0 {
			return nil, nil, fmt.Errorf("AutoDelivery recycle bin destination %q has invalid serial id %d", source.ID, source.SerialID)
		}
		if _, exists := depots[source.DepotID]; !exists {
			return nil, nil, fmt.Errorf("AutoDelivery destination %q references unknown depot %q", source.ID, source.DepotID)
		}
		if strings.TrimSpace(source.RouteNode) == "" || strings.TrimSpace(source.ZipRouteNode) == "" {
			return nil, nil, fmt.Errorf("AutoDelivery destination %q has incomplete route nodes", source.ID)
		}
		depotRoute := depots[source.DepotID]
		areaID, err := localizedAreaID(source.Area)
		if err != nil {
			return nil, nil, fmt.Errorf("AutoDelivery destination %q: %w", source.ID, err)
		}
		if depotID, exists := areaDepotIDs[areaID]; exists && depotID != source.DepotID {
			return nil, nil, fmt.Errorf("delivery area %q mixes depots %q and %q", areaID, depotID, source.DepotID)
		}
		areaDepotIDs[areaID] = source.DepotID

		destinationTexts := localizedTexts(source.Name)
		objectiveTexts := localizedTexts(source.Mission)
		sourceAreaTexts := localizedTexts(source.Area)
		if len(destinationTexts) == 0 || len(objectiveTexts) == 0 || len(sourceAreaTexts) == 0 {
			return nil, nil, fmt.Errorf("AutoDelivery destination %q has incomplete localized text", source.ID)
		}

		destinations = append(destinations, destination{
			ID:               source.ID,
			Kind:             source.Kind,
			Map:              depotRoute.Map,
			AreaID:           areaID,
			DepotID:          source.DepotID,
			RouteNode:        source.RouteNode,
			ZipRouteNode:     source.ZipRouteNode,
			RetryRouteNode:   source.RetryRouteNode,
			SerialID:         source.SerialID,
			Names:            source.Name,
			AreaNames:        source.Area,
			DestinationTexts: destinationTexts,
			ObjectiveTexts:   objectiveTexts,
		})
		areaTextsByID[areaID] = appendUniqueTexts(areaTextsByID[areaID], sourceAreaTexts...)
	}

	sort.Slice(destinations, func(i, j int) bool { return destinations[i].ID < destinations[j].ID })
	areas := make([]area, 0, len(areaTextsByID))
	for areaID, texts := range areaTextsByID {
		areas = append(areas, area{ID: areaID, DepotID: areaDepotIDs[areaID], Texts: texts})
	}
	sort.Slice(areas, func(i, j int) bool { return areas[i].ID < areas[j].ID })
	return areas, destinations, nil
}

func localizedName(names map[string]string, fallback string) string {
	return localizedNameForLang(names, i18n.Lang(), fallback)
}

func localizedNameForLang(names map[string]string, lang string, fallback string) string {
	lang = i18n.NormalizeLang(lang)
	if name := strings.TrimSpace(names[lang]); name != "" {
		return name
	}
	if name := strings.TrimSpace(names[i18n.DefaultLang]); name != "" {
		return name
	}

	languages := make([]string, 0, len(names))
	for language := range names {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	for _, language := range languages {
		if name := strings.TrimSpace(names[language]); name != "" {
			return name
		}
	}
	return fallback
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
