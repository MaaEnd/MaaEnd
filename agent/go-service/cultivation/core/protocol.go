package core

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ProtocolSpaceOverride builds the pipeline override a client would apply when
// running the upstream ProtocolSpace task with its default options, with the
// given cases chosen instead (choices: option name -> case name), and the
// cultivation module patch (TargetInventory mode, goals) on top. Overrides are
// deep-merged so that checkbox cases (e.g. one attach key per weekday) add up.
func ProtocolSpaceOverride(doc TaskDocument, module ModulePlan, choices map[string]string) (map[string]any, error) {
	var options []string
	for _, task := range doc.Task {
		if task.Entry == module.Entry {
			options = task.Option
		}
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("missing upstream task for %s", module.Entry)
	}
	out := map[string]any{}
	var walk func(names []string, depth int) error
	walk = func(names []string, depth int) error {
		if depth > 8 {
			return errors.New("option nesting too deep")
		}
		for _, name := range names {
			def, ok := doc.Option[name]
			if !ok {
				return fmt.Errorf("missing upstream option %s", name)
			}
			if def.Type == "input" {
				// Inputs are SupplyPlanLimits; the module patch renders them.
				continue
			}
			selected, err := def.selected(choices[name])
			if err != nil {
				return fmt.Errorf("option %s: %w", name, err)
			}
			for _, c := range def.Cases {
				if !selected[c.Name] {
					continue
				}
				if err := deepMerge(out, c.PipelineOverride); err != nil {
					return err
				}
				if err := walk(c.Option, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(options, 0); err != nil {
		return nil, err
	}
	if err := deepMerge(out, module.Patch); err != nil {
		return nil, err
	}
	return out, nil
}

// selected returns the chosen case names: the explicit choice, else the
// default case(s), else the first case (the interface default).
func (def OptionDefinition) selected(choice string) (map[string]bool, error) {
	names := map[string]bool{}
	switch {
	case choice != "":
		names[choice] = true
	case len(def.DefaultCase) > 0:
		var one string
		var many []string
		if err := json.Unmarshal(def.DefaultCase, &one); err == nil {
			names[one] = true
		} else if err = json.Unmarshal(def.DefaultCase, &many); err == nil {
			for _, name := range many {
				names[name] = true
			}
		} else {
			return nil, errors.New("unreadable default_case")
		}
	case len(def.Cases) > 0 && def.Type != "checkbox":
		names[def.Cases[0].Name] = true
	}
	for name := range names {
		found := false
		for _, c := range def.Cases {
			found = found || c.Name == name
		}
		if !found {
			return nil, fmt.Errorf("unknown case %s", name)
		}
	}
	return names, nil
}

// deepMerge copies src into dst; nested objects merge, other values replace.
func deepMerge(dst, src map[string]any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	var copied map[string]any
	if err = json.Unmarshal(data, &copied); err != nil {
		return err
	}
	for key, value := range copied {
		inner, ok := value.(map[string]any)
		existing, ok2 := dst[key].(map[string]any)
		if ok && ok2 {
			if err := deepMerge(existing, inner); err != nil {
				return err
			}
			continue
		}
		dst[key] = value
	}
	return nil
}
