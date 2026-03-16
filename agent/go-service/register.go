package main

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/aspectratio"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autoecofarm"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autofight"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autostockpile"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/batchaddfriends"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/blueprintimport"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/charactercontroller"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/clearhitcount"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/dailyrewards"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/hdrcheck"
	maptracker "github.com/MaaXYZ/MaaEnd/agent/go-service/map-tracker"
	puzzle "github.com/MaaXYZ/MaaEnd/agent/go-service/puzzle-solver"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/quantizedsliding"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/resell"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/subtask"
	"github.com/rs/zerolog/log"
)

func registerAll() {
	// Pre-Check Custom
	aspectratio.Register()
	hdrcheck.Register()

	// General Custom
	subtask.Register()
	clearhitcount.Register()

	// Business Custom
	blueprintimport.Register()
	charactercontroller.Register()
	resell.Register()
	puzzle.Register()
	quantizedsliding.Register()
	essencefilter.Register()
	dailyrewards.Register()
	maptracker.Register()
	batchaddfriends.Register()
	autoecofarm.Register()
	autofight.Register()
	autostockpile.Register()
	log.Info().
		Msg("All custom components and sinks registered successfully")
}
