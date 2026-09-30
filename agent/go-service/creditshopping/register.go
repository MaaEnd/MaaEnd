package creditshopping

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册信用点购物扩展动作。
func Register() {
	maa.AgentServerRegisterCustomAction(creditShoppingScanItemActionName, &RecordShelfSnapshotsAction{})
}
