package essencefilter

import (
	"encoding/json"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type essenceAfterBattleNthParams struct {
	// 要委托调用的 pipeline 识别节点名（例如 "EssenceFullScreenDetectAll"）。
	RecognitionNodeName string `json:"recognitionNodeName"`
}

// EssenceFilterAfterBattleNthRecognition
//   - 自定义识别（CustomRecognition）：内部通过 ctx.RunRecognition 委托调用现有的 pipeline 识别节点，
//     但识别输入图像使用 Maa 框架准备好的 arg.Img（避免在 Action 里再次截屏）。
//   - 通过传入 {"index": st.RowIndex} 来取“第 N 个命中”，命中后会执行 st.RowIndex++（下次取下一个）。
//   - 如果 st.RowIndex 超出结果数量上限（表现为不命中 / 无结果 / 报错），返回 (nil, false)。
type EssenceFilterAfterBattleNthRecognition struct{}

var _ maa.CustomRecognitionRunner = &EssenceFilterAfterBattleNthRecognition{}

func (r *EssenceFilterAfterBattleNthRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// 下面这段是获取全局运行状态的代码，用来拿到本次筛选任务的状态（包括当前要取第几个命中 st.RowIndex）。
	st := getRunState()
	if st == nil {
		return nil, false
	}

	// 下面这段是入参合法性检查，用来确保 Maa 已经准备好了识别用的截图（arg.Img）。
	// 如果没有图像，就无法委托调用 pipeline 识别节点，直接返回 false 交给 pipeline 的 on_error 处理。
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", "EssenceFilter").Str("recognition", "AfterBattleNthEssence").Msg("arg.Img nil")
		return nil, false
	}

	// 下面这段是初始化参数默认值，用来让 pipeline 不传参时也能工作（默认使用全屏识别节点 EssenceFullScreenDetectAll）。
	params := essenceAfterBattleNthParams{
		RecognitionNodeName: "EssenceFullScreenDetectAll",
	}

	// 下面这段是解析 JSON 参数的代码，用来允许你在 pipeline 里配置要委托的识别节点名（recognitionNodeName）。
	if strings.TrimSpace(arg.CustomRecognitionParam) != "" {
		if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
			log.Error().Err(err).Str("component", "EssenceFilter").Str("recognition", "AfterBattleNthEssence").Msg("CustomRecognitionParam parse failed")
			return nil, false
		}
	}

	// 下面这段是参数校验代码，用来避免识别节点名为空导致 RunRecognition 调用失败。
	if strings.TrimSpace(params.RecognitionNodeName) == "" {
		return nil, false
	}

	// 委托调用 pipeline 节点识别，但强制覆盖 index = st.RowIndex，用于取第 N 个命中。
	// 下面这段是核心委托识别逻辑，用来复用 pipeline 里已经写好的识别节点（模板/ROI/阈值等都在 JSON 里维护），
	// 同时使用本 CustomRecognition 的 arg.Img 作为输入图像，并通过 index 指定取第几个结果。
	detail, err := ctx.RunRecognition(
		params.RecognitionNodeName,
		arg.Img,
		map[string]any{
			params.RecognitionNodeName: map[string]any{
				"index": st.RowIndex,
			},
		},
	)

	// 下面这段是错误/越界处理代码：
	// - err/detail nil：识别调用失败
	// - !detail.Hit：没有命中（通常意味着 index 超过结果数量）
	// - detail.Results nil：识别结果为空
	// 返回 false 的意义是：让 pipeline 认为本次识别失败，从而走 on_error（例如结束流程）。
	if err != nil || detail == nil || !detail.Hit || detail.Results == nil {
		log.Info().Str("component", "EssenceFilter").Str("recognition", "AfterBattleNthEssence").Str("node", params.RecognitionNodeName).Int("index", st.RowIndex).Err(err).Msg("no hit (maybe out of range)")
		return nil, false
	}

	// 优先取 Best（指针）；若为空则退化取 Filtered/All 的第一个结果。
	// 下面这段是“从识别结果中挑出一个 box”的代码：
	// 在 index 模式下，通常 Best 就是第 N 个命中；如果 Best 为空，则退化使用 Filtered/All 的第一个结果。
	var picked *maa.RecognitionResult
	if detail.Results.Best != nil {
		picked = detail.Results.Best
	} else if len(detail.Results.Filtered) > 0 {
		picked = detail.Results.Filtered[0]
	} else if len(detail.Results.All) > 0 {
		picked = detail.Results.All[0]
	}
	if picked == nil {
		return nil, false
	}

	// 下面这段是类型转换与安全检查代码：
	// 我们期望委托的识别节点返回的是 TemplateMatch 结果，这样才能拿到矩形框（x,y,w,h）作为 Click 的目标。
	tm, ok := picked.AsTemplateMatch()
	if !ok {
		return nil, false
	}

	// 下面这段是更新“当前取第几个命中”的全局计数，并把本次命中的矩形框返回给 pipeline。
	// pipeline 的 Click 会使用这里返回的 Box 作为点击目标；Detail 目前不需要，留空即可。
	b := tm.Box
	st.RowIndex++
	return &maa.CustomRecognitionResult{
		Box:    maa.Rect{b.X(), b.Y(), b.Width(), b.Height()},
		Detail: "",
	}, true
}
