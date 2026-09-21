package aicopilot

import (
	"context"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerPrompts publishes ready-made operating goals, so the user can pick
// one in the AI client instead of typing a prompt. Clients that ignore the
// prompts primitive simply do not show them.
func registerPrompts(srv *mcp.Server) {
	presets := []struct {
		name        string
		description string
		body        string
	}{
		{
			name:        "run_daily_routine",
			description: i18n.T("aicopilot.prompt.run_daily_routine.desc"),
			body:        i18n.T("aicopilot.prompt.run_daily_routine.body"),
		},
		{
			name:        "diagnose_stuck_screen",
			description: i18n.T("aicopilot.prompt.diagnose_stuck_screen.desc"),
			body:        i18n.T("aicopilot.prompt.diagnose_stuck_screen.body"),
		},
	}

	for _, preset := range presets {
		body := preset.body
		srv.AddPrompt(&mcp.Prompt{
			Name:        preset.name,
			Description: preset.description,
		}, func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{
				Messages: []*mcp.PromptMessage{{
					Role:    "user",
					Content: &mcp.TextContent{Text: body},
				}},
			}, nil
		})
	}
}
