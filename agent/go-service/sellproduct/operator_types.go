package sellproduct

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	// 以下名称必须与 Pipeline 中 custom_recognition 对应的组件名保持一致。
	selectBestOperatorRecognitionName  = "SellProductSelectBestOperator"
	currentBestOperatorRecognitionName = "SellProductCurrentBestOperator"
	operatorCacheReadyRecognitionName  = "SellProductOperatorCacheReady"
	operatorListBottomRecognitionName  = "SellProductOperatorListBottom"
	operatorScanOutcomeRecognitionName = "SellProductOperatorScanOutcome"
	operatorConflictRecognitionName    = "SellProductOperatorConflict"
	operatorSessionActionName          = "SellProductOperatorSession"

	// cache 仅复用完整本地快照，缺失时先扫描；refresh 强制先完整扫描一次干员列表。
	operatorCacheModeCache   = "cache"
	operatorCacheModeRefresh = "refresh"

	// usage 决定本次选择针对目标据点、恢复原岗位，还是仅扫描全部候选干员。
	operatorActionUsageTarget  = "target"
	operatorActionUsageRestore = "restore"
	operatorActionUsageAll     = "all"

	// 列表到底后，Pipeline 根据 result 区分“扫描完成”和“缓存中未找到目标”两条分支。
	operatorListBottomResultScanDone = "scan_done"
	operatorListBottomResultRetry    = "retry"
	operatorListBottomResultNotFound = "not_found"
	operatorListBottomResultError    = "error"

	// 派驻冲突按来源据点是否属于本次启用范围分为可接管与受保护两类。
	operatorConflictResultManaged   = "managed"
	operatorConflictResultProtected = "protected"
)

// operatorCandidate 描述一个可供自动选择的干员。
// Priority 的数值越小优先级越高；Expected 保存各语言 OCR 可能识别出的完整名称。
type operatorCandidate struct {
	// Name 是内部稳定标识，主要用于去重、分配和日志输出。
	Name string `json:"name"`
	// DisplayName 是按当前客户端语言选择的用户可见名称。
	DisplayName string `json:"display_name"`
	// Expected 是传给 OCR 匹配逻辑的多语言名称集合。
	Expected []string `json:"expected"`
	// Priority 表示候选顺序，值越小越优先。
	Priority int `json:"priority"`
	// BonusTier 表示据点发展值未满时的加成档位，先比较发展值、再比较交易收益，值越小越优先。
	BonusTier int `json:"bonus_tier"`
	// OutpostProsperityMaxBonusTier 表示据点发展值已满时忽略发展值加成后的售卖档位。
	OutpostProsperityMaxBonusTier int `json:"outpost_prosperity_max_bonus_tier"`
}

// operatorCandidateGroup 表示某个据点及其可恢复到该岗位的干员集合。
type operatorCandidateGroup struct {
	Location   string              `json:"location"`
	Candidates []operatorCandidate `json:"candidates"`
}

// operatorActionParam 是 Pipeline 传入自定义识别器的原始参数。
// ROI 坐标以项目统一的 1280x720 基准分辨率表示。
type operatorActionParam struct {
	Mode     string `json:"mode"`
	Usage    string `json:"usage"`
	Location string `json:"location"`
	Result   string `json:"result"`
	ROI      []int  `json:"roi"`
}

// operatorSelectionParam 是补齐资源数据后的运行时参数。
// Candidates 用于目标据点选择，RestoreGroups 用于全局恢复分配，ScanCandidates
// 则覆盖所有可能出现的相关干员，供刷新拥有列表缓存时统一识别。
type operatorSelectionParam struct {
	Usage                         string
	Location                      string
	Candidates                    []operatorCandidate
	TargetCandidatesByLocation    map[string][]operatorCandidate
	RestoreGroups                 []operatorCandidateGroup
	ScanCandidates                []operatorCandidate
	KnownOperators                []operatorCandidate
	ActiveLocations               map[string]struct{}
	CompletedRestoreLocations     map[string]struct{}
	TargetAssignments             map[string]operatorCandidate
	LockedRestoreAssignments      map[string]operatorCandidate
	ExcludedOperators             map[string]struct{}
	OutpostProsperityMaxLocations map[string]struct{}
}

// parseOperatorActionParam 解析并校验 Pipeline 参数。
// 参数错误会直接让本次识别失败，避免使用不完整配置误点其他界面元素。
func parseOperatorActionParam(raw string) (*operatorActionParam, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("custom_action_param is empty")
	}

	var p operatorActionParam
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("unmarshal custom_action_param: %w", err)
	}
	p.Mode = strings.TrimSpace(p.Mode)
	if p.Mode != operatorCacheModeCache && p.Mode != operatorCacheModeRefresh {
		return nil, fmt.Errorf("invalid mode %q", p.Mode)
	}
	p.Usage = strings.TrimSpace(p.Usage)
	if p.Usage != operatorActionUsageTarget && p.Usage != operatorActionUsageRestore && p.Usage != operatorActionUsageAll {
		return nil, fmt.Errorf("invalid usage %q", p.Usage)
	}
	p.Location = strings.TrimSpace(p.Location)
	if p.Location == "" {
		return nil, fmt.Errorf("location is empty")
	}
	p.Result = strings.TrimSpace(p.Result)
	if len(p.ROI) != 4 {
		return nil, fmt.Errorf("invalid roi length %d, expected 4", len(p.ROI))
	}
	return &p, nil
}

// normalizeOperatorCandidates 清洗候选数据并生成稳定顺序。
// 去重以内部 Name 为准，同名候选只保留第一次出现的配置，从而让资源文件顺序
// 在 Priority 相同时成为确定性的次级排序条件。
func normalizeOperatorCandidates(candidates []operatorCandidate) []operatorCandidate {
	normalized := make([]operatorCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate.Name = strings.TrimSpace(candidate.Name)
		candidate.DisplayName = strings.TrimSpace(candidate.DisplayName)
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

// uniqueNonEmptyStrings 去除空字符串和重复值，并保留首次出现的顺序。
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

// sortOperatorCandidates 先按售卖加成档位、再按稳定优先级排序。
func sortOperatorCandidates(candidates []operatorCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].BonusTier != candidates[j].BonusTier {
			return candidates[i].BonusTier < candidates[j].BonusTier
		}
		return candidates[i].Priority < candidates[j].Priority
	})
}

// normalizeOperatorCandidateGroups 清洗据点分组，并保证一个 Location 只参与一次分配。
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

// filterOwnedCandidates 从候选集中筛出当前账号已经拥有的干员 ID。
func filterOwnedCandidates(candidates []operatorCandidate, owned map[string]struct{}) []operatorCandidate {
	if len(candidates) == 0 || len(owned) == 0 {
		return nil
	}
	filtered := make([]operatorCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := owned[candidate.Name]; ok {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

// collectScanCandidates 返回数据加载器提供的完整干员候选域。
func collectScanCandidates(p *operatorSelectionParam) []operatorCandidate {
	if p == nil {
		return nil
	}
	return normalizeOperatorCandidates(p.ScanCandidates)
}
