package touchcheck

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestRejectionTaskIsolation(t *testing.T) {
	const rejectedID, independentID = int64(101), int64(102)
	t.Cleanup(func() { rejections.Delete(rejectedID) })
	rejections.Store(rejectedID, true)
	if !Rejected(rejectedID) || Rejected(independentID) {
		t.Fatal("input rejection leaked into another task")
	}
	for _, event := range []maa.EventStatus{maa.EventStatusSucceeded, maa.EventStatusFailed, maa.EventStatusStarting} {
		rejections.Store(rejectedID, true)
		(&Sink{}).OnTaskerTask(nil, event, maa.TaskerTaskDetail{TaskID: uint64(rejectedID)})
		if Rejected(rejectedID) {
			t.Fatalf("rejection was retained after lifecycle event %v", event)
		}
	}
}
