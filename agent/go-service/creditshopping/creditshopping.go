package creditshopping

import (
	"encoding/json"
	"fmt"
	"image"
	"regexp"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

// MaxCredit 信用点上限阈值
const MaxCredit = 300

// SoldOutDarkPixelThreshold 售罄商品暗像素阈值
const SoldOutDarkPixelThreshold = 120
const SoldOutDarkPixelCount = 10000

type Rect struct {
	X, Y, W, H int
}

type CommodityROIConfig struct {
	RectMove    Rect
	DiscountROI Rect
	PriceROI    Rect
	NameROI     Rect
}

// Credit OCR 区域配置
var CreditOCRConfig = maa.NodeOCRParam{
	ROI:       maa.NewTargetRect(maa.Rect{1083, 17, 76, 26}), // 信用点显示区域
	OrderBy:   "Expected",
	Expected:  []string{"[0-9]+"},
	Threshold: 0.3,
}

var DefaultCommodityConfig = CommodityROIConfig{
	RectMove:    Rect{-95, -145, 150, 200},
	DiscountROI: Rect{95, 0, 55, 30},
	PriceROI:    Rect{105, 137, 40, 20},
	NameROI:     Rect{0, 170, 150, 40},
}

type CreditShoppingParams struct {
	Blacklist                 StringSlice `json:"blacklist"`
	BuyFirst                  StringSlice `json:"buy_first"`
	ForceShoppingIfCreditFull bool        `json:"force_shopping_if_credit_full"`
	OnlyBuyDiscount           bool        `json:"only_buy_discount"`
	ReserveMaxCredit          bool        `json:"reserve_max_credit"`
}

type StringSlice []string

func (s *StringSlice) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*s = arr
		return nil
	}

	// 尝试解析为字符串（分号分隔）
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		if str == "" {
			*s = []string{}
		} else {
			parts := strings.Split(str, ";")
			result := make([]string, 0, len(parts))
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					result = append(result, trimmed)
				}
			}
			*s = result
		}
		return nil
	}

	return fmt.Errorf("cannot unmarshal %s into StringSlice", string(data))
}

