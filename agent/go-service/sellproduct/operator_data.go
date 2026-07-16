package sellproduct

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
)

const (
	// 打包运行时从 data 读取；仓库开发和单测环境则优先读取 tools 下的源数据。
	settlementTradeResourcePath    = "data/settlement_trade.json"
	settlementTradeDevResourcePath = "tools/pipeline-generate/data/settlement_trade.json"
	// 中文本地化文件中的 operator.* 顺序与游戏列表一致，用作同类干员的稳定排序依据。
	operatorLocaleOrderResourcePath  = "locales/interface/zh_cn.json"
	operatorLocaleOrderResourcePath2 = "assets/locales/interface/zh_cn.json"
)

var (
	// loadOperatorSelectionDataFunc 是为单元测试保留的替换点。
	loadOperatorSelectionDataFunc = loadOperatorSelectionDataCached
	operatorSelectionDataOnce     sync.Once
	operatorSelectionDataCache    *operatorSelectionData
	operatorSelectionDataErr      error
)

// operatorSelectionData 是从聚落贸易资源派生出的最小运行时数据。
// TargetCandidates 按目标据点索引增益干员，RestoreGroups 保存各据点生产恢复候选。
type operatorSelectionData struct {
	TargetCandidates map[string][]operatorCandidate
	RestoreGroups    []operatorCandidateGroup
}

// 以下结构只映射 settlement_trade.json 中与干员和货品选择相关的字段。
// 保持窄结构可避免上游资源增加无关字段时影响本模块解析。
type settlementTradeFile struct {
	Settlements map[string]settlementTradeSettlement `json:"settlements"`
}

type settlementTradeSettlement struct {
	SettlementName     map[string]string                         `json:"settlementName"`
	DomainID           string                                    `json:"domainId"`
	SettlementFeatures []settlementTradeFeature                  `json:"settlementFeatures"`
	ByProsperityLevel  map[string]settlementTradeProsperityLevel `json:"byProsperityLevel"`
}

type settlementTradeProsperityLevel struct {
	TradeItems []settlementTradeItem `json:"tradeItems"`
}

type settlementTradeItem struct {
	ItemID string            `json:"itemId"`
	Name   map[string]string `json:"name"`
}

type settlementTradeFeature struct {
	Bonuses           []settlementTradeBonus    `json:"bonuses"`
	MatchingOperators []settlementTradeOperator `json:"matchingOperators"`
}

type settlementTradeBonus struct {
	Type string `json:"type"`
}

type settlementTradeOperator struct {
	CharID string            `json:"charId"`
	Name   map[string]string `json:"name"`
}

// settlementLocation 为无序的 settlements map 补充稳定 ID，便于后续确定性遍历。
type settlementLocation struct {
	SettlementID string
	LocationID   string
	Settlement   settlementTradeSettlement
}

// loadOperatorSelectionData 读取聚落贸易和本地化顺序，并构建选择算法所需候选集。
func loadOperatorSelectionData() (*operatorSelectionData, error) {
	var data settlementTradeFile
	if err := readSettlementTradeFile(&data); err != nil {
		return nil, err
	}
	localeOrder := loadOperatorLocaleOrder()
	return buildOperatorSelectionData(data, localeOrder), nil
}

// loadOperatorSelectionDataCached 在 Agent 生命周期内复用不可变的候选数据，避免每次心跳
// 都重新解析 settlement_trade.json 和本地化顺序。资源更新后本就需要重启 Go Agent。
func loadOperatorSelectionDataCached() (*operatorSelectionData, error) {
	operatorSelectionDataOnce.Do(func() {
		operatorSelectionDataCache, operatorSelectionDataErr = loadOperatorSelectionData()
	})
	return operatorSelectionDataCache, operatorSelectionDataErr
}

// readSettlementTradeFile 同时兼容源码仓库和发布包目录结构。
// 开发数据优先，确保本地修改生成器数据后无需重新打包资源即可调试。
func readSettlementTradeFile(out *settlementTradeFile) error {
	for _, path := range []string{
		settlementTradeDevResourcePath,
		settlementTradeResourcePath,
	} {
		if err := readJsonFromRepoOrResource(path, out); err == nil {
			return nil
		}
	}
	return fmt.Errorf("settlement_trade.json not found")
}

// buildOperatorSelectionData 为每个据点分别生成“售卖收益最优”和“恢复生产岗位”候选。
func buildOperatorSelectionData(data settlementTradeFile, localeOrder map[string]int) *operatorSelectionData {
	locations := settlementLocations(data)
	result := &operatorSelectionData{
		TargetCandidates: make(map[string][]operatorCandidate, len(locations)),
		RestoreGroups:    make([]operatorCandidateGroup, 0, len(locations)),
	}
	for _, loc := range locations {
		targetCandidates := buildTargetCandidates(loc.Settlement, localeOrder)
		restoreCandidates := buildRestoreCandidates(loc.Settlement, localeOrder)
		result.TargetCandidates[loc.LocationID] = targetCandidates
		result.RestoreGroups = append(result.RestoreGroups, operatorCandidateGroup{
			Location:   loc.LocationID,
			Candidates: restoreCandidates,
		})
	}
	result.RestoreGroups = normalizeOperatorCandidateGroups(result.RestoreGroups)
	return result
}

