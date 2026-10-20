package cultivation

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var modulePreviewPath = func() string { return fsutil.OutputPath("debug", "record", "CultivationModules.preview.json") }

func modulePreview(plan core.Plan) (core.ModulePreview, error) {
	paths := []string{"tasks/ProtocolSpace.json", "tasks/AutoCollect.json", "tasks/AutoEssence/AutoEssence.json", "tasks/AutoEssence/Target/Target.json"}
	docs := make([]core.TaskDocument, len(paths))
	for index, path := range paths {
		if err := resource.ReadJsonResource(path, &docs[index]); err != nil {
			return core.ModulePreview{}, err
		}
	}
	return core.BuildModules(plan, docs[0], docs[1], docs[2], docs[3])
}

func showModules(ctx *maa.Context, plan core.Plan, export bool) error {
	preview, err := modulePreview(plan)
	if err != nil {
		return err
	}
	if export {
		data, err := json.MarshalIndent(preview, "", "  ")
		if err != nil {
			return err
		}
		if err = fsutil.WriteFileAtomic(modulePreviewPath(), data, 0600); err != nil {
			return err
		}
		maafocus.Print(ctx, i18n.T("cultivation.modules_exported"))
	}
	ready := 0
	var lines []string
	for _, row := range preview.Rows {
		if row.Owned >= row.Need {
			ready++
		}
		name := row.Key
		if row.Module == "ProtocolSpace" {
			name = i18n.T("option.SupplyPlanLimits.inputs." + row.Key + ".label")
		} else {
			for _, material := range plan.Materials {
				if material.ID == row.Key {
					name = iconqty.ItemDisplayName(material.ID)
					if material.Unknown {
						short := strings.TrimPrefix(material.ID, core.UnknownIDPrefix)
						if len(short) > 6 {
							short = short[:6]
						}
						name = i18n.T("cultivation.unknown_item", short)
					}
					break
				}
			}
		}
		lines = append(lines, fmt.Sprintf("%s [%s]: %d / %d (-%d)", name, i18n.T("cultivation.module."+row.Module), row.Owned, row.Need, max(row.Need-row.Owned, 0)))
	}
	if len(plan.Materials) == 0 && preview.MaterialsReady && plan.ChosenWeapon() == nil {
		// Every tab said it needs nothing: the target is already reached.
		maafocus.Print(ctx, i18n.T("cultivation.nothing_needed", plan.Header.Operator, plan.Header.Expectation))
		return nil
	}
	state := "cultivation.materials_missing"
	if preview.MaterialsReady {
		state = "cultivation.materials_ready"
	}
	if preview.Completion != nil {
		maafocus.Print(ctx, i18n.T("cultivation.user_confirmed", plan.Header.Operator, preview.Completion.At.Local().Format("2006-01-02 15:04:05 -07:00")))
	}
	maafocus.Print(ctx, i18n.T("cultivation.progress", plan.Header.Operator, plan.Header.Expectation, ready, len(preview.Rows), plan.ReadAt.Local().Format("2006-01-02 15:04:05 -07:00"), plan.Revision)+"\n"+
		i18n.T(state)+"\n"+strings.Join(lines, "\n"))
	if preview.Completion == nil {
		maafocus.Print(ctx, i18n.T("cultivation.modules_pending"))
	}
	unknown := 0
	for _, material := range plan.Materials {
		if material.Unknown {
			unknown++
		}
	}
	if unknown > 0 {
		maafocus.Print(ctx, i18n.T("cultivation.unknown_notice", unknown))
	}
	if len(plan.Weapons) > 0 {
		traits := weaponTraits()
		var names []string
		for index, weapon := range plan.Weapons {
			entry := fmt.Sprintf("%d. %s", index+1, weapon.Name)
			if e := weapon.Essence; e != nil && e.Injected != "" {
				entry = i18n.T("cultivation.weapon_entry_injected", index+1, weapon.Name, strings.Join(traits[weapon.ID], " | "), e.Injected)
			} else if e != nil {
				entry = i18n.T("cultivation.weapon_entry", index+1, weapon.Name, strings.Join(traits[weapon.ID], " | "), e.Flawless)
			}
			names = append(names, entry)
		}
		chosen := plan.WeaponChoice
		if chosen == "" {
			chosen = i18n.T("cultivation.weapon_none")
		}
		maafocus.Print(ctx, i18n.T("cultivation.weapons", strings.Join(names, "\n"), chosen))
		if weapon := plan.ChosenWeapon(); weapon != nil {
			key := "cultivation.essence_missing"
			if weapon.EssenceSatisfied() {
				key = "cultivation.essence_satisfied"
			}
			maafocus.Print(ctx, i18n.T(key, weapon.Name))
		}
	}
	return nil
}

// weaponTraits maps weapon IDs to their three essence traits from the upstream
// EssenceFilter weapon data; missing data only hides the traits.
func weaponTraits() map[string][]string {
	out := map[string][]string{}
	engine, err := matchapi.NewDefaultEngine()
	if err != nil {
		log.Warn().Err(err).Str("component", "Cultivation").Msg("weapon traits unavailable")
		return out
	}
	for _, weapon := range engine.Weapons() {
		out[weapon.InternalID] = weapon.SkillsChinese
	}
	return out
}
