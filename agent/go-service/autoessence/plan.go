package autoessence

import (
	"fmt"
	"sort"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
)

// FarmPlan is one pre-inscription plan at a farm location.
type FarmPlan struct {
	LocationName string
	LocationKey  string
	Slot1IDs     [3]int
	Slot1Names   [3]string
	FixedSlot    int // 2 or 3
	FixedID      int
	FixedName    string
	Needs        []matchapi.WeaponData
	Matched      []matchapi.WeaponData
}

type skillIndex map[int]map[int][]matchapi.WeaponData

func buildSkillIndex(allTargets []matchapi.SkillCombination, slotIdx int) skillIndex {
	idx := make(skillIndex)
	for _, combo := range allTargets {
		if len(combo.SkillIDs) < 3 {
			continue
		}
		s1, sN := combo.SkillIDs[0], combo.SkillIDs[slotIdx]
		if idx[s1] == nil {
			idx[s1] = make(map[int][]matchapi.WeaponData)
		}
		idx[s1][sN] = append(idx[s1][sN], combo.Weapon)
	}
	return idx
}

func buildFeasibleWeaponSet(
	allTargets []matchapi.SkillCombination,
	slot2Set, slot3Set map[int]bool,
) map[string]bool {
	feasible := make(map[string]bool)
	for _, combo := range allTargets {
		if len(combo.SkillIDs) < 3 {
			continue
		}
		if slot2Set[combo.SkillIDs[1]] && slot3Set[combo.SkillIDs[2]] {
			feasible[combo.Weapon.ChineseName] = true
		}
	}
	return feasible
}

func lookupWeapons(
	idx skillIndex,
	s1Set [3]int,
	fixedID int,
	feasible map[string]bool,
	missingNames map[string]bool,
) (matched, needs []matchapi.WeaponData) {
	for _, s1ID := range s1Set {
		for _, w := range idx[s1ID][fixedID] {
			if feasible != nil && !feasible[w.ChineseName] {
				continue
			}
			matched = append(matched, w)
			if missingNames[w.ChineseName] {
				needs = append(needs, w)
			}
		}
	}
	return
}

func enumPlansAtLocation(
	slot1Pool, availSlot2, availSlot3 []matchapi.SkillPool,
	idx2, idx3 skillIndex,
	feasible map[string]bool,
	missingNames map[string]bool,
	locationName, locationKey string,
) []FarmPlan {
	n1 := len(slot1Pool)
	var plans []FarmPlan
	for i := 0; i < n1-2; i++ {
		for j := i + 1; j < n1-1; j++ {
			for k := j + 1; k < n1; k++ {
				s1Names := [3]string{slot1Pool[i].Chinese, slot1Pool[j].Chinese, slot1Pool[k].Chinese}
				s1IDs := [3]int{slot1Pool[i].ID, slot1Pool[j].ID, slot1Pool[k].ID}
				for _, s2 := range availSlot2 {
					matched, needs := lookupWeapons(idx2, s1IDs, s2.ID, feasible, missingNames)
					if len(needs) == 0 {
						continue
					}
					plans = append(plans, FarmPlan{
						LocationName: locationName,
						LocationKey:  locationKey,
						Slot1IDs:     s1IDs,
						Slot1Names:   s1Names,
						FixedSlot:    2,
						FixedID:      s2.ID,
						FixedName:    s2.Chinese,
						Needs:        needs,
						Matched:      matched,
					})
				}
				for _, s3 := range availSlot3 {
					matched, needs := lookupWeapons(idx3, s1IDs, s3.ID, feasible, missingNames)
					if len(needs) == 0 {
						continue
					}
					plans = append(plans, FarmPlan{
						LocationName: locationName,
						LocationKey:  locationKey,
						Slot1IDs:     s1IDs,
						Slot1Names:   s1Names,
						FixedSlot:    3,
						FixedID:      s3.ID,
						FixedName:    s3.Chinese,
						Needs:        needs,
						Matched:      matched,
					})
				}
			}
		}
	}
	sort.SliceStable(plans, func(i, j int) bool {
		if len(plans[i].Needs) != len(plans[j].Needs) {
			return len(plans[i].Needs) > len(plans[j].Needs)
		}
		return len(plans[i].Matched) > len(plans[j].Matched)
	})
	return plans
}

