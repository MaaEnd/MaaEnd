package sellproduct

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods"
	sellstrategy "github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods/strategy"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/selectiondata"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/operator"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const locationPlanActionName = "SellProductLocationPlan"

type locationPlanActionParam struct {
	Location string `json:"location"`
}

// LocationPlanAction 输出一个据点的货品与干员合并计划。
type LocationPlanAction struct{}

var _ maa.CustomActionRunner = (*LocationPlanAction)(nil)

func (a *LocationPlanAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, err := parseLocationPlanActionParam(arg)
	if err != nil {
		log.Error().Err(err).Str("component", locationPlanActionName).Msg("invalid params")
		return false
	}
	if err := printRuntimeLocationPlan(ctx, param.Location); err != nil {
		log.Warn().Err(err).
			Str("component", locationPlanActionName).
			Str("location", param.Location).
			Msg("failed to print outpost plan")
		maafocus.Print(ctx, i18n.T(
			"sellproduct.runtime.location_entered",
			selectiondata.LocationName(param.Location),
		))
	}
	return true
}

func parseLocationPlanActionParam(arg *maa.CustomActionArg) (*locationPlanActionParam, error) {
	if arg == nil {
		return nil, fmt.Errorf("custom action arg is nil")
	}
	var param locationPlanActionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		return nil, fmt.Errorf("unmarshal custom_action_param: %w", err)
	}
	param.Location = strings.TrimSpace(param.Location)
	if param.Location == "" {
		return nil, fmt.Errorf("location is empty")
	}
	return &param, nil
}

type runtimeLocationPlan struct {
	LocationName       string
	TargetOperator     string
	RestoreOperator    string
	SelectionStrategy  sellstrategy.Kind
	Items              []goods.LocationPlanItem
	ExcludedOutOfStock []string
	ReserveSatisfied   []goods.LocationPlanItem
	ExcludedByUser     []string
}

func printRuntimeLocationPlan(ctx *maa.Context, location string) error {
	operatorPlan, err := operator.BuildLocationPlan(location)
	if err != nil {
		return err
	}
	goodsPlan, err := goods.BuildLocationPlan(location)
	if err != nil {
		return err
	}
	plan := runtimeLocationPlan{
		LocationName:       operatorPlan.LocationName,
		TargetOperator:     operatorPlan.TargetOperator,
		RestoreOperator:    operatorPlan.RestoreOperator,
		SelectionStrategy:  goodsPlan.Strategy,
		Items:              goodsPlan.Items,
		ExcludedOutOfStock: goodsPlan.ExcludedOutOfStock,
		ReserveSatisfied:   goodsPlan.ReserveSatisfied,
		ExcludedByUser:     goodsPlan.ExcludedByUser,
	}
	maafocus.Print(ctx, runtimeLocationPlanMessage(plan))
	return nil
}

func runtimeLocationPlanMessage(plan runtimeLocationPlan) string {
	itemNames := make([]string, 0, len(plan.Items))
	reserveDescriptions := make([]string, 0, len(plan.Items))
	for _, item := range plan.Items {
		itemNames = append(itemNames, item.Name)
		if item.ReserveQuantity > 0 {
			reserveDescriptions = append(reserveDescriptions, i18n.T(
				"sellproduct.runtime.plan.reserve_rule",
				item.Name,
				item.ReserveQuantity,
			))
		}
	}
	reserveSatisfiedNames := make([]string, 0, len(plan.ReserveSatisfied))
	for _, item := range plan.ReserveSatisfied {
		reserveSatisfiedNames = append(reserveSatisfiedNames, item.Name)
		if item.ReserveQuantity > 0 {
			reserveDescriptions = append(reserveDescriptions, i18n.T(
				"sellproduct.runtime.plan.reserve_rule",
				item.Name,
				item.ReserveQuantity,
			))
		}
	}

	itemOrder := runtimeLocationPlanGoodsOrder(plan.SelectionStrategy, itemNames)
	excludedOutOfStock := runtimePlanTextOrNone(strings.Join(plan.ExcludedOutOfStock, i18n.Separator()))
	reserveSatisfied := runtimePlanTextOrNone(strings.Join(reserveSatisfiedNames, i18n.Separator()))
	excludedByUser := runtimePlanTextOrNone(strings.Join(plan.ExcludedByUser, i18n.Separator()))
	reservePlan := i18n.T("sellproduct.runtime.plan.no_reserve")
	if len(reserveDescriptions) > 0 {
		reservePlan = strings.Join(reserveDescriptions, i18n.Separator())
	}
	return i18n.T(
		"sellproduct.runtime.location_plan",
		runtimePlanTextOrNone(plan.LocationName),
		runtimePlanTextOrNone(plan.TargetOperator),
		runtimePlanTextOrNone(plan.RestoreOperator),
		itemOrder,
		excludedOutOfStock,
		reserveSatisfied,
		excludedByUser,
		reservePlan,
	)
}

func runtimeLocationPlanGoodsOrder(strategy sellstrategy.Kind, itemNames []string) string {
	if strategy == sellstrategy.KindStock {
		return i18n.T("sellproduct.runtime.plan.stock_priority")
	}
	return runtimePlanTextOrNone(strings.Join(itemNames, " → "))
}

func runtimePlanTextOrNone(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return i18n.T("sellproduct.runtime.plan.none")
}
