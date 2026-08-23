package listcomplete

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomRecognition("ListCompleteRecognition", &Recognition{})
	maa.AgentServerRegisterCustomRecognition("ScrollbarRecognition", &ScrollbarRecognition{})
	maa.AgentServerRegisterCustomRecognition("ScrollbarCompleteRecognition", &ScrollbarCompleteRecognition{})
}
