package sellproduct

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/operator"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// Register 向 MaaAgentServer 注册本包中的所有 Custom 组件。
func Register() {
	goods.Register()
	operator.Register()
	maa.AgentServerRegisterCustomAction(locationPlanActionName, &LocationPlanAction{})
}
