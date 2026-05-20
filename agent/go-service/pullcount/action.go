package pullcount

import (
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "PullCountCalculator"

	defaultSingleTicketNode = "PullCountCalculatorSingleTicketOCR"
	defaultTenTicketNode    = "PullCountCalculatorTenTicketOCR"
	defaultOriginiumNode    = "PullCountCalculatorOriginiumOCR"
	defaultOroberylNode     = "PullCountCalculatorOroberylOCR"
)

var _ maa.CustomActionRunner = &Action{}

// Action calculates available limited recruitment pulls from OCR resource values.
type Action struct{}

type actionParam struct {
	SingleTicketNode string `json:"single_ticket_node"`
	TenTicketNode    string `json:"ten_ticket_node"`
	OriginiumNode    string `json:"originium_node"`
	OroberylNode     string `json:"oroberyl_node"`

	ReservedOriginium   int `json:"reserved_originium"`
	OriginiumToOroberyl int `json:"originium_to_oroberyl"`
	OroberylPerPull     int `json:"oroberyl_per_pull"`
	TenTicketMultiplier int `json:"ten_ticket_multiplier"`
}

type resourceValues struct {
	SingleTicket int
	TenTicket    int
	Originium    int
	Oroberyl     int
}

type calculationResult struct {
	UsableOriginium   int
	ConvertedOroberyl int
	ResourcePulls     int
	TicketPulls       int
	TotalPulls        int
}

// --- 入口与参数 --- //

// Run captures the current screen, reads recruitment resources, and prints the pull count.
func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		log.Error().Str("component", componentName).Msg("context is nil")
		return false
	}
	if arg == nil {
		log.Error().Str("component", componentName).Msg("custom action arg is nil")
		return false
	}

	param, err := parseActionParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse action params")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}

	img, err := captureCurrentImage(ctx)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to capture image")
		maafocus.Print(ctx, i18n.T("pullcount.error.capture_failed"))
		return false
	}

	values, err := readResourceValues(ctx, img, param)
	if err != nil {
		log.Warn().Err(err).Str("component", componentName).Msg("failed to read resource values")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", err.Error()))
		return false
	}

	result := calculatePullCount(values, param)
	maafocus.Print(ctx, formatResultFocus(values, result))

	log.Info().
		Str("component", componentName).
		Int("single_ticket", values.SingleTicket).
		Int("ten_ticket", values.TenTicket).
		Int("originium", values.Originium).
		Int("oroberyl", values.Oroberyl).
		Int("usable_originium", result.UsableOriginium).
		Int("converted_oroberyl", result.ConvertedOroberyl).
		Int("resource_pulls", result.ResourcePulls).
		Int("ticket_pulls", result.TicketPulls).
		Int("total_pulls", result.TotalPulls).
		Msg("pull count calculated")

	return true
}

// parseActionParam parses the custom action config and fills default constants.
func parseActionParam(raw string) (*actionParam, error) {
	param := actionParam{
		SingleTicketNode:    defaultSingleTicketNode,
		TenTicketNode:       defaultTenTicketNode,
		OriginiumNode:       defaultOriginiumNode,
		OroberylNode:        defaultOroberylNode,
		ReservedOriginium:   29,
		OriginiumToOroberyl: 75,
		OroberylPerPull:     500,
		TenTicketMultiplier: 10,
	}

	if strings.TrimSpace(raw) == "" {
		return &param, nil
	}

	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return nil, err
	}

	param.SingleTicketNode = defaultIfEmpty(param.SingleTicketNode, defaultSingleTicketNode)
	param.TenTicketNode = defaultIfEmpty(param.TenTicketNode, defaultTenTicketNode)
	param.OriginiumNode = defaultIfEmpty(param.OriginiumNode, defaultOriginiumNode)
	param.OroberylNode = defaultIfEmpty(param.OroberylNode, defaultOroberylNode)
	if param.OriginiumToOroberyl <= 0 {
		return nil, fmt.Errorf("originium_to_oroberyl must be positive")
	}
	if param.OroberylPerPull <= 0 {
		return nil, fmt.Errorf("oroberyl_per_pull must be positive")
	}
	if param.TenTicketMultiplier <= 0 {
		return nil, fmt.Errorf("ten_ticket_multiplier must be positive")
	}

	return &param, nil
}

