package protocolspace

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const pathOperatorGrowthDetailData = "data/ProtocolSpace/operatorgrowth_detail_data.json"

type CalculateOperatorMaterialsAction struct{}
type SumOperatorMaterialsAction struct{}
type OverrideOperatorSupplyPlanAction struct{}

var (
	_ maa.CustomActionRunner = &CalculateOperatorMaterialsAction{}
	_ maa.CustomActionRunner = &SumOperatorMaterialsAction{}
	_ maa.CustomActionRunner = &OverrideOperatorSupplyPlanAction{}
)

// CalculateOperatorMaterialsAction 汇总干员当前培养进度对应的剩余材料，并写入当前节点的 attach。
type operatorMaterialsParam struct {
	Operator           string `json:"__ProtocolSpaceGrowthOperator"`           //干员ID
	OperatorLevel      int    `json:"__ProtocolSpaceGrowthOperatorLevel"`      //干员等级
	BreakNode          int    `json:"__ProtocolSpaceGrowthBreaknode"`          //突破阶段
	NormalAttackLevel  int    `json:"__ProtocolSpaceGrowthNormalAttackLevel"`  //普通攻击
	NormalSkillLevel   int    `json:"__ProtocolSpaceGrowthNormalSkillLevel"`   //战技
	ComboSkillLevel    int    `json:"__ProtocolSpaceGrowthComboSkillLevel"`    //连携技
	UltimateSkillLevel int    `json:"__ProtocolSpaceGrowthUltimateSkillLevel"` //终结技
	TalentLevel        int    `json:"__ProtocolSpaceGrowthTalentLevel"`        //天赋
}

type operatorGrowthDetailData struct {
	LevelUp   map[int]map[string]int             `json:"level_up"`
	Operators map[string]operatorGrowthMaterials `json:"operators"`
}

type operatorGrowthMaterials struct {
	BreakNodes  map[int]map[string]int            `json:"break_nodes"`
	Skills      map[string]map[int]map[string]int `json:"skills"`
	TalentNodes map[int]map[string]int            `json:"talent_nodes"`
}

// 明细已按等级累计到满级，每类只取当前索引，避免再次累加后续等级。
func calculateSingleOperatorMaterials(params operatorMaterialsParam, data operatorGrowthDetailData) (map[string]int, error) {
	operator, ok := data.Operators[params.Operator]
	if !ok {
		return nil, fmt.Errorf("operator %q is missing from growth data", params.Operator)
	}
	selections := []struct {
		name  string
		level int
		costs map[int]map[string]int
	}{
		{"level_up", params.OperatorLevel, data.LevelUp},
		{"break_nodes", params.BreakNode, operator.BreakNodes},
		{"NormalAttack", params.NormalAttackLevel, operator.Skills["NormalAttack"]},
		{"NormalSkill", params.NormalSkillLevel, operator.Skills["NormalSkill"]},
		{"ComboSkill", params.ComboSkillLevel, operator.Skills["ComboSkill"]},
		{"UltimateSkill", params.UltimateSkillLevel, operator.Skills["UltimateSkill"]},
		{"talent_nodes", params.TalentLevel, operator.TalentNodes},
	}
	materialList := make(map[string]int)
	for _, selection := range selections {
		costs, ok := selection.costs[selection.level]
		if !ok || costs == nil {
			return nil, fmt.Errorf("operator %q: missing %s[%d]", params.Operator, selection.name, selection.level)
		}
		for id, count := range costs {
			materialList[id] += count
		}
	}
	return materialList, nil
}

// 根据等级参数，按传入进度汇总材料后写入当前节点的 attach。
func (a *CalculateOperatorMaterialsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	const component = "ProtocolSpaceCalculateOperatorMaterialsAction"
	if ctx == nil || arg == nil {
		log.Error().Str("component", component).Msg("missing custom action context or arguments")
		return false
	}

	var params operatorMaterialsParam
	//合法性校验
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse operator material parameters")
		return false
	}
	var data operatorGrowthDetailData
	if err := resource.ReadJsonResource(pathOperatorGrowthDetailData, &data); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to read operator growth data")
		return false
	}
	//材料计算
	materialList, err := calculateSingleOperatorMaterials(params, data)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("operator", params.Operator).Msg("failed to calculate operator materials")
		return false
	}

	//写入本节点attach导出
	if err := writeOperatorMaterials(ctx, arg.CurrentTaskName, materialList); err != nil {
		log.Error().Err(err).Str("component", component).Str("node", arg.CurrentTaskName).Msg("failed to export operator material result")
		return false
	}

	return true
}

// SumOperatorMaterialsAction 汇总启用角色节点的材料，并写入当前节点的 attach。

type sumOperatorMaterialsParam struct {
	Nodes []string `json:"nodes"`
}

type operatorMaterialNode struct {
	Enabled *bool          `json:"enabled"`
	Attach  map[string]int `json:"attach"`
}

// sumOperatorMaterials 每次重新汇总，跳过未启用角色，避免计入其上次运行的材料。
func sumOperatorMaterials(nodes []string, getNodeJSON func(string) (string, error)) (map[string]int, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("operator nodes are required")
	}
	total := make(map[string]int)
	for _, name := range nodes {
		raw, err := getNodeJSON(name)
		if err != nil {
			return nil, fmt.Errorf("read operator node %q: %w", name, err)
		}
		var node operatorMaterialNode
		if err := json.Unmarshal([]byte(raw), &node); err != nil {
			return nil, fmt.Errorf("parse operator node %q: %w", name, err)
		}
		if node.Enabled != nil && !*node.Enabled {
			continue
		}
		if node.Attach == nil {
			continue
		}
		for id, count := range node.Attach {
			total[id] += count
		}
	}
	return total, nil
}

