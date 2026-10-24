package protocolspace

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type consumableQuantityRecognitionParams struct {
	QuantityNode        string   `json:"quantity_node"`
	OperatorNodes       []string `json:"operator_nodes"`
	MatchWhenSufficient bool     `json:"match_when_sufficient"`
}

// ConsumableQuantityRecognition compares the live stack OCR with the enabled
// operator-slot nodes selected by the task options.
type ConsumableQuantityRecognition struct{}

var _ maa.CustomRecognitionRunner = &ConsumableQuantityRecognition{}

func (r *ConsumableQuantityRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "ProtocolSpaceConsumableQuantityRecognition").Msg("received nil context or arg")
		return nil, false
	}

	var params consumableQuantityRecognitionParams
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "ProtocolSpaceConsumableQuantityRecognition").Msg("failed to parse params")
		return nil, false
	}
	params.QuantityNode = strings.TrimSpace(params.QuantityNode)
	if params.QuantityNode == "" || len(params.OperatorNodes) != 4 {
		log.Error().Str("component", "ProtocolSpaceConsumableQuantityRecognition").Msg("quantity_node and four operator_nodes are required")
		return insufficientResult(arg, params.MatchWhenSufficient)
	}

	selectedCount, err := countEnabledOperatorNodes(ctx, params.OperatorNodes)
	if err != nil {
		log.Error().Err(err).Str("component", "ProtocolSpaceConsumableQuantityRecognition").Msg("failed to read selected operator slots")
		return insufficientResult(arg, params.MatchWhenSufficient)
	}
	if selectedCount == 0 {
		return insufficientResult(arg, params.MatchWhenSufficient)
	}

	quantity, quantityErr := readQuantity(ctx, arg, params.QuantityNode)
	if quantityErr != nil {
		// Missing or unreadable count must take the safe skip path.
		log.Info().Err(quantityErr).Int("selected_operators", selectedCount).Msg("consumable quantity unavailable; skip use")
		return insufficientResult(arg, params.MatchWhenSufficient)
	}

	sufficient := quantity >= selectedCount
	log.Info().
		Str("component", "ProtocolSpaceConsumableQuantityRecognition").
		Int("quantity", quantity).
		Int("selected_operators", selectedCount).
		Bool("sufficient", sufficient).
		Msg("compared consumable quantity with selected operators")
	if sufficient != params.MatchWhenSufficient {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

func insufficientResult(arg *maa.CustomRecognitionArg, matchWhenSufficient bool) (*maa.CustomRecognitionResult, bool) {
	if matchWhenSufficient {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

func countEnabledOperatorNodes(ctx *maa.Context, nodes []string) (int, error) {
	count := 0
	for _, name := range nodes {
		name = strings.TrimSpace(name)
		if name == "" {
			return 0, fmt.Errorf("operator node name is empty")
		}
		raw, err := ctx.GetNodeJSON(name)
		if err != nil {
			return 0, fmt.Errorf("get operator node %s: %w", name, err)
		}
		var node struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.Unmarshal([]byte(raw), &node); err != nil {
			return 0, fmt.Errorf("parse operator node %s: %w", name, err)
		}
		if node.Enabled != nil && *node.Enabled {
			count++
		}
	}
	return count, nil
}

func readQuantity(ctx *maa.Context, arg *maa.CustomRecognitionArg, nodeName string) (int, error) {
	detail, err := ctx.RunRecognition(nodeName, arg.Img)
	if err != nil {
		return 0, fmt.Errorf("run quantity OCR node %s: %w", nodeName, err)
	}
	if detail == nil || !detail.Hit {
		return 0, fmt.Errorf("quantity OCR node %s did not match", nodeName)
	}
	quantity, err := ocrnum.Extract(detail)
	if err != nil {
		return 0, fmt.Errorf("parse quantity OCR node %s: %w", nodeName, err)
	}
	return quantity, nil
}
