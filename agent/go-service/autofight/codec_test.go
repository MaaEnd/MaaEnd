package autofight

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestDecodeDataCode(t *testing.T) {
	// Create a dummy JSON
	dummyProject := EndaxisProject{
		Version: "1.0.0",
		ScenarioList: []EndaxisScenario{
			{
				ID:   "sc_1",
				Name: "Test Scenario",
				Data: EndaxisScenarioData{
					PrepDuration: 5.0,
					Tracks: []EndaxisTrack{
						{
							ID: "ENDMINISTRATOR",
							Actions: []EndaxisAction{
								{
									InstanceID: "act_1",
									Type:       "skill",
									StartTime:  6.0,
									Duration:   1.0,
								},
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

	// Compress with Gzip
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(jsonBytes); err != nil {
		t.Fatalf("failed to compress: %v", err)
	}
	gzWriter.Close()

	// Base64 URL encode (no padding)
	dataCode := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(buf.Bytes())

	// Test decoding
	project, err := DecodeDataCode(dataCode)
	if err != nil {
		t.Fatalf("DecodeDataCode failed: %v", err)
	}

	if project.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", project.Version)
	}
	if project.ActiveScenarioID != "sc_1" {
		t.Errorf("expected active scenario sc_1, got %s", project.ActiveScenarioID)
	}
	if len(project.ScenarioList) != 1 {
		t.Fatalf("expected 1 scenario, got %d", len(project.ScenarioList))
	}
	if len(project.ScenarioList[0].Data.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(project.ScenarioList[0].Data.Tracks))
	}
	if project.ScenarioList[0].Data.Tracks[0].ID != "ENDMINISTRATOR" {
		t.Errorf("expected track ID ENDMINISTRATOR, got %s", project.ScenarioList[0].Data.Tracks[0].ID)
	}
}

func TestDecodeDataCode_Empty(t *testing.T) {
	_, err := DecodeDataCode("")
	if err == nil {
		t.Errorf("expected error for empty data code, got nil")
	}
}

func TestDecodeDataCode_InvalidBase64(t *testing.T) {
	// This has padding which is invalid for NoPadding
	_, err := DecodeDataCode("aW52YWxpZA==")
	if err == nil {
		t.Errorf("expected error for invalid base64, got nil")
	}
}
