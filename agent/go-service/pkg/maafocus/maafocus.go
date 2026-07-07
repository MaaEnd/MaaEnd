// Package maafocus provides helpers to send focus payloads from go-service
// events so the client can render related UI focus hints.
package maafocus

import (
	"fmt"
	"strings"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const nodeName = "_GO_SERVICE_FOCUS_"

// 进程级的限频时间戳表：以 PrintThrottle 的 content 字符串作为 key，
// 记录上次成功输出的时间。结构小、访问不频繁，简单 mutex 足够。
// 该表只增不减，不做主动清理：调用方应保证 content 取值集合有界
// （例如来自 i18n 模板而不是任意外部输入）。
var (
	throttleHistory = make(map[string]time.Time)
)

// Print sends focus payload on node action starting event,
// so that the client can make the payload visible to users.
//
// The actual UI rendering is handled by client side.
// See https://maafw.com/en/docs/3.1-PipelineProtocol#node-notifications
//
// If the content is too large, consider using [PrintLargeContent]
// to avoid potential performance issues.
func Print(ctx *maa.Context, content string) {
	if ctx == nil {
		log.Warn().
			Str("event", "node_action_starting").
			Msg("context is nil, skip sending focus")
		return
	}

	pp := maa.NewPipeline()
	node := maa.NewNode(nodeName).
		SetFocus(map[string]any{
			maa.EventNodeAction.Starting(): content,
		}).
		SetPreDelay(0).
		SetPostDelay(0)
	pp.AddNode(node)

	if _, err := ctx.RunAction(nodeName, maa.Rect{}, "", pp); err != nil {
		log.Warn().
			Err(err).
			Str("event", "node_action_starting").
			Msg("failed to send focus")
	}
}

// PrintThrottle behaves like [Print] but throttles output by content:
// the same content string will be forwarded to [Print] at most once within
// the given interval. Distinct content strings are tracked independently.
//
// Typical use: a recognition/action handler that is invoked once per polling
// tick and would otherwise flood the user with identical status hints.
//
// Notes:
//   - The throttle key is the exact (already-formatted) content string;
//     identical strings from different call sites share the same window.
//   - Throttle state is process-wide and never auto-evicted; callers should
//     keep the set of distinct content values bounded.
//   - If interval <= 0, this degenerates to a plain [Print] call.
func PrintThrottle(ctx *maa.Context, interval time.Duration, content string) {
	if interval > 0 && !shouldPrintThrottled(content, interval) {
		return
	}
	Print(ctx, content)
}

func shouldPrintThrottled(content string, interval time.Duration) bool {
	now := time.Now()
	if last, ok := throttleHistory[content]; ok && now.Sub(last) < interval {
		return false
	}
	throttleHistory[content] = now
	return true
}

// PrintLargeContent sends payload to [fmt.Println] as an alternative to the [Print] function.
// So that the content will not be recorded into Maa's log system in order to reduce Maa's log size.
//
// If the content contains newlines, consider using [PrintLargeContentTrimNewline]
// to avoid client parsing issues.
func PrintLargeContent(content string) {
	fmt.Println(content)
}

// PrintLargeContentTrimNewline sends payload to [fmt.Println]
// and replaces all newlines and continuous blanks with a single space.
//
// This function is useful when printing HTML content.
func PrintLargeContentTrimNewline(content string) {
	content = strings.Join(strings.Fields(content), " ")
	fmt.Println(content)
}
