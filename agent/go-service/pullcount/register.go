package pullcount

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers pull count calculator custom components.
func Register() {
	maa.AgentServerRegisterCustomAction("PullCountCalculatorAction", &Action{})
}
