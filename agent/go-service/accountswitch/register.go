package accountswitch

import maa "github.com/MaaXYZ/maa-framework-go/v4"

const (
	componentName = "accountswitch"

	windowActionName = "AccountSwitchWindowAction"
)

func Register() {
	maa.AgentServerRegisterCustomAction(windowActionName, &WindowAction{})
}

var _ maa.CustomActionRunner = (*WindowAction)(nil)