// writeOperatorMaterials 将旧材料键归零后写入新结果，避免 attach 字典合并保留旧数量。
func writeOperatorMaterials(ctx *maa.Context, nodeName string, materials map[string]int) error {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return err
	}
	var node operatorMaterialNode
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return err
	}
	for id := range node.Attach {
		if _, ok := materials[id]; !ok {
			materials[id] = 0
		}
	}
	return ctx.OverridePipeline(map[string]any{
		nodeName: map[string]any{"attach": materials},
	})
}

// Run 读取角色节点的计算结果，将汇总材料写入当前节点的 attach。
func (a *SumOperatorMaterialsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	const component = "ProtocolSpaceSumOperatorMaterialsAction"
	if ctx == nil || arg == nil {
		log.Error().Str("component", component).Msg("missing custom action context or arguments")
		return false
	}
	var params sumOperatorMaterialsParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse operator summary parameters")
		return false
	}
	materials, err := sumOperatorMaterials(params.Nodes, ctx.GetNodeJSON)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to sum operator materials")
		return false
	}
	if err := writeOperatorMaterials(ctx, arg.CurrentTaskName, materials); err != nil {
		log.Error().Err(err).Str("component", component).Str("node", arg.CurrentTaskName).Msg("failed to export operator material summary")
		return false
	}
	return true
}

// OverrideOperatorSupplyPlanAction 将培养材料总量写入库存模式的调度与领奖达标表达式。

type overrideOperatorSupplyPlanParam struct {
	ReplaceMapping map[string]string `json:"replace_mapping"` // 原样匹配的文本 -> 汇总材料 ID
	Nodes          []string          `json:"nodes"`           // 需要覆写表达式的节点
	DataNode       string            `json:"datanode"`        // 数据源
}

// buildOperatorSupplyPlanPatch 按映射键原样匹配表达式文本，替换为汇总材料数量。
func buildOperatorSupplyPlanPatch(materials map[string]int, params overrideOperatorSupplyPlanParam, getNodeJSON func(string) (string, error)) (map[string]any, error) {
	if len(params.ReplaceMapping) == 0 {
		return nil, fmt.Errorf("replace_mapping is required")
	}
	if len(params.Nodes) == 0 {
		return nil, fmt.Errorf("nodes are required")
	}
	//根据replace_mapping创建文本替换配置
	replacements := make([]string, 0, len(params.ReplaceMapping)*2)
	for _, key := range slices.Sorted(maps.Keys(params.ReplaceMapping)) {
		material := params.ReplaceMapping[key]
		if key == "" || strings.TrimSpace(material) == "" {
			return nil, fmt.Errorf("replace_mapping contains an invalid key or material ID")
		}
		// 汇总中不存在的材料需求为零；匹配文本及其分隔符均由配置指定。
		replacements = append(replacements, key, strconv.Itoa(materials[material]))
	}
	replacer := strings.NewReplacer(replacements...)
	patch := make(map[string]any, len(params.Nodes))
	//读取所有节点CustomRecognitionParam，并将表达式中对应的占位符替换为对应的数值
	for _, name := range params.Nodes {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("nodes contains an empty node name")
		}
		raw, err := getNodeJSON(name)
		if err != nil {
			return nil, fmt.Errorf("read supply node %q: %w", name, err)
		}
		var node struct {
			Recognition struct {
				Param struct {
					CustomRecognitionParam struct {
						Expression string `json:"expression"`
					} `json:"custom_recognition_param"`
				} `json:"param"`
			} `json:"recognition"`
		}
		if err := json.Unmarshal([]byte(raw), &node); err != nil {
			return nil, fmt.Errorf("parse supply node %q: %w", name, err)
		}
		expression := node.Recognition.Param.CustomRecognitionParam.Expression
		patch[name] = map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"custom_recognition_param": map[string]any{"expression": replacer.Replace(expression)},
				},
			},
		}
	}
	return patch, nil
}

// Run 读取汇总节点的 attach，在当前任务内覆写库存目标，由 Pipeline 进入库存流程。
func (a *OverrideOperatorSupplyPlanAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	const component = "ProtocolSpaceOverrideOperatorSupplyPlanAction"
	if ctx == nil || arg == nil {
		log.Error().Str("component", component).Msg("missing custom action context or arguments")
		return false
	}
	//入参解析
	var params overrideOperatorSupplyPlanParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse supply plan replacement parameters")
		return false
	}

	summaryDataNodeRaw, err := ctx.GetNodeJSON(params.DataNode)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to read operator material summary")
		return false
	}
	var summaryDataNode operatorMaterialNode
	if err := json.Unmarshal([]byte(summaryDataNodeRaw), &summaryDataNode); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse operator material summary")
		return false
	}
	//替换expression的占位符
	patch, err := buildOperatorSupplyPlanPatch(summaryDataNode.Attach, params, ctx.GetNodeJSON)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to build supply plan targets")
		return false
	}
	if err := ctx.OverridePipeline(patch); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to override supply plan targets")
		return false
	}
	return true
}
