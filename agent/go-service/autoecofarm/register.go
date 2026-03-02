package autoecofarm

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomActionRunner = &MoveToTarget3D{}
	_ maa.CustomActionRunner = &MoveToTargetPart{}
)

// Register registers the aspect ratio checker as a tasker sink
func Register() {
	maa.AgentServerRegisterCustomAction("MoveToTarget3D", &MoveToTarget3D{})
	maa.AgentServerRegisterCustomAction("MoveToTargetPart", &MoveToTargetPart{})
}
