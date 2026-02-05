package essence

import (
	"encoding/json"
	"image"
	"regexp"
	"strings"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type tooltipJudgeParam struct {
	S1Node string `json:"s1_node"`
	S2Node string `json:"s2_node"`
	S3Node string `json:"s3_node"`
	OnlyDecision string `json:"only_decision"`
	PreferredWeapons string `json:"preferred_weapons"`
	MatchMode string `json:"match_mode"`
	PreferredWeaponFlags map[string]string `json:"preferred_weapon_flags"`
}

var attrCleanupRe = regexp.MustCompile(`[0-9+\-%.]`)

// EssenceTooltipJudgeRecognition 从 Tooltip OCR 读取三条属性并判定。
// 依赖 pipeline 节点：EssenceTooltip_S1 / EssenceTooltip_S2 / EssenceTooltip_S3。
type EssenceTooltipJudgeRecognition struct{}

// Run 实现 CustomRecognitionRunner 接口。
func (r *EssenceTooltipJudgeRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	log.Info().
		Str("recognition", arg.CustomRecognitionName).
		Msg("essence: starting EssenceTooltipJudge recognition")

	if arg.Img == nil {
		log.Warn().Msg("essence: arg image is nil")
		return nil, false
	}

	param := tooltipJudgeParam{
		S1Node: "EssenceTooltip_S1",
		S2Node: "EssenceTooltip_S2",
		S3Node: "EssenceTooltip_S3",
	}
	if arg.CustomRecognitionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil {
			log.Warn().Err(err).Msg("essence: failed to parse tooltip judge param, using defaults")
		}
	}

	s1, ok1 := ocrAttrFromNode(ctx, arg.Img, param.S1Node)
	s2, ok2 := ocrAttrFromNode(ctx, arg.Img, param.S2Node)
	s3, ok3 := ocrAttrFromNode(ctx, arg.Img, param.S3Node)

	if !ok1 || !ok2 || !ok3 {
		log.Warn().
			Bool("s1", ok1).
			Bool("s2", ok2).
			Bool("s3", ok3).
			Msg("essence: failed to OCR all attributes")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}

	preferred := extractPreferredWeaponsFromFlags(param.PreferredWeaponFlags)
	if len(preferred) == 0 {
		preferred = splitWeaponTokens(param.PreferredWeapons)
	}
	result := JudgeEssenceWithPreferredWeapons(s1, s2, s3, preferred, param.MatchMode)
	if param.OnlyDecision != "" && result.Decision != param.OnlyDecision {
		log.Info().
			Str("decision", result.Decision).
			Str("onlyDecision", param.OnlyDecision).
			Msg("essence: decision mismatch, recognition miss")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}
	data, err := json.Marshal(result)
	if err != nil {
		log.Error().Err(err).Interface("result", result).Msg("essence: failed to marshal JudgeResult")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}

	log.Info().
		Str("s1", s1).
		Str("s2", s2).
		Str("s3", s3).
		Str("decision", result.Decision).
		Msg("essence: finished EssenceTooltipJudge recognition")

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(data),
	}, true
}

func ocrAttrFromNode(ctx *maa.Context, img image.Image, nodeName string) (string, bool) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil {
		log.Error().Err(err).Str("node", nodeName).Msg("essence: RunRecognition failed")
		return "", false
	}
	if detail == nil || detail.Results == nil {
		log.Warn().Str("node", nodeName).Msg("essence: no recognition results")
		return "", false
	}

	for _, results := range [][]*maa.RecognitionResult{detail.Results.Filtered, detail.Results.Best, detail.Results.All} {
		if len(results) == 0 {
			continue
		}
		if ocrResult, ok := results[0].AsOCR(); ok {
			attr := normalizeAttrText(ocrResult.Text)
			if attr != "" {
				return attr, true
			}
		}
	}
	return "", false
}

func normalizeAttrText(text string) string {
	if text == "" {
		return ""
	}
	clean := attrCleanupRe.ReplaceAllString(text, "")
	clean = strings.TrimSpace(clean)
	clean = strings.TrimLeft(clean, "·•* ")
	clean = strings.TrimRight(clean, "_-— ")
	return strings.TrimSpace(clean)
}

func splitWeaponTokens(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '\n', '\r', '\t', ' ':
			return true
		default:
			return false
		}
	})
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}


