package batchaddfriends

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 本文件在「判定不达标、准备跳过这个玩家」时输出诊断信息。
//
// 为什么需要它：区间判定由 Pipeline 的 ExpressionRecognition 完成，它只回答
// 成立/不成立，不会说明是哪一项不满足。用户看到的就只有「明明等级够了却没加」。
// 这里把名片上读到的三个数值原样打出来，并点明是哪一项不达标。
const (
	componentRejectLog = "BatchAddFriendsStrangersRejectLogRecognition"

	// 界面上展示的字段名（i18n key）。
	labelAuthorityLevel = "batchaddfriends.level.authority"
	labelValleyIVLevel  = "batchaddfriends.level.valleyIV"
	labelWulingLevel    = "batchaddfriends.level.wuling"
	labelWarEchoLevel   = "batchaddfriends.level.warEcho"
)

// profileValueNode 是名片页上需要输出诊断的节点。
type profileValueNode struct {
	labelKey string
	node     string
}

var profileValueNodes = []profileValueNode{
	{labelAuthorityLevel, "BatchAddFriendsStrangersProfileAuthorityLevel"},
	{labelValleyIVLevel, "BatchAddFriendsStrangersProfileValleyIVLevel"},
	{labelWulingLevel, "BatchAddFriendsStrangersProfileWulingLevel"},
}

// profileFlagNode 是需要输出「有 / 无」的节点（模板匹配类）。
//
// 注意：开关关闭时该节点是 DirectHit，恒命中，日志会显示「有」；
// 这时候本来也没在检查它，显示「有」等价于「不拦」。
var profileFlagNodes = []profileValueNode{
	{labelWarEchoLevel, "BatchAddFriendsStrangersProfileWarEcho"},
}

// RejectLogRecognition 读取名片上的三个数值并写日志/推送提示，随后无条件命中，
// 让流程继续走「关闭名片 → 记入黑名单」。
//
// 参数 failed 是「不达标项」的 i18n key（由 Pipeline 静态写死，不含占位符），
// 这样日志第一句就能直接点出原因，而不是让人自己从三个数值里找。
type RejectLogRecognition struct{}

var _ maa.CustomRecognitionRunner = &RejectLogRecognition{}

// Run implements maa.CustomRecognitionRunner.
// 输出本次判定不达标的原因，以及名片上实际读到的数值。
func (r *RejectLogRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}

	var params struct {
		Failed string `json:"failed"`
		// 战争回响增辉是否参与判定：由任务选项注入。
		// 关闭时不要在日志里出现这一项，否则 DirectHit 会恒显示「有」，看着像真的检测到了。
		Echo bool `json:"echo"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(arg.CustomRecognitionParam)), &params); err != nil {
		// 参数损坏时字段会退化为零值：failed 回退成通用文案、echo 关闭。
		// 保留回退行为（少一项总比不出日志好），但要留下线索，避免排查时无从下手。
		log.Warn().
			Err(err).
			Str("component", componentRejectLog).
			Str("param", arg.CustomRecognitionParam).
			Msg("reject log param parse failed, fallback to defaults")
	}

	failedKey := strings.TrimSpace(params.Failed)
	if failedKey == "" {
		// 不能回退到某个具体条件（比如「权限等级」），否则参数丢失时会把真正的
		// 失败原因伪装成那一项，比不显示更容易误导人。
		failedKey = "batchaddfriends.reject.condition"
	}

	parts := make([]string, 0, len(profileValueNodes)+len(profileFlagNodes))
	for _, item := range profileValueNodes {
		parts = append(parts, fmt.Sprintf("%s=%s", i18n.T(item.labelKey), readValue(ctx, arg, item.node)))
	}
	if params.Echo {
		for _, item := range profileFlagNodes {
			parts = append(parts, fmt.Sprintf("%s=%s", i18n.T(item.labelKey), readFlag(ctx, arg, item.node)))
		}
	}

	message := i18n.T("batchaddfriends.reject.skipped", i18n.T(failedKey), strings.Join(parts, "，"))
	log.Info().Str("component", componentRejectLog).Str("failed", failedKey).Msg(message)
	maafocus.Print(ctx, message)

	// 返回值本身不参与点击（后续节点用自己的 X 模板定位），给一个占位框即可。
	return &maa.CustomRecognitionResult{Box: maa.Rect{0, 0, 1, 1}}, true
}

// readFlag 读一个「有 / 无」节点，返回便于阅读的字符串。
func readFlag(ctx *maa.Context, arg *maa.CustomRecognitionArg, node string) string {
	detail, err := ctx.RunRecognition(node, arg.Img, nil)
	if err != nil || detail == nil {
		return i18n.T("batchaddfriends.reject.unreadable")
	}
	if detail.Hit {
		return i18n.T("batchaddfriends.reject.present")
	}
	return i18n.T("batchaddfriends.reject.absent")
}

// readValue 读一个数值节点的结果，返回便于阅读的字符串。
func readValue(ctx *maa.Context, arg *maa.CustomRecognitionArg, node string) string {
	detail, err := ctx.RunRecognition(node, arg.Img, nil)
	if err != nil || detail == nil || !detail.Hit {
		return i18n.T("batchaddfriends.reject.unreadable")
	}
	value, err := ocrnum.Extract(detail)
	if err != nil {
		return i18n.T("batchaddfriends.reject.unparsable")
	}
	return fmt.Sprintf("%d", value)
}
