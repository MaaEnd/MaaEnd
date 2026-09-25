package essencefilter

import (
	"sort"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/rs/zerolog/log"
)

// 840 全收集模式的纯计算内核。
//
// 该文件不接触 maa 上下文、不点击、不滑动：它只回答三个问题——
//  1. 一次只读盘点看到了哪些词条组合与等级签名；
//  2. 按「保留模式」每个等级签名应该保留几份（配额）；
//  3. 执行遍遇到某份基质时该锁定、丢弃还是跳过。
//
// 设计文档：../../../../840-全收集模式-设计方案.md §4.3 / §4.6

const (
	collectionSlot1MaxLevel = 6
	collectionSlot2MaxLevel = 6
	collectionSlot3MaxLevel = 3

	// collectionBonusTotalThreshold 是模式 B 的阈值：三槽等级之和「严格大于」它时额外保留。
	// 注意这与面向武器的扩展规则（future_promising_min_total）无关，后者是 6 且可配置。
	collectionBonusTotalThreshold = 9
)

// collectionLevels 是一份基质的三槽等级，语义顺序与 EssenceInventory.json 一致：
// [0] 基础属性 1..6、[1] 附加属性 1..6、[2] 技能属性 1..3。
type collectionLevels [3]int

// collectionCombo 是 840 词条全集中的一类（一个 slot1 x slot2 x slot3 组合）。
type collectionCombo [3]int

// collectionKey 是配额的最小单位：一个组合 + 一个精确等级签名。
// 配额以签名为键（而不是记录格子位置），执行遍因此与扫描顺序无关。
type collectionKey struct {
	combo  collectionCombo
	levels collectionLevels
}

func (l collectionLevels) total() int {
	return l[0] + l[1] + l[2]
}

// collectionKeepMode 是任务选项里的「保留模式」。
type collectionKeepMode int

const (
	collectionKeepModeA collectionKeepMode = iota // 每类保留 1 个最优
	collectionKeepModeB                           // A + 额外保留三槽之和 > 9 的
	collectionKeepModeC                           // A + 额外保留第三词条满级(3)的
)

// parseCollectionKeepMode 把 attach 里的字符串映射为保留模式；未知值回落到 A（选项默认值）。
func parseCollectionKeepMode(v string) collectionKeepMode {
	switch v {
	case "B":
		return collectionKeepModeB
	case "C":
		return collectionKeepModeC
	default:
		return collectionKeepModeA
	}
}

func (m collectionKeepMode) String() string {
	switch m {
	case collectionKeepModeB:
		return "B"
	case collectionKeepModeC:
		return "C"
	default:
		return "A"
	}
}

// collectionLevelsBetter 是保留偏好比较链，按序比较、前者分不出胜负才看后者：
//
//	① slot1/slot2 中等级 6 的个数    多者优
//	② slot3 等级                     高者优
//	③ slot1+slot2 之和               大者优
//	④ 三槽最大数                     大者优
//
// 返回正数表示 a 更优，负数表示 b 更优，0 表示比较链无法区分两者。
//
// 注意比较链「不」单独比较三槽之和：因此同类中 (6,1,3) 优于 (5,5,3)，
// 即使后者之和更大。三槽之和只用于模式 B 的阈值筛选。
func collectionLevelsBetter(a, b collectionLevels) int {
	if d := collectionSixCount(a) - collectionSixCount(b); d != 0 {
		return d
	}
	if d := a[2] - b[2]; d != 0 {
		return d
	}
	if d := (a[0] + a[1]) - (b[0] + b[1]); d != 0 {
		return d
	}
	return collectionMaxLevel(a) - collectionMaxLevel(b)
}

// collectionLevelsRank 在比较链之上补一个确定性次序，供比较链无法区分的场合选出一个。
// 取较小签名（字典序）可以保证同一份库存无论 map 遍历顺序如何都得到同一结果。
func collectionLevelsRank(a, b collectionLevels) int {
	if d := collectionLevelsBetter(a, b); d != 0 {
		return d
	}
	for i := range a {
		if a[i] != b[i] {
			return b[i] - a[i]
		}
	}
	return 0
}

func collectionSixCount(l collectionLevels) int {
	n := 0
	for _, v := range l[:2] {
		if v == collectionSlot1MaxLevel {
			n++
		}
	}
	return n
}