func getAttachStringSlice(attach map[string]any, key string) []string {
	if attach == nil {
		return nil
	}
	v, ok := attach[key]
	if !ok {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func getAttachBool(attach map[string]any, key string) bool {
	if attach == nil {
		return false
	}
	v, ok := attach[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

// CommodityInfo 商品信息
type CommodityInfo struct {
	Index    int
	Name     string
	Price    int
	Discount int
	Box      Rect
	SoldOut  bool
}

// ShoppingPhase 购物阶段（与 MAA 一致的三阶段）
type ShoppingPhase int

const (
	PhaseShoppingFirst ShoppingPhase = iota // 优先购买阶段（buy_first 白名单）
	PhaseNormal                             // 正常购买阶段（blacklist 黑名单）
	PhaseForce                              // 强制购买阶段（信用溢出时无视名单）
)

func (p ShoppingPhase) String() string {
	switch p {
	case PhaseShoppingFirst:
		return "ShoppingFirst"
	case PhaseNormal:
		return "Normal"
	case PhaseForce:
		return "Force"
	default:
		return "Unknown"
	}
}

// shoppingState 购物状态
var shoppingState struct {
	AllCommodities []CommodityInfo      // 所有识别到的商品（按位置排序）
	Commodities    []CommodityInfo      // 当前阶段待购买商品
	CurrentIdx     int                  // 当前处理索引
	Params         CreditShoppingParams // 购物参数
	Phase          ShoppingPhase        // 当前购物阶段
	BuyCount       int                  // 已购买数量
}

// resetShoppingState 重置购物状态
func resetShoppingState() {
	shoppingState.AllCommodities = nil
	shoppingState.Commodities = nil
	shoppingState.CurrentIdx = 0
	shoppingState.Params = CreditShoppingParams{}
	shoppingState.Phase = PhaseShoppingFirst
	shoppingState.BuyCount = 0
}

// CreditShoppingAction 信用购物自定义动作
type CreditShoppingAction struct{}

func (a *CreditShoppingAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 检查是否为首次调用（状态为空）
	if shoppingState.AllCommodities == nil {
		return a.firstCall(ctx, arg)
	}
	return a.subsequentCall(ctx, arg)
}

func (a *CreditShoppingAction) firstCall(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 从 attach 中解析参数
	node, _ := ctx.GetNodeData("CreditShoppingShopping")
	attach := node.Attach
	params := CreditShoppingParams{
		BuyFirst:                  getAttachStringSlice(attach, "buy_first"),
		Blacklist:                 getAttachStringSlice(attach, "blacklist"),
		ForceShoppingIfCreditFull: getAttachBool(attach, "force_shopping_if_credit_full"),
		OnlyBuyDiscount:           getAttachBool(attach, "only_buy_discount"),
		ReserveMaxCredit:          getAttachBool(attach, "reserve_max_credit"),
	}

	log.Info().
		Strs("blacklist", params.Blacklist).
		Strs("buy_first", params.BuyFirst).
		Bool("force_shopping_if_credit_full", params.ForceShoppingIfCreditFull).
		Bool("only_buy_discount", params.OnlyBuyDiscount).
		Bool("reserve_max_credit", params.ReserveMaxCredit).
		Msg("[CreditShopping]购物参数")

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[CreditShopping]无法获取控制器")
		return false
	}

	controller.PostScreencap().Wait()

	credit := creditOCR(ctx, controller)
	log.Info().Int("credit", credit).Msg("[CreditShopping]当前信用点")

	allCommodities := detectAndAnalyzeCommodities(ctx, controller)
	if len(allCommodities) == 0 {
		log.Warn().Msg("[CreditShopping]未识别到任何商品")
		return false
	}
	log.Info().Int("count", len(allCommodities)).Msg("[CreditShopping]识别到商品数量")

	shoppingState.Params = params
	shoppingState.AllCommodities = allCommodities
	shoppingState.BuyCount = 0

	if len(params.BuyFirst) > 0 {
		shoppingState.Phase = PhaseShoppingFirst
	} else {
		shoppingState.Phase = PhaseNormal
	}

	shoppingState.Commodities = selectForPhase(shoppingState.Phase, shoppingState.AllCommodities, params)
	shoppingState.CurrentIdx = 0

	if len(shoppingState.Commodities) == 0 {
		return a.tryNextPhase(ctx, arg, credit)
	}

	return a.buyNextItem(ctx, arg)
}

func (a *CreditShoppingAction) subsequentCall(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	shoppingState.BuyCount++

	log.Info().
		Int("buyCount", shoppingState.BuyCount).
		Str("phase", shoppingState.Phase.String()).
		Int("currentIdx", shoppingState.CurrentIdx).
		Int("total", len(shoppingState.Commodities)).
		Msg("[CreditShopping]购买成功，继续处理")

	if shoppingState.CurrentIdx >= len(shoppingState.Commodities) {
		controller := ctx.GetTasker().GetController()
		credit := -1
		if controller != nil {
			controller.PostScreencap().Wait()
			credit = creditOCR(ctx, controller)
		}
		return a.tryNextPhase(ctx, arg, credit)
	}

	return a.buyNextItem(ctx, arg)
}

// tryNextPhase 尝试切换到下一阶段
func (a *CreditShoppingAction) tryNextPhase(ctx *maa.Context, arg *maa.CustomActionArg, credit int) bool {
	params := shoppingState.Params

	for {
		nextPhase := shoppingState.Phase + 1

		// 检查是否有下一阶段
		if nextPhase > PhaseForce {
			// 所有阶段都完成了
			log.Info().Int("total_bought", shoppingState.BuyCount).Msg("[CreditShopping]所有阶段购物完成")
			resetShoppingState()
			return true
		}

		// 强制阶段需要额外条件
		if nextPhase == PhaseForce {
			if !params.ForceShoppingIfCreditFull {
				// 未启用强制购物
				log.Info().Int("total_bought", shoppingState.BuyCount).Msg("[CreditShopping]购物完成（未启用强制购物）")
				resetShoppingState()
				return true
			}
			// 检查信用是否溢出
			if credit >= 0 && credit <= MaxCredit {
				log.Info().Int("credit", credit).Int("total_bought", shoppingState.BuyCount).
					Msg("[CreditShopping]信用点未溢出，跳过强制购物阶段")
				resetShoppingState()
				return true
			}
		}

		// 切换到下一阶段
		shoppingState.Phase = nextPhase
		shoppingState.Commodities = selectForPhase(nextPhase, shoppingState.AllCommodities, params)
		shoppingState.CurrentIdx = 0

		log.Info().
			Str("phase", nextPhase.String()).
			Int("commodities", len(shoppingState.Commodities)).
			Msg("[CreditShopping]切换到新阶段")

		if len(shoppingState.Commodities) > 0 {
			return a.buyNextItem(ctx, arg)
		}
	}
}

// buyNextItem 购买下一个商品
func (a *CreditShoppingAction) buyNextItem(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		return false
	}

	if shoppingState.CurrentIdx >= len(shoppingState.Commodities) {
		credit := -1
		controller.PostScreencap().Wait()
		credit = creditOCR(ctx, controller)
		return a.tryNextPhase(ctx, arg, credit)
	}

	commodity := shoppingState.Commodities[shoppingState.CurrentIdx]
	shoppingState.CurrentIdx++

	if shoppingState.Phase == PhaseNormal {
		controller.PostScreencap().Wait()

		if shoppingState.Params.ReserveMaxCredit {
			currentCredit := creditOCR(ctx, controller)
			if currentCredit >= 0 && currentCredit <= MaxCredit {
				log.Info().Int("credit", currentCredit).Msg("[CreditShopping]信用点已低于上限，停止正常购物阶段")
				return a.tryNextPhase(ctx, arg, currentCredit)
			}
		}

		if shoppingState.Params.OnlyBuyDiscount {
			if commodity.Discount <= 0 {
				log.Info().Str("product", commodity.Name).Msg("[CreditShopping]遇到无折扣商品，停止正常购物阶段")
				currentCredit := creditOCR(ctx, controller)
				return a.tryNextPhase(ctx, arg, currentCredit)
			}
		}
	}

	if shoppingState.Phase == PhaseForce {
		controller.PostScreencap().Wait()
		currentCredit := creditOCR(ctx, controller)
		if currentCredit >= 0 && currentCredit <= MaxCredit {
			log.Info().Int("credit", currentCredit).Int("total_bought", shoppingState.BuyCount).
				Msg("[CreditShopping]信用点不再溢出，停止强制购物")
			resetShoppingState()
			return true
		}
	}

	log.Info().
		Str("phase", shoppingState.Phase.String()).
		Int("index", shoppingState.CurrentIdx).
		Int("total", len(shoppingState.Commodities)).
		Str("name", commodity.Name).
		Int("price", commodity.Price).
		Int("discount", commodity.Discount).
		Msg("[CreditShopping]准备购买商品")

	ctx.OverridePipeline(map[string]interface{}{
		"CreditShoppingBuyItem": map[string]interface{}{
			"target": []int{commodity.Box.X, commodity.Box.Y, commodity.Box.W, commodity.Box.H},
		},
	})

	ctx.OverrideNext(arg.CurrentTaskName, []string{"CreditShoppingBuyItem"})

	return true
}

func selectForPhase(phase ShoppingPhase, allCommodities []CommodityInfo, params CreditShoppingParams) []CommodityInfo {
	candidates := make([]CommodityInfo, 0)

	switch phase {
	case PhaseShoppingFirst:
		// 优先购买阶段：只选 buy_first 中的商品
		for _, commodity := range allCommodities {
			if commodity.SoldOut {
				continue
			}
			if containsAny(commodity.Name, params.BuyFirst) {
				candidates = append(candidates, commodity)
			}
		}

	case PhaseNormal:
		// 正常购买：排除 blacklist 中的商品
		for _, commodity := range allCommodities {
			if commodity.SoldOut {
				continue
			}
			if len(params.Blacklist) > 0 && containsAny(commodity.Name, params.Blacklist) {
				log.Info().Str("product", commodity.Name).Msg("[CreditShopping]在黑名单中，跳过")
				continue
			}
			candidates = append(candidates, commodity)
		}

	case PhaseForce:
		// 强制购买：无视名单，购买所有非售罄商品
		for _, commodity := range allCommodities {
			if commodity.SoldOut {
				continue
			}
			candidates = append(candidates, commodity)
		}
	}

	log.Info().
		Str("phase", phase.String()).
		Int("candidates", len(candidates)).
		Msg("[CreditShopping]阶段商品选择完成")
	for _, commodity := range candidates {
		log.Info().
			Int("index", commodity.Index).
			Str("name", commodity.Name).
			Int("price", commodity.Price).
			Int("discount", commodity.Discount).
			Msg("[CreditShopping]商品信息")
	}

	return candidates
}

func detectAndAnalyzeCommodities(ctx *maa.Context, controller *maa.Controller) []CommodityInfo {
	img := controller.CacheImage()
	if img == nil {
		log.Warn().Msg("[CreditShopping]截图失败")
		return nil
	}

	// 通过信用点图标定位所有商品框
	matchParam := maa.NodeTemplateMatchParam{
		Template:  []string{"CreditShopping/CreditIcon.png"},
		Threshold: []float64{0.8},
		OrderBy:   "Vertical",
	}

	detail := ctx.RunRecognitionDirect("TemplateMatch", matchParam, img)
	if detail == nil || detail.DetailJson == "" {
		log.Warn().Msg("[CreditShopping]未识别到信用点图标")
		return nil
	}

	if len(detail.Results.All) == 0 {
		log.Warn().Msg("[CreditShopping]信用点图标解析失败")
		return nil
	}

	log.Info().Int("iconCount", len(detail.Results.All)).Msg("[CreditShopping]识别到信用点图标数量")

	config := DefaultCommodityConfig
	commodities := make([]CommodityInfo, 0, len(detail.Results.All))

	for idx, item := range detail.Results.All {
		result, ok := item.AsTemplateMatch()
		if !ok || len(result.Box) < 4 {
			continue
		}
		iconBox := Rect{X: result.Box[0], Y: result.Box[1], W: result.Box[2], H: result.Box[3]}

		iconCenterX := iconBox.X + iconBox.W/2
		iconCenterY := iconBox.Y + iconBox.H/2

		commodityBox := Rect{
			X: iconCenterX + config.RectMove.X,
			Y: iconCenterY + config.RectMove.Y,
			W: config.RectMove.W,
			H: config.RectMove.H,
		}

		commodity := CommodityInfo{
			Index: idx + 1,
			Box:   commodityBox,
		}

		commodity.SoldOut = checkSoldOut(ctx, img, commodityBox)
		commodity.Name = getOCRText(ctx, controller, commodityBox, config.NameROI)
		commodity.Price, _ = strconv.Atoi(getOCRText(ctx, controller, commodityBox, config.PriceROI))
		commodity.Discount, _ = strconv.Atoi(getOCRText(ctx, controller, commodityBox, config.DiscountROI))

		log.Info().
			Int("index", commodity.Index).
			Ints("rect", []int{commodityBox.X, commodityBox.Y, commodityBox.W, commodityBox.H}).
			Bool("soldOut", commodity.SoldOut).
			Str("name", commodity.Name).
			Int("price", commodity.Price).
			Int("discount", commodity.Discount).
			Msg("[CreditShopping]商品信息")

		commodities = append(commodities, commodity)
	}

	return commodities
}

func checkSoldOut(ctx *maa.Context, img image.Image, box Rect) bool {
	colorParam := &maa.NodeColorMatchParam{
		ROI:    maa.NewTargetRect(maa.Rect{box.X, box.Y, box.W, box.H}),
		Lower:  [][]int{{0}},
		Upper:  [][]int{{SoldOutDarkPixelThreshold}},
		Count:  SoldOutDarkPixelCount,
		Method: 6,
	}

	detail := ctx.RunRecognitionDirect("ColorMatch", colorParam, img)
	if detail == nil {
		log.Warn().Msg("[CreditShopping]检测售罄商品失败")
		return false
	}

	isSoldOut := detail.Hit
	return isSoldOut
}

func getOCRText(ctx *maa.Context, controller *maa.Controller, box Rect, offset Rect) string {
	img := controller.CacheImage()
	if img == nil {
		return ""
	}

	nameX := box.X + offset.X
	nameY := box.Y + offset.Y
	nameW := offset.W
	nameH := offset.H

	ocrParam := &maa.NodeOCRParam{
		ROI:       maa.NewTargetRect(maa.Rect{nameX, nameY, nameW, nameH}),
		OrderBy:   "Horizontal",
		Threshold: 0.5,
	}

	detail := ctx.RunRecognitionDirect(maa.NodeRecognitionTypeOCR, ocrParam, img)
	if detail == nil || detail.DetailJson == "" {
		return ""
	}

	text, _ := extractTextFromOCRDetail(detail)
	return text
}

func creditOCR(ctx *maa.Context, controller *maa.Controller) int {
	img := controller.CacheImage()
	if img == nil {
		log.Warn().Msg("[CreditShopping]截图失败")
		return -1
	}

	detail := ctx.RunRecognitionDirect(maa.NodeRecognitionTypeOCR, CreditOCRConfig, img)
	if detail == nil || detail.DetailJson == "" {
		log.Warn().Msg("[CreditShopping]信用点OCR无结果")
		return -1
	}

	num, ok := extractNumberFromOCRDetail(detail)
	if !ok {
		log.Warn().Msg("[CreditShopping]信用点解析失败")
		return -1
	}

	return num
}

func containsAny(text string, list []string) bool {
	for _, item := range list {
		if strings.Contains(text, item) {
			return true
		}
	}
	return false
}

func extractNumberFromOCRDetail(detail *maa.RecognitionDetail) (int, bool) {
	for _, result := range detail.Results.All {
		if data, ok := result.AsOCR(); ok {
			text := data.Text
			if text != "" {
				re := regexp.MustCompile(`\d+`)
				matches := re.FindAllString(text, -1)
				if len(matches) > 0 {
					digitsOnly := strings.Join(matches, "")
					if num, err := strconv.Atoi(digitsOnly); err == nil {
						return num, true
					}
				}
			}
		}
	}

	return 0, false
}

func extractTextFromOCRDetail(detail *maa.RecognitionDetail) (string, bool) {
	for _, result := range detail.Results.All {
		data, ok := result.AsOCR()
		if !ok {
			continue
		}
		return data.Text, true
	}

	return "", false
}
