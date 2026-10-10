package giftoperator

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	portraitInsetX    = 12
	portraitInsetY    = 18
	portraitWidth     = 56
	portraitHeight    = 48
	portraitThreshold = 0.90
)

type portraitGeometry struct {
	width, height, iconOffsetX, iconOffsetY int
}

type portraitRunner interface {
	recognitionRunner
	RunRecognitionDirect(maa.RecognitionType, maa.RecognitionParam, image.Image) (*maa.RecognitionDetail, error)
	OverrideImage(string, image.Image) error
}

var _ portraitRunner = (*maa.Context)(nil)

// readPortraitGeometry 复用控制器资源内的卡片布局，不能按图标尺寸推算头像大小。
func readPortraitGeometry(store nodeStore) (portraitGeometry, error) {
	raw, err := store.GetNodeJSON("GiftOperatorNotSelect")
	if err != nil {
		return portraitGeometry{}, err
	}
	var node struct {
		ROIOffset   maa.Rect        `json:"roi_offset"`
		Recognition json.RawMessage `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return portraitGeometry{}, err
	}
	if len(node.Recognition) > 0 && node.Recognition[0] == '{' {
		var recognition struct {
			Param struct {
				ROIOffset maa.Rect `json:"roi_offset"`
			} `json:"param"`
		}
		if err := json.Unmarshal(node.Recognition, &recognition); err != nil {
			return portraitGeometry{}, err
		}
		node.ROIOffset = recognition.Param.ROIOffset
	}
	switch node.ROIOffset[1] {
	case -85:
		return portraitGeometry{80, 72, -19, -85}, nil
	case -106:
		return portraitGeometry{100, 90, -24, -106}, nil
	default:
		return portraitGeometry{}, fmt.Errorf("unsupported contact card offset %v", node.ROIOffset)
	}
}

func pendingPortrait(s session) (portraitEntry, bool) {
	for _, entry := range s.Portraits {
		if entry.Operator == s.Pending && entry.Template != "" {
			return entry, true
		}
	}
	return portraitEntry{}, false
}

func giftPortraitTemplates(template string) []string {
	templates := make([]string, 15)
	for i := range templates {
		templates[i] = fmt.Sprintf("%s-gift-%d.png", strings.TrimSuffix(template, ".png"), i)
	}
	return templates
}

// recognizeGenericCandidate 不依赖干员名单，只为选中的新头像保存运行时模板。
// 已记录身份按头像排除；点击前仅重新查找 Pending 身份，卡片位置可以变化。
func recognizeGenericCandidate(runner portraitRunner, img image.Image, s session, geometry portraitGeometry, clicking bool) (*candidate, int, error) {
	detail, err := runner.RunRecognition("GiftOperatorTrustIcon", img)
	if err != nil || detail == nil || !detail.Hit {
		return nil, 0, err
	}
	var matches struct {
		Filtered []maa.TemplateMatchResult `json:"filtered"`
		Best     *maa.TemplateMatchResult  `json:"best"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &matches); err != nil {
		return nil, 0, fmt.Errorf("parse contact trust icons: %w", err)
	}
	if len(matches.Filtered) == 0 && matches.Best != nil {
		matches.Filtered = []maa.TemplateMatchResult{*matches.Best}
	}
	portraits := s.Portraits
	if clicking {
		pending, ok := pendingPortrait(s)
		if !ok {
			return nil, 0, fmt.Errorf("pending portrait is unavailable")
		}
		portraits = []portraitEntry{pending}
	}
	// Custom 回调的图像覆盖只对当前 ctx 生效，每次从任务内的像素恢复。
	for _, entry := range portraits {
		if err := restorePortrait(runner, entry); err != nil {
			return nil, 0, err
		}
	}
	var candidates []candidate
	for _, icon := range matches.Filtered {
		box := maa.Rect{icon.Box[0] + geometry.iconOffsetX, icon.Box[1] + geometry.iconOffsetY, geometry.width, geometry.height}
		if !rectInImage(box, img) {
			continue
		}
		info := candidate{Box: box}
		for _, entry := range portraits {
			matchedBox, hit, err := matchContactPortrait(runner, img, box, entry.Template)
			if err != nil {
				return nil, 0, err
			}
			if hit {
				info.Operator, info.Template, info.Portrait, info.Box = entry.Operator, entry.Template, entry.Image, matchedBox
				break
			}
		}
		if clicking && info.Operator != s.Pending {
			continue
		}
		if info.Operator != "" && (containsOperator(s.Completed, info.Operator) || containsOperator(s.Excluded, info.Operator)) {
			log.Info().Str("component", "GiftOperatorCandidateRecognition").Str("operator", info.Operator).
				Interface("box", info.Box).Msg("excluded previously processed portrait")
			continue
		}
		candidates = append(candidates, info)
	}
	best, trust, err := selectGiftCandidate(candidates, func(info candidate) (int, bool, error) {
		return recognizeCandidateTrust(runner, img, info.Box)
	})
	if err != nil || best == nil || best.Template != "" {
		return best, trust, err
	}
	best.Operator = fmt.Sprintf("portrait-%d", len(s.Portraits)+1)
	best.Template = fmt.Sprintf("GiftOperator/RuntimePortrait/%s.png", best.Operator)
	best.Portrait, err = capturePortrait(runner, img, best.Box, best.Template)
	if err != nil {
		return nil, 0, err
	}
	return best, trust, nil
}

