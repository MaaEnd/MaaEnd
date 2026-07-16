package dailyrewards

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers all custom recognition and action components for dailyrewards package
func Register() {
	maa.AgentServerRegisterCustomRecognition("DailyEventGoToRecognition", &DailyEventGoToRecognition{})
}
