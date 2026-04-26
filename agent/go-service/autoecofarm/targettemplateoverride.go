package autoecofarm

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type autoEcoFarmOverrideTargetTemplateParam struct {
	// Template 是目标模板图路径（相对 resource/image）。
	// 例如：AutoEcoFarm/AutoEcoFarmFarmlandWithBack.png
	// 该值会被覆写到每个目标节点的 recognition.param.template。
	Template string `json:"template"`
	// NodeNames 是要被覆写模板的节点名列表。
	// 注意：这里要求显式传入，若为空（或只包含空白字符串）会直接失败返回。
	// 这样可以避免“误覆写默认节点”带来的不可预期行为。
	NodeNames []string `json:"nodeNames"`
}

type autoEcoFarmOverrideTargetTemplate struct{}

func normalizeTargetTemplateNodeNames(nodeNames []string) []string {
	// 清洗用户传入的节点名列表：
	// 1) 去掉每个节点名前后的空白字符；
	// 2) 过滤空字符串；
	// 3) 保留原有顺序，便于日志和排查时对照用户配置。
	normalized := make([]string, 0, len(nodeNames))
	for _, nodeName := range nodeNames {
		trimmed := strings.TrimSpace(nodeName)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func buildTemplateOverrideForNode(template string) map[string]any {
	// 构造“单个节点”的覆写片段。
	// 这里只改 recognition.param.template，不触碰其他字段（例如 roi/threshold/next）。
	// 这样能把影响范围压到最小，避免误改节点其他行为。
	return map[string]any{
		"recognition": map[string]any{
			"param": map[string]any{
				"template": []string{template},
			},
		},
	}
}

// overrideTargetTemplatePath 根据传入模板和节点列表构造 override，并一次性应用到 pipeline。
//
// 设计意图：
// - 把“参数校验/归一化”和“override 构造”分层，便于后续维护；
// - 仅覆写 template 字段，保证是最小变更；
// - 调用方可通过 nodeNames 精确控制影响的节点集合。
func overrideTargetTemplatePath(ctx *maa.Context, template string, nodeNames []string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	if template == "" {
		return fmt.Errorf("template is empty")
	}

	normalizedNodeNames := normalizeTargetTemplateNodeNames(nodeNames)
	if len(normalizedNodeNames) == 0 {
		return fmt.Errorf("node names are empty")
	}

	// 按传入节点逐个构造 override，最后一次性调用 OverridePipeline，
	// 避免多次调用导致的中间态问题，也更容易在日志中定位一次动作的整体结果。
	override := make(map[string]any, len(normalizedNodeNames))
	for _, nodeName := range normalizedNodeNames {
		override[nodeName] = buildTemplateOverrideForNode(template)
	}

	return ctx.OverridePipeline(override)
}

// Run 是 custom action 入口：
// 1) 解析 custom_action_param；
// 2) 归一化 template 与 nodeNames；
// 3) 校验关键参数；
// 4) 调用 overrideTargetTemplatePath 执行覆写；
// 5) 输出日志，便于定位配置问题与运行结果。
func (a *autoEcoFarmOverrideTargetTemplate) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", "AutoEcoFarm").
			Msg("override target template: nil arg")
		return false
	}

	var params autoEcoFarmOverrideTargetTemplateParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoEcoFarm").
			Str("param", arg.CustomActionParam).
			Msg("override target template: parse param failed")
		return false
	}

	template := strings.TrimSpace(params.Template)
	nodeNames := normalizeTargetTemplateNodeNames(params.NodeNames)
	// 这里强制要求显式传 nodeNames，避免误操作覆写到非预期节点。
	// 只要 nodeNames 为空，本 action 就视为参数不合法并返回 false。
	if len(nodeNames) == 0 {
		log.Error().
			Str("component", "AutoEcoFarm").
			Str("template", template).
			Msg("override target template: nodeNames is empty")
		return false
	}
	if err := overrideTargetTemplatePath(ctx, template, nodeNames); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoEcoFarm").
			Str("template", template).
			Msg("override target template: apply pipeline override failed")
		return false
	}

	log.Debug().
		Str("component", "AutoEcoFarm").
		Str("template", template).
		Interface("node_names", nodeNames).
		Msg("override target template: pipeline override applied")
	return true
}