// defaultIfEmpty returns fallback when value is empty after trimming spaces.
func defaultIfEmpty(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

// --- OCR 读取 --- //

// captureCurrentImage requests a fresh screenshot from the current controller.
func captureCurrentImage(ctx *maa.Context) (image.Image, error) {
	tasker := ctx.GetTasker()
	if tasker == nil {
		return nil, fmt.Errorf("tasker is nil")
	}

	controller := tasker.GetController()
	if controller == nil {
		return nil, fmt.Errorf("controller is nil")
	}

	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, fmt.Errorf("cached image is nil")
	}
	return img, nil
}

// readResourceValues reads all resource counters needed for the pull calculation.
func readResourceValues(ctx *maa.Context, img image.Image, param *actionParam) (resourceValues, error) {
	var values resourceValues
	var err error

	if values.SingleTicket, err = runIntegerOCR(ctx, img, param.SingleTicketNode, i18n.T("pullcount.resource.single_ticket")); err != nil {
		return values, err
	}
	if values.TenTicket, err = runIntegerOCR(ctx, img, param.TenTicketNode, i18n.T("pullcount.resource.ten_ticket")); err != nil {
		return values, err
	}
	if values.Originium, err = runIntegerOCR(ctx, img, param.OriginiumNode, i18n.T("pullcount.resource.originium")); err != nil {
		return values, err
	}
	if values.Oroberyl, err = runIntegerOCR(ctx, img, param.OroberylNode, i18n.T("pullcount.resource.oroberyl")); err != nil {
		return values, err
	}

	return values, nil
}

// runIntegerOCR executes one OCR node and extracts the first integer-like value.
func runIntegerOCR(ctx *maa.Context, img image.Image, nodeName string, label string) (int, error) {
	detail, err := ctx.RunRecognition(nodeName, img)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", label, err)
	}
	if detail == nil || !detail.Hit {
		return 0, fmt.Errorf("%s: OCR not hit", label)
	}

	for _, text := range ocrTextCandidates(detail) {
		value, err := parseIntegerText(text)
		if err == nil {
			return value, nil
		}
	}

	return 0, fmt.Errorf("%s: no integer OCR result", label)
}

// ocrTextCandidates returns OCR texts in preferred reading order.
func ocrTextCandidates(detail *maa.RecognitionDetail) []string {
	if detail == nil || detail.Results == nil {
		return nil
	}

	sources := [][]*maa.RecognitionResult{
		resultsFromBest(detail.Results.Best),
		detail.Results.Filtered,
		detail.Results.All,
	}

	texts := make([]string, 0)
	seen := make(map[string]struct{})
	for _, source := range sources {
		for _, result := range source {
			if result == nil {
				continue
			}
			ocrResult, ok := result.AsOCR()
			if !ok {
				continue
			}
			text := strings.TrimSpace(ocrResult.Text)
			if text == "" {
				continue
			}
			if _, exists := seen[text]; exists {
				continue
			}
			seen[text] = struct{}{}
			texts = append(texts, text)
		}
	}

	return texts
}

// resultsFromBest wraps a best result so it can share list processing code.
func resultsFromBest(best *maa.RecognitionResult) []*maa.RecognitionResult {
	if best == nil {
		return nil
	}
	return []*maa.RecognitionResult{best}
}

// parseIntegerText extracts decimal digits from a compact OCR counter.
func parseIntegerText(text string) (int, error) {
	var b strings.Builder
	for _, r := range text {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if digits == "" {
		return 0, fmt.Errorf("no digits in %q", text)
	}
	return strconv.Atoi(digits)
}

// --- 计算与展示 --- //

// calculatePullCount converts resources and vouchers into available limited pulls.
func calculatePullCount(values resourceValues, param *actionParam) calculationResult {
	usableOriginium := values.Originium - param.ReservedOriginium
	if usableOriginium < 0 {
		usableOriginium = 0
	}

	convertedOroberyl := usableOriginium * param.OriginiumToOroberyl
	resourcePulls := (values.Oroberyl + convertedOroberyl) / param.OroberylPerPull
	ticketPulls := values.SingleTicket + values.TenTicket*param.TenTicketMultiplier

	return calculationResult{
		UsableOriginium:   usableOriginium,
		ConvertedOroberyl: convertedOroberyl,
		ResourcePulls:     resourcePulls,
		TicketPulls:       ticketPulls,
		TotalPulls:        resourcePulls + ticketPulls,
	}
}

// formatResultFocus builds the user-visible calculation summary.
func formatResultFocus(values resourceValues, result calculationResult) string {
	return i18n.T(
		"pullcount.result",
		result.TotalPulls,
		values.Oroberyl,
		values.Originium,
		result.UsableOriginium,
		result.ConvertedOroberyl,
		values.SingleTicket,
		values.TenTicket,
		result.TicketPulls,
		result.ResourcePulls,
	)
}
