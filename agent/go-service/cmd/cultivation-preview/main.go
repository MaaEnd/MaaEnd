// cultivation-preview validates a saved plan without loading MaaFramework or a device.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

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
	paths := []string{protocolFile, filepath.Join(filepath.Dir(protocolFile), "AutoCollect.json"),
		filepath.Join(filepath.Dir(protocolFile), "AutoEssence/AutoEssence.json"), filepath.Join(filepath.Dir(protocolFile), "AutoEssence/Target/Target.json")}
	var docs [4]core.TaskDocument
	for index, path := range paths {
		raw, err = os.ReadFile(path)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(jsonclean.Clean(raw), &docs[index]); err != nil {
			return err
		}
	}
	preview, err := core.BuildModules(plan, docs[0], docs[1], docs[2], docs[3])
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(preview)
}
