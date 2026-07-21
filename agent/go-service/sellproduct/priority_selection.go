package sellproduct

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	priorityItemRecognitionName = "SellProductPriorityItem"
	prioritySessionActionName   = "SellProductPrioritySession"

	priorityResultSelect        = "select"
	priorityResultExhausted     = "exhausted"
	priorityOperationRegister   = "register"
	priorityOperationCommit     = "commit"
	priorityOperationOutOfStock = "out_of_stock"
)

type priorityItemRecognitionParam struct {
	Location string `json:"location"`
	Result   string `json:"result"`
}

type prioritySessionActionParam struct {
	Operation string `json:"operation"`
	Location  string `json:"location,omitempty"`
	ItemID    string `json:"item_id,omitempty"`
}

type priorityExhaustionObservation struct {
	Signature string
	Count     int
}

type prioritySelectionSessionState struct {
	Preferred  []string
	Attempted  map[string]map[string]struct{}
	Pending    map[string]string
	Current    map[string]string
	OutOfStock map[string]struct{}
	Exhaustion map[string]priorityExhaustionObservation
}

var (
	prioritySelectionMu sync.Mutex
	prioritySelection   = newPrioritySelectionSessionState()
)

// PriorityItemRecognition 在选择货品界面中，按稀有度、单价顺序返回下一个未尝试货品。
// exhausted 需要连续两次观察到相同的“只剩已尝试货品”集合，避免单帧 OCR 波动误判结束。
type PriorityItemRecognition struct{}

var _ maa.CustomRecognitionRunner = (*PriorityItemRecognition)(nil)

func (r *PriorityItemRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	param, err := parsePriorityItemRecognitionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", priorityItemRecognitionName).Msg("invalid params")
		return nil, false
	}
	groupsByLocation, err := loadItemPriorityGroupsFunc()
	if err != nil {
		log.Error().Err(err).Str("component", priorityItemRecognitionName).Msg("failed to load item priorities")
		return nil, false
	}
	groups := prioritizeItemGroups(groupsByLocation[param.Location], priorityItemsSnapshot())
	if len(groups) == 0 {
		log.Error().Str("component", priorityItemRecognitionName).Str("location", param.Location).
			Msg("item priority list is empty")
		return nil, false
	}

	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, maa.OCRParam{
		ROI: maa.NewTargetRect(arg.Roi),
	}, arg.Img)
	if err != nil || detail == nil {
		log.Warn().Err(err).Str("component", priorityItemRecognitionName).Msg("inner OCR failed")
		return nil, false
	}
	ocrItems := collectOCRResults(detail)
	if len(ocrItems) == 0 {
		return nil, false
	}

	attempted, outOfStock, pending := prioritySelectionSnapshot(param.Location)
	blacklisted := reserveBlacklistedItemsSnapshot()
	match, itemID, recognized := findPriorityItemMatch(
		ocrItems,
		groups,
		attempted,
		outOfStock,
		blacklisted,
		pending,
	)
	switch param.Result {
	case priorityResultSelect:
		if match == nil {
			return nil, false
		}
		prioritySelectionSetPending(param.Location, itemID)
		detailJSON, _ := json.Marshal(map[string]any{
			"item_id":             itemID,
			"ocr_text":            match.ocrText,
			"matched_candidate":   match.candidate,
			"recognized_item_ids": recognized,
		})
		return &maa.CustomRecognitionResult{Box: match.box, Detail: string(detailJSON)}, true
	case priorityResultExhausted:
		if match != nil || pending != "" || len(recognized) == 0 {
			prioritySelectionResetExhaustion(param.Location)
			return nil, false
		}
		if !prioritySelectionObserveExhaustion(param.Location, recognized) {
			return nil, false
		}
		detailJSON, _ := json.Marshal(map[string]any{
			"location":            param.Location,
			"recognized_item_ids": recognized,
		})
		return &maa.CustomRecognitionResult{Detail: string(detailJSON)}, true
	default:
		return nil, false
	}
}

// PrioritySessionAction 在初始化阶段登记用户优先级，在 Pipeline 确认换货后提交待选结果，
// 并在 Pipeline 确认缺货时把当前物品加入本次任务共享的缺货集合。
type PrioritySessionAction struct{}

var _ maa.CustomActionRunner = (*PrioritySessionAction)(nil)

func (a *PrioritySessionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, err := parsePrioritySessionActionParam(arg)
	if err != nil {
		log.Error().Err(err).Str("component", prioritySessionActionName).Msg("invalid params")
		return false
	}
	switch param.Operation {
	case priorityOperationRegister:
		if param.ItemID == "" {
			log.Debug().Str("component", prioritySessionActionName).
				Msg("unconfigured priority item slot skipped")
			return true
		}
		registered := registerPriorityItem(param.ItemID)
		log.Info().Str("component", prioritySessionActionName).
			Str("item_id", param.ItemID).
			Bool("registered", registered).
			Msg("preferred selling item registered")
		return true
	case priorityOperationCommit:
		itemID, ok := prioritySelectionCommit(param.Location)
		if !ok {
			log.Error().Str("component", prioritySessionActionName).Str("location", param.Location).
				Msg("priority selection commit has no pending item")
			return false
		}
		setSelectedReserveItem(itemID)
		printRuntimeItemSwitched(ctx, param.Location, itemID)
		return true
	case priorityOperationOutOfStock:
		itemID, marked, ok := prioritySelectionMarkOutOfStock(param.Location)
		if !ok {
			log.Error().Str("component", prioritySessionActionName).Str("location", param.Location).
				Msg("out-of-stock mark has no committed item")
			return false
		}
		log.Info().Str("component", prioritySessionActionName).
			Str("location", param.Location).
			Str("item_id", itemID).
			Bool("marked", marked).
			Msg("selling item marked out of stock for current task")
		if marked {
			printRuntimeItemOutOfStock(ctx, param.Location, itemID)
		}
		return true
	default:
		return false
	}
}

