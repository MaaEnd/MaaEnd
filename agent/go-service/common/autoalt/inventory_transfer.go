package autoalt

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// inventoryTransferNodes 描述一种转移方式的三个 Pipeline 子节点。
// PC 上依次为按下修饰键、点击物品、抬起修饰键；ADB 资源覆盖为按住物品、点击转移按钮、松开物品。
type inventoryTransferNodes struct {
	press   string
	hit     string
	release string
}

// InventoryTransferAction 对识别框中的物品执行一次仓库与背包之间的转移。
type InventoryTransferAction struct {
	name  string
	nodes inventoryTransferNodes
}

var _ maa.CustomActionRunner = &InventoryTransferAction{}

func newInventoryTransferAction(name, mode string) *InventoryTransferAction {
	prefix := "__InventoryTransfer" + mode
	return &InventoryTransferAction{
		name: name,
		nodes: inventoryTransferNodes{
			press:   prefix + "Press",
			hit:     prefix + "Hit",
			release: prefix + "Release",
		},
	}
}

func (a *InventoryTransferAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) (success bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", a.name).Msg("inventory transfer received nil context or arg")
		return false
	}
	if arg.Box.Width() <= 0 || arg.Box.Height() <= 0 {
		log.Error().
			Str("component", a.name).
			Interface("box", arg.Box).
			Msg("inventory transfer recognition box is empty")
		return false
	}

	// 即使中途失败也尝试抬起，避免修饰键或触点保持按下。
	defer func() {
		if !runPipelineAction(ctx, a.name, a.nodes.release, arg.Box) {
			success = false
		}
	}()

	if !runPipelineAction(ctx, a.name, a.nodes.press, arg.Box) {
		return false
	}

	// hit 用 RunTask 执行：ADB 上需要识别并点击转移按钮，PC 上只是一次点击。
	detail, err := ctx.RunTask(a.nodes.hit, map[string]any{
		a.nodes.hit: map[string]any{
			"target": rectToSlice(arg.Box),
		},
	})
	if err != nil || detail == nil || !detail.Status.Success() {
		log.Error().
			Err(err).
			Str("component", a.name).
			Str("node", a.nodes.hit).
			Bool("detail_present", detail != nil).
			Msg("inventory transfer hit task failed")
		return false
	}
	return true
}
