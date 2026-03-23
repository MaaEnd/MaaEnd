package essencefilter

import (
	"encoding/json"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type essenceAfterBattleNthParams struct {
	RecognitionNodeName string `json:"recognitionNodeName"`
}

// EssenceFilterAfterBattleNthRecognition
//   - 缓存为空时：扫描一次，覆盖填充 Filtered 结果到 RowBoxes，返回第 0 个
//   - 缓存不为空时：返回 RowBoxes[RowIndex]，RowIndex++
//   - RowIndex 超过上限：返回 false
type EssenceFilterAfterBattleNthRecognition struct{}

var _ maa.CustomRecognitionRunner = &EssenceFilterAfterBattleNthRecognition{}

func (r *EssenceFilterAfterBattleNthRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	st := getRunState()
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

	// 缓存不为空：返回当前结果，RowIndex++
	if st.RowIndex < len(st.RowBoxes) {
		box := st.RowBoxes[st.RowIndex]
		st.RowIndex++
		return &maa.CustomRecognitionResult{
			Box:    maa.Rect{box[0], box[1], box[2], box[3]},
			Detail: "",
		}, true
	}

	// 缓存为空：识别一次，覆盖填充 Filtered 结果
	detail, err := ctx.RunRecognition(params.RecognitionNodeName, arg.Img, nil)
	if err != nil || detail == nil || !detail.Hit || detail.Results == nil || detail.Results.Filtered == nil {
		log.Info().Str("component", "EssenceFilter").Str("recognition", "AfterBattleNthEssence").Str("node", params.RecognitionNodeName).Err(err).Msg("scan failed")
		return nil, false
	}

	st.RowBoxes = nil
	for _, res := range detail.Results.Filtered {
		tm, ok := res.AsTemplateMatch()
		if !ok {
			continue
		}
		b := tm.Box
		st.RowBoxes = append(st.RowBoxes, [4]int{b.X(), b.Y(), b.Width(), b.Height()})
	}

	// RowIndex 超过上限：返回 false
	if st.RowIndex >= len(st.RowBoxes) {
		log.Info().Str("component", "EssenceFilter").Str("recognition", "AfterBattleNthEssence").Int("row_index", st.RowIndex).Int("boxes", len(st.RowBoxes)).Msg("out of range")
		return nil, false
	}

	// 返回第 0 个，RowIndex++
	box := st.RowBoxes[st.RowIndex]
	st.RowIndex++
	return &maa.CustomRecognitionResult{
		Box:    maa.Rect{box[0], box[1], box[2], box[3]},
		Detail: "",
	}, true
}
