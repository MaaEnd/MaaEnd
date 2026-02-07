package essence

import (
	"encoding/json"
	"image"
	"regexp"
	"strings"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

// tooltipJudgeParam defines OCR node names and preferred weapons input.
// tooltipJudgeParam 定义 OCR 节点名与偏好武器输入。
type tooltipJudgeParam struct {
	S1Node               string            `json:"s1_node"`
	S2Node               string            `json:"s2_node"`
	S3Node               string            `json:"s3_node"`
	OnlyDecision         string            `json:"only_decision"`
	PreferredWeaponFlags map[string]string `json:"preferred_weapon_flags"`
}

// attrCleanupRe strips noise like numbers and symbols from OCR output.
// attrCleanupRe 去除 OCR 噪声（数字/符号）。
var attrCleanupRe = regexp.MustCompile(`[0-9+\-%.]`)

// EssenceTooltipJudgeRecognition 从 Tooltip OCR 读取三条属性并判定。
// 依赖 pipeline 节点：EssenceTooltip_S1 / EssenceTooltip_S2 / EssenceTooltip_S3。
type EssenceTooltipJudgeRecognition struct{}

// Run 实现 CustomRecognitionRunner 接口。
// Run implements CustomRecognitionRunner.
func (r *EssenceTooltipJudgeRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	essLog.Info().
		Str("recognition", arg.CustomRecognitionName).
		Msg("starting EssenceTooltipJudge recognition")

	if arg.Img == nil {
		essLog.Warn().Msg("arg image is nil")
		return nil, false
	}

	param := tooltipJudgeParam{
		S1Node: "EssenceTooltip_S1",
		S2Node: "EssenceTooltip_S2",
		S3Node: "EssenceTooltip_S3",
	}
	if arg.CustomRecognitionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil {
			essLog.Warn().Err(err).Msg("failed to parse tooltip judge param, using defaults")
		}
	}

	s1, ok1 := ocrAttrFromNode(ctx, arg.Img, param.S1Node)
	s2, ok2 := ocrAttrFromNode(ctx, arg.Img, param.S2Node)
	s3, ok3 := ocrAttrFromNode(ctx, arg.Img, param.S3Node)

	if !ok1 || !ok2 || !ok3 {
		essLog.Warn().
			Bool("s1", ok1).
			Bool("s2", ok2).
			Bool("s3", ok3).
			Msg("failed to OCR all attributes")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}

	preferred := extractPreferredWeaponsFromFlags(param.PreferredWeaponFlags)
	result := JudgeEssenceWithPreferredWeapons(s1, s2, s3, preferred)
	if param.OnlyDecision != "" && result.Decision != param.OnlyDecision {
		essLog.Info().
			Str("decision", result.Decision).
			Str("onlyDecision", param.OnlyDecision).
			Msg("decision mismatch, recognition miss")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}
	data, err := json.Marshal(result)
	if err != nil {
		essLog.Error().Err(err).Interface("result", result).Msg("failed to marshal JudgeResult")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}

	essLog.Info().
		Str("s1", s1).
		Str("s2", s2).
		Str("s3", s3).
		Str("decision", result.Decision).
		Msg("finished EssenceTooltipJudge recognition")

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(data),
	}, true
}

// ocrAttrFromNode reads OCR text from a pipeline node.
// ocrAttrFromNode 从指定 OCR 节点读取文本。
func ocrAttrFromNode(ctx *maa.Context, img image.Image, nodeName string) (string, bool) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil {
		essLog.Error().Err(err).Str("node", nodeName).Msg("RunRecognition failed")
		return "", false
	}
	if detail == nil || detail.Results == nil {
		essLog.Warn().Str("node", nodeName).Msg("no recognition results")
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

// normalizeAttrText removes digits/symbols and trims extra marks.
// normalizeAttrText 清理数字/符号并去除装饰字符。
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
