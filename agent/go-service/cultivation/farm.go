package cultivation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// farmOptions are the CultivationFarm task options, set as node attach.
type farmOptions struct {
	// Claim is a ProtocolSpaceObtainModeClaim case; Medication a
	// ProtocolSpaceUseSpMedication case.
	Claim      string `json:"claim"`
	Medication string `json:"medication"`
}

// farm runs the upstream ProtocolSpace task in TargetInventory mode with the
// saved plan's goals: SupplyPlan syncs the depot, picks the stage of the first
// missing target and repeats until every target is met or sanity runs out.
func farm(ctx *maa.Context, node string, choice *int) error {
	raw, err := ctx.GetNodeJSON(node)
	if err != nil {
		return err
	}
	var wrapper struct {
		Attach farmOptions `json:"attach"`
	}
	if err = json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return err
	}
	options := wrapper.Attach
	if options.Claim == "" || options.Medication == "" {
		return errors.New("missing farm options")
	}
	planMu.Lock()
	stored, err := loadPlan()
	planMu.Unlock()
	if err != nil {
		return err
	}
	if stored.Plan == nil {
		maafocus.Print(ctx, i18n.T("cultivation.farm_no_plan"))
		return errors.New("no complete cultivation plan")
	}
	plan := *stored.Plan
	if choice != nil {
		if err = applyWeaponChoice(ctx, &plan, *choice); err != nil {
			return err
		}
	}
	modules, err := modulePreview(plan)
	if err != nil {
		return err
	}
	var module *core.ModulePlan
	for index := range modules.Modules {
		if modules.Modules[index].Name == "ProtocolSpace" {
			module = &modules.Modules[index]
		}
	}
	if module == nil {
		return errors.New("missing ProtocolSpace module")
	}
	var lines []string
	for _, row := range modules.Rows {
		if row.Module == "ProtocolSpace" {
			lines = append(lines, fmt.Sprintf("%s: %d / %d", i18n.T("option.SupplyPlanLimits.inputs."+row.Key+".label"), row.Owned, row.Need))
		}
	}
	if len(lines) == 0 {
		maafocus.Print(ctx, i18n.T("cultivation.farm_nothing", plan.Header.Operator))
		return nil
	}
	var doc core.TaskDocument
	if err = resource.ReadJsonResource("tasks/ProtocolSpace.json", &doc); err != nil {
		return err
	}
	override, err := core.ProtocolSpaceOverride(doc, *module, map[string]string{
		"ProtocolSpaceMode":            "TargetInventory",
		"ProtocolSpaceObtainModeClaim": options.Claim,
		"ProtocolSpaceUseSpMedication": options.Medication,
	})
	if err != nil {
		return err
	}
	maafocus.Print(ctx, i18n.T("cultivation.farm_start", plan.Header.Operator, plan.Header.Expectation)+"\n"+strings.Join(lines, "\n"))
	log.Info().Str("component", "Cultivation").Interface("targets", module.Targets).Str("claim", options.Claim).
		Str("medication", options.Medication).Msg("cultivation farming via ProtocolSpace")
	detail, err := ctx.RunTask(module.Entry, override)
	if err != nil {
		return err
	}
	if detail == nil || !detail.Status.Success() {
		return errors.New("ProtocolSpace task did not succeed")
	}
	// Owned counts in the plan are from the scan; only a new scan shows the result.
	maafocus.Print(ctx, i18n.T("cultivation.farm_done"))
	return nil
}
