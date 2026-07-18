package trialofswordmancy

import "sync"

// 剩余放弃次数的持久化（唯一带状态的字段）。
//
// 为何这一项要持久化、不能像其它字段那样每步从截图读：界面上不直接显示剩余放弃次数，
// 只有点击「放弃」后弹出的确认框里才写（「本日剩余放弃次数x次」/「已用完」）。
// 所以采用「探测-缓存」：未知(-1)时点一下放弃、OCR 弹窗、点取消回到正常界面，拿到次数并缓存；
// 之后步骤直接读缓存。
//
// 生命周期：
//   - 进程内初始化为 -1（未知）。
//   - 路由到 放弃(Abandon) 或 开始演算(Calculate) 时重置为 -1——前者因为放弃会扣 1 次（缓存失效），
//     后者作为回合结束的统一兜底，下回合首步重新探测。
var (
	abandMu    sync.Mutex
	abandCount = -1 // -1 = 未知，需探测
)

func getAband() int {
	abandMu.Lock()
	defer abandMu.Unlock()
	return abandCount
}

func setAband(n int) {
	abandMu.Lock()
	defer abandMu.Unlock()
	abandCount = n
}

// resetAband 把缓存的剩余放弃次数置为 -1（未知），下次 recognition 会重新探测。
func resetAband() {
	abandMu.Lock()
	defer abandMu.Unlock()
	abandCount = -1
}
