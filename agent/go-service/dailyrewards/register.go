package dailyrewards

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomRecognitionRunner = &DailyEventUnreadItemInitRecognition{}
	_ maa.CustomRecognitionRunner = &DailyEventUnreadItemSwitchRecognition{}
	_ maa.CustomRecognitionRunner = &DailyEventUnreadDetailInitRecognition{}
	_ maa.CustomRecognitionRunner = &DailyEventUnreadDetailPickRecognition{}
)

// Register registers all custom recognition and action components for dailyrewards package
func Register() {
	maa.AgentServerRegisterCustomRecognition("DailyEventUnreadItemInitRecognition", &DailyEventUnreadItemInitRecognition{})
	maa.AgentServerRegisterCustomRecognition("DailyEventUnreadItemSwitchRecognition", &DailyEventUnreadItemSwitchRecognition{})
	maa.AgentServerRegisterCustomRecognition("DailyEventUnreadDetailInitRecognition", &DailyEventUnreadDetailInitRecognition{})
	maa.AgentServerRegisterCustomRecognition("DailyEventUnreadDetailPickRecognition", &DailyEventUnreadDetailPickRecognition{})
}
