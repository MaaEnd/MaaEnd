package gamelaunch

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

type launchOptions struct {
	Launcher struct {
		Path string `json:"Path"`
	} `json:"GameLaunchLauncher"`
}

// Run starts the CN PC game through the official launcher before controller connection.
// The Client appends the pretask option values as the final JSON argument.
func Run(args []string) bool {
	path, err := parseLauncherPath(args)
	if err == nil && path == "" {
		log.Info().Str("component", "gamelaunch").Msg("Official launcher path is empty; skipping game launch")
		return true
	}
	if err == nil {
		err = launch(path)
	}
	if err != nil {
		log.Error().Err(err).Str("component", "gamelaunch").Msg("Failed to launch game")
		return false
	}
	return true
}

func parseLauncherPath(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	var opts launchOptions
	if err := json.Unmarshal([]byte(args[len(args)-1]), &opts); err != nil {
		return "", fmt.Errorf("parse GameLaunch options: %w", err)
	}
	return strings.TrimSpace(opts.Launcher.Path), nil
}
