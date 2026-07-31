package operator

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/selectiondata"
)

var (
	// loadOperatorSelectionDataFunc 是单元测试使用的数据加载注入点。
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
}

// operatorCandidate 描述一个可供自动选择的干员。
// Priority 的数值越小优先级越高；Expected 保存各语言 OCR 可能识别出的完整名称。
type operatorCandidate struct {
	Name                          string   `json:"name"`
	Expected                      []string `json:"expected"`
	Priority                      int      `json:"priority"`
	BonusTier                     int      `json:"bonus_tier"`
	OutpostProsperityMaxBonusTier int      `json:"outpost_prosperity_max_bonus_tier"`
}

// operatorCandidateGroup 表示某个据点及其可恢复到该岗位的干员集合。
type operatorCandidateGroup struct {
	Location   string              `json:"location"`
	Candidates []operatorCandidate `json:"candidates"`
}

func loadOperatorSelectionData() (*operatorSelectionData, error) {
	data, err := loadSelectionData()
	if err != nil {
		return nil, err
	}
	return buildOperatorSelectionData(data)
}

func loadSelectionData() (*selectiondata.File, error) {
	data, err := selectiondata.LoadCached()
	if err != nil {
		return nil, err
	}
	if err := selectiondata.ValidateOperators(data); err != nil {
		return nil, err
	}
	return data, nil
}

// loadOperatorSelectionDataCached 在 Agent 生命周期内复用不可变的候选数据。
func loadOperatorSelectionDataCached() (*operatorSelectionData, error) {
	operatorSelectionDataOnce.Do(func() {
		operatorSelectionDataCache, operatorSelectionDataErr = loadOperatorSelectionData()
	})
	return operatorSelectionDataCache, operatorSelectionDataErr
}

func buildOperatorSelectionData(data *selectiondata.File) (*operatorSelectionData, error) {
	if err := selectiondata.ValidateOperators(data); err != nil {
		return nil, err
	}
	result := &operatorSelectionData{
		TargetCandidates: make(map[string][]operatorCandidate, len(data.LocationOrder)),
		RestoreGroups:    make([]operatorCandidateGroup, 0, len(data.LocationOrder)),
		KnownOperators:   make([]operatorCandidate, 0, len(data.Operators)),
		LocationOrder:    append([]string(nil), data.LocationOrder...),
	}

	operatorNames := make([]string, 0, len(data.Operators))
	for name := range data.Operators {
		operatorNames = append(operatorNames, name)
	}
	sort.Strings(operatorNames)
	for priority, name := range operatorNames {
		candidate, err := operatorCandidateFromData(data, name, priority, 0, 0)
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
		targetCandidates, err := buildTargetSelectionOperatorCandidates(data, location.TargetOperators)
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
	data *selectiondata.File,
	names []string,
) ([]operatorCandidate, error) {
	candidates := make([]operatorCandidate, 0, len(names))
	for priority, name := range names {
		candidate, err := operatorCandidateFromData(data, name, priority, 0, 0)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return normalizeOperatorCandidates(candidates), nil
}

func buildTargetSelectionOperatorCandidates(
	data *selectiondata.File,
	entries []selectiondata.TargetOperator,
) ([]operatorCandidate, error) {
	candidates := make([]operatorCandidate, 0, len(entries))
	for priority, entry := range entries {
		candidate, err := operatorCandidateFromData(
			data,
			entry.Name,
			priority,
			entry.BonusTier,
			entry.OutpostProsperityMaxBonusTier,
		)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return normalizeOperatorCandidates(candidates), nil
}

func operatorCandidateFromData(
	data *selectiondata.File,
	name string,
	priority int,
	bonusTier int,
	outpostProsperityMaxBonusTier int,
) (operatorCandidate, error) {
	name = strings.TrimSpace(name)
	entry, ok := data.Operators[name]
	if !ok {
		return operatorCandidate{}, fmt.Errorf("operator %q not found", name)
	}
	candidate := operatorCandidate{
		Name:                          name,
		Expected:                      selectiondata.ExpectedNames(entry.Names),
		Priority:                      priority,
		BonusTier:                     bonusTier,
		OutpostProsperityMaxBonusTier: outpostProsperityMaxBonusTier,
	}
	normalized := normalizeOperatorCandidates([]operatorCandidate{candidate})
	if len(normalized) == 0 {
		return operatorCandidate{}, fmt.Errorf("operator %q data is invalid", name)
	}
	return normalized[0], nil
}

// normalizeOperatorCandidates 清洗候选数据并生成稳定顺序。
// 去重以内部 Name 为准，同名候选只保留第一次出现的配置。
func normalizeOperatorCandidates(candidates []operatorCandidate) []operatorCandidate {
	normalized := make([]operatorCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate.Name = strings.TrimSpace(candidate.Name)
		candidate.Expected = uniqueNonEmptyStrings(candidate.Expected)
		if candidate.Name == "" || len(candidate.Expected) == 0 {
			continue
		}
		if _, ok := seen[candidate.Name]; ok {
			continue
		}
		seen[candidate.Name] = struct{}{}
		normalized = append(normalized, candidate)
	}
	sortOperatorCandidates(normalized)
	return normalized
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func sortOperatorCandidates(candidates []operatorCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].BonusTier != candidates[j].BonusTier {
			return candidates[i].BonusTier < candidates[j].BonusTier
		}
		return candidates[i].Priority < candidates[j].Priority
	})
}

func normalizeOperatorCandidateGroups(groups []operatorCandidateGroup) []operatorCandidateGroup {
	normalized := make([]operatorCandidateGroup, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		group.Location = strings.TrimSpace(group.Location)
		if group.Location == "" {
			continue
		}
		if _, ok := seen[group.Location]; ok {
			continue
		}
		group.Candidates = normalizeOperatorCandidates(group.Candidates)
		if len(group.Candidates) == 0 {
			continue
		}
		seen[group.Location] = struct{}{}
		normalized = append(normalized, group)
	}
	return normalized
}
