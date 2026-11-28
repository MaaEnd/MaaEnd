package countdown

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	ActionName = `CountDownAction`
)

func Register() {
	err := maa.AgentServerRegisterCustomAction(ActionName, &Action{})
	if err != nil {
		log.Error().Err(err).Msg(ActionName + " register failed")
		return
	}
}
