// Package aicopilot lets an external AI client drive MaaEnd through MCP.
//
// [StandbyAction] keeps a task alive while serving MCP-over-HTTP on loopback.
// Tools that touch MaaFramework are funneled back into the action goroutine,
// because *maa.Context is only usable there and only while Run has not
// returned.
package aicopilot

import (
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &StandbyAction{}

// componentName labels this package in structured logs.
const componentName = "AICopilot"

// stopPollInterval bounds how long the standby loop takes to notice that the
// user stopped the task from the client.
const stopPollInterval = 200 * time.Millisecond

// StandbyAction serves the MCP endpoint until the user stops the task, running
// every AI-issued command on its own goroutine.
type StandbyAction struct{}

func (a *StandbyAction) Run(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	s := newSession()
	endpoint, err := s.serve()
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to start the MCP endpoint")
		maafocus.Print(ctx, i18n.T("aicopilot.serve_failed", err))
		return false
	}
	defer s.shutdown()

	log.Info().
		Str("component", componentName).
		Str("endpoint", endpoint).
		Msg("MCP endpoint ready")
	maafocus.Print(ctx, i18n.T("aicopilot.ready", endpoint))

	ticker := time.NewTicker(stopPollInterval)
	defer ticker.Stop()

	for {
		select {
		case cmd := <-s.cmdCh:
			s.run(ctx, cmd)
		case <-ticker.C:
			if ctx.GetTasker().Stopping() {
				log.Info().
					Str("component", componentName).
					Msg("stop requested, releasing the MCP endpoint")
				return true
			}
		}
	}
}
