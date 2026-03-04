package inputreplay

import "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("ReplayRecordedInput", &ReplayRecordedInput{})
}
