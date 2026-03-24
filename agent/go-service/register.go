package main

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autoecofarm"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autofight"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autostockpile"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/batchaddfriends"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/blueprintimport"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/autoaltclick"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/charactercontroller"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/clearhitcount"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/subtask"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/creditshopping"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/dailyrewards"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/expressionrecognition"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/itemtransfer"
	maptracker "github.com/MaaXYZ/MaaEnd/agent/go-service/map-tracker"
	puzzle "github.com/MaaXYZ/MaaEnd/agent/go-service/puzzle-solver"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/quantizedsliding"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/resell"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/scenemanager"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/taskersink/aspectratio"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/taskersink/hdrcheck"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/taskersink/processcheck"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/visitfriends"
	"github.com/rs/zerolog/log"
)

func registerAll() {
	// Pre-Check Custom
	aspectratio.Register()
	hdrcheck.Register()
	processcheck.Register()

	// General Custom
	subtask.Register()
	clearhitcount.Register()
	expressionrecognition.Register()
	autoaltclick.Register()
	charactercontroller.Register()

	// Business Custom
	blueprintimport.Register()
	resell.Register()
	puzzle.Register()
	quantizedsliding.Register()
	essencefilter.Register()
	dailyrewards.Register()
	creditshopping.Register()
	maptracker.Register()
	batchaddfriends.Register()
	autoecofarm.Register()
	autofight.Register()
	visitfriends.Register()
	scenemanager.Register()
	autostockpile.Register()
	itemtransfer.Register()
	log.Info().
		Msg("All custom components and sinks registered successfully")
}
