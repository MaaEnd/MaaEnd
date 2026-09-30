package creditshopping

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const creditShoppingApplyItemTemplatesActionName = "CreditShoppingApplyItemTemplatesAction"

const itemTemplateDir = "CreditShopping/Item/"

// ignoredAttachKeys 是其它组件占用的 attach 键，不表示一件商品。
var ignoredAttachKeys = map[string]struct{}{
	"ready":   {},
	"visited": {},
}

var _ maa.CustomActionRunner = &ApplyItemTemplatesAction{}

type applyItemTemplatesParam struct {
	Targets []string `json:"targets"`
}

// ApplyItemTemplatesAction 把信用点商店物品节点 attach 里启用的物品模板写入该节点的 template。
// 字符串值视为模板路径；true 视为 CreditShopping/Item/<键>.png。没有启用项时保留 Pipeline 里预写的 template。
type ApplyItemTemplatesAction struct{}

func (a *ApplyItemTemplatesAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", component).Msg("apply item templates: nil context")
		return false
	}
	var param applyItemTemplatesParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("component", component).Msg("apply item templates: failed to parse params")
		return false
	}
	targets := normalizeTargets(param.Targets)
	if len(targets) == 0 {
		log.Error().Str("component", component).Msg("apply item templates: targets is required")
		return false
	}
	for _, target := range targets {
		raw, err := ctx.GetNodeJSON(target)
		if err != nil {
			log.Error().Err(err).Str("component", component).Str("target", target).Msg("apply item templates: failed to read node")
			return false
		}
		templates, err := itemTemplatesFromNode(raw)
		if err != nil {
			log.Error().Err(err).Str("component", component).Str("target", target).Msg("apply item templates: failed to parse node")
			return false
		}
		if len(templates) == 0 {
			log.Info().Str("component", component).Str("target", target).Msg("apply item templates: no attach templates, keep pipeline template")
			continue
		}
		if err := ctx.OverridePipeline(map[string]any{
			target: map[string]any{
				"template": templates,
			},
		}); err != nil {
			log.Error().Err(err).Str("component", component).Str("target", target).Msg("apply item templates: override failed")
			return false
		}
		log.Info().
			Str("component", component).
			Str("target", target).
			Int("templates", len(templates)).
			Msg("apply item templates: template list updated")
	}
	return true
}

func normalizeTargets(targets []string) []string {
	out := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}

func itemTemplatesFromNode(raw string) ([]string, error) {
	var node map[string]any
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return nil, fmt.Errorf("unmarshal item node: %w", err)
	}
	return itemTemplatesFromAttach(node["attach"]), nil
}

// itemTemplatesFromAttach 按键名顺序收集启用的模板路径。
func itemTemplatesFromAttach(raw any) []string {
	attach, ok := raw.(map[string]any)
	if !ok || len(attach) == 0 {
		return nil
	}
	keys := make([]string, 0, len(attach))
	for key := range attach {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	seen := make(map[string]struct{})
	out := make([]string, 0, len(keys))
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	for _, key := range keys {
		if _, ignored := ignoredAttachKeys[key]; ignored {
			continue
		}
		switch value := attach[key].(type) {
		case bool:
			if value {
				add(itemTemplateDir + strings.TrimSpace(key) + ".png")
			}
		case string:
			add(templatePath(value))
		case []any:
			for _, item := range value {
				text, ok := item.(string)
				if ok {
					add(templatePath(text))
				}
			}
		}
	}
	return out
}

func templatePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasSuffix(lower, ".png") || strings.Contains(value, "/") {
		return value
	}
	return ""
}
