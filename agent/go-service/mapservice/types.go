package mapservice

// SliceMeta `slice_meta.json` structure
type SliceMeta struct {
	OriginalFile string      `json:"original_file"`
	OriginalSize []int       `json:"original_size"`
	Grid         []int       `json:"grid"`
	Overlap      int         `json:"overlap"`
	Slices       []SliceInfo `json:"slices"`
	Calibration  Calibration `json:"calibration"`
}

type SliceInfo struct {
	Row       int    `json:"row"`
	Col       int    `json:"col"`
	Filename  string `json:"filename"`
	CropRect  []int  `json:"crop_rect"`  // [x1, y1, x2, y2]
	BaseRect  []int  `json:"base_rect"`  // [x1, y1, x2, y2]
	WorldRect []int  `json:"world_rect"` // [wx1, wy1, wx2, wy2]
}

type Calibration struct {
	Type  string  `json:"type"`
	Scale float64 `json:"scale"`
	Notes string  `json:"notes"`
}

type MapPosition struct {
	ZoneID     string  `json:"zone_id"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Score      float64 `json:"score"`
	SliceIndex int     `json:"slice_index"`
}
