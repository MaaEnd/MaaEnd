package ims

import (
	"os"
	"path/filepath"
	"testing"
	"text/template"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func resetBatchGainTestState(t *testing.T) {
	t.Helper()

	cancelBatchFlush()
	batchGainMu.Lock()
	batchGains = make(map[string]int)
	batchHasTasks = false
	batchFlushSeq = 0
	batchGainMu.Unlock()

	t.Cleanup(func() {
		cancelBatchFlush()
		batchGainMu.Lock()
		batchGains = make(map[string]int)
		batchHasTasks = false
		batchFlushSeq = 0
		batchGainMu.Unlock()
		batchFlushDelay = defaultBatchFlushDelay
		emitBatchGainSummary = maafocus.PrintLargeContentTrimNewline
		renderBatchGainSummary = renderBatchGainSummaryHTML
	})
}

func captureBatchGainEmit(t *testing.T) <-chan string {
	t.Helper()

	events := make(chan string, 4)
	emitBatchGainSummary = func(content string) {
		events <- content
	}
	renderBatchGainSummary = func([]batchGainRow) string {
		return "batch-summary"
	}
	return events
}

func TestRecordGainAccumulatesAndIgnoresNonPositive(t *testing.T) {
	resetBatchGainTestState(t)

	recordGain("item_a", 2)
	recordGain("item_a", 3)
	recordGain("item_b", 1)
	recordGain("item_b", 0)
	recordGain("item_b", -1)
	recordGain("  ", 4)

	got := batchGainSnapshot()
	if got["item_a"] != 5 {
		t.Fatalf("item_a = %d, want 5", got["item_a"])
	}
	if got["item_b"] != 1 {
		t.Fatalf("item_b = %d, want 1", got["item_b"])
	}
	if _, exists := got[""]; exists {
		t.Fatalf("blank item id must be ignored: %#v", got)
	}
}

func TestBuildBatchGainRowsSortsByRarityDescending(t *testing.T) {
	resetBatchGainTestState(t)
	withRecognitionItemsPath(t, writeRecognitionItems(t, `{
		"item_common": {"rarity": 1},
		"item_rare": {"rarity": 6},
		"item_mid": {"rarity": 4}
	}`))

	rows := buildBatchGainRows(map[string]int{
		"item_rare":   1,
		"item_common": 2,
		"item_mid":    3,
	})

	got := []string{rows[0].ItemID, rows[1].ItemID, rows[2].ItemID}
	want := []string{"item_rare", "item_mid", "item_common"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row order = %v, want %v (rarest must be first)", got, want)
		}
	}
	if rows[0].Rarity != 6 {
		t.Fatalf("first row rarity = %d, want 6", rows[0].Rarity)
	}
}

func TestBuildBatchGainRowsRarityTieBreaksByItemID(t *testing.T) {
	resetBatchGainTestState(t)
	withRecognitionItemsPath(t, writeRecognitionItems(t, `{
		"item_b": {"rarity": 4},
		"item_a": {"rarity": 4}
	}`))

	rows := buildBatchGainRows(map[string]int{
		"item_b": 1,
		"item_a": 2,
	})

	if rows[0].ItemID != "item_a" || rows[1].ItemID != "item_b" {
		t.Fatalf("tie order = [%s %s], want [item_a item_b]", rows[0].ItemID, rows[1].ItemID)
	}
}

func TestBuildBatchGainRowsKeepsUnknownItems(t *testing.T) {
	resetBatchGainTestState(t)
	withRecognitionItemsPath(t, writeRecognitionItems(t, `{
		"item_known": {"rarity": 4}
	}`))

	rows := buildBatchGainRows(map[string]int{
		"item_unknown": 1,
		"item_known":   2,
	})

	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2", len(rows))
	}
	unknown := rows[len(rows)-1]
	if unknown.ItemID != "item_unknown" || unknown.Rarity != 0 {
		t.Fatalf("unknown item must stay with rarity 0 at the bottom: %#v", unknown)
	}
	if unknown.Name != "item_unknown" {
		t.Fatalf("unknown item name = %q, want item id fallback", unknown.Name)
	}
}

func TestFlushBatchGainSummaryEmitsAndClears(t *testing.T) {
	resetBatchGainTestState(t)
	events := captureBatchGainEmit(t)

	recordGain("item_a", 2)
	flushBatchGainSummary(true)

	select {
	case got := <-events:
		if got != "batch-summary" {
			t.Fatalf("emitted %q, want batch-summary", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected batch gain summary to be emitted")
	}

	if got := batchGainSnapshot(); len(got) != 0 {
		t.Fatalf("gains must be cleared after flush, got %#v", got)
	}
}

func TestFlushBatchGainSummarySkipsEmptyWhenNotForced(t *testing.T) {
	resetBatchGainTestState(t)
	events := captureBatchGainEmit(t)

	flushBatchGainSummary(false)
	select {
	case got := <-events:
		t.Fatalf("unexpected empty summary without force: %q", got)
	case <-time.After(50 * time.Millisecond):
	}

	flushBatchGainSummary(true)
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("forced flush must emit even without gains")
	}
}

func TestGainSummarySinkFlushesAfterIdle(t *testing.T) {
	resetBatchGainTestState(t)
	batchFlushDelay = 20 * time.Millisecond
	events := captureBatchGainEmit(t)
	sink := &gainSummarySink{}

	recordGain("item_a", 2)
	sink.OnTaskerTask(nil, maa.EventStatusSucceeded, maa.TaskerTaskDetail{Entry: "TaskA"})

	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("expected idle flush after task end")
	}
	if got := batchGainSnapshot(); len(got) != 0 {
		t.Fatalf("gains must be cleared after idle flush, got %#v", got)
	}
}

