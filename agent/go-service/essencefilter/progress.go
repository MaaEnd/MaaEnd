package essencefilter

import (
	"slices"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

// TargetProgress describes the physical essences available for one shared combination.
type TargetProgress struct {
	WeaponIDs []string
	Names     []string
	Have      int
	Need      int
}

// TargetProgress groups selected weapons by their exact three-skill combination.
func (s *InventoryStock) TargetProgress(weapons []matchapi.WeaponData, selected []string) []TargetProgress {
	var groups []TargetProgress
	positions := make(map[[3]int]int)
	for _, weapon := range weapons {
		if !slices.Contains(selected, weapon.InternalID) {
			continue
		}
		key, ok := skillIDsTriple(weapon.SkillIDs)
		if !ok {
			continue
		}
		index, found := positions[key]
		if !found {
			index = len(groups)
			positions[key] = index
			groups = append(groups, TargetProgress{})
		}
		group := &groups[index]
		group.WeaponIDs = append(group.WeaponIDs, weapon.InternalID)
		group.Names = append(group.Names, weapon.ChineseName)
		group.Need++
	}
	for i := range groups {
		groups[i].Have = s.Count(groups[i].WeaponIDs)
	}
	return groups
}

// Text describes a group's available count and outstanding physical copies.
func (g TargetProgress) Text() string {
	return i18n.T("essencefilter.progress.group", strings.Join(g.Names, "／"), g.Have, g.Need, max(0, g.Need-g.Have))
}

type rewardReportRow struct {
	kind    string
	quality string
	weapons []string
	count   int
}

// Each processed grid position contributes once, using its confirmed final mark.
func (r *rewardInventory) reportRows(selected []string) []rewardReportRow {
	var rows []rewardReportRow
	positions := make(map[string]int)
	for i := 1; i <= len(r.items); i++ {
		item := r.items[i]
		kind := "skipped"
		switch {
		case r.originalMarks[i] == lockedMark:
			kind = "already_locked"
		case r.originalMarks[i] == discarded:
			kind = "already_discarded"
		case item.Mark == lockedMark:
			kind = "locked"
			if len(selected) > 0 && !slices.ContainsFunc(item.weaponIDs, func(id string) bool { return slices.Contains(selected, id) }) {
				kind = "incidental"
			}
		case item.Mark == discarded:
			kind = "discarded"
		case item.Quality == gold && len(item.weaponIDs) > 0:
			kind = "quota_full"
		}
		key := kind + "/" + item.Quality + "/" + strings.Join(item.weaponIDs, ",")
		index, found := positions[key]
		if !found {
			index = len(rows)
			positions[key] = index
			rows = append(rows, rewardReportRow{kind: kind, quality: item.Quality, weapons: item.weaponIDs})
		}
		rows[index].count++
	}
	return rows
}

func rewardReport(st *RunState, before *inventorySnapshot) string {
	weapons := st.MatchEngine.Weapons()
	names := make(map[string]string, len(weapons))
	for _, weapon := range weapons {
		names[weapon.InternalID] = weapon.ChineseName
	}
	selected := st.PipelineOpts.LockLimitWeaponIDs
	var lines []string
	observedWeapons := make(map[string]bool)
	for _, row := range st.Loot.reportRows(selected) {
		var matched []string
		for _, id := range row.weapons {
			matched = append(matched, names[id])
			observedWeapons[id] = true
		}
		label := strings.Join(matched, "／")
		if label == "" {
			label = i18n.T("essencefilter.reward." + row.quality)
		}
		lines = append(lines, i18n.T("essencefilter.reward."+row.kind, label, row.count))
	}
	if len(lines) == 0 {
		lines = append(lines, i18n.T("essencefilter.reward.empty"))
	}
	if len(selected) > 0 {
		priorStock := stockFromGroups(before.groups(weapons))
		after := st.Loot.appendTo(before)
		newStock := stockFromGroups(after.groups(weapons))
		for _, group := range newStock.TargetProgress(weapons, selected) {
			if slices.ContainsFunc(group.WeaponIDs, func(id string) bool { return observedWeapons[id] }) {
				lines = append(lines, group.Text())
			}
		}
		remaining := len(newStock.MissingWeapons(selected))
		lines = append(lines, i18n.T("essencefilter.reward.remaining", remaining, len(priorStock.MissingWeapons(selected))-remaining))
	}
	return i18n.RenderHTML("essencefilter.progress", map[string]any{
		"Title": i18n.T("essencefilter.reward.title"), "Lines": lines,
	})
}
