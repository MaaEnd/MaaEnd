package essencefilter

import (
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	_ maa.ResourceEventSink = &resourcePathSink{}
)

func Register() {
	fmt.Println("[EssenceFilter] Registering Sink...")
	maa.AgentServerAddResourceSink(&resourcePathSink{})

	fmt.Println("========== [EssenceFilter] Register() called ==========")
	log.Info().Msg("[EssenceFilter] 开始注册 Custom Actions")

	fmt.Println("[EssenceFilter] Registering EssenceFilterInitAction...")
	maa.AgentServerRegisterCustomAction("EssenceFilterInitAction", &EssenceFilterInitAction{})
	log.Info().Msg("[EssenceFilter] 已注册 EssenceFilterInitAction")

	fmt.Println("[EssenceFilter] Registering EssenceFilterCheckItemAction...")
	maa.AgentServerRegisterCustomAction("EssenceFilterCheckItemAction", &EssenceFilterCheckItemAction{})
	log.Info().Msg("[EssenceFilter] 已注册 EssenceFilterCheckItemAction")

	fmt.Println("[EssenceFilter] Registering EssenceFilterRowCollectAction...")
	maa.AgentServerRegisterCustomAction("EssenceFilterRowCollectAction", &EssenceFilterRowCollectAction{})
	log.Info().Msg("[EssenceFilter] 已注册 EssenceFilterRowCollectAction")

	fmt.Println("[EssenceFilter] Registering EssenceFilterRowNextItemAction...")
	maa.AgentServerRegisterCustomAction("EssenceFilterRowNextItemAction", &EssenceFilterRowNextItemAction{})
	log.Info().Msg("[EssenceFilter] 已注册 EssenceFilterRowNextItemAction")

	fmt.Println("[EssenceFilter] Registering EssenceFilterSkillDecisionAction...")
	maa.AgentServerRegisterCustomAction("EssenceFilterSkillDecisionAction", &EssenceFilterSkillDecisionAction{})
	log.Info().Msg("[EssenceFilter] 已注册 EssenceFilterSkillDecisionAction")

	fmt.Println("[EssenceFilter] Registering EssenceFilterFinishAction...")
	maa.AgentServerRegisterCustomAction("EssenceFilterFinishAction", &EssenceFilterFinishAction{})
	log.Info().Msg("[EssenceFilter] 已注册 EssenceFilterFinishAction")

	fmt.Println("[EssenceFilter] Registering EssenceFilterTraceAction...")
	maa.AgentServerRegisterCustomAction("EssenceFilterTraceAction", &EssenceFilterTraceAction{})
	log.Info().Msg("[EssenceFilter] 已注册 EssenceFilterTraceAction")

	fmt.Println("========== [EssenceFilter] Register() completed ==========")
}
