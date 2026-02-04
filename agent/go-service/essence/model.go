package essence

// Weapon 定义了从 endfield-essence-planner 导出的武器数据结构。
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

// Dungeon 定义了副本数据结构。
// 对应 window.DUNGEONS 中的字段。
type Dungeon struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	S2Pool []string `json:"s2_pool"`
	S3Pool []string `json:"s3_pool"`
}

