package autostockpile

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register 注册 autostockpile 包提供的自定义动作与识别器。
func Register() {
	if err := InitItemMap("zh_cn"); err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Msg("failed to init item map during registration, OCR name matching may be disabled")
	}

	maa.AgentServerRegisterCustomAction("AutoStockpile.SelectItem", &SelectItemAction{})
	maa.AgentServerRegisterCustomRecognition("AutoStockpile.Recognition", &ItemValueChangeRecognition{})
}
