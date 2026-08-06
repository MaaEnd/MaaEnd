package cultivationcalc

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers cultivation-calc custom components.
func Register() {
	maa.AgentServerRegisterCustomAction("EvaluateCultivationBundle", &EvaluateCultivationBundle{})
}
