package autodelivery

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册 AutoDelivery 的公共自定义组件。
func Register() {
	maa.AgentServerRegisterCustomAction(resolveDepotActionName, &AutoDeliveryResolveDepotAction{})
	maa.AgentServerRegisterCustomAction(resolveDestinationActionName, &AutoDeliveryResolveDestinationAction{})
}
