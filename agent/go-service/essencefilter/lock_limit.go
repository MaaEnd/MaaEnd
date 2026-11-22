package essencefilter

import (
	"slices"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
)

// lockLimitCombination is the quota plan of one skill combination.
type lockLimitCombination struct {
	quota          int
	batchLocks     int
	snapshotLocked int
}

// lockLimitDecision is the lock-limit verdict for one scanned essence.
type lockLimitDecision struct {
	Lock  bool
	Quota int
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
			combo = &lockLimitCombination{}
			p.combinations[ids] = combo
		}
		combo.quota++
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
			levels := essence.Levels
			if levels[0] < 1 || levels[0] > 6 || levels[1] < 1 || levels[1] > 6 || levels[2] < 1 || levels[2] > 3 {
				continue
			}
			combo.snapshotLocked += essence.Locked
		}
	}
	return p
}

// applyToExact limits locks only for combinations shared by selected weapons.
func (p *lockLimitPlanner) applyToExact(match *matchapi.MatchResult, locked bool) lockLimitDecision {
	if p == nil {
		return lockLimitDecision{}
	}
	verdict := p.decide(match.SkillIDs, locked)
	if verdict.Quota > 0 {
		match.ShouldLock = verdict.Lock
	}
	return verdict
}

// decide returns the verdict for one scanned essence and records the run progress.
func (p *lockLimitPlanner) decide(skillIDs []int, locked bool) lockLimitDecision {
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
