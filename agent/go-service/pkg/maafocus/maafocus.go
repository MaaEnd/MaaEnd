package maafocus

import (
	"errors"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

const nodeName = "_GO_SERVICE_FOCUS_"

// ErrNilContext indicates the provided context is nil.
var ErrNilContext = errors.New("context is nil")

// NodeActionStarting sets the focus to the node action starting event
// content is the content to be displayed on the UI
func NodeActionStarting(ctx *maa.Context, content string) error {
	if ctx == nil {
		return ErrNilContext
	}

	pp := maa.NewPipeline()
	pp.AddNode(maa.NewNode(nodeName,
		maa.WithFocus(map[string]any{
			maa.EventNodeAction.Starting(): content,
		}),
		maa.WithPreDelay(0),
		maa.WithPostDelay(0),
	))
	_, err := ctx.RunTask(nodeName, pp)
	return err
}
