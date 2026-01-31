package importtask

import "github.com/MaaXYZ/maa-framework-go/v3"

var (
	_ maa.CustomActionRunner = &ImportBluePrintsInitTextAction{}
	_ maa.CustomActionRunner = &ImportBluePrintsFinishAction{}
	_ maa.CustomActionRunner = &ImportBluePrintsEnterCodeAction{}
)

// Register registers all custom action components for importtask package
func Register() {
	maa.AgentServerRegisterCustomAction("ImportBluePrintsInitTextAction", &ImportBluePrintsInitTextAction{})
	maa.AgentServerRegisterCustomAction("ImportBluePrintsFinishAction", &ImportBluePrintsFinishAction{})
	maa.AgentServerRegisterCustomAction("ImportBluePrintsEnterCodeAction", &ImportBluePrintsEnterCodeAction{})
}
