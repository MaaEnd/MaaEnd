package listcomplete

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName    = "ListCompleteRecognition"
	attachReady      = "ready"
	defaultThreshold = 0.9
	templateNameFmt  = "ListCompleteRecognition/%s.png"
)

var _ maa.CustomRecognitionRunner = &Recognition{}
var _ nodeStore = (*maa.Context)(nil)

// Recognition 是通用的列表完成识别器：通过指定区域的模板相似度判断列表是否已到底。
//
// 返回 true 表示列表已到底；返回 false 表示尚未到底（仍应继续滑动）。
//
// 首次调用（当前节点 attach.ready 为空/false）时截取 roi 经 OverrideImage 写入运行时模板，
// 将 ready 置 true，并返回 false（尚不能判定到底）。之后用 TemplateMatch 对比：
// 相似度 >= threshold（默认 0.9）视为画面未变、列表到底，返回 true；
// 低于阈值则重新截取并 OverrideImage，返回 false。
//
// Pipeline 用法：将本识别放在滑动节点之前；命中则走「到底」分支，未命中再落到滑动节点。
// 调用方必须保证识别时画面已静止：滑动节点应配置 post_wait_freezes，否则惯性/回弹
// 会被当成区域变化而无法判定到底。
//
// 识别区域使用节点原生 roi（V2：recognition.param.roi，与 custom_recognition 同级；经 arg.Roi 传入）；缺省为全屏。
// threshold 可在 custom_recognition_param 中传入，默认 0.9。
type Recognition struct{}

type params struct {
	// Threshold 为模板匹配阈值，默认 0.9；命中（>= 阈值）视为列表到底并返回 true。
	Threshold float64 `json:"threshold"`
}

// nodeStore 抽象 attach 读写，便于单测用 fake 跨多次调用保留状态。
type nodeStore interface {
	GetNodeJSON(nodeName string) (string, error)
	OverridePipeline(pipelineOverride any) error
}

// Run implements maa.CustomRecognitionRunner.
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().
			Str("component", componentName).
			Msg("nil context, arg or image")
		return nil, false
	}

	p, err := parseParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse params")
		return nil, false
	}

	currentNode := strings.TrimSpace(arg.CurrentTaskName)
	if currentNode == "" {
		log.Error().
			Str("component", componentName).
			Msg("current task name is empty")
		return nil, false
	}

	roi, err := resolveROI(arg.Img, arg.Roi)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", currentNode).
			Ints("roi", []int{arg.Roi[0], arg.Roi[1], arg.Roi[2], arg.Roi[3]}).
			Msg("invalid roi")
		return nil, false
	}

	ready, err := loadReady(ctx, currentNode)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", currentNode).
			Msg("failed to load attach.ready")
		return nil, false
	}

	templateName := fmt.Sprintf(templateNameFmt, currentNode)
	box := maa.Rect{roi[0], roi[1], roi[2], roi[3]}

	if !ready {
		if err := captureAndOverride(ctx, arg.Img, roi, templateName); err != nil {
			log.Error().
				Err(err).
				Str("component", componentName).
				Str("node", currentNode).
				Str("template", templateName).
				Msg("failed to capture template on first run")
			return nil, false
		}
		if err := saveReady(ctx, currentNode, true); err != nil {
			log.Error().
				Err(err).
				Str("component", componentName).
				Str("node", currentNode).
				Msg("failed to save attach.ready")
			return nil, false
		}
		log.Info().
			Str("component", componentName).
			Str("node", currentNode).
			Str("template", templateName).
			Ints("roi", []int{roi[0], roi[1], roi[2], roi[3]}).
			Float64("threshold", p.Threshold).
			Msg("first run: template captured, not complete yet")
		return nil, false
	}

	matched, score, err := matchTemplate(ctx, arg.Img, roi, templateName, p.Threshold)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", componentName).
			Str("node", currentNode).
			Str("template", templateName).
			Msg("template match failed, recapturing")
		matched = false
		score = 0
	}

	if matched {
		log.Info().
			Str("component", componentName).
			Str("node", currentNode).
			Str("template", templateName).
			Float64("score", score).
			Float64("threshold", p.Threshold).
			Msg("template matched, list complete")
		return &maa.CustomRecognitionResult{
			Box: box,
			Detail: marshalDetail(map[string]any{
				"node":      currentNode,
				"template":  templateName,
				"roi":       []int{roi[0], roi[1], roi[2], roi[3]},
				"threshold": p.Threshold,
				"score":     score,
				"first_run": false,
				"ready":     true,
				"complete":  true,
			}),
		}, true
	}

	if err := captureAndOverride(ctx, arg.Img, roi, templateName); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", currentNode).
			Str("template", templateName).
			Msg("failed to recapture template")
		return nil, false
	}

	log.Info().
		Str("component", componentName).
		Str("node", currentNode).
		Str("template", templateName).
		Float64("score", score).
		Float64("threshold", p.Threshold).
		Msg("template changed, recaptured, not complete")
	return nil, false
}

