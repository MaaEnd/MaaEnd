package autofight

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// EndaxisProject represents the top-level structure of exported data.
type EndaxisProject struct {
	Version          string            `json:"version"`
	ScenarioList     []EndaxisScenario `json:"scenarioList"`
	ActiveScenarioID string            `json:"activeScenarioId"`
}

// EndaxisScenario represents a single battle scenario.
type EndaxisScenario struct {
	ID   string              `json:"id"`
	Name string              `json:"name"`
	Data EndaxisScenarioData `json:"data"`
}

// EndaxisScenarioData holds tracks and other combat parameters.
type EndaxisScenarioData struct {
	Tracks       []EndaxisTrack `json:"tracks"`
	PrepDuration float64        `json:"prepDuration"`
}

// EndaxisTrack holds the combat actions for a single character in the party.
type EndaxisTrack struct {
	ID      string          `json:"id"`
	Actions []EndaxisAction `json:"actions"`
}

// EndaxisAction represents a specific action performed by a character.
type EndaxisAction struct {
	InstanceID string  `json:"instanceId"`
	Type       string  `json:"type"` // e.g., "skill", "link", "ultimate", "execution", "attack", "dodge"
	StartTime  float64 `json:"startTime"`
	Duration   float64 `json:"duration"`
}

// DecodeDataCode decodes a URL-safe Base64 encoded, Gzip compressed JSON string.
// It returns an EndaxisProject representing the strategy data.
func DecodeDataCode(dataCode string) (*EndaxisProject, error) {
	if dataCode == "" {
		return nil, fmt.Errorf("data code is empty")
	}

	// 1. Base64 URL-safe Decode
	decodedBytes, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(dataCode)
	if err != nil {
		return nil, fmt.Errorf("failed to decode Base64: %w", err)
	}

	// 2. Gzip Decompression
	bReader := bytes.NewReader(decodedBytes)
	gzReader, err := gzip.NewReader(bReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	jsonBytes, err := io.ReadAll(gzReader)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress gzip: %w", err)
	}

	// 3. JSON Unmarshal
	var project EndaxisProject
	if err := json.Unmarshal(jsonBytes, &project); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return &project, nil
}
