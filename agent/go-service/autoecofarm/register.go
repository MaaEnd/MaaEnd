package autoecofarm

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomActionRunner = &MoveToTarget3D{}
)

// Register registers the aspect ratio checker as a tasker sink
func Register() {
	maa.AgentServerRegisterCustomAction("MoveToTarget3D", &MoveToTarget3D{})
}
