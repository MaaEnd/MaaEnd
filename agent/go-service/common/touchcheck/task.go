package touchcheck

import (
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// Rejections contain task IDs only, never borrowed callback handles.
var rejections sync.Map

// Rejected reports whether this task has already failed its touch capability check.
func Rejected(taskID int64) bool {
	_, rejected := rejections.Load(taskID)
	return rejected
}

// Propagate prevents a parent Custom action from swallowing an input rejection.
func Propagate(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil || !Rejected(arg.TaskID) {
		return false
	}
	PreventRecovery(ctx, arg.CurrentTaskName)
	return true
}

// Sink releases rejection state when the owning task ends; other tasks are independent.
type Sink struct{}

var _ maa.TaskerEventSink = &Sink{}

// OnTaskerTask clears task-scoped state at lifecycle boundaries.
func (s *Sink) OnTaskerTask(_ *maa.Tasker, _ maa.EventStatus, detail maa.TaskerTaskDetail) {
	rejections.Delete(int64(detail.TaskID))
}
