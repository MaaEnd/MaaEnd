package textinput

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("TextInputInitAction", &InitAction{})
	maa.AgentServerRegisterCustomAction("TextInputAction", &InputAction{})
	maa.AgentServerRegisterCustomRecognition("TextInputExhaustedRecognition", &ExhaustedRecognition{})
}
