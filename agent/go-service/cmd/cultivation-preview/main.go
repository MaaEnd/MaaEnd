// cultivation-preview validates a saved plan without loading MaaFramework or a device.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/jsonclean"
)

func main() {
	planFile := flag.String("plan", "", "saved plan JSON (required)")
	interfaceFile := flag.String("protocol", "", "ProtocolSpace.json option template (required)")
	flag.Parse()
	if err := run(*planFile, *interfaceFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(planFile, protocolFile string) error {
	if planFile == "" || protocolFile == "" {
		return fmt.Errorf("-plan and -protocol are required")
	}
	raw, err := os.ReadFile(planFile)
	if err != nil {
		return err
	}
	var plan core.Plan
	if err = json.Unmarshal(raw, &plan); err != nil {
		return err
	}
	goals, manual, err := core.BuildTargets(plan)
	if err != nil {
		return err
	}
	raw, err = os.ReadFile(protocolFile)
	if err != nil {
		return err
	}
	var source struct {
		Option map[string]struct {
			PipelineOverride json.RawMessage `json:"pipeline_override"`
		} `json:"option"`
	}
	if err = json.Unmarshal(jsonclean.Clean(raw), &source); err != nil {
		return err
	}
	template := source.Option["SupplyPlanLimits"].PipelineOverride
	if len(template) == 0 {
		return fmt.Errorf("missing SupplyPlanLimits")
	}
	patch, err := core.RenderOverrides(template, goals)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"executable": false, "goals": goals, "manual": manual, "weapon": plan.Weapon, "pipeline_override": patch})
}