func portraitCropBox(box maa.Rect) maa.Rect {
	return maa.Rect{box[0] + portraitInsetX*box[2]/80, box[1] + portraitInsetY*box[3]/72,
		portraitWidth * box[2] / 80, portraitHeight * box[3] / 72}
}

func matchContactPortrait(runner portraitRunner, img image.Image, box maa.Rect, template string) (maa.Rect, bool, error) {
	if template == "" {
		return maa.Rect{}, false, fmt.Errorf("recorded portrait template is empty")
	}
	detail, err := runner.RunRecognitionDirect(maa.RecognitionTypeTemplateMatch, &maa.TemplateMatchParam{
		ROI: maa.NewTargetRect(box), Template: []string{template}, Threshold: []float64{portraitThreshold},
		Method: maa.TemplateMatchMethodCCOEFF_NORMED,
	}, img)
	if err != nil || detail == nil || !detail.Hit {
		return maa.Rect{}, false, err
	}
	inner := portraitCropBox(box)
	if detail.Box[2] != inner[2] || detail.Box[3] != inner[3] {
		return maa.Rect{}, false, fmt.Errorf("recorded portrait size changed: %v", detail.Box)
	}
	matched := maa.Rect{detail.Box[0] - (inner[0] - box[0]), detail.Box[1] - (inner[1] - box[1]), box[2], box[3]}
	return matched, rectInImage(matched, img), nil
}

// capturePortrait 将身份像素交给 session 保存，不保存 ctx 或 Resource 句柄。
func capturePortrait(runner portraitRunner, img image.Image, box maa.Rect, template string) (string, error) {
	inner := portraitCropBox(box)
	if !rectInImage(inner, img) {
		return "", fmt.Errorf("portrait crop is outside screenshot: %v", inner)
	}
	portrait := minicv.ImageCropRect(minicv.ImageConvertRGBA(img), image.Rect(inner[0], inner[1], inner[0]+inner[2], inner[1]+inner[3]))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, portrait); err != nil {
		return "", fmt.Errorf("encode runtime portrait: %w", err)
	}
	if err := installPortrait(runner, portrait, template); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encoded.Bytes()), nil
}

// restorePortrait 在当前回调内重新安装身份模板及送礼界面的派生模板。
func restorePortrait(runner portraitRunner, entry portraitEntry) error {
	if entry.Image == "" || entry.Template == "" {
		return fmt.Errorf("stored portrait image or template is missing for %s", entry.Operator)
	}
	// 联络头像内区不足 100×100；拒绝无界像素数据与意外的整屏模板。
	if len(entry.Image) > 128*1024 {
		return fmt.Errorf("stored portrait image is too large for %s", entry.Operator)
	}
	data, err := base64.StdEncoding.DecodeString(entry.Image)
	if err != nil {
		return fmt.Errorf("decode stored portrait for %s: %w", entry.Operator, err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 100 || config.Height > 100 {
		return fmt.Errorf("invalid stored portrait PNG for %s", entry.Operator)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("read stored portrait PNG for %s: %w", entry.Operator, err)
	}
	return installPortrait(runner, minicv.ImageConvertRGBA(img), entry.Template)
}

func installPortrait(runner portraitRunner, portrait *image.RGBA, template string) error {
	if err := runner.OverrideImage(template, portrait); err != nil {
		return fmt.Errorf("save runtime portrait: %w", err)
	}
	for i, name := range giftPortraitTemplates(template) {
		if err := runner.OverrideImage(name, minicv.ImageScale(portrait, 0.75+0.02*float64(i))); err != nil {
			return fmt.Errorf("save scaled gift portrait: %w", err)
		}
	}
	return nil
}

func rectInImage(box maa.Rect, img image.Image) bool {
	return img != nil && box[2] > 0 && box[3] > 0 && image.Rect(box[0], box[1], box[0]+box[2], box[1]+box[3]).In(img.Bounds())
}
