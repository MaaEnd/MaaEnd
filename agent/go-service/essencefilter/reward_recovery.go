package essencefilter

import (
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type EssenceRewardFailedAction struct{}

var _ maa.CustomActionRunner = &EssenceRewardFailedAction{}

// Discard the current batch and leave the account's last published inventory intact.
func (*EssenceRewardFailedAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	inventoryTasks.Lock()
	account := inventoryTasks.byID[arg.TaskID]
	inventoryTasks.Unlock()
	if account != nil {
		account.release()
	}
	if st := currentRun; st != nil && st.TaskID == arg.TaskID {
		st.Loot = nil
	}
	reportSimpleByKey(ctx, "inventory.reward_pending")
	return true
}

func readRewardMark(ctx *maa.Context, ctrl *maa.Controller) (string, error) {
	if ctrl == nil {
		return "", fmt.Errorf("missing controller")
	}
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return "", fmt.Errorf("missing detail frame")
	}
	return recognizeDetailMark(func(suffix string) (bool, error) {
		result, err := ctx.RunRecognition("EssenceFilterAfterBattle"+suffix, img, nil)
		return result != nil && result.Hit, err
	})
}

// A visible active mark identifies the state. Only an unmarked item needs both inactive buttons.
func recognizeDetailMark(hit func(string) (bool, error)) (string, error) {
	locked, err := hit("CheckLocked")
	if err != nil {
		return "", err
	}
	discard, err := hit("CheckDiscarded")
	if err != nil {
		return "", err
	}
	switch {
	case locked && discard:
		return "", fmt.Errorf("detail shows both locked and discarded")
	case locked:
		return lockedMark, nil
	case discard:
		return discarded, nil
	}
	for _, node := range []string{"ClickLockItem", "ClickDiscardItem"} {
		visible, err := hit(node)
		if err != nil {
			return "", err
		}
		if !visible {
			return "", fmt.Errorf("unconfirmed unmarked detail: %s not visible", node)
		}
	}
	return unmarked, nil
}
