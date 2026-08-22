package autodelivery

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the shared automatic-delivery components.
func Register() {
	maa.AgentServerRegisterCustomAction(autoDeliveryResolveDestinationComponent, &AutoDeliveryResolveDestinationAction{})
}
