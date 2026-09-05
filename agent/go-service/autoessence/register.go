package autoessence

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction(actionLocationOptionsCheck, &LocationOptionsCheckAction{})
	maa.AgentServerRegisterCustomAction(actionApplyLocationEngraveOverride, &ApplyLocationEngraveOverrideAction{})
}
