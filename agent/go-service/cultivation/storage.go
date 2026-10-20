package cultivation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

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
		if stored.Plan.Revision != stored.Revision {
			return storedPlan{}, errors.New("plan/envelope revision mismatch")
		}
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
	return savePlanWithWeapon(plan, expected, nil)
}

// savePlanWithWeapon validates the scan's explicit weapon selection before commit.
// A nil choice preserves a previous selection only when it is still listed.
func savePlanWithWeapon(plan core.Plan, expected uint64, choice *int) error {
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
	// A rescan keeps the weapon chosen before if that weapon is still listed.
	if choice != nil {
		plan.WeaponChoice, err = weaponChoiceName(plan, *choice)
		if err != nil {
			return err
		}
	} else if plan.WeaponChoice == "" && stored.Plan != nil {
		for _, weapon := range plan.Weapons {
			if weapon.Name == stored.Plan.WeaponChoice {
				plan.WeaponChoice = weapon.Name
			}
		}
	}
	if _, _, err = core.BuildTargets(plan); err != nil {
		return err
	}
	stored.Revision++
	plan.Revision = stored.Revision
	plan.Completion = nil
	stored.Plan = &plan
	return writePlan(stored)
}

// setWeaponChoice records which usable weapon to develop. It is a setting on
// the stored scan, so the revision stays; a changed plan revision rejects it.
func setWeaponChoice(revision uint64, name string) error {
	planMu.Lock()
	defer planMu.Unlock()
	stored, err := loadPlan()
	if err != nil {
		return err
	}
	if stored.Plan == nil || stored.Revision != revision {
		return errors.New("plan cleared or changed before the weapon choice was saved")
	}
	plan := *stored.Plan
	if plan.WeaponChoice == name {
		return nil
	}
	plan.WeaponChoice = name
	plan.Completion = nil
	if _, _, err = core.BuildTargets(plan); err != nil {
		return err
	}
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

// confirmPlan records the user's dedicated confirmation-task selection.
// It changes no game state and cannot authorize any farming execution.
func confirmPlan() error {
	planMu.Lock()
	defer planMu.Unlock()
	stored, err := loadPlan()
	if err != nil {
		return err
	}
	if stored.Plan == nil {
		return errors.New("no cultivation plan to confirm")
	}
	stored.Revision++
	stored.Plan.Revision = stored.Revision
	stored.Plan.Completion = &core.Completion{Method: "user_confirmed", Revision: stored.Revision, Header: stored.Plan.Header, At: time.Now().UTC()}
	return writePlan(stored)
}
