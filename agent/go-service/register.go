package main

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/aspectratio"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/creditshopping"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/dailyrewards"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/hdrcheck"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/importtask"
	maptracker "github.com/MaaXYZ/MaaEnd/agent/go-service/map-tracker"
	puzzle "github.com/MaaXYZ/MaaEnd/agent/go-service/puzzle-solver"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/realtime"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/resell"
	"github.com/rs/zerolog/log"
)

func registerAll() {
	// Register all custom components from each package
	realtime.Register()
	importtask.Register()
	resell.Register()
	puzzle.Register()
	essencefilter.Register()
	creditshopping.Register()
	dailyrewards.Register()
	maptracker.Register()

	// Register aspect ratio checker (uses TaskerSink, not custom action/recognition)
	aspectratio.Register()

	// Register HDR checker (uses TaskerSink, warns if HDR is enabled but doesn't stop task)
	hdrcheck.Register()

	log.Info().
		Msg("All custom components and sinks registered successfully")
}
