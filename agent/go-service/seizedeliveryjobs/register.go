package seizedeliveryjobs

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentName = "SeizeDeliveryJobs"

// Register 注册 SeizeDeliveryJobs 的全部自定义识别与动作。
func Register() {
	registerCustomRecognition("SeizeDeliveryJobsFindTargetRecognition", &SeizeDeliveryJobsFindTargetRecognition{})
	registerCustomRecognition("SeizeDeliveryJobsScanTargetRecognition", &SeizeDeliveryJobsScanTargetRecognition{})
	registerCustomAction("SeizeDeliveryJobsScanTargetAction", &SeizeDeliveryJobsScanTargetAction{})
	registerCustomAction("SeizeDeliveryJobsNoProgressAction", &SeizeDeliveryJobsNoProgressAction{})
	registerCustomAction("SeizeDeliveryJobsResetScanStateAction", &SeizeDeliveryJobsResetScanStateAction{})
}

// registerCustomRecognition 注册自定义识别，并记录包含组件上下文的失败日志。
func registerCustomRecognition(name string, runner maa.CustomRecognitionRunner) {
	if err := maa.AgentServerRegisterCustomRecognition(name, runner); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("recognition", name).
			Msg("failed to register custom recognition")
	}
}

// registerCustomAction 注册自定义动作，并记录包含组件上下文的失败日志。
func registerCustomAction(name string, runner maa.CustomActionRunner) {
	if err := maa.AgentServerRegisterCustomAction(name, runner); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("action", name).
			Msg("failed to register custom action")
	}
}
