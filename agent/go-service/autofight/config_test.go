package autofight

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func createTestDataCode(t *testing.T) string {
	dummyProject := EndaxisProject{
		Version: "1.0.0",
		ScenarioList: []EndaxisScenario{
			{
				ID:   "sc_1",
				Name: "Test Config Scenario",
				Data: EndaxisScenarioData{
					PrepDuration: 5.0,
					Tracks: []EndaxisTrack{
						{
							ID: "SOME_CHAR",
							Actions: []EndaxisAction{
								{InstanceID: "act_2", Type: "attack", StartTime: 8.0, Duration: 1.0},
								{InstanceID: "act_1", Type: "skill", StartTime: 6.0, Duration: 1.0},
							},
						},
					},
				},
			},
		},
		ActiveScenarioID: "sc_1",
	}

	jsonBytes, err := json.Marshal(dummyProject)
	if err != nil {
		t.Fatalf("failed to marshal JSON: %v", err)
	}

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(jsonBytes); err != nil {
		t.Fatalf("failed to compress: %v", err)
	}
	gzWriter.Close()

	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(buf.Bytes())
}

func TestConfigLoadAndGet(t *testing.T) {
	ClearConfig() // Ensure clean state
	if HasConfig() {
		t.Errorf("expected no config initially")
	}

	dataCode := createTestDataCode(t)
	err := LoadConfig(dataCode)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if !HasConfig() {
		t.Errorf("expected config to be loaded")
	}

	config := GetConfig()
	if config == nil {
		t.Fatalf("expected non-nil config")
	}

	if config.ScenarioName != "Test Config Scenario" {
		t.Errorf("expected scenario name 'Test Config Scenario', got '%s'", config.ScenarioName)
	}

	if len(config.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(config.Tracks))
	}

	// Verify flattening and sorting
	events := config.Events
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].Type != EventSkill || events[0].Time != 1.0 {
		t.Errorf("expected first event to be Skill at 1.0, got %s at %f", events[0].Type, events[0].Time)
	}
	if events[1].Type != EventSwitchOperator || events[1].Time != 3.0 {
		t.Errorf("expected second event to be SwitchOperator at 3.0, got %s at %f", events[1].Type, events[1].Time)
	}
}

func TestConfigLoadRedundant(t *testing.T) {
	ClearConfig()
	dataCode := createTestDataCode(t)

	// First load
	err := LoadConfig(dataCode)
	if err != nil {
		t.Fatalf("first LoadConfig failed: %v", err)
	}

	// Should fast exit on second load
	err2 := LoadConfig(dataCode)
	if err2 != nil {
		t.Fatalf("redundant LoadConfig failed: %v", err2)
	}
}

func TestConfigClear(t *testing.T) {
	dataCode := createTestDataCode(t)
	LoadConfig(dataCode)

	if !HasConfig() {
		t.Fatalf("expected config to exist")
	}

	ClearConfig()
	if HasConfig() {
		t.Errorf("expected config to be cleared")
	}
}
