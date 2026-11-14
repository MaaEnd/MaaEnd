package essencefilter

import (
	"slices"
	"sort"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
)

// Success rate of raising a level to the target level. Slots 1 and 2 share one table;
// slot 3 has its own.
var (
	enhancementSuccessSlot12 = map[int]float64{2: 0.6, 3: 0.24, 4: 0.109, 5: 0.05, 6: 0.027}
	enhancementSuccessSlot3  = map[int]float64{2: 0.109, 3: 0.042}
)

// ExpectedEnhanceAttempts returns the expected number of enhancement attempts needed to
// raise levels to [6,6,3]: the sum of the reciprocal success rates of every level gained.
// ok is false when a level is out of range for its slot.
func ExpectedEnhanceAttempts(levels [3]int) (attempts float64, ok bool) {
	for index, level := range levels {
		table := enhancementSuccessSlot12
		maxLevel := 6
		if index == 2 {
			table = enhancementSuccessSlot3
			maxLevel = 3
		}
		if level < 1 || level > maxLevel {
			return 0, false
		}
		for target := level + 1; target <= maxLevel; target++ {
			attempts += 1 / table[target]
		}
	}
	return attempts, true
}

// lockLimitReference describes the five- and six-star weapons sharing one skill combination.
type lockLimitReference struct {
	quota   int
	weapons []string
	skills  [3]string
}

// lockLimitRow is one level triple held for a skill combination.
type lockLimitRow struct {
	levels        [3]int
	expected      float64
	count         int
	locked        int
	inTop         int
	needLock      int
	lockedThisRun int
}

// lockLimitCombination is the quota plan of one skill combination.
type lockLimitCombination struct {
	skillIDs [3]int
	*lockLimitReference
	rows           map[[3]int]*lockLimitRow
	ordered        []*lockLimitRow
	snapshotLocked int
	excess         int
	reminded       bool
}

// lockLimitDecision is the lock-limit verdict for one scanned essence.
type lockLimitDecision struct {
	Lock bool
	// MissingGroup marks a five/six-star combination absent from the inventory snapshot.
	MissingGroup bool
	// MissingRow marks a level triple absent from its inventory group.
	MissingRow bool
	// OverLimit marks the first encounter of a combination's over-quota row in this run.
	OverLimit bool
	Quota     int
	Expected  float64
}

// lockLimitPlanner decides lock or skip for exact-matching essences under the
// per-combination quota. It neither touches the pipeline nor the file system.
type lockLimitPlanner struct {
	references   map[[3]int]*lockLimitReference
	combinations map[[3]int]*lockLimitCombination
}

// newLockLimitPlanner builds quota plans from an inventory snapshot and the engine weapon table.
// Quotas count five- and six-star weapons only, so four-star-only combinations are never planned.
func newLockLimitPlanner(groups []inventoryGroup, weapons []matchapi.WeaponData) *lockLimitPlanner {
	p := &lockLimitPlanner{
		references:   make(map[[3]int]*lockLimitReference),
		combinations: make(map[[3]int]*lockLimitCombination),
	}
	weaponByID := make(map[string]matchapi.WeaponData, len(weapons))
	for _, weapon := range weapons {
		weaponByID[weapon.InternalID] = weapon
		if weapon.Rarity != 5 && weapon.Rarity != 6 {
			continue
		}
		ids, ok := skillIDsTriple(weapon.SkillIDs)
		if !ok {
			continue
		}
		ref := p.references[ids]
		if ref == nil {
			ref = &lockLimitReference{}
			if skills, ok := skillsTriple(weapon.SkillsChinese); ok {
				ref.skills = skills
			}
			p.references[ids] = ref
		}
		ref.quota++
		ref.weapons = append(ref.weapons, weapon.ChineseName)
	}
	for _, group := range groups {
		ids, ok := resolveGroupSkillIDs(group, weaponByID)
		if !ok {
			continue
		}
		ref := p.references[ids]
		if ref == nil || ref.quota == 0 {
			continue
		}
		combo := p.combinations[ids]
		if combo == nil {
			combo = &lockLimitCombination{
				skillIDs:           ids,
				lockLimitReference: ref,
				rows:               make(map[[3]int]*lockLimitRow),
			}
			p.combinations[ids] = combo
		}
		for _, essence := range group.Essences {
			if essence.Count <= 0 || essence.Locked < 0 {
				continue
			}
			expected, ok := ExpectedEnhanceAttempts(essence.Levels)
			if !ok {
				continue
			}
			row := combo.rows[essence.Levels]
			if row == nil {
				row = &lockLimitRow{levels: essence.Levels, expected: expected}
				combo.rows[essence.Levels] = row
			}
			row.count += essence.Count
			row.locked += essence.Locked
		}
	}
	for _, combo := range p.combinations {
		combo.buildRanking()
	}
	return p
}

