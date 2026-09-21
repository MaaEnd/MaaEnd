package aicopilot

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog/log"
)

const (
	// mcpPath completes the URL that the user pastes into the AI client.
	mcpPath = "/mcp"

	// basePort starts the probe range. Consecutive ports are tried so a second
	// MaaEnd instance, or a socket the OS has not released yet, does not make
	// the task fail outright.
	basePort  = 12711
	portTries = 10

	readHeaderTimeout = 10 * time.Second
)

// serve binds the MCP endpoint on loopback and returns the URL to advertise.
func (s *session) serve() (string, error) {
	listener, err := listenLoopback()
	if err != nil {
		return "", err
	}

	srv := s.newMCPServer()
	mux := http.NewServeMux()
	mux.Handle(mcpPath, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))

	s.endpoint = fmt.Sprintf("http://%s%s", listener.Addr().String(), mcpPath)
	s.httpSrv = &http.Server{Handler: mux, ReadHeaderTimeout: readHeaderTimeout}

	go func() {
		if err := s.httpSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().
				Err(err).
				Str("component", componentName).
				Msg("MCP HTTP server stopped unexpectedly")
		}
	}()

	return s.endpoint, nil
}

// shutdown releases the port and unblocks any AI client still waiting.
func (s *session) shutdown() {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.httpSrv == nil {
			return
		}
		if err := s.httpSrv.Close(); err != nil {
			log.Warn().
				Err(err).
				Str("component", componentName).
				Msg("failed to close the MCP HTTP server")
		}
	})
}

// listenLoopback binds the first free port in the probe range. Binding is
// restricted to 127.0.0.1 so the endpoint is never reachable from the network.
func listenLoopback() (net.Listener, error) {
	var lastErr error
	for port := basePort; port < basePort+portTries; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return listener, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no free port in [%d, %d): %w", basePort, basePort+portTries, lastErr)
}
