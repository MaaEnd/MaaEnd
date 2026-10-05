package essencefilter

import (
	"encoding/json"
	"sort"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type essenceAfterBattleNthParams struct {
	RecognitionNodeName string `json:"recognitionNodeName"`
}

// EssenceFilterAfterBattleNthRecognition 在战斗结算后按行序依次返回全屏识别结果中的第 N 个框。
// 识别节点只提供目标区域。点击顺序由 sortBoxesByRow 决定：先上后下，同一行从左到右。
// 框缓存在 RowBoxes 中，通过递增 RowIndex 逐个吐出；缓存用尽后重新识别并刷新。
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

	if st.RowIndex < len(st.RowBoxes) {
		box := st.RowBoxes[st.RowIndex]
		st.RowIndex++
		return &maa.CustomRecognitionResult{
			Box:    maa.Rect{box[0], box[1], box[2], box[3]},
			Detail: "",
		}, true
	}

	detail, err := ctx.RunRecognition(params.RecognitionNodeName, arg.Img, nil)
	if err != nil || detail == nil || !detail.Hit || detail.Results == nil || detail.Results.Filtered == nil {
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
	sortBoxesByRow(st.RowBoxes)

	if st.RowIndex >= len(st.RowBoxes) {
		return nil, false
	}

	box := st.RowBoxes[st.RowIndex]
	st.RowIndex++
	return &maa.CustomRecognitionResult{
		Box:    maa.Rect{box[0], box[1], box[2], box[3]},
		Detail: "",
	}, true
}

// sortBoxesByRow 按行排列目标框：先上后下，同一行从左到右。
// 与当前行首框的纵向差不超过该框高度一半时，仍视为同一行。
// 这样同一列上下两格的 x 相差 1 像素时，下排不会排到上排前面。
func sortBoxesByRow(boxes [][4]int) {
	if len(boxes) < 2 {
		return
	}
	sort.SliceStable(boxes, func(i, j int) bool {
		if boxes[i][1] != boxes[j][1] {
			return boxes[i][1] < boxes[j][1]
		}
		return boxes[i][0] < boxes[j][0]
	})

	rows := make([][][4]int, 0, 2)
	row := [][4]int{boxes[0]}
	anchorY, anchorH := boxes[0][1], boxes[0][3]
	for _, box := range boxes[1:] {
		if box[1]-anchorY > rowBand(anchorH) {
			rows = append(rows, row)
			row = [][4]int{box}
			anchorY, anchorH = box[1], box[3]
			continue
		}
		row = append(row, box)
	}
	rows = append(rows, row)

	ordered := make([][4]int, 0, len(boxes))
	for _, row := range rows {
		sort.SliceStable(row, func(i, j int) bool {
			return row[i][0] < row[j][0]
		})
		ordered = append(ordered, row...)
	}
	copy(boxes, ordered)
}

func rowBand(height int) int {
	band := height / 2
	if band < 1 {
		return 1
	}
	return band
}
