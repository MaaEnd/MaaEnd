package essence

// Weapon mirrors the essence-planner weapon schema.
// Weapon 对应 essence-planner 的武器结构。
// 对应 window.WEAPONS 中的字段。
type Weapon struct {
	Name   string   `json:"name"`
	Short  string   `json:"short,omitempty"`
	Chars  []string `json:"chars,omitempty"`
	Rarity int      `json:"rarity"`
	Type   string   `json:"type"`
	S1     string   `json:"s1"`
	S2     string   `json:"s2"`
	S3     string   `json:"s3"`
}

