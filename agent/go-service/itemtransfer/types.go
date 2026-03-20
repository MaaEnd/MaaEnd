package itemtransfer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

type ItemOrderData struct {
	Items         map[string]ItemInfo `json:"items"`
	CategoryOrder map[string][]string `json:"category_order"`
}

type ItemInfo struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

type FallbackParams struct {
	TargetClass int  `json:"target_class"`
	Descending  bool `json:"descending"`
	Side        string `json:"side"` // "repo" or "bag"; defaults to "repo"
}

type gridItem struct {
	Box      [4]int
	ClassID  uint64
	Score    float64
	CenterX  int
	CenterY  int
}

const (
	componentName = "itemtransfer"

	repoNNDNode = "ItemTransferDetectAllItems"
	bagNNDNode  = "ItemTransferDetectAllItemsBag"
	tooltipOCRNode = "ItemTransferTooltipOCR"

	tooltipOffsetX = 15
	tooltipOffsetY = 0
	tooltipWidth   = 155
	tooltipHeight  = 70

	repoScrollTargetX = 640
	repoScrollTargetY = 350
	bagScrollTargetX  = 837
	bagScrollTargetY  = 350
	scrollDY          = -180

	maxScrollAttempts = 20
	maxBinaryRetries  = 30
)

var (
	cachedData     *ItemOrderData
	cachedDataOnce sync.Once
	cachedDataErr  error
)

func loadItemOrderData() (*ItemOrderData, error) {
	cachedDataOnce.Do(func() {
		dir, err := findDataDir()
		if err != nil {
			cachedDataErr = err
			return
		}
		b, err := os.ReadFile(filepath.Join(dir, "item_order.json"))
		if err != nil {
			cachedDataErr = err
			return
		}
		var data ItemOrderData
		if err := json.Unmarshal(b, &data); err != nil {
			cachedDataErr = err
			return
		}
		cachedData = &data
		log.Info().
			Str("component", componentName).
			Int("item_count", len(data.Items)).
			Int("category_count", len(data.CategoryOrder)).
			Msg("item order data loaded")
	})
	return cachedData, cachedDataErr
}

func findDataDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("MAAEND_ITEMTRANSFER_DATA_DIR")); v != "" {
		if fileExists(filepath.Join(v, "item_order.json")) {
			return v, nil
		}
	}

	wd, err := os.Getwd()
	if err == nil {
		base := wd
		for i := 0; i < 8; i++ {
			cand := filepath.Join(base, "assets", "data", "ItemTransfer")
			if fileExists(filepath.Join(cand, "item_order.json")) {
				return cand, nil
			}
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}

	if exePath, err2 := os.Executable(); err2 == nil {
		base := filepath.Dir(exePath)
		for i := 0; i < 8; i++ {
			cand := filepath.Join(base, "assets", "data", "ItemTransfer")
			if fileExists(filepath.Join(cand, "item_order.json")) {
				return cand, nil
			}
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}

	return "", errors.New("cannot resolve ItemTransfer data dir; set MAAEND_ITEMTRANSFER_DATA_DIR")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
