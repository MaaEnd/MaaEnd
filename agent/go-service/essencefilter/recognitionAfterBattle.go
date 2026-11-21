package essencefilter

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type essenceAfterBattleNthParams struct {
	RecognitionNodeName string `json:"recognitionNodeName"`
}

// EssenceFilterAfterBattleNthRecognition 在战斗结算后按行序依次返回全屏识别结果中的第 N 个框。
// C++ 返回逐格分类结果，保留奖励网格的行列顺序。
// 框缓存在 RowBoxes 中，通过递增 RowIndex 逐个吐出；每批只在首格采样，按卡片中心点击。
type EssenceFilterAfterBattleNthRecognition struct{}

var _ maa.CustomRecognitionRunner = &EssenceFilterAfterBattleNthRecognition{}

func (r *EssenceFilterAfterBattleNthRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	st := currentRun
	if st == nil {
		return nil, false
	}
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", "EssenceFilter").Str("recognition", "AfterBattleNthEssence").Msg("arg.Img nil")
		return nil, false
	}

	params := essenceAfterBattleNthParams{
		RecognitionNodeName: "EssenceFullScreenDetectAll",
	}
	if strings.TrimSpace(arg.CustomRecognitionParam) != "" {
		if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
			log.Error().Err(err).Str("component", "EssenceFilter").Str("recognition", "AfterBattleNthEssence").Msg("CustomRecognitionParam parse failed")
			return nil, false
		}
	}
	if strings.TrimSpace(params.RecognitionNodeName) == "" {
		return nil, false
	}

	if st.RowBoxes == nil {
		detail, err := ctx.RunRecognition(params.RecognitionNodeName, arg.Img, nil)
		if err != nil || detail == nil || !detail.Hit || detail.Results == nil || detail.Results.Best == nil {
			return nil, false
		}

		custom, ok := detail.Results.Best.AsCustom()
		if !ok || custom == nil || st.Loot == nil {
			return nil, false
		}
		boxes, qualities, err := parseRewardGrid(custom.Detail)
		if err != nil {
			log.Warn().Err(err).Msg("reward grid is incomplete")
			return nil, false
		}
		st.RowBoxes = boxes
		st.Loot.qualities = qualities
		st.Loot.gridVerified = true
	}

	if st.RowIndex >= len(st.RowBoxes) {
		return nil, false
	}

	box := st.RowBoxes[st.RowIndex]
	st.RowIndex++
	return &maa.CustomRecognitionResult{
		Box:    maa.Rect{box[0] + box[2]/2, box[1] + box[3]/2, 1, 1},
		Detail: "",
	}, true
}

// Every occupied cell needs positive evidence, including ordinary rewards.
func parseRewardGrid(raw string) ([][4]int, []string, error) {
	var grid struct {
		Complete bool `json:"complete"`
		Cells    []struct {
			Box     [4]int `json:"box"`
			Quality string `json:"quality"`
		} `json:"cells"`
	}
	if err := json.Unmarshal([]byte(raw), &grid); err != nil {
		return nil, nil, err
	}
	if !grid.Complete || len(grid.Cells) == 0 {
		return nil, nil, fmt.Errorf("unresolved reward grid")
	}
	var boxes [][4]int
	var qualities []string
	seen := make(map[[4]int]bool)
	for _, cell := range grid.Cells {
		if cell.Box[0] < 0 || cell.Box[1] < 0 || cell.Box[2] <= 0 || cell.Box[3] <= 0 || seen[cell.Box] {
			return nil, nil, fmt.Errorf("invalid or duplicate reward cell")
		}
		for box := range seen {
			if cell.Box[0] < box[0]+box[2] && box[0] < cell.Box[0]+cell.Box[2] && cell.Box[1] < box[1]+box[3] && box[1] < cell.Box[1]+cell.Box[3] {
				return nil, nil, fmt.Errorf("overlapping reward cells")
			}
		}
		seen[cell.Box] = true
		switch cell.Quality {
		case gold, purple:
			boxes = append(boxes, cell.Box)
			qualities = append(qualities, cell.Quality)
		case "other":
		default:
			return nil, nil, fmt.Errorf("unknown reward cell")
		}
	}
	return boxes, qualities, nil
}
