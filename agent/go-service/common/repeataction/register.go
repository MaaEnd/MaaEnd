package repeataction

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("RepeatUntilFoundAction", &RepeatUntilFoundAction{})
	maa.AgentServerRegisterCustomAction("RepeatUntilNotFoundAction", &RepeatUntilNotFoundAction{})
}
