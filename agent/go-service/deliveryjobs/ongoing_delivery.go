package deliveryjobs

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autodelivery"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	resolveOngoingDepotActionName = "DeliveryJobsResolveOngoingDepotAction"
	resolveOngoingDepotNode       = "DeliveryJobsResolveOngoingDepot"
	// 节点名后缀就是 MaaEnd 侧的仓储节点 ID：送货任务详情里的区域名与仓储节点名一致，
	// 由 tools/pipeline-generate/DeliveryJobs/model.mjs 在生成时断言，两边不会各写一套规则。
	ongoingDeliveryNodePrefix = "DeliveryJobsOngoingDeliveryFor"
)

// DeliveryJobsResolveOngoingDepotAction 从送货任务详情解析残留任务所属的仓储节点，
// 再把后续交给该仓储节点自己的残留任务处理方式。
type DeliveryJobsResolveOngoingDepotAction struct{}

var _ maa.CustomActionRunner = &DeliveryJobsResolveOngoingDepotAction{}

// Run 用区域 OCR 匹配仓储节点，并覆盖当前节点的 next 指向该节点的分派节点。
func (a *DeliveryJobsResolveOngoingDepotAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) (resolved bool) {
	// 解析失败会让后续流程走偏，需要让用户看到原因。
	defer func() {
		if !resolved {
			maafocus.Print(ctx, i18n.T("deliveryjobs.focus.ongoing_depot_unresolved"))
		}
	}()

	if ctx == nil || arg == nil || arg.RecognitionDetail == nil {
		log.Error().
			Str("component", resolveOngoingDepotActionName).
			Msg("action context or recognition detail is missing")
		return false
	}

	areaText, err := autodelivery.AreaTextFromRecognition(arg.RecognitionDetail)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", resolveOngoingDepotActionName).
			Msg("failed to read delivery area OCR")
		return false
	}

	resolution, err := autodelivery.ResolveAreaFromText(areaText)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", resolveOngoingDepotActionName).
			Str("areaText", areaText).
			Float64("similarity", resolution.Similarity).
			Float64("runnerUpSimilarity", resolution.RunnerUpSimilarity).
			Msg("failed to resolve the ongoing delivery job depot from area OCR")
		return false
	}
	if resolution.DepotID == "" {
		log.Error().
			Str("component", resolveOngoingDepotActionName).
			Str("area", resolution.ID).
			Str("areaText", areaText).
			Msg("delivery area has no associated depot")
		return false
	}

	nextNode := ongoingDeliveryNodePrefix + resolution.ID
	if err := ctx.OverridePipeline(map[string]any{
		resolveOngoingDepotNode: map[string]any{
			"next": []string{nextNode},
		},
	}); err != nil {
		log.Error().
			Err(err).
			Str("component", resolveOngoingDepotActionName).
			Str("node", nextNode).
			Msg("failed to dispatch the ongoing delivery job to its depot")
		return false
	}

	// 区域名与仓储节点名在五种语言下逐字一致（见 model.mjs 的生成期断言），
	// 所以直接用仓储节点 ID 取它自己的本地化名。
	maafocus.Print(ctx, i18n.T(
		"deliveryjobs.focus.ongoing_depot_resolved",
		i18n.T("global.region."+resolution.ID),
	))

	log.Info().
		Str("component", resolveOngoingDepotActionName).
		Str("depot", resolution.DepotID).
		Str("area", resolution.ID).
		Str("areaText", areaText).
		Str("next", nextNode).
		Msg("dispatched the ongoing delivery job to its own depot handling")
	return true
}
