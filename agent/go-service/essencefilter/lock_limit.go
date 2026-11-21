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

// lockLimitRow is one level triple held for a skill combination.
type lockLimitRow struct {
	levels     [3]int
	expected   float64
	locked     int
	keptLocked int
}

// lockLimitCombination is the quota plan of one skill combination.
type lockLimitCombination struct {
	quota          int
	weapons        []string
	skills         [3]string
	rows           map[[3]int]*lockLimitRow
	ordered        []*lockLimitRow
	batchLocks     int
	snapshotLocked int
	excess         int
}

// lockLimitDecision is the lock-limit verdict for one scanned essence.
type lockLimitDecision struct {
	Lock bool
	// NeedsUnlock marks a locked essence that sits outside the lock quota.
	NeedsUnlock bool
	Quota       int
}

// lockLimitPlanner decides lock or skip for exact-matching essences under the
// per-combination quota. It neither touches the pipeline nor the file system.
type lockLimitPlanner struct {
	combinations map[[3]int]*lockLimitCombination
}

// isLockLimitTarget reports whether a weapon contributes a place to this farming task's quota.
func isLockLimitTarget(weapon matchapi.WeaponData, selected []string) bool {
	return (weapon.Rarity == 5 || weapon.Rarity == 6) && slices.Contains(selected, weapon.InternalID)
}

// newLockLimitPlanner resolves inventory groups against the full catalog and counts only selected weapons.
func newLockLimitPlanner(groups []inventoryGroup, weapons []matchapi.WeaponData, selected []string) *lockLimitPlanner {
	p := &lockLimitPlanner{
		combinations: make(map[[3]int]*lockLimitCombination),
	}
	weaponByID := make(map[string]matchapi.WeaponData, len(weapons))
	for _, weapon := range weapons {
		weaponByID[weapon.InternalID] = weapon
		if !isLockLimitTarget(weapon, selected) {
			continue
		}
		ids, ok := skillIDsTriple(weapon.SkillIDs)
		if !ok {
			continue
		}
		combo := p.combinations[ids]
		if combo == nil {
			combo = &lockLimitCombination{rows: make(map[[3]int]*lockLimitRow)}
			combo.skills, _ = skillsTriple(weapon.SkillsChinese)
			p.combinations[ids] = combo
		}
		combo.quota++
		combo.weapons = append(combo.weapons, weapon.ChineseName)
	}
	for _, group := range groups {
		ids, ok := resolveGroupSkillIDs(group, weaponByID)
		if !ok {
			continue
		}
		combo := p.combinations[ids]
		if combo == nil {
			continue
		}
		for _, essence := range group.Essences {
			if essence.Locked <= 0 || essence.Locked > essence.Count {
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
			row.locked += essence.Locked
		}
	}
	for _, combo := range p.combinations {
		combo.buildRanking()
	}
	return p
}

// Rank existing locks only for the inventory excess summary.
func (c *lockLimitCombination) buildRanking() {
	c.ordered = make([]*lockLimitRow, 0, len(c.rows))
	for _, row := range c.rows {
		c.ordered = append(c.ordered, row)
		c.snapshotLocked += row.locked
	}
	sort.Slice(c.ordered, func(i, j int) bool {
		if c.ordered[i].expected != c.ordered[j].expected {
			return c.ordered[i].expected < c.ordered[j].expected
		}
		return slices.Compare(c.ordered[i].levels[:], c.ordered[j].levels[:]) < 0
	})
	keepLocked := c.quota
	for _, row := range c.ordered {
		row.keptLocked = min(row.locked, keepLocked)
		keepLocked -= row.keptLocked
		c.excess += row.locked - row.keptLocked
	}
}

// applyToExact limits locks only for combinations shared by selected weapons.
func (p *lockLimitPlanner) applyToExact(match *matchapi.MatchResult, locked bool) lockLimitDecision {
	if p == nil {
		return lockLimitDecision{}
	}
	verdict := p.decide(match.SkillIDs, match.Levels, locked)
	if verdict.Quota > 0 {
		match.ShouldLock = verdict.Lock
	}
	return verdict
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
		return decision
	}
	decision.Quota = combo.quota
	if locked {
		if row := combo.rows[levels]; row != nil {
			decision.NeedsUnlock = row.locked > row.keptLocked
		}
		return decision
	}
	if combo.snapshotLocked+combo.batchLocks < combo.quota {
		decision.Lock = true
		combo.batchLocks++
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

// lockLimitSummary is one combination holding essences that need unlocking.
type lockLimitSummary struct {
	Skills  [3]string
	Weapons []string
	Quota   int
	Locked  int
	Excess  int
	Rows    []lockLimitSummaryRow
}

// lockLimitSummaryRow names the level triple and the count of essences that need unlocking.
type lockLimitSummaryRow struct {
	Levels [3]int
	Count  int
}

// lockedTotal returns the combination's locked essences, counting the locks made in this run.
func (c *lockLimitCombination) lockedTotal() int {
	return c.snapshotLocked + c.batchLocks
}

// overLimitSummaries lists what needs unlocking, ordered by skill combination and levels.
func (p *lockLimitPlanner) overLimitSummaries() []lockLimitSummary {
	var summaries []lockLimitSummary
	for _, combo := range p.combinations {
		if combo.excess == 0 {
			continue
		}
		summary := lockLimitSummary{
			Skills:  combo.skills,
			Weapons: combo.weapons,
			Quota:   combo.quota,
			Locked:  combo.lockedTotal(),
			Excess:  combo.excess,
		}
		for _, row := range combo.ordered {
			if row.locked <= row.keptLocked {
				continue
			}
			summary.Rows = append(summary.Rows, lockLimitSummaryRow{
				Levels: row.levels,
				Count:  row.locked - row.keptLocked,
			})
		}
		sort.Slice(summary.Rows, func(i, j int) bool {
			return slices.Compare(summary.Rows[i].Levels[:], summary.Rows[j].Levels[:]) < 0
		})
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return slices.Compare(summaries[i].Skills[:], summaries[j].Skills[:]) < 0
	})
	return summaries
}
