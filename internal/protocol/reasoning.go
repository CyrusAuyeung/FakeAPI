package protocol

import (
	"strings"

	"fakeapi/internal/core"
)

func responseReasoning(value any, r *core.Request) *core.Error {
	m, e := objectField(value, "reasoning")
	if e != nil {
		return e
	}
	if e := fields(m, "effort summary generate_summary mode", "reasoning."); e != nil {
		return e
	}
	if e := reasoningEffort(m["effort"], r); e != nil {
		return e
	}
	if v := m["mode"]; v != nil && v != "standard" {
		return core.Invalid("reasoning.mode")
	}
	if m["summary"] != nil && m["generate_summary"] != nil {
		return core.Invalid("reasoning.summary")
	}
	for _, key := range []string{"summary", "generate_summary"} {
		if value := m[key]; value != nil {
			s, ok := value.(string)
			if !ok {
				return core.Invalid("reasoning." + key)
			}
			switch s {
			case "none", "auto", "concise", "detailed":
				include := s != "none"
				r.Reasoning.Include = &include
				r.ThinkingOff = r.ThinkingOff || !include
			default:
				return core.Invalid("reasoning." + key)
			}
		}
	}
	return nil
}

func anthropicReasoning(value any, r *core.Request) *core.Error {
	m, e := objectField(value, "thinking")
	if e != nil || value == nil {
		return e
	}
	if e := fields(m, "type budget_tokens display", "thinking."); e != nil {
		return e
	}
	enabled := true
	switch str(m["type"]) {
	case "disabled":
		enabled = false
	case "enabled":
		v, ok := number(m["budget_tokens"])
		if !ok || v < 1024 || v > 1e6 || v != float64(int(v)) || r.MaxTokens != nil && v >= float64(*r.MaxTokens) {
			return core.Invalid("thinking.budget_tokens")
		}
		budget := int(v)
		r.Reasoning.Budget = &budget
	case "adaptive":
		r.Reasoning.Adaptive = true
	default:
		return core.Invalid("thinking.type")
	}
	if str(m["type"]) != "enabled" && m["budget_tokens"] != nil {
		return core.Invalid("thinking.budget_tokens")
	}
	r.Reasoning.Enabled = &enabled
	r.ThinkingOff = r.ThinkingOff || !enabled
	if v := m["display"]; v != nil {
		s, ok := v.(string)
		if !ok || s != "summarized" && s != "omitted" {
			return core.Invalid("thinking.display")
		}
		include := s != "omitted"
		r.Reasoning.Include = &include
		r.ThinkingOff = r.ThinkingOff || !include
	}
	return nil
}

func geminiReasoning(value any, r *core.Request) *core.Error {
	m, e := objectField(value, "thinkingConfig")
	if e != nil {
		return e
	}
	if e := fields(m, "includeThoughts thinkingBudget thinkingLevel", "thinkingConfig."); e != nil {
		return e
	}
	if m["thinkingBudget"] != nil && m["thinkingLevel"] != nil {
		return core.Invalid("thinkingConfig.conflicting_controls")
	}
	if value := m["thinkingBudget"]; value != nil {
		v, ok := number(value)
		if !ok || v < -1 || v > 32768 || v != float64(int(v)) {
			return core.Invalid("thinkingConfig.thinkingBudget")
		}
		budget, enabled := int(v), v != 0
		r.Reasoning.Enabled = &enabled
		r.ThinkingOff = !enabled
		if budget > 0 {
			r.Reasoning.Budget = &budget
		}
		if budget == -1 {
			r.Reasoning.Adaptive = true
		}
	}
	if value := m["thinkingLevel"]; value != nil {
		s, ok := value.(string)
		if !ok {
			return core.Invalid("thinkingConfig.thinkingLevel")
		}
		s = strings.ToLower(s)
		if s != "minimal" && s != "low" && s != "medium" && s != "high" {
			return core.Invalid("thinkingConfig.thinkingLevel")
		}
		if e := reasoningEffort(s, r); e != nil {
			return e
		}
	}
	if value := m["includeThoughts"]; value != nil {
		include, ok := value.(bool)
		if !ok {
			return core.Invalid("thinkingConfig.includeThoughts")
		}
		r.Reasoning.Include = &include
		r.ThinkingOff = r.ThinkingOff || !include
	}
	return nil
}
