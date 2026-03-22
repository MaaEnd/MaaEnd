package itemtransfer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

type itemOrderData struct {
	Items         map[string]itemInfo `json:"items"`
	CategoryOrder map[string][]string `json:"category_order"`
}

type itemInfo struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

type fallbackParams struct {
	TargetClass int    `json:"target_class"`
	Descending  bool   `json:"descending"`
	Side        string `json:"side"`
}

type gridItem struct {
	Box     [4]int
	ClassID uint64
	Score   float64
	CenterX int
	CenterY int
}

const (
	componentName = "itemtransfer"

	repoNNDNode    = "ItemTransferDetectAllItems"
	bagNNDNode     = "ItemTransferDetectAllItemsBag"
	tooltipOCRNode = "ItemTransferTooltipOCR"

	tooltipOffsetX = 15
	tooltipOffsetY = 0
	tooltipWidth   = 155
	tooltipHeight  = 70
)

var (
	cachedData     *itemOrderData
	cachedDataOnce sync.Once
	cachedDataErr  error
)

func loadItemOrderData() (*itemOrderData, error) {
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
		var data itemOrderData
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
	var tried []string

	if v := strings.TrimSpace(os.Getenv("MAAEND_ITEMTRANSFER_DATA_DIR")); v != "" {
		p := filepath.Join(v, "item_order.json")
		if fileExists(p) {
			return v, nil
		}
		tried = append(tried, v)
	}

	wd, err := os.Getwd()
	if err == nil {
		base := wd
		for i := 0; i < 8; i++ {
			cand := filepath.Join(base, "assets", "data", "ItemTransfer")
			if fileExists(filepath.Join(cand, "item_order.json")) {
				return cand, nil
			}
			tried = append(tried, cand)
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
			tried = append(tried, cand)
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}

	return "", fmt.Errorf("cannot find item_order.json in any of %d candidate paths: %v; set MAAEND_ITEMTRANSFER_DATA_DIR", len(tried), tried)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