// settlementLocations 将 map 转换为稳定切片。
// 先按 DomainID、再按 settlementID 排序，避免 Go map 随机遍历导致候选和测试结果抖动。
func settlementLocations(data settlementTradeFile) []settlementLocation {
	locations := make([]settlementLocation, 0, len(data.Settlements))
	for settlementID, settlement := range data.Settlements {
		locations = append(locations, settlementLocation{
			SettlementID: settlementID,
			LocationID:   settlementLocationID(settlementID, settlement),
			Settlement:   settlement,
		})
	}
	sort.SliceStable(locations, func(i, j int) bool {
		a := locations[i]
		b := locations[j]
		if a.Settlement.DomainID != b.Settlement.DomainID {
			return a.Settlement.DomainID < b.Settlement.DomainID
		}
		return a.SettlementID < b.SettlementID
	})
	return locations
}

// settlementLocationID 把资源侧聚落 ID 映射为 Pipeline 使用的 Location 名称。
// 已接入据点使用显式映射；未知据点回退到英文名，便于资源扩展时保持可读性。
func settlementLocationID(settlementID string, settlement settlementTradeSettlement) string {
	switch settlementID {
	case "stm_tundra_1":
		return "RefugeeCamp"
	case "stm_tundra_2":
		return "InfrastructureOutpost"
	case "stm_tundra_3":
		return "ReconstructionCommand"
	case "stm_hongs_1":
		return "SkyKingFlats"
	default:
		return toPascalCase(firstNonEmpty(settlement.SettlementName["EN"], settlementID))
	}
}

// buildTargetCandidates 生成售卖时的收益候选。
// 优先级规则为：经验+信用点 > 仅信用点 > 仅经验 > 其他。Priority 越小越优先，
// 同一收益等级再按游戏干员列表顺序排列，确保 OCR 命中和选择结果稳定。
func buildTargetCandidates(settlement settlementTradeSettlement, localeOrder map[string]int) []operatorCandidate {
	operators := collectOperatorBonusTypes(settlement, map[string]struct{}{
		"expProfit":   {},
		"moneyProfit": {},
	})
	entries := sortedOperatorEntries(operators, localeOrder)
	candidates := make([]operatorCandidate, 0, len(entries))
	for _, entry := range entries {
		priority := 3
		if _, hasExp := entry.BonusTypes["expProfit"]; hasExp {
			if _, hasMoney := entry.BonusTypes["moneyProfit"]; hasMoney {
				priority = 0
			} else {
				priority = 2
			}
		} else if _, hasMoney := entry.BonusTypes["moneyProfit"]; hasMoney {
			priority = 1
		}
		candidates = append(candidates, operatorCandidate{
			Name:      entry.Name,
			CacheName: entry.CacheName,
			Expected:  entry.Expected,
			Priority:  priority,
		})
	}
	return normalizeOperatorCandidates(candidates)
}

// buildRestoreCandidates 生成售卖后应恢复到各据点生产岗位的候选。
// 此处 Priority 直接采用本地化列表顺序，之后由全局分配算法在不重复使用干员的
// 前提下最小化所有据点的优先级成本。
func buildRestoreCandidates(settlement settlementTradeSettlement, localeOrder map[string]int) []operatorCandidate {
	operators := collectOperatorBonusTypes(settlement, map[string]struct{}{
		"moneyProduceSpeed": {},
	})
	entries := sortedOperatorEntries(operators, localeOrder)
	candidates := make([]operatorCandidate, 0, len(entries))
	for index, entry := range entries {
		candidates = append(candidates, operatorCandidate{
			Name:      entry.Name,
			CacheName: entry.CacheName,
			Expected:  entry.Expected,
			Priority:  index,
		})
	}
	return normalizeOperatorCandidates(candidates)
}

// operatorDataEntry 是聚合单个干员多个 settlementFeature 后的中间表示。
type operatorDataEntry struct {
	Name       string
	CacheName  string
	Expected   []string
	BonusTypes map[string]struct{}
}

