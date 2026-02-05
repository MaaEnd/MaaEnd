package baker

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type NodeClearHitCountAction struct{}

func (nu *NodeClearHitCountAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		NodeName string `json:"node_name"`
	}
	var needClearHitNodeName string

	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("[Baker]序列化参数失败" + err.Error())
		return false
	}

	needClearHitNodeName = params.NodeName
	if err := ctx.ClearHitCount(needClearHitNodeName); err != nil {
		log.Error().Err(err).Msg("[Baker]清理节点失败" + err.Error())
		return false
	}

	log.Info().Msg("[Baker]清理节点成功" + needClearHitNodeName)
	return true
}