func TestGainSummarySinkCancelsOnNextTaskStart(t *testing.T) {
	resetBatchGainTestState(t)
	batchFlushDelay = 20 * time.Millisecond
	events := captureBatchGainEmit(t)
	sink := &gainSummarySink{}

	recordGain("item_a", 2)
	sink.OnTaskerTask(nil, maa.EventStatusSucceeded, maa.TaskerTaskDetail{Entry: "TaskA"})
	sink.OnTaskerTask(nil, maa.EventStatusStarting, maa.TaskerTaskDetail{Entry: "TaskB"})

	select {
	case got := <-events:
		t.Fatalf("flush must be cancelled by the next task start, got %q", got)
	case <-time.After(100 * time.Millisecond):
	}
	if got := batchGainSnapshot(); got["item_a"] != 2 {
		t.Fatalf("gains must survive a cancelled flush, got %#v", got)
	}
}

func TestGainSummarySinkIgnoresFlushEntry(t *testing.T) {
	resetBatchGainTestState(t)
	batchFlushDelay = 20 * time.Millisecond
	events := captureBatchGainEmit(t)
	sink := &gainSummarySink{}

	recordGain("item_a", 2)
	sink.OnTaskerTask(nil, maa.EventStatusSucceeded, maa.TaskerTaskDetail{Entry: gainSummaryFlushEntry})

	select {
	case got := <-events:
		t.Fatalf("flush entry must not arm the idle timer, got %q", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestGainSummaryFlushActionFlushesAndClears(t *testing.T) {
	resetBatchGainTestState(t)
	events := captureBatchGainEmit(t)

	recordGain("item_a", 2)
	if ok := (&GainSummaryFlush{}).Run(nil, &maa.CustomActionArg{}); !ok {
		t.Fatal("GainSummaryFlush action returned false")
	}

	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("expected GainSummaryFlush action to emit summary")
	}
	if got := batchGainSnapshot(); len(got) != 0 {
		t.Fatalf("gains must be cleared by flush action, got %#v", got)
	}
}

func TestShutdownFlushEmitsEmptyAfterTaskStarted(t *testing.T) {
	resetBatchGainTestState(t)
	events := captureBatchGainEmit(t)
	sink := &gainSummarySink{}

	sink.OnTaskerTask(nil, maa.EventStatusStarting, maa.TaskerTaskDetail{Entry: "TaskA"})
	FlushBatchGainSummary()

	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("shutdown flush must emit the empty message after a task ran")
	}
}

func TestShutdownFlushSkipsWithoutAnyTask(t *testing.T) {
	resetBatchGainTestState(t)
	events := captureBatchGainEmit(t)

	FlushBatchGainSummary()

	select {
	case got := <-events:
		t.Fatalf("shutdown flush without tasks must stay silent, got %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestShutdownFlushDoesNotRepeatAfterIdleFlush(t *testing.T) {
	resetBatchGainTestState(t)
	batchFlushDelay = 20 * time.Millisecond
	events := captureBatchGainEmit(t)
	sink := &gainSummarySink{}

	sink.OnTaskerTask(nil, maa.EventStatusStarting, maa.TaskerTaskDetail{Entry: "TaskA"})
	sink.OnTaskerTask(nil, maa.EventStatusSucceeded, maa.TaskerTaskDetail{Entry: "TaskA"})
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("expected idle flush after task end")
	}

	FlushBatchGainSummary()
	select {
	case got := <-events:
		t.Fatalf("shutdown flush after an idle flush must not repeat, got %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestShutdownFlushDoesNotRepeatAfterFlush(t *testing.T) {
	resetBatchGainTestState(t)
	events := captureBatchGainEmit(t)
	sink := &gainSummarySink{}

	sink.OnTaskerTask(nil, maa.EventStatusStarting, maa.TaskerTaskDetail{Entry: "TaskA"})
	FlushBatchGainSummary()
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("first shutdown flush should emit")
	}

	FlushBatchGainSummary()
	select {
	case got := <-events:
		t.Fatalf("second shutdown flush must not repeat, got %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBatchGainSummaryTemplateParses(t *testing.T) {
	path := filepath.Join("..", "..", "..", "assets", "locales", "go-service", "HTML", "ims-batch-gain-summary.html")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = template.New("batch-gain-summary").Funcs(template.FuncMap{
		"t":          func(suffix string) string { return suffix },
		"escapeHTML": func(value string) string { return value },
	}).Parse(string(content))
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
}

func writeRecognitionItems(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recognition_items.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