func collectionMaxLevel(l collectionLevels) int {
	m := l[0]
	for _, v := range l[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// collectionClass 累积一次盘点中属于同一组合的全部基质。
type collectionClass struct {
	counts map[collectionLevels]int
	total  int
}

// collectionState 是一次只读盘点的结果：每个组合 -> 各等级签名的数量。
type collectionState struct {
	classes map[collectionCombo]*collectionClass
	scanned int
}

func newCollectionState() *collectionState {
	return &collectionState{classes: make(map[collectionCombo]*collectionClass)}
}

// record 记入一份基质。match 为 nil（OCR 无法解析为三个不同槽位）时忽略，调用方无需判空。
func (s *collectionState) record(match *matchapi.CollectionMatch) {
	if s == nil || match == nil {
		return
	}
	if s.classes == nil {
		s.classes = make(map[collectionCombo]*collectionClass)
	}
	combo := collectionCombo(match.SkillIDs)
	levels := collectionLevels(match.Levels)
	class := s.classes[combo]
	if class == nil {
		class = &collectionClass{counts: make(map[collectionLevels]int)}
		s.classes[combo] = class
	}
	class.counts[levels]++
	class.total++
	s.scanned++
}

// classes 返回本次盘点记录到的组合数量（即已收集的类数）。
func (s *collectionState) classCount() int {
	if s == nil {
		return 0
	}
	return len(s.classes)
}

// collectionBestLevels 返回某一类应保留的等级签名，以及比较链无法与它区分的其它签名。
// 返回的 tie 非空时，选择由确定性次序兜底，调用方必须输出日志便于事后核对。
func collectionBestLevels(counts map[collectionLevels]int) (collectionLevels, []collectionLevels) {
	var best collectionLevels
	first := true
	for levels := range counts {
		if first {
			best, first = levels, false
			continue
		}
		if collectionLevelsRank(levels, best) > 0 {
			best = levels
		}
	}
	if first { // 空类
		return collectionLevels{}, nil
	}
	var tie []collectionLevels
	for levels := range counts {
		if levels == best {
			continue
		}
		if collectionLevelsBetter(levels, best) == 0 {
			tie = append(tie, levels)
		}
	}
	sort.Slice(tie, func(i, j int) bool {
		return collectionLevelsRank(tie[i], tie[j]) > 0
	})
	return best, tie
}

// collectionPlan 是执行遍的输入：每个等级签名允许保留几份，以及执行过程中的计数。
type collectionPlan struct {
	mode collectionKeepMode

	// lockKeepers=false 时保留项不新增锁定：配额照样消耗（决定「哪些不被弃置」），
	// 但命中配额的那一份走跳过而不是锁定。
	lockKeepers bool

	// remaining 是尚未用掉的保留配额，执行遍每锁定一份就减一。
	remaining map[collectionKey]int
	// quota 是初始配额，用于汇报（remaining 会被消耗）。
	quota map[collectionKey]int

	locked    int
	spared    int // 命中保留配额但未锁定（lockKeepers=false）
	discarded int
	skipped   int
}

// buildCollectionPlan 按保留模式算出每个等级签名的保留配额。
//
//	模式 A：每类保留 1 份比较链最优的；
//	模式 B：A + 额外保留所有三槽之和 > 9 的（该类若存在则全部保留）；
//	模式 C：A + 额外保留所有第三词条满级(=3)的。
func buildCollectionPlan(st *collectionState, mode collectionKeepMode) *collectionPlan {
	return buildCollectionPlanWithPolicy(st, mode, true)
}

// buildCollectionPlanWithPolicy 与 buildCollectionPlan 相同，但可关闭「保留即锁定」。
func buildCollectionPlanWithPolicy(st *collectionState, mode collectionKeepMode, lockKeepers bool) *collectionPlan {
	plan := &collectionPlan{
		mode:        mode,
		lockKeepers: lockKeepers,
		remaining:   make(map[collectionKey]int),
		quota:       make(map[collectionKey]int),
	}
	if st == nil {
		return plan
	}
	for combo, class := range st.classes {
		if len(class.counts) == 0 {
			continue
		}
		// 先为每一个「盘点到过」的签名建条目（配额 0）。这一步是必须的：
		// 否则被比较链淘汰的签名在执行遍会落进「未知签名 -> 跳过」分支，
		// 导致该丢弃的基质原样留在仓库里。
		for levels := range class.counts {
			plan.ensure(combo, levels)
		}

		best, tie := collectionBestLevels(class.counts)
		if len(tie) > 0 {
			log.Warn().
				Str("component", "EssenceCollection").
				Str("step", "KeepTie").
				Ints("combo", combo[:]).
				Ints("chosen", best[:]).
				Ints("tied", flattenLevels(tie)).
				Msg("comparison chain cannot separate level signatures; keeping one")
		}
		plan.set(combo, best, 1)

		switch mode {
		case collectionKeepModeB:
			for levels, n := range class.counts {
				if levels.total() > collectionBonusTotalThreshold {
					plan.set(combo, levels, n)
				}
			}
		case collectionKeepModeC:
			for levels, n := range class.counts {
				if levels[2] == collectionSlot3MaxLevel {
					plan.set(combo, levels, n)
				}
			}
		}
	}
	return plan
}

// ensure 为签名建立条目（配额 0）。它把「盘点到过但不需要保留」与「从未见过」区分开：
// 前者会走丢弃分支，后者走跳过分支。
func (p *collectionPlan) ensure(combo collectionCombo, levels collectionLevels) {
	key := collectionKey{combo: combo, levels: levels}
	if _, ok := p.quota[key]; !ok {
		p.quota[key] = 0
		p.remaining[key] = 0
	}
}

// set 写入配额。配额只增不减：模式 B/C 的额外保留只会放宽，不会收紧模式 A 的「保留 1 份」。
func (p *collectionPlan) set(combo collectionCombo, levels collectionLevels, n int) {
	key := collectionKey{combo: combo, levels: levels}
	p.ensure(combo, levels)
	if n > p.quota[key] {
		p.quota[key] = n
		p.remaining[key] = n
	}
}

func flattenLevels(levels []collectionLevels) []int {
	out := make([]int, 0, len(levels)*3)
	for _, l := range levels {
		out = append(out, l[:]...)
	}
	return out
}

// keepTotal 返回本次计划一共要保留多少份基质（所有签名配额之和）。
func (p *collectionPlan) keepTotal() int {
	if p == nil {
		return 0
	}
	total := 0
	for _, n := range p.quota {
		total += n
	}
	return total
}

// collectionDecision 是执行遍对一份基质的处置结论。
type collectionDecision int

const (
	// collectionSkip 表示不动这份基质：它没有出现在盘点结果里。
	collectionSkip collectionDecision = iota
	collectionLock
	collectionDiscard
)

// decide 给出执行遍的处置结论；决定锁定时消耗一份配额。
//
// 安全规则：签名不在配额表中时返回 collectionSkip 而不是 collectionDiscard。
// 该签名在只读盘点中从未出现，可能是 OCR 误读或两遍之间库存发生变化；
// 宁可不动，也不能因为一次误读而丢弃一份基质。
func (p *collectionPlan) decide(combo collectionCombo, levels collectionLevels) collectionDecision {
	if p == nil {
		return collectionSkip
	}
	key := collectionKey{combo: combo, levels: levels}
	left, known := p.remaining[key]
	if !known {
		p.skipped++
		return collectionSkip
	}
	if left > 0 {
		p.remaining[key] = left - 1
		if !p.lockKeepers {
			// 保留但不新增锁定：配额已消耗，这一份不被弃置即可。
			p.spared++
			return collectionSkip
		}
		p.locked++
		return collectionLock
	}
	p.discarded++
	return collectionDiscard
}

// collectionUniverse 返回词条池决定的全集（当前数据为 5 x 12 x 14 = 840），按组合升序。
func collectionUniverse(pools matchapi.SkillPools) []collectionCombo {
	universe := make([]collectionCombo, 0, len(pools.Slot1)*len(pools.Slot2)*len(pools.Slot3))
	for _, s1 := range pools.Slot1 {
		for _, s2 := range pools.Slot2 {
			for _, s3 := range pools.Slot3 {
				universe = append(universe, collectionCombo{s1.ID, s2.ID, s3.ID})
			}
		}
	}
	return universe
}

// missing 返回全集里本次盘点「一份都没有」的组合，即建议刷取的目标。
func (s *collectionState) missing(universe []collectionCombo) []collectionCombo {
	missing := make([]collectionCombo, 0, len(universe))
	for _, combo := range universe {
		if s != nil {
			if class := s.classes[combo]; class != nil && class.total > 0 {
				continue
			}
		}
		missing = append(missing, combo)
	}
	return missing
}

// collectionScarcity 返回能产出某个组合的地点数量。数量越少越难凑，网页建议按它升序排列。
func collectionScarcity(locations []matchapi.Location, combo collectionCombo) int {
	n := 0
	for _, loc := range locations {
		if collectionLocationHas(loc, combo) {
			n++
		}
	}
	return n
}

// collectionLocationsFor 返回能产出某个组合的地点名（保持 locations.json 的顺序）。
func collectionLocationsFor(locations []matchapi.Location, combo collectionCombo) []string {
	names := make([]string, 0, len(locations))
	for _, loc := range locations {
		if collectionLocationHas(loc, combo) {
			names = append(names, loc.Name)
		}
	}
	return names
}

func collectionLocationHas(loc matchapi.Location, combo collectionCombo) bool {
	return containsInt(loc.Slot2IDs, combo[1]) && containsInt(loc.Slot3IDs, combo[2])
}

func containsInt(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// sortCollectionMissing 按稀缺度升序排列缺失组合（可选地点最少的最先刷），
// 稀缺度相同时按组合升序，保证输出稳定。
func sortCollectionMissing(missing []collectionCombo, locations []matchapi.Location) {
	scarcity := make(map[collectionCombo]int, len(missing))
	for _, combo := range missing {
		scarcity[combo] = collectionScarcity(locations, combo)
	}
	sort.SliceStable(missing, func(i, j int) bool {
		if scarcity[missing[i]] != scarcity[missing[j]] {
			return scarcity[missing[i]] < scarcity[missing[j]]
		}
		return comboLess(missing[i], missing[j])
	})
}

func comboLess(a, b collectionCombo) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
