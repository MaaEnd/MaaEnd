package batchaddfriends

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestPickNextRowLocked 校验挑行按「序号」而不是像素坐标：
// 黑名单里的序号被跳过，其余按从上到下依次返回。
func TestPickNextRowLocked(t *testing.T) {
	resetRejectedRows()
	buttons := []maa.Rect{
		{0, 100, 1, 1},
		{0, 200, 1, 1},
		{0, 300, 1, 1},
	}

	cases := []struct {
		name     string
		rejected []int
		wantIdx  int
		wantOK   bool
	}{
		{name: "全部可选时取第一行", rejected: nil, wantIdx: 0, wantOK: true},
		{name: "第一行被拒绝时取第二行", rejected: []int{0}, wantIdx: 1, wantOK: true},
		{name: "前两行被拒绝时取第三行", rejected: []int{0, 1}, wantIdx: 2, wantOK: true},
		{name: "全部被拒绝时无可选行", rejected: []int{0, 1, 2}, wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rejectedRows.mu.Lock()
			rejectedRows.indexes = tc.rejected
			idx, ok := pickNextRowLocked(buttons)
			rejectedRows.mu.Unlock()

			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && idx != tc.wantIdx {
				t.Fatalf("index = %d, want %d", idx, tc.wantIdx)
			}
		})
	}
}

// TestPickNextRowIgnoresY 校验同一序号在不同纵向位置都算同一行，
// 界面轻微位移不会让已拒绝的行重新被挑中。
func TestPickNextRowIgnoresY(t *testing.T) {
	resetRejectedRows()

	rejectedRows.mu.Lock()
	rejectedRows.indexes = []int{0}
	shifted := []maa.Rect{
		{0, 110, 1, 1},
		{0, 210, 1, 1},
	}
	idx, ok := pickNextRowLocked(shifted)
	rejectedRows.mu.Unlock()

	if !ok || idx != 1 {
		t.Fatalf("got idx=%d ok=%v, want idx=1 ok=true", idx, ok)
	}
}

// TestResetRejectedRows 校验任务开始时黑名单与「最近挑中行」都会被清空。
func TestResetRejectedRows(t *testing.T) {
	rejectedRows.mu.Lock()
	rejectedRows.indexes = []int{1, 3}
	rejectedRows.last = 2
	rejectedRows.hasLast = true
	rejectedRows.mu.Unlock()

	resetRejectedRows()

	rejectedRows.mu.Lock()
	defer rejectedRows.mu.Unlock()
	if len(rejectedRows.indexes) != 0 {
		t.Fatalf("indexes = %v, want empty", rejectedRows.indexes)
	}
	if rejectedRows.last != 0 || rejectedRows.hasLast {
		t.Fatalf("last = %d, hasLast = %v, want 0 / false", rejectedRows.last, rejectedRows.hasLast)
	}
}

// TestAvatarBox 校验头像点击框换算：X 用头像列常量，Y 取按钮框中心。
func TestAvatarBox(t *testing.T) {
	box := avatarBox(maa.Rect{120, 300, 60, 20})
	want := maa.Rect{pickRowAvatarCenterX, 300 + 20/2 + pickRowAvatarDy, pickRowAvatarSize, pickRowAvatarSize}
	if box != want {
		t.Fatalf("avatarBox = %v, want %v", box, want)
	}
}

// TestBumpEmptyBatch 校验连续空批计数：达到上限后才判定超限。
func TestBumpEmptyBatch(t *testing.T) {
	consecutiveEmptyBatches = 0
	defer func() { consecutiveEmptyBatches = 0 }()

	for i := 1; i <= maxConsecutiveEmptyBatches; i++ {
		count, exceeded := bumpEmptyBatch()
		if count != i {
			t.Fatalf("count = %d, want %d", count, i)
		}
		if exceeded {
			t.Fatalf("第 %d 次不应超限", i)
		}
	}

	count, exceeded := bumpEmptyBatch()
	if count != maxConsecutiveEmptyBatches+1 {
		t.Fatalf("count = %d, want %d", count, maxConsecutiveEmptyBatches+1)
	}
	if !exceeded {
		t.Fatalf("第 %d 次应超限", count)
	}
}