// buildRanking sorts rows by expected attempts and assigns each row its share of the top N.
func (c *lockLimitCombination) buildRanking() {
	c.ordered = make([]*lockLimitRow, 0, len(c.rows))
	for _, row := range c.rows {
		c.ordered = append(c.ordered, row)
	}
	sort.Slice(c.ordered, func(i, j int) bool {
		if c.ordered[i].expected != c.ordered[j].expected {
			return c.ordered[i].expected < c.ordered[j].expected
		}
		return slices.Compare(c.ordered[i].levels[:], c.ordered[j].levels[:]) < 0
	})
	better := 0
	for _, row := range c.ordered {
		row.inTop = min(row.count, max(c.quota-better, 0))
		row.needLock = max(row.inTop-row.locked, 0)
		better += row.count
		c.snapshotLocked += row.locked
		c.excess += max(row.locked-row.inTop, 0)
	}
}

// decide returns the verdict for one scanned essence and records the run progress.
func (p *lockLimitPlanner) decide(skillIDs []int, levels [3]int, locked bool) lockLimitDecision {
	var decision lockLimitDecision
	ids, ok := skillIDsTriple(skillIDs)
	if !ok {
		return decision
	}
	combo := p.combinations[ids]
	if combo == nil {
		// A four-star-only combination has no quota and is reported as a plain mismatch.
		if ref := p.references[ids]; ref != nil && ref.quota > 0 {
			decision.MissingGroup = true
			decision.Quota = ref.quota
		}
		return decision
	}
	decision.Quota = combo.quota
	row := combo.rows[levels]
	if row == nil {
		decision.MissingRow = true
		return decision
	}
	decision.Expected = row.expected
	if locked {
		if row.locked > row.inTop && !combo.reminded {
			decision.OverLimit = true
			combo.reminded = true
		}
		return decision
	}
	if row.lockedThisRun < row.needLock {
		decision.Lock = true
		row.lockedThisRun++
	}
	return decision
}

func resolveGroupSkillIDs(group inventoryGroup, weaponByID map[string]matchapi.WeaponData) ([3]int, bool) {
	for _, id := range group.WeaponIDs {
		weapon, ok := weaponByID[id]
		if !ok {
			continue
		}
		return skillIDsTriple(weapon.SkillIDs)
	}
	return [3]int{}, false
}

func skillIDsTriple(ids []int) ([3]int, bool) {
	if len(ids) != 3 {
		return [3]int{}, false
	}
	return [3]int{ids[0], ids[1], ids[2]}, true
}

func skillsTriple(skills []string) ([3]string, bool) {
	if len(skills) != 3 {
		return [3]string{}, false
	}
	return [3]string{skills[0], skills[1], skills[2]}, true
}

// lockLimitSummary is one over-quota combination for the end-of-run report.
type lockLimitSummary struct {
	Skills  [3]string
	Weapons []string
	Quota   int
	Locked  int
	Excess  int
}

// lockedTotal returns the combination's locked essences, counting the locks made in this run.
func (c *lockLimitCombination) lockedTotal() int {
	total := c.snapshotLocked
	for _, row := range c.ordered {
		total += row.lockedThisRun
	}
	return total
}

// overLimitSummaries lists the combinations holding locked suboptimal essences, ordered by skill IDs.
func (p *lockLimitPlanner) overLimitSummaries() []lockLimitSummary {
	var summaries []lockLimitSummary
	for _, combo := range p.combinations {
		if combo.excess == 0 {
			continue
		}
		summaries = append(summaries, lockLimitSummary{
			Skills:  combo.skills,
			Weapons: combo.weapons,
			Quota:   combo.quota,
			Locked:  combo.lockedTotal(),
			Excess:  combo.excess,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return slices.Compare(summaries[i].Skills[:], summaries[j].Skills[:]) < 0
	})
	return summaries
}
