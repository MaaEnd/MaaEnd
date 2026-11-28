package yuanyuanmanor

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register registers Yuanyuan Manor custom recognitions and actions with the Maa agent server.
func Register() {
	maa.AgentServerRegisterCustomRecognition(recognizeMenuName, &MenuRecognition{})
	maa.AgentServerRegisterCustomAction(decideMenuName, &MenuDecideAction{})
	maa.AgentServerRegisterCustomAction(checkMenuCardName, &MenuCheckCardAction{})
	maa.AgentServerRegisterCustomAction("YuanyuanManorInnkeepingDecide", &DecisionAction{})

	log.Info().
		Str("component", component).
		Msg("yuanyuanmanor custom recognition/actions registered")
}
