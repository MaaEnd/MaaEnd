package sellproduct

import (
	"fmt"
	"sync"
)

var (
	// loadOperatorSelectionDataFunc 是为单元测试保留的替换点。
	loadOperatorSelectionDataFunc = loadOperatorSelectionDataCached
	operatorSelectionDataOnce     sync.Once
	operatorSelectionDataCache    *operatorSelectionData
	operatorSelectionDataErr      error
)

// operatorSelectionData 是从生成数据展开出的最小运行时候选集。
type operatorSelectionData struct {
	TargetCandidates map[string][]operatorCandidate
	RestoreGroups    []operatorCandidateGroup
	KnownOperators   []operatorCandidate
	LocationOrder    []string
	LocationNames    map[string]string
}

func loadOperatorSelectionData() (*operatorSelectionData, error) {
	data, err := loadSellProductSelectionDataCached()
	if err != nil {
		return nil, err
	}
	return buildOperatorSelectionData(data)
}

// loadOperatorSelectionDataCached 在 Agent 生命周期内复用不可变的候选数据。
func loadOperatorSelectionDataCached() (*operatorSelectionData, error) {
	operatorSelectionDataOnce.Do(func() {
		operatorSelectionDataCache, operatorSelectionDataErr = loadOperatorSelectionData()
	})
	return operatorSelectionDataCache, operatorSelectionDataErr
}

func buildOperatorSelectionData(data *sellProductSelectionDataFile) (*operatorSelectionData, error) {
	if err := validateSellProductSelectionData(data); err != nil {
		return nil, err
	}
	result := &operatorSelectionData{
		TargetCandidates: make(map[string][]operatorCandidate, len(data.LocationOrder)),
		RestoreGroups:    make([]operatorCandidateGroup, 0, len(data.LocationOrder)),
		KnownOperators:   make([]operatorCandidate, 0, len(data.KnownOperatorOrder)),
		LocationOrder:    append([]string(nil), data.LocationOrder...),
		LocationNames:    make(map[string]string, len(data.LocationOrder)),
	}

	for priority, name := range data.KnownOperatorOrder {
		candidate, err := selectionOperatorCandidate(data, name, priority)
		if err != nil {
			return nil, fmt.Errorf("known operator: %w", err)
		}
		result.KnownOperators = append(result.KnownOperators, candidate)
	}
	for _, locationName := range data.LocationOrder {
		location, ok := data.Locations[locationName]
		if !ok {
			return nil, fmt.Errorf("location %q not found", locationName)
		}
		result.LocationNames[locationName] = localizedSelectionName(location.Names, locationName)
		targetCandidates, err := buildSelectionOperatorCandidates(data, location.TargetOperators)
		if err != nil {
			return nil, fmt.Errorf("location %q target operators: %w", locationName, err)
		}
		restoreCandidates, err := buildSelectionOperatorCandidates(data, location.RestoreOperators)
		if err != nil {
			return nil, fmt.Errorf("location %q restore operators: %w", locationName, err)
		}
		result.TargetCandidates[locationName] = targetCandidates
		if len(restoreCandidates) > 0 {
			result.RestoreGroups = append(result.RestoreGroups, operatorCandidateGroup{
				Location:   locationName,
				Candidates: restoreCandidates,
			})
		}
	}
	result.KnownOperators = normalizeOperatorCandidates(result.KnownOperators)
	result.RestoreGroups = normalizeOperatorCandidateGroups(result.RestoreGroups)
	return result, nil
}

func buildSelectionOperatorCandidates(
	data *sellProductSelectionDataFile,
	names []string,
) ([]operatorCandidate, error) {
	candidates := make([]operatorCandidate, 0, len(names))
	for priority, name := range names {
		candidate, err := selectionOperatorCandidate(data, name, priority)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return normalizeOperatorCandidates(candidates), nil
}
