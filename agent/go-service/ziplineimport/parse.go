//go:build linux

package ziplineimport

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
)

// markDTO 对应 mark/list 响应里 data.marks / data.saveMarks 的单条标记。
//
// 滑索数据在 data.saveMarks（data.marks 是官方点位/资源/敌人）。pos 的 x/z 张成水平面、
// y 是高度，与 Ziplines.json 的坐标语义一致。pos 缺省时为 nil（连线之类没有落点的标记）。
type markDTO struct {
	TemplateID string   `json:"templateId"`
	MapID      string   `json:"mapId"`
	LevelID    string   `json:"levelId"`
	Pos        *markPos `json:"pos"`
}

type markPos struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type marksPayload struct {
	Data struct {
		// marks 是官方点位/资源/敌人，非滑索，解析时有意忽略（与 cpp 一致）。
		// 保留字段仅是记录接口结构，代码不读它。
		Marks     []markDTO `json:"marks"`
		SaveMarks []markDTO `json:"saveMarks"`
	} `json:"data"`
}

// capturedResponse 是一条被抄下的 mark/list 响应（URL 与响应体）。
type capturedResponse struct {
	url  string
	body []byte
}

// isMarkListResponse 判断请求是否命中 mark/list 接口。只匹配路径片段，
// 避免被 query 里的参数出现顺序影响。
func isMarkListResponse(path string) bool {
	return strings.Contains(path, "/map/mark/list")
}

// saveMarksPresent 判断响应是否为合法的 mark/list（至少带 data.saveMarks 结构）。
// 未登录时 saveMarks 是空数组，也算 present（只是内容为空）。
func saveMarksPresent(body []byte) bool {
	var p marksPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return false
	}
	return p.Data.SaveMarks != nil
}

// queryValue 取 URL query 里某个参数的值；取不到返回空串。
func queryValue(rawurl, key string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

// marksByMap 解析一份 mark/list 响应，按每条标记自己的 mapId 归集（标记缺 mapId 时
// 回退到请求 URL 上的 mapId），再按 templateIDs 过滤、丢弃没有 pos 的标记。
// 返回 mapId -> 滑索列表。
//
// 与 cpp 一致，只读 data.saveMarks（用户的滑索标记）；data.marks 是官方点位/资源/敌人，
// 不是滑索，绝不能并入——否则未登录时 marks 非空会把 covered 撑满、导致提前判成抓齐。
func marksByMap(body []byte, templateIDs []string, fallbackMapID string) map[string][]ziplineMark {
	var p marksPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil
	}
	out := make(map[string][]ziplineMark)
	for _, m := range p.Data.SaveMarks {
		if len(templateIDs) > 0 && !contains(templateIDs, m.TemplateID) {
			continue
		}
		if m.Pos == nil {
			continue
		}
		mapID := m.MapID
		if mapID == "" {
			mapID = fallbackMapID
		}
		if mapID == "" {
			continue
		}
		out[mapID] = append(out[mapID], ziplineMark{
			TemplateID: m.TemplateID,
			LevelID:    m.LevelID,
			X:          m.Pos.X,
			Y:          m.Pos.Y,
			Z:          m.Pos.Z,
		})
	}
	return out
}

// accountScopedMarks 把本次抓到的响应归集为「唯一账号 + 按地图分组的标记」。
// 返回值 accountID 为伪匿名哈希；roleID 为原始网页 roleId（仅供提示，调用方不得落盘）。
//
// 与 cpp PersistCaptured 一致：只有带回非空 saveMarks 的响应才参与账号判定；roleId 缺失
// 或格式非法的响应一律忽略，既不推进 covered 也不落盘。一次导入必须恰好对应一个 roleId，
// 否则返回错误让调用方整批拒绝——宁可本次不保存，也不能把两个账号的坐标混在一起，或猜
// 一个账号归属。
//
// templateIDs 为空表示不过滤，全量保留（供电结构必须随滑索架一并入库）。
func accountScopedMarks(responses []capturedResponse, templateIDs []string) (accountID string, roleID string, byMap map[string][]ziplineMark, err error) {
	byMap = make(map[string][]ziplineMark)
	roleIDs := make([]string, 0, 1)
	seenRoleIDs := make(map[string]bool)
	for _, r := range responses {
		fallbackMapID := queryValue(r.url, "mapId")
		// 先不过滤地解析一次：只有真的带标记的响应才需要判定账号，登录前的公开空列表
		// 不应该污染账号集合。
		if len(marksByMap(r.body, nil, fallbackMapID)) == 0 {
			continue
		}

		rawRoleID := queryValue(r.url, "roleId")
		if !captureuid.IsValidRawUID(rawRoleID) {
			log.Debug().Str("component", componentName).Int("role_id_len", len(rawRoleID)).
				Msg("zipline import: ignore mark response without valid roleId")
			continue
		}
		if !seenRoleIDs[rawRoleID] {
			seenRoleIDs[rawRoleID] = true
			roleIDs = append(roleIDs, rawRoleID)
		}

		for mapID, marks := range marksByMap(r.body, templateIDs, fallbackMapID) {
			byMap[mapID] = append(byMap[mapID], marks...)
		}
	}

	if len(roleIDs) != 1 {
		return "", "", nil, fmt.Errorf("one import must contain exactly one roleId, got %d", len(roleIDs))
	}
	accountID, err = captureuid.AccountIDFromRawUID(roleIDs[0])
	if err != nil {
		return "", "", nil, fmt.Errorf("derive account identity: %w", err)
	}
	return accountID, roleIDs[0], byMap, nil
}

// dedupMarks 去掉完全重合（template_id/level_id/x/y/z 完全相同）的重复标记，并按该键
// 排定落盘顺序。与 cpp PersistCaptured 的去重逻辑一致。
func dedupMarks(marks []ziplineMark) []ziplineMark {
	if len(marks) == 0 {
		return marks
	}
	key := func(m ziplineMark) string {
		return m.TemplateID + "\x00" + m.LevelID + "\x00" +
			strconv.FormatFloat(m.X, 'g', -1, 64) + "\x00" +
			strconv.FormatFloat(m.Y, 'g', -1, 64) + "\x00" +
			strconv.FormatFloat(m.Z, 'g', -1, 64)
	}
	sort.SliceStable(marks, func(i, j int) bool { return key(marks[i]) < key(marks[j]) })
	out := marks[:0]
	var last string
	for i, m := range marks {
		k := key(m)
		if i == 0 || k != last {
			out = append(out, m)
			last = k
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
