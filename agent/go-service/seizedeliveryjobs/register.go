package seizedeliveryjobs

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentName = "SeizeDeliveryJobs"

// Register registers all SeizeDeliveryJobs custom recognitions and actions.
func Register() {
	registerCustomRecognition("SeizeDeliveryJobsFindTargetRecognition", &SeizeDeliveryJobsFindTargetRecognition{})
	registerCustomRecognition("SeizeDeliveryJobsScanTargetRecognition", &SeizeDeliveryJobsScanTargetRecognition{})
	registerCustomAction("SeizeDeliveryJobsScanTargetAction", &SeizeDeliveryJobsScanTargetAction{})
	registerCustomAction("SeizeDeliveryJobsNoProgressAction", &SeizeDeliveryJobsNoProgressAction{})
	registerCustomAction("SeizeDeliveryJobsResetScanStateAction", &SeizeDeliveryJobsResetScanStateAction{})
}

// registerCustomRecognition registers a recognition and logs failures with its component context.
func registerCustomRecognition(name string, runner maa.CustomRecognitionRunner) {
	if err := maa.AgentServerRegisterCustomRecognition(name, runner); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("recognition", name).
			Msg("failed to register custom recognition")
	}
}

// registerCustomAction registers an action and logs failures with its component context.
func registerCustomAction(name string, runner maa.CustomActionRunner) {
	if err := maa.AgentServerRegisterCustomAction(name, runner); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("action", name).
			Msg("failed to register custom action")
	}
}
