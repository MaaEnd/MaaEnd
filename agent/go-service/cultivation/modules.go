package cultivation

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

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
					name = material.Name
					break
				}
			}
		}
		lines = append(lines, fmt.Sprintf("%s [%s]: %d / %d (-%d)", name, i18n.T("cultivation.module."+row.Module), row.Owned, row.Need, max(row.Need-row.Owned, 0)))
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
	if plan.Weapon != nil {
		maafocus.Print(ctx, i18n.T("cultivation.weapon", plan.Weapon.Name))
	}
	if !export {
		return nil
	}
	data, err := json.MarshalIndent(preview, "", "  ")
	if err != nil {
		return err
	}
	if err = fsutil.WriteFileAtomic(fsutil.OutputPath("debug", "record", "CultivationModules.preview.json"), data, 0600); err != nil {
		return err
	}
	maafocus.Print(ctx, i18n.T("cultivation.modules_exported"))
	return nil
}
