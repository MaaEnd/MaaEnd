package shakecamera

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("ShakeCamera", &ShakeCameraAction{})
}
