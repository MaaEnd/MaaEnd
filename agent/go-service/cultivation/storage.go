package cultivation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
)

type storedPlan struct {
	Version  int        `json:"version"`
	Revision uint64     `json:"revision"`
	Plan     *core.Plan `json:"plan"`
}

var planMu sync.Mutex
var recordPath = func() string { return fsutil.OutputPath("debug", "record", "CultivationPlan.json") }

func planPath() string { return recordPath() }

func loadPlan() (storedPlan, error) {
	data, err := os.ReadFile(planPath())
	if os.IsNotExist(err) {
		return storedPlan{Version: 1}, nil
	}
	if err != nil {
		return storedPlan{}, err
	}
	if string(data) == "null" {
		return storedPlan{}, errors.New("null cultivation plan")
	}
	var stored storedPlan
	if err = json.Unmarshal(data, &stored); err != nil {
		return storedPlan{}, fmt.Errorf("invalid plan, refusing overwrite: %w", err)
	}
	if stored.Version != 1 {
		return storedPlan{}, errors.New("missing or unsupported cultivation record version")
	}
	if stored.Plan != nil {
		if _, _, err = core.BuildTargets(*stored.Plan); err != nil {
			return storedPlan{}, err
		}
	}
	return stored, nil
}

func writePlan(stored storedPlan) error {
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	path := planPath()
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0600)
}

func savePlan(plan core.Plan, expected uint64) error {
	if _, _, err := core.BuildTargets(plan); err != nil {
		return err
	}
	planMu.Lock()
	defer planMu.Unlock()
	stored, err := loadPlan()
	if err != nil {
		return err
	}
	if stored.Revision != expected {
		return errors.New("plan cleared or changed during scan")
	}
	stored.Revision++
	plan.Revision = stored.Revision
	stored.Plan = &plan
	return writePlan(stored)
}

func clearPlan() error {
	planMu.Lock()
	defer planMu.Unlock()
	stored, err := loadPlan()
	if err != nil {
		return err
	}
	stored.Revision++
	stored.Plan = nil
	return writePlan(stored)
}
