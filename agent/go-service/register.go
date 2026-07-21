package main

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/accountswitch"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autoecofarm"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autofight"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autosell"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autostockpile"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/autostockstaple"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/batchaddfriends"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/bettersliding"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/blueprintimport"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/attachregex"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/autoalt"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/charactercontroller"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/clearhitcount"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/expendable"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/expressionrecognition"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/failurecollector"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/falseaction"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/listcomplete"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/pipelineoverride"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/poststop"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/repeataction"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/schedule"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/subtask"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/creditshopping"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/dijiangrewards"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/itemtransfer"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pullcount"
	puzzle "github.com/MaaXYZ/MaaEnd/agent/go-service/puzzle-solver"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/scenemanager"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/seizedeliveryjobs"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/taskersink/aspectratio"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/taskersink/cursormove"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/taskersink/hdrcheck"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/taskersink/processcheck"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/taskersink/taskfail"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy"
	webevent202605 "github.com/MaaXYZ/MaaEnd/agent/go-service/webevent/202605"
	"github.com/rs/zerolog/log"
)

func registerAll() {
	// Resource Sink
	resource.EnsureResourcePathSink()

	// Pre-Check Custom
	aspectratio.Register()
	hdrcheck.Register()
	processcheck.Register()
	taskfail.Register()
	cursormove.Register()

	// General Custom
	subtask.Register()
	failurecollector.Register()
	clearhitcount.Register()
	pipelineoverride.Register()
	expressionrecognition.Register()
	listcomplete.Register()
	expendable.Register()
	attachregex.Register()
	autoalt.Register()
	charactercontroller.Register()
	falseaction.Register()
	repeataction.Register()
	poststop.Register()
	schedule.Register()

	// Business Custom
	accountswitch.Register()
	captureuid.Register()
	autosell.Register()
	blueprintimport.Register()
	puzzle.Register()
	bettersliding.Register()
	essencefilter.Register()
	dijiangrewards.Register()
	maptracker.Register()
	batchaddfriends.Register()
	autoecofarm.Register()
	autofight.Register()
	scenemanager.Register()
	seizedeliveryjobs.Register()
	autostockstaple.Register()
	autostockpile.Register()
	itemtransfer.Register()
	sellproduct.Register()
	creditshopping.Register()
	webevent202605.Register()
	pullcount.Register()
	trialofswordmancy.Register()
	log.Info().
		Msg("All custom components and sinks registered successfully")
}
