package autostockpile

import (
	"fmt"
	"image"
	"math"
	"regexp"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var stockBillRe = regexp.MustCompile(`(\d+\.?\d*)万?/`)

const stockBillNodeName = "AutoStockpileGetStockBill"

type quantityUpperBound struct {
	MaxBuy            int
	CappedQuantity    int
	ConstraintApplied bool
	Limited           bool
}

func parseStockBillAmount(texts []string) (int, bool) {
	for _, text := range texts {
		match := stockBillRe.FindStringSubmatch(text)
		if len(match) < 2 {
			continue
		}
		val, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		beforeSlash := strings.SplitN(match[0], "/", 2)[0]
		if strings.Contains(beforeSlash, "万") {
			return int(val * 10000), true
		}
		return int(val), true
	}
	return 0, false
}

func runStockBillOCR(ctx *maa.Context, img image.Image) (int, bool) {
	detail, err := ctx.RunRecognition(stockBillNodeName, img, nil)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "stock_bill_ocr").
			Msg("failed to run stock bill ocr")
		return 0, false
	}

	texts := extractOCRTexts(detail)
	amount, ok := parseStockBillAmount(texts)
	if !ok {
		log.Warn().
			Str("component", autoStockpileComponent).
			Str("step", "stock_bill_ocr").
			Msg("failed to parse stock bill amount")
		return 0, false
	}

	log.Info().
		Str("component", autoStockpileComponent).
		Int("stock_bill_amount", amount).
		Msg("stock bill ocr parsed")
	return amount, true
}

func resolveMaxBuy(stockBillAmount, reserveAmount, price int) int {
	if price <= 0 {
		return 0
	}
	if reserveAmount <= 0 {
		return math.MaxInt32
	}
	maxBuy := (stockBillAmount - reserveAmount) / price
	if maxBuy < 0 {
		return 0
	}
	return maxBuy
}

func shouldAbortForInsufficientFunds(stockBillOK bool, stockBillAmount, reserveStockBill int) bool {
	return stockBillOK && reserveStockBill > 0 && stockBillAmount <= reserveStockBill
}

func resolveQuantityUpperBound(stockBillAvailable bool, stockBillAmount, reserveStockBill, price, quotaCurrent int) (quantityUpperBound, error) {
	if quotaCurrent < 0 {
		quotaCurrent = 0
	}

	if reserveStockBill <= 0 {
		// reserve_stock_bill 未启用时，不施加调度券约束；此时 MaxBuy 仅为占位值，
		// 日志/展示层必须以 ConstraintApplied 判断 max_buy 是否有业务语义。
		return quantityUpperBound{
			MaxBuy:            0,
			CappedQuantity:    quotaCurrent,
			ConstraintApplied: false,
		}, nil
	}

	if !stockBillAvailable {
		return quantityUpperBound{}, fmt.Errorf("stock bill is unavailable while reserve_stock_bill=%d", reserveStockBill)
	}

	maxBuy := resolveMaxBuy(stockBillAmount, reserveStockBill, price)
	cappedQuantity := min(quotaCurrent, maxBuy)
	if cappedQuantity < 0 {
		cappedQuantity = 0
	}

	return quantityUpperBound{
		MaxBuy:            maxBuy,
		CappedQuantity:    cappedQuantity,
		ConstraintApplied: true,
		Limited:           cappedQuantity < quotaCurrent,
	}, nil
}
