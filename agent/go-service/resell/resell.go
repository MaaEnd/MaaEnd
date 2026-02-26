package resell

import (
	"encoding/json"
	"strconv"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ProfitRecord 存储每个商品的利润信息
type ProfitRecord struct {
	Row       int
	Col       int
	CostPrice int
	SalePrice int
	Profit    int
}

// ResellInitAction 解析参数、清空状态，跳转到配额检查
type ResellInitAction struct{}

func (a *ResellInitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Msg("[Resell]开始倒卖流程")
	var params struct {
		MinimumProfit interface{} `json:"MinimumProfit"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("[Resell]反序列化失败")
		return false
	}

	var MinimumProfit int
	switch v := params.MinimumProfit.(type) {
	case float64:
		MinimumProfit = int(v)
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			log.Error().Err(err).Msgf("Failed to parse MinimumProfit string: %s", v)
			return false
		}
		MinimumProfit = parsed
	default:
		log.Error().Msgf("Invalid MinimumProfit type: %T", v)
		return false
	}

	setMinProfit(MinimumProfit)
	clearRecords()
	log.Info().Int("MinimumProfit", MinimumProfit).Msg("[Resell]参数已解析")

	ctx.OverrideNext(arg.CurrentTaskName, []maa.NodeNextItem{{Name: "ResellCheckQuota"}})
	return true
}
