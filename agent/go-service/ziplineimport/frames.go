//go:build linux

package ziplineimport

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// 标定帧里的一条（只取本地图编号）。map_id 留空表示「不限地图」，不算进 expected。
type ziplineFrameDTO struct {
	ZoneName string `json:"zone_name"`
	MapID    string `json:"map_id"`
}

// 标定的滑索类型。只取 template_id；name 仅用于人工对照，代码不读。
type ziplineTypeDTO struct {
	TemplateID string `json:"template_id"`
	Name       string `json:"name"`
}

type framesFile struct {
	Frames []ziplineFrameDTO `json:"frames"`
	Types  []ziplineTypeDTO  `json:"types"`
}

// defaultFramesPath: <exe>/../data/MapNavigator/zipline_frames.json，
// 与 cpp ZiplineFrames::DefaultPath 相同，是「该抓齐哪些图」的标定来源。
func defaultFramesPath() string {
	return filepath.Join(exeDir(), "..", "data", "MapNavigator", "zipline_frames.json")
}

// loadFramesFile 读取标定帧文件。文件不存在或内容坏掉一律返回零值结构，
// 由调用方按空集处理，不报错——与 cpp ZiplineFrames::load 对缺文件的宽容一致。
func loadFramesFile() framesFile {
	var f framesFile
	raw, err := os.ReadFile(defaultFramesPath())
	if err != nil {
		return f
	}
	_ = json.Unmarshal(raw, &f)
	return f
}

// expectedMaps 返回去重后的非空 map_id 集合（即 cpp 的 mapIds() 语义），
// 是「该抓齐哪些图」的完成判据来源。
func expectedMaps() map[string]bool {
	out := make(map[string]bool)
	for _, fr := range loadFramesFile().Frames {
		if fr.MapID != "" {
			out[fr.MapID] = true
		}
	}
	return out
}

// towerTemplateIDs 返回标定 types[] 里声明的滑索架 template_id 集合。
// 只用于「导入完成」提示的展示口径与逐模板日志对照——落盘本身全量保留，不做过滤：
// 供电桩等供电结构必须随滑索架一并入库，寻路的通电判定依赖它们。
func towerTemplateIDs() map[string]bool {
	out := make(map[string]bool)
	for _, t := range loadFramesFile().Types {
		if t.TemplateID != "" {
			out[t.TemplateID] = true
		}
	}
	return out
}
