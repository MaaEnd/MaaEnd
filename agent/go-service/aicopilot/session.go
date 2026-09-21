package aicopilot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Session states reported by the get_status tool.
const (
	stateStandby = "standby"
	stateBusy    = "busy"
)

// submitGrace lets a request wait for the standby loop to come back from its
// stop-polling tick before the session is reported as busy.
const submitGrace = 500 * time.Millisecond

var (
	errBusy   = errors.New("MaaEnd is still running the previous command, poll get_status and retry")
	errClosed = errors.New("the MaaEnd session has been stopped")
)

type commandResult struct {
	value any
	err   error
}

// command is one unit of work that has to run on the standby action goroutine.
type command struct {
	tool  string
	entry string
	exec  func(ctx *maa.Context) (any, error)
	resp  chan commandResult
}

// session owns the MCP endpoint and the hand-off channel to the standby loop.
type session struct {
	cmdCh chan *command
	done  chan struct{}

	endpoint string
	httpSrv  *http.Server

	closeOnce sync.Once

	mu      sync.Mutex
	running *command
}

func newSession() *session {
	return &session{
		cmdCh: make(chan *command),
		done:  make(chan struct{}),
	}
}

// submitCommand hands exec to the standby loop and waits for its result.
//
// cmdCh is unbuffered, so the hand-off only succeeds while the loop is idle: a
// request arriving during a long run_pipeline is rejected with errBusy rather
// than queued behind it, which keeps the AI client in control of retry timing.
func submitCommand[T any](
	s *session,
	reqCtx context.Context,
	tool, entry string,
	exec func(ctx *maa.Context) (T, error),
) (T, error) {
	var zero T

	cmd := &command{
		tool:  tool,
		entry: entry,
		exec: func(ctx *maa.Context) (any, error) {
			return exec(ctx)
		},
		resp: make(chan commandResult, 1),
	}

	grace := time.NewTimer(submitGrace)
	defer grace.Stop()

	select {
	case s.cmdCh <- cmd:
	case <-grace.C:
		return zero, errBusy
	case <-s.done:
		return zero, errClosed
	case <-reqCtx.Done():
		return zero, reqCtx.Err()
	}

	select {
	case result := <-cmd.resp:
		if result.err != nil {
			return zero, result.err
		}
		typed, ok := result.value.(T)
		if !ok {
			return zero, fmt.Errorf("tool %q produced an unexpected result of type %T", tool, result.value)
		}
		return typed, nil
	case <-s.done:
		return zero, errClosed
	case <-reqCtx.Done():
		return zero, reqCtx.Err()
	}
}

// run executes one command on the standby action goroutine.
func (s *session) run(ctx *maa.Context, cmd *command) {
	s.mu.Lock()
	s.running = cmd
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = nil
		s.mu.Unlock()
	}()

	if cmd.entry != "" {
		maafocus.Print(ctx, i18n.T("aicopilot.command_entry", cmd.tool, cmd.entry))
	} else {
		maafocus.Print(ctx, i18n.T("aicopilot.command", cmd.tool))
	}

	value, err := cmd.exec(ctx)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", componentName).
			Str("tool", cmd.tool).
			Str("entry", cmd.entry).
			Msg("AI command failed")
	}

	// resp is buffered, so an AI client that gave up waiting cannot block the
	// standby loop.
	cmd.resp <- commandResult{value: value, err: err}
}

// status snapshots what the session is doing for the get_status tool.
func (s *session) status() statusOutput {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()

	out := statusOutput{State: stateStandby, Endpoint: s.endpoint}
	if running != nil {
		out.State = stateBusy
		out.Tool = running.tool
		out.Entry = running.entry
	}
	return out
}
