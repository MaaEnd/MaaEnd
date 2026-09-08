package inventory

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// TransferAllAction 对指定物品执行全部转移，具体输入方式由平台资源决定。
type TransferAllAction struct{}

var _ maa.CustomActionRunner = &TransferAllAction{}

// Run 使用 Pipeline 已解析的目标框执行操作，返回值不代表物品数量验证结果。
func (a *TransferAllAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	return runTransfer(ctx, arg, "InventoryTransferAllAction", "__InventoryTransferAll", "all")
}
