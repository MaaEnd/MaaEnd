package trialofswordmancy

import (
	"encoding/json"
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	component = "trialofswordmancy"

	recognizeBoardName         = "TrialOfSwordmancy.RecognizeBoard"
	recognizeFullDeckName      = "TrialOfSwordmancy.RecognizeFullDeck"
	recognizeRemainAbandonName = "TrialOfSwordmancy.RecognizeRemainAbandon"
	decideName                 = "TrialOfSwordmancy.Decide"
	initSolverName             = "TrialOfSwordmancy.InitSolver"
)

const (
	nodeDrawCard        = "TrialOfSwordmancyDrawCard"
	nodeDoDoubleReward  = "TrialOfSwordmancyDoDoubleReward"
	nodeDrawCardSuccess = "TrialOfSwordmancyDrawCardSuccess"

	nodeAbandon       = "TrialOfSwordmancyDailyAbandon"
	nodePreStartTrial = "TrialOfSwordmancyPreStartTrial"

	nodeFinishOnOverflow = "TrialOfSwordmancyFinishOnOverflow"

	// 以下前缀 + 序号组成完整节点名（EnemyCard1-5 / BattlePts0-10）。
	nodeEnemyCardPrefix = "TrialOfSwordmancyEnemyCard"
	nodeBattlePtsPrefix = "TrialOfSwordmancyBattlePts"
)

const (
	nodeRemainCalcText        = "TrialOfSwordmancyRemainCalcText"
	nodeRemainDoubleText      = "TrialOfSwordmancyRemainDoubleText"
	nodeAbandonPopupText      = "TrialOfSwordmancyAbandonPopupText"
	nodeAbandonPopupExhausted = "TrialOfSwordmancyAbandonPopupExhausted"
	nodeIsDoubled             = "TrialOfSwordmancyIsDoubled"

	nodeDeck               = "TrialOfSwordmancyDeck"
	nodeDeckCountPrefix    = "TrialOfSwordmancyDeckCount"
	nodeHandPointPrefix    = "TrialOfSwordmancyHandPoint"
	nodeHandPositionPrefix = "TrialOfSwordmancyHandPosition"
)

// nodeROI 读取节点（JSON 中定义）的识别 ROI，供 Go 按名调用识别后筛选结果。
func nodeROI(ctx *maa.Context, nodeName string) (maa.Rect, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return maa.Rect{}, err
	}
	var node struct {
		Recognition struct {
			Param struct {
				ROI maa.Rect `json:"roi"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return maa.Rect{}, err
	}
	return node.Recognition.Param.ROI, nil
}

// overrideDrawCardNodes 按当前手牌覆盖抽牌节点在静态 JSON 里会失效的两个等待锚点：
// 落位槽随抽牌递增，战力点随手牌变化。override 为字段级浅合并，custom_action_param 需全量给出。
func overrideDrawCardNodes(ctx *maa.Context, hand [5]int) error {
	slot := 1
	for _, c := range hand {
		slot += c
	}
	if slot > 5 {
		slot = 5
	}
	enemyCard := nodeEnemyCardPrefix + strconv.Itoa(slot)

	power := solver.PowerOf(hand)
	waitNode := nodeBattlePtsPrefix + strconv.Itoa(power)

	if err := ctx.OverridePipeline(map[string]any{
		nodeDrawCard: map[string]any{
			"custom_action_param": map[string]any{
				"action":       "Click",
				"interval_ms":  200,
				"repeat_count": 20,
				"wait_node":    waitNode,
			},
		},
		nodeDrawCardSuccess: map[string]any{
			"all_of": []string{enemyCard},
		},
	}); err != nil {
		return err
	}

	log.Debug().
		Str("component", component).
		Int("slot", slot).
		Str("allOf", enemyCard).
		Int("power", power).
		Str("waitNode", waitNode).
		Msg("draw card nodes overridden")
	return nil
}
