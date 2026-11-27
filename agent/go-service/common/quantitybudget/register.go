package quantitybudget

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// actionName 是注册名，同时作为日志里的 component。
const actionName = "QuantityBudget"

// Register 注册按预算反推滑条目标数量的自定义动作。
func Register() {
	maa.AgentServerRegisterCustomAction(actionName, &QuantityBudgetAction{})
}