// SelectBestPlan picks the farm plan covering the most missing weapons.
// missingIDs are weapon internal IDs that still need one essence.
// Returns nil when no plan covers any missing weapon.
func SelectBestPlan(engine *matchapi.Engine, missingIDs []string) (*FarmPlan, error) {
	if engine == nil {
		return nil, fmt.Errorf("nil match engine")
	}
	if len(missingIDs) == 0 {
		return nil, nil
	}

	want := make(map[string]bool, len(missingIDs))
	for _, id := range missingIDs {
		want[id] = true
	}

	allWeapons := engine.Weapons()
	var targets []matchapi.SkillCombination
	missingNames := make(map[string]bool)
	for _, w := range allWeapons {
		if !want[w.InternalID] {
			continue
		}
		if len(w.SkillIDs) < 3 {
			continue
		}
		targets = append(targets, matchapi.SkillCombination{
			Weapon:        w,
			SkillsChinese: w.SkillsChinese,
			SkillIDs:      w.SkillIDs,
		})
		missingNames[w.ChineseName] = true
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no weapon data for missing ids")
	}

	// Include all weapons as matched candidates so plan matched counts stay meaningful,
	// but needs only count missing. Feasibility uses the missing targets' skill pools
	// via location slot checks against each missing combo; expand targets to all weapons
	// that share rarities with missing set for matched scoring.
	allTargets := make([]matchapi.SkillCombination, 0, len(allWeapons))
	for _, w := range allWeapons {
		if len(w.SkillIDs) < 3 {
			continue
		}
		allTargets = append(allTargets, matchapi.SkillCombination{
			Weapon:        w,
			SkillsChinese: w.SkillsChinese,
			SkillIDs:      w.SkillIDs,
		})
	}

	slot1Pool := engine.SkillPools().Slot1
	slot2Pool := engine.SkillPools().Slot2
	slot3Pool := engine.SkillPools().Slot3
	idx2 := buildSkillIndex(allTargets, 1)
	idx3 := buildSkillIndex(allTargets, 2)

	var best *FarmPlan
	for _, loc := range engine.Locations() {
		key, ok := LocationKeyByName(loc.Name)
		if !ok {
			continue
		}
		slot2Set := make(map[int]bool, len(loc.Slot2IDs))
		for _, id := range loc.Slot2IDs {
			slot2Set[id] = true
		}
		slot3Set := make(map[int]bool, len(loc.Slot3IDs))
		for _, id := range loc.Slot3IDs {
			slot3Set[id] = true
		}
		var avail2, avail3 []matchapi.SkillPool
		for _, s := range slot2Pool {
			if slot2Set[s.ID] {
				avail2 = append(avail2, s)
			}
		}
		for _, s := range slot3Pool {
			if slot3Set[s.ID] {
				avail3 = append(avail3, s)
			}
		}
		// Feasibility for needs uses missing targets only.
		feasible := buildFeasibleWeaponSet(targets, slot2Set, slot3Set)
		// Also mark other weapons feasible for matched scoring.
		for name, ok := range buildFeasibleWeaponSet(allTargets, slot2Set, slot3Set) {
			if ok {
				feasible[name] = true
			}
		}
		plans := enumPlansAtLocation(slot1Pool, avail2, avail3, idx2, idx3, feasible, missingNames, loc.Name, key)
		if len(plans) == 0 {
			continue
		}
		cand := plans[0]
		if best == nil ||
			len(cand.Needs) > len(best.Needs) ||
			(len(cand.Needs) == len(best.Needs) && len(cand.Matched) > len(best.Matched)) {
			cp := cand
			best = &cp
		}
	}
	return best, nil
}