func parseParams(raw string) (*params, error) {
	p := &params{Threshold: defaultThreshold}
	if strings.TrimSpace(raw) == "" {
		return p, nil
	}
	if err := json.Unmarshal([]byte(raw), p); err != nil {
		return nil, err
	}
	if p.Threshold == 0 {
		p.Threshold = defaultThreshold
	}
	if p.Threshold <= 0 || p.Threshold > 1 {
		return nil, fmt.Errorf("threshold must be in (0, 1], got %v", p.Threshold)
	}
	return p, nil
}

// resolveROI 将节点原生 roi（arg.Roi）规范化为落在图像范围内的 [x,y,w,h]；
// 宽高无效时回退全屏。
func resolveROI(img image.Image, roi maa.Rect) (maa.Rect, error) {
	bounds := img.Bounds()
	full := maa.Rect{bounds.Min.X, bounds.Min.Y, bounds.Dx(), bounds.Dy()}
	if full[2] <= 0 || full[3] <= 0 {
		return maa.Rect{}, fmt.Errorf("image has empty bounds")
	}
	if roi[2] <= 0 || roi[3] <= 0 {
		return full, nil
	}
	rect := image.Rect(roi[0], roi[1], roi[0]+roi[2], roi[1]+roi[3]).Intersect(bounds)
	if rect.Empty() {
		return maa.Rect{}, fmt.Errorf("roi %v is outside image bounds %v", roi, bounds)
	}
	return maa.Rect{rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy()}, nil
}

func captureAndOverride(ctx *maa.Context, img image.Image, roi maa.Rect, templateName string) error {
	cropped := cropROI(img, roi)
	if cropped == nil || cropped.Bounds().Empty() {
		return fmt.Errorf("cropped template is empty")
	}
	return ctx.OverrideImage(templateName, cropped)
}

func cropROI(img image.Image, roi maa.Rect) *image.RGBA {
	rgba := minicv.ImageConvertRGBA(img)
	return minicv.ImageCropRect(rgba, image.Rect(roi[0], roi[1], roi[0]+roi[2], roi[1]+roi[3]))
}

func matchTemplate(
	ctx *maa.Context,
	img image.Image,
	roi maa.Rect,
	templateName string,
	threshold float64,
) (bool, float64, error) {
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeTemplateMatch,
		&maa.TemplateMatchParam{
			ROI:       maa.NewTargetRect(roi),
			Template:  []string{templateName},
			Threshold: []float64{threshold},
		},
		img,
	)
	if err != nil {
		return false, 0, err
	}
	if detail == nil || !detail.Hit {
		score := bestTemplateScore(detail)
		return false, score, nil
	}
	return true, bestTemplateScore(detail), nil
}

func bestTemplateScore(detail *maa.RecognitionDetail) float64 {
	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return 0
	}
	tm, ok := detail.Results.Best.AsTemplateMatch()
	if !ok || tm == nil {
		return 0
	}
	return tm.Score
}

func loadReady(store nodeStore, nodeName string) (bool, error) {
	raw, err := store.GetNodeJSON(nodeName)
	if err != nil {
		return false, err
	}
	var wrapper struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return false, err
	}
	if wrapper.Attach == nil {
		return false, nil
	}
	rawReady, ok := wrapper.Attach[attachReady]
	if !ok || len(rawReady) == 0 || string(rawReady) == "null" {
		return false, nil
	}
	var ready bool
	if err := json.Unmarshal(rawReady, &ready); err != nil {
		return false, fmt.Errorf("attach.%s must be bool: %w", attachReady, err)
	}
	return ready, nil
}

func saveReady(store nodeStore, nodeName string, ready bool) error {
	raw, err := store.GetNodeJSON(nodeName)
	if err != nil {
		return err
	}

	var wrapper struct {
		Attach map[string]any `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return err
	}
	if wrapper.Attach == nil {
		wrapper.Attach = make(map[string]any)
	}
	wrapper.Attach[attachReady] = ready

	return store.OverridePipeline(map[string]any{
		nodeName: map[string]any{
			"attach": wrapper.Attach,
		},
	})
}

// marshalDetail 序列化识别 Detail；失败时记日志并以空串继续，不影响命中判定。
func marshalDetail(payload map[string]any) string {
	detailJSON, err := json.Marshal(payload)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Interface("payload", payload).
			Msg("failed to marshal recognition detail")
		return ""
	}
	return string(detailJSON)
}
