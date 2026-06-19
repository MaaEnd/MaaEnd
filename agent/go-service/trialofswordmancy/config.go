package trialofswordmancy

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// partialConfig 是节点参数里可选的基础设定片段。指针字段：nil 表示缺省、用默认值。
type partialConfig struct {
	Deck         *[5]int  `json:"deck"`
	Reward       *[11]int `json:"reward"`
	MaxDouble    *int     `json:"maxDouble"`
	OverflowMode *string  `json:"overflowMode"`
}

// loadConfigFromNode 从节点（TrialOfSwordmancyDecide）读取基础设定并解析出 solver.Config。
//
// 配置来源优先级：等级 4 默认值 < attach < custom_action_param（后者覆盖前者）。
// 缺省字段用等级 4 默认值补齐（迁移文档 §7.2 / 计划「Config 走接口封装」）。
//
// 走 ctx.GetNodeJSON 而非 arg.CustomActionParam：前者能看到 task option 的
// pipeline_override 合并后的运行时配置（见 memory: getnodejson-sees-interface-overrides），
// 这样换牌库/换溢出模式只需在 option 里 override 节点参数，无需改代码。
func loadConfigFromNode(ctx *maa.Context, nodeName string) (solver.Config, error) {
	nodeJSON, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return solver.DefaultConfig, fmt.Errorf("get node json %s: %w", nodeName, err)
	}

	var node struct {
		CustomActionParam json.RawMessage `json:"custom_action_param"`
		Attach            json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(nodeJSON), &node); err != nil {
		return solver.DefaultConfig, fmt.Errorf("parse node json %s: %w", nodeName, err)
	}

	cfg := solver.DefaultConfig
	// attach 先叠加，custom_action_param 后叠加（优先级更高）
	if err := applyPartial(&cfg, node.Attach); err != nil {
		return solver.DefaultConfig, fmt.Errorf("parse attach of %s: %w", nodeName, err)
	}
	if err := applyPartial(&cfg, node.CustomActionParam); err != nil {
		return solver.DefaultConfig, fmt.Errorf("parse custom_action_param of %s: %w", nodeName, err)
	}
	return cfg, nil
}

func applyPartial(cfg *solver.Config, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var p partialConfig
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if p.Deck != nil {
		cfg.Deck = *p.Deck
	}
	if p.Reward != nil {
		cfg.Reward = *p.Reward
	}
	if p.MaxDouble != nil {
		cfg.MaxDouble = *p.MaxDouble
	}
	if p.OverflowMode != nil {
		mode, err := parseOverflowModeString(*p.OverflowMode)
		if err != nil {
			return err
		}
		cfg.OverflowMode = mode
	}
	return nil
}

func parseOverflowModeString(s string) (solver.OverflowMode, error) {
	switch s {
	case "OverflowNone":
		return solver.OverflowNone, nil
	case "OverflowOnce":
		return solver.OverflowOnce, nil
	case "OverflowTwice":
		return solver.OverflowTwice, nil
	default:
		return solver.OverflowTwice, fmt.Errorf("invalid overflowMode %q", s)
	}
}

// —— 求解器缓存：按 Config 哈希键，Config 变化才重新 Solve ——
var (
	solverCacheMu sync.Mutex
	solverCache   = map[string]*solver.Solver{}
)

// solverFor 返回（必要时构造并预求解）给定配置的 *solver.Solver。
// 每步查询复用同一实例，仅 Config 变化（牌库刷新 / 用户换溢出模式）时才重 Solve。
func solverFor(cfg solver.Config) *solver.Solver {
	key := solver.ConfigKey(cfg)
	solverCacheMu.Lock()
	defer solverCacheMu.Unlock()
	if s, ok := solverCache[key]; ok {
		return s
	}
	s := solver.NewSolver(cfg)
	s.Solve() // 预求解并缓存
	solverCache[key] = s
	return s
}
