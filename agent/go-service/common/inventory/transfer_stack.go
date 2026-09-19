package inventory

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// TransferStackAction 对指定物品执行一组转移，具体输入方式由平台资源决定。
type TransferStackAction struct{}

var _ maa.CustomActionRunner = &TransferStackAction{}

// Run 使用 Pipeline 已解析的目标框执行操作，返回值不代表物品数量验证结果。
func (a *TransferStackAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	return runTransfer(ctx, arg, "InventoryTransferStackAction", "__InventoryTransferStack", "stack")
}