func parsePriorityItemRecognitionParam(raw string) (*priorityItemRecognitionParam, error) {
	var param priorityItemRecognitionParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return nil, fmt.Errorf("unmarshal custom_recognition_param: %w", err)
	}
	param.Location = strings.TrimSpace(param.Location)
	param.Result = strings.TrimSpace(param.Result)
	if param.Location == "" {
		return nil, fmt.Errorf("location is empty")
	}
	if param.Result != priorityResultSelect && param.Result != priorityResultExhausted {
		return nil, fmt.Errorf("invalid result %q", param.Result)
	}
	return &param, nil
}

func parsePrioritySessionActionParam(arg *maa.CustomActionArg) (*prioritySessionActionParam, error) {
	if arg == nil {
		return nil, fmt.Errorf("custom action arg is nil")
	}
	var param prioritySessionActionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		return nil, fmt.Errorf("unmarshal custom_action_param: %w", err)
	}
	param.Operation = strings.TrimSpace(param.Operation)
	param.Location = strings.TrimSpace(param.Location)
	param.ItemID = strings.TrimSpace(param.ItemID)
	switch param.Operation {
	case priorityOperationRegister:
	case priorityOperationCommit, priorityOperationOutOfStock:
		if param.Location == "" {
			return nil, fmt.Errorf("location is empty")
		}
	default:
		return nil, fmt.Errorf("invalid operation %q", param.Operation)
	}
	return &param, nil
}

func newPrioritySelectionSessionState() prioritySelectionSessionState {
	return prioritySelectionSessionState{
		Preferred:  []string{},
		Attempted:  map[string]map[string]struct{}{},
		Pending:    map[string]string{},
		Current:    map[string]string{},
		OutOfStock: map[string]struct{}{},
		Exhaustion: map[string]priorityExhaustionObservation{},
	}
}

// registerPriorityItem 返回是否成功登记。重复物品保留首次出现的槽位顺序。
func registerPriorityItem(itemID string) bool {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	for _, registered := range prioritySelection.Preferred {
		if registered == itemID {
			return false
		}
	}
	prioritySelection.Preferred = append(prioritySelection.Preferred, itemID)
	return true
}

func priorityItemsSnapshot() []string {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	return append([]string{}, prioritySelection.Preferred...)
}

func priorityOutOfStockSnapshot() map[string]struct{} {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	outOfStock := make(map[string]struct{}, len(prioritySelection.OutOfStock))
	for itemID := range prioritySelection.OutOfStock {
		outOfStock[itemID] = struct{}{}
	}
	return outOfStock
}

func resetPrioritySelectionSession() {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	prioritySelection = newPrioritySelectionSessionState()
}

func prioritySelectionSnapshot(location string) (map[string]struct{}, map[string]struct{}, string) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	attempted := make(map[string]struct{}, len(prioritySelection.Attempted[location]))
	for itemID := range prioritySelection.Attempted[location] {
		attempted[itemID] = struct{}{}
	}
	outOfStock := make(map[string]struct{}, len(prioritySelection.OutOfStock))
	for itemID := range prioritySelection.OutOfStock {
		outOfStock[itemID] = struct{}{}
	}
	return attempted, outOfStock, prioritySelection.Pending[location]
}

func prioritySelectionSetPending(location, itemID string) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	prioritySelection.Pending[location] = itemID
	delete(prioritySelection.Exhaustion, location)
}

func prioritySelectionCommit(location string) (string, bool) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	itemID := prioritySelection.Pending[location]
	if itemID == "" {
		return "", false
	}
	if prioritySelection.Attempted[location] == nil {
		prioritySelection.Attempted[location] = map[string]struct{}{}
	}
	prioritySelection.Attempted[location][itemID] = struct{}{}
	prioritySelection.Current[location] = itemID
	delete(prioritySelection.Pending, location)
	delete(prioritySelection.Exhaustion, location)
	return itemID, true
}

func prioritySelectionMarkOutOfStock(location string) (string, bool, bool) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	itemID := prioritySelection.Current[location]
	if itemID == "" {
		return "", false, false
	}
	_, exists := prioritySelection.OutOfStock[itemID]
	prioritySelection.OutOfStock[itemID] = struct{}{}
	delete(prioritySelection.Exhaustion, location)
	return itemID, !exists, true
}

func prioritySelectionResetExhaustion(location string) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	delete(prioritySelection.Exhaustion, location)
}

func prioritySelectionObserveExhaustion(location string, recognized []string) bool {
	ids := append([]string{}, recognized...)
	sort.Strings(ids)
	signature := strings.Join(ids, "|")
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	observation := prioritySelection.Exhaustion[location]
	if observation.Signature == signature {
		observation.Count++
	} else {
		observation = priorityExhaustionObservation{Signature: signature, Count: 1}
	}
	prioritySelection.Exhaustion[location] = observation
	return observation.Count >= 2
}
