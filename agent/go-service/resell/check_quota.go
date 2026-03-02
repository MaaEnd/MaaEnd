package resell

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// extractRecoDetailJson 从 RecognitionDetail 提取 custom reco 的 Detail JSON（兼容直接或 best.detail 包裹格式）
func extractRecoDetailJson(rd *maa.RecognitionDetail) string {
	if rd == nil || rd.DetailJson == "" {
		return ""
	}
	// 尝试直接解析
	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(rd.DetailJson), &wrapped); err == nil && len(wrapped.Best.Detail) > 0 {
		return string(wrapped.Best.Detail)
	}
	return rd.DetailJson
}

// ResellCheckQuotaAction 根据 custom reco 的识别结果计算溢出量，跳转到扫描第一个商品
type ResellCheckQuotaAction struct{}

func (a *ResellCheckQuotaAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	overflowAmount := 0
	detailJSON := extractRecoDetailJson(arg.RecognitionDetail)
	if detailJSON != "" {
		var reco quotaRecoResult
		if err := json.Unmarshal([]byte(detailJSON), &reco); err != nil {
			log.Warn().Err(err).Msg("[Resell]解析识别结果失败")
		} else if reco.X >= 0 && reco.Y > 0 && reco.B >= 0 {
			overflowAmount = reco.X + reco.B - reco.Y
			log.Info().Int("overflow", overflowAmount).Msg("[Resell]配额溢出量已计算")
		}
	}

	setOverflow(overflowAmount)
	//每次识别配额的时候代表在新一地区的商店，重置当前扫描位置
	_ = ctx.OverridePipeline(map[string]any{
		"ResellScan": map[string]any{
			"custom_action_param": map[string]any{
				"row": 1,
				"col": 1,
			},
		},
	})
	return true
}
