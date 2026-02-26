package resell

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ResellCheckQuotaAction 执行配额 OCR，计算溢出量，跳转到扫描第一个商品
type ResellCheckQuotaAction struct{}

func (a *ResellCheckQuotaAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[Resell]无法获取控制器")
		return false
	}

	overflowAmount := 0
	log.Info().Msg("[Resell]检查配额溢出状态…")
	ResellDelayFreezesTime(ctx, 500)
	MoveMouseSafe(controller)
	controller.PostScreencap().Wait()

	x, y, _, b := ocrAndParseQuota(ctx, controller)
	if x >= 0 && y > 0 && b >= 0 {
		overflowAmount = x + b - y
	} else {
		log.Info().Msg("[Resell]未能解析配额或未找到，按正常流程继续")
	}

	setOverflow(overflowAmount)
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