// collectOperatorBonusTypes 收集具有 accepted 增益类型的干员。
// 同一干员可能出现在多个 feature 中，因此以内部 Name 合并 BonusTypes；只有至少
// 命中一种目标增益的 feature 才会参与，管理员角色也会被明确排除。
func collectOperatorBonusTypes(settlement settlementTradeSettlement, accepted map[string]struct{}) map[string]operatorDataEntry {
	operators := map[string]operatorDataEntry{}
	for _, feature := range settlement.SettlementFeatures {
		matchedBonusTypes := make([]string, 0, len(feature.Bonuses))
		for _, bonus := range feature.Bonuses {
			if _, ok := accepted[bonus.Type]; ok {
				matchedBonusTypes = append(matchedBonusTypes, bonus.Type)
			}
		}
		if len(matchedBonusTypes) == 0 {
			continue
		}
		for _, operator := range feature.MatchingOperators {
			if isIgnoredOperator(operator) {
				continue
			}
			name := toPascalCase(firstNonEmpty(operator.Name["EN"], operator.CharID))
			cacheName := firstNonEmpty(
				operator.Name["CN"],
				operator.Name["TC"],
				operator.Name["EN"],
				operator.Name["JP"],
				operator.Name["KR"],
				operator.CharID,
			)
			expected := operatorExpectedNames(operator.Name)
			if name == "" || cacheName == "" || len(expected) == 0 {
				continue
			}
			entry := operators[name]
			if entry.Name == "" {
				entry = operatorDataEntry{
					Name:       name,
					CacheName:  cacheName,
					Expected:   expected,
					BonusTypes: map[string]struct{}{},
				}
			}
			for _, bonusType := range matchedBonusTypes {
				entry.BonusTypes[bonusType] = struct{}{}
			}
			operators[name] = entry
		}
	}
	return operators
}

// sortedOperatorEntries 按游戏列表顺序输出 map 中的干员；缺少顺序时以 Name 兜底。
func sortedOperatorEntries(operators map[string]operatorDataEntry, localeOrder map[string]int) []operatorDataEntry {
	entries := make([]operatorDataEntry, 0, len(operators))
	for _, entry := range operators {
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		aOrder := operatorLocaleOrder(entries[i].Name, localeOrder)
		bOrder := operatorLocaleOrder(entries[j].Name, localeOrder)
		if aOrder != bOrder {
			return aOrder < bOrder
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// loadOperatorLocaleOrder 从 zh_cn.json 的 operator.* 键出现顺序推导游戏内列表顺序。
// 这里只读取键，不解析完整 JSON，是因为对象反序列化后会丢失原始字段顺序。
func loadOperatorLocaleOrder() map[string]int {
	pattern := regexp.MustCompile(`"operator\.([^"]+)"\s*:`)
	for _, path := range []string{
		operatorLocaleOrderResourcePath,
		operatorLocaleOrderResourcePath2,
	} {
		content, err := readBytesFromRepoOrResource(path)
		if err == nil {
			return operatorLocaleOrderMap(pattern.FindAllSubmatch(content, -1))
		}
	}
	return nil
}

// readJsonFromRepoOrResource 读取源码文件或嵌入资源，并反序列化为目标结构。
func readJsonFromRepoOrResource(relativePath string, out any) error {
	content, err := readBytesFromRepoOrResource(relativePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(content, out)
}

// readBytesFromRepoOrResource 优先访问仓库内文件，发布环境找不到时回退到嵌入资源。
func readBytesFromRepoOrResource(relativePath string) ([]byte, error) {
	if content, err := os.ReadFile(filepath.Join(repoRootFromSource(), filepath.FromSlash(relativePath))); err == nil {
		return content, nil
	}
	return resource.ReadResource(relativePath)
}

// repoRootFromSource 根据当前源码文件位置反推仓库根目录，仅用于开发环境资源查找。
func repoRootFromSource() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// operatorLocaleOrderMap 将正则匹配结果转换为“干员名 -> 出现序号”的索引。
func operatorLocaleOrderMap(matches [][][]byte) map[string]int {
	order := map[string]int{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := string(match[1])
		if _, ok := order[name]; ok {
			continue
		}
		order[name] = len(order)
	}
	return order
}

// operatorLocaleOrder 返回干员在本地化文件中的顺序；未知干员排在所有已知干员之后。
func operatorLocaleOrder(name string, localeOrder map[string]int) int {
	if order, ok := localeOrder[name]; ok {
		return order
	}
	return int(^uint(0) >> 1)
}

// operatorExpectedNames 按项目支持的语言生成 OCR 完整名称候选，并清理空值和重复值。
func operatorExpectedNames(names map[string]string) []string {
	return uniqueNonEmptyStrings([]string{
		names["CN"],
		names["TC"],
		names["EN"],
		names["JP"],
		names["KR"],
	})
}

// isIgnoredOperator 排除资源中用于代表玩家自身、不可作为设施干员选择的管理员。
func isIgnoredOperator(operator settlementTradeOperator) bool {
	return operator.Name["EN"] == "Endministrator" || operator.Name["CN"] == "管理员"
}

// firstNonEmpty 返回首个非空值，用于多语言名称和资源 ID 的分级回退。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

// toPascalCase 把资源名称转换为 Pipeline/内部使用的 PascalCase 标识。
// 非字母数字字符只作为分词符，不参与最终标识。
func toPascalCase(value string) string {
	var parts []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		parts = append(parts, current.String())
		current.Reset()
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()

	var result strings.Builder
	for _, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		result.WriteString(strings.ToUpper(string(runes[0])))
		if len(runes) > 1 {
			result.WriteString(string(runes[1:]))
		}
	}
	return result.String()
}
