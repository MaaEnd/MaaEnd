package blueprintimport

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomActionRunner = &ImportBluePrintsInitTextAction{}
	_ maa.CustomActionRunner = &ImportBluePrintsFinishAction{}
	_ maa.CustomActionRunner = &ImportBluePrintsEnterCodeAction{}
)

// Register registers all custom action components for blueprintimport package
func Register() {
	maa.AgentServerRegisterCustomAction("ImportBluePrintsInitTextAction", &ImportBluePrintsInitTextAction{})
	maa.AgentServerRegisterCustomAction("ImportBluePrintsFinishAction", &ImportBluePrintsFinishAction{})
	maa.AgentServerRegisterCustomAction("ImportBluePrintsEnterCodeAction", &ImportBluePrintsEnterCodeAction{})
}
