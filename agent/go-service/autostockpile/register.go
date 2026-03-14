package autostockpile

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册 autostockpile 包提供的自定义动作与识别器。
func Register() {
	maa.AgentServerRegisterCustomAction("AutoStockpile.SelectItem", &SelectItemAction{})
	maa.AgentServerRegisterCustomRecognition("AutoStockpile.Recognition", &ItemValueChangeRecognition{})
}
