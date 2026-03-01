package autofight

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// EndaxisProject 顶层结构
type EndaxisProject struct {
	Version          string                 `json:"version"`
	ScenarioList     []EndaxisScenario      `json:"scenarioList"`
	ActiveScenarioID string                 `json:"activeScenarioId"`
	SystemConstants  EndaxisSystemConstants `json:"systemConstants"`
}

// EndaxisSystemConstants 系统常量（技力等全局参数）
type EndaxisSystemConstants struct {
	MaxSp              int     `json:"maxSp"`
	InitialSp          int     `json:"initialSp"`
	SpRegenRate        float64 `json:"spRegenRate"`
	SkillSpCostDefault int     `json:"skillSpCostDefault"`
}

// EndaxisScenario 单个方案
type EndaxisScenario struct {
	ID   string              `json:"id"`
	Name string              `json:"name"`
	Data EndaxisScenarioData `json:"data"`
}

// EndaxisScenarioData 方案数据
type EndaxisScenarioData struct {
	Tracks       []EndaxisTrack `json:"tracks"`
	PrepDuration float64        `json:"prepDuration"`
}

// EndaxisTrack 单个角色轨道
type EndaxisTrack struct {
	ID           string          `json:"id"`
	Actions      []EndaxisAction `json:"actions"`
	InitialGauge float64         `json:"initialGauge"`
}

// EndaxisAction 某个角色执行的具体动作
type EndaxisAction struct {
	InstanceID    string  `json:"instanceId"`
	Type          string  `json:"type"` // skill, link, ultimate, execution, attack, dodge
	StartTime     float64 `json:"startTime"`
	Duration      float64 `json:"duration"`
	SpCost        float64 `json:"spCost"`
	GaugeCost     float64 `json:"gaugeCost"`
	AnimationTime float64 `json:"animationTime"`
}

// DecodeDataCode 解码 Endaxis 数据码（URL-safe Base64 → Gzip → JSON）
func DecodeDataCode(dataCode string) (*EndaxisProject, error) {
	if dataCode == "" {
		return nil, fmt.Errorf("data code is empty")
	}

	// 1. Base64 URL-safe 解码
	decodedBytes, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(dataCode)
	if err != nil {
		return nil, fmt.Errorf("failed to decode Base64: %w", err)
	}

	// 2. Gzip 解压
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

	// 3. JSON 反序列化
	var project EndaxisProject
	if err := json.Unmarshal(jsonBytes, &project); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return &project, nil
}
