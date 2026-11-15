package ims

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentBatchGain    = "BatchGainSummary"
	gainSummaryFlushEntry = "GainSummaryFlush"
)

// defaultBatchFlushDelay is how long the agent waits after the last task ends
// before treating the run as finished. Clients that know the batch boundary can
// post gainSummaryFlushEntry instead and skip this wait.
var defaultBatchFlushDelay = 5 * time.Second

var batchFlushDelay = defaultBatchFlushDelay

var (
	batchGainMu     sync.Mutex
	batchGains      = make(map[string]int)
	batchHasTasks   bool
	batchFlushTimer *time.Timer
	batchFlushSeq   uint64
)

// Test seams: tests replace the emitter and renderer to avoid stdout and i18n
// template loading.
var (
	emitBatchGainSummary   = maafocus.PrintLargeContentTrimNewline
	renderBatchGainSummary = renderBatchGainSummaryHTML
)

type batchGainRow struct {
	ItemID   string
	Name     string
	Quantity int
	IconSrc  string
	Rarity   int
}

// recordGain accumulates one recognized reward stack. It is independent of the
// IMS cache readiness: A3 still reports items when hasData is false.
func recordGain(itemID string, quantity int) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || quantity <= 0 {
		return
	}
	batchGainMu.Lock()
	defer batchGainMu.Unlock()
	batchGains[itemID] += quantity
}

// batchGainSnapshot returns a copy of the current batch totals.
func batchGainSnapshot() map[string]int {
	batchGainMu.Lock()
	defer batchGainMu.Unlock()
	out := make(map[string]int, len(batchGains))
	for itemID, quantity := range batchGains {
		out[itemID] = quantity
	}
	return out
}

// takeBatchGains atomically steals the current totals and cancels any pending
// idle flush.
func takeBatchGains() map[string]int {
	batchGainMu.Lock()
	defer batchGainMu.Unlock()

	batchFlushSeq++
	if batchFlushTimer != nil {
		batchFlushTimer.Stop()
		batchFlushTimer = nil
	}

	out := batchGains
	batchGains = make(map[string]int)
	batchHasTasks = false
	return out
}

func markBatchTaskStarted() {
	batchGainMu.Lock()
	batchHasTasks = true
	batchGainMu.Unlock()
}

// buildBatchGainRows resolves display metadata and sorts by rarity descending so
// the rarest items appear first in the log entry.
func buildBatchGainRows(items map[string]int) []batchGainRow {
	if len(items) == 0 {
		return nil
	}

	catalog, err := loadRecognitionItems()
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", componentBatchGain).
			Msg("recognition catalog unavailable, batch summary rarity disabled")
	}

	rows := make([]batchGainRow, 0, len(items))
	for itemID, quantity := range items {
		rarity := 0
		if catalog != nil {
			if meta, ok := catalog[itemID]; ok {
				rarity = meta.Rarity
			}
		}
		rows = append(rows, batchGainRow{
			ItemID:   itemID,
			Name:     iconqty.ItemDisplayName(itemID),
			Quantity: quantity,
			IconSrc:  itemIconSrc(itemID),
			Rarity:   rarity,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Rarity != rows[j].Rarity {
			return rows[i].Rarity > rows[j].Rarity
		}
		return rows[i].ItemID < rows[j].ItemID
	})
	return rows
}

// flushBatchGainSummary emits the accumulated totals and clears them. When
// force is false and there is nothing to report, it returns silently (shutdown
// best-effort path).
func flushBatchGainSummary(force bool) {
	items := takeBatchGains()
	if len(items) == 0 && !force {
		return
	}

	rows := buildBatchGainRows(items)
	emitBatchGainSummary(renderBatchGainSummary(rows))
	log.Info().
		Str("component", componentBatchGain).
		Int("item_count", len(rows)).
		Msg("batch gain summary flushed")
}

// FlushBatchGainSummary is the shutdown hook. MXU stops the agent immediately
// after the last task, so this is often the only chance to report the batch.
// If no task ran since the last flush, it stays silent; if a task ran but
// produced no recognized items, it still emits the empty message.
func FlushBatchGainSummary() {
	batchGainMu.Lock()
	hadTasks := batchHasTasks
	batchGainMu.Unlock()
	flushBatchGainSummary(hadTasks)
}

func renderBatchGainSummaryHTML(rows []batchGainRow) string {
	return i18n.RenderHTML("ims.batch_gain_summary", map[string]any{
		"Items":    rows,
		"IconSize": focusItemIconSizePx,
	})
}

// armBatchFlush (re)starts the idle timer that approximates the end of a run
// for clients that do not post gainSummaryFlushEntry.
//
// The tasker handle is deliberately not retained: MaaFramework handles are only
// valid inside the callback that received them. The timer relies on the next
// Tasker.Task.Starting event to cancel it instead of polling tasker state.
func armBatchFlush() {
	batchGainMu.Lock()
	batchFlushSeq++
	seq := batchFlushSeq
	if batchFlushTimer != nil {
		batchFlushTimer.Stop()
	}
	batchFlushTimer = time.AfterFunc(batchFlushDelay, func() {
		batchGainMu.Lock()
		if seq != batchFlushSeq {
			batchGainMu.Unlock()
			return
		}
		batchFlushTimer = nil
		batchGainMu.Unlock()

		flushBatchGainSummary(true)
	})
	batchGainMu.Unlock()
}

func cancelBatchFlush() {
	batchGainMu.Lock()
	defer batchGainMu.Unlock()
	batchFlushSeq++
	if batchFlushTimer != nil {
		batchFlushTimer.Stop()
		batchFlushTimer = nil
	}
}

// gainSummarySink tracks task boundaries. A normal task ending arms the idle
// timer; any task starting cancels it. The explicit flush entry is ignored so
// its own lifecycle does not re-arm the timer.
type gainSummarySink struct{}

var _ maa.TaskerEventSink = &gainSummarySink{}

func (*gainSummarySink) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if detail.Entry == gainSummaryFlushEntry {
		if event == maa.EventStatusStarting {
			cancelBatchFlush()
		}
		return
	}

	switch event {
	case maa.EventStatusStarting:
		markBatchTaskStarted()
		cancelBatchFlush()
	case maa.EventStatusSucceeded, maa.EventStatusFailed:
		armBatchFlush()
	}
}

// GainSummaryFlush is the explicit batch-end action. Clients can post
// gainSummaryFlushEntry to flush immediately instead of waiting for the idle
// fallback.
type GainSummaryFlush struct{}

var _ maa.CustomActionRunner = &GainSummaryFlush{}

func (a *GainSummaryFlush) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", componentBatchGain).
			Msg("nil custom action arg")
		return false
	}
	flushBatchGainSummary(true)
	return true
}
