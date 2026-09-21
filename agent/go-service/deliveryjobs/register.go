package deliveryjobs

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册 DeliveryJobs 的自定义组件。
func Register() {
	maa.AgentServerRegisterCustomAction(resolveOngoingDepotActionName, &DeliveryJobsResolveOngoingDepotAction{})
}
