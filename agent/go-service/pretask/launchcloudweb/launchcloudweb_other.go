//go:build !windows

package launchcloudweb

import "github.com/rs/zerolog/log"

func run() bool {
	log.Error().
		Str("component", component).
		Msg("cloud web shell only supports Windows")
	return false
}
