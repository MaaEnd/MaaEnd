package inventory

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// TransferHalfAction 对指定物品执行一半转移，具体输入方式由平台资源决定。
type TransferHalfAction struct{}

var _ maa.CustomActionRunner = &TransferHalfAction{}

// Run 使用 Pipeline 已解析的目标框执行操作，返回值不代表物品数量验证结果。
func (a *TransferHalfAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	return runTransfer(ctx, arg, "InventoryTransferHalfAction", "__InventoryTransferHalf", "half")
}
