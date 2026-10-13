package upstream

import (
	"fakeapi/internal/config"
	"fakeapi/internal/core"
)

func reasoningParameters(r *core.Request, t config.Target) (map[string]any, *core.Error) {
	out := map[string]any{}
	control := r.Reasoning
	if control.Effort == "" {
		control.Effort, _ = r.Parameters["reasoning_effort"].(string)
	}
	mode := t.Reasoning.Mode
	if mode == "" {
		mode = "effort"
	}
	if control.Effort == "" && control.Enabled == nil && control.Budget == nil && control.Include == nil && !control.Adaptive {
		if mode == "thinking_effort" && t.Reasoning.History == "preserve" {
			out["thinking"] = map[string]any{"clear_thinking": false}
		}
		return out, nil
	}
	fail := func(message string) (map[string]any, *core.Error) {
		return nil, core.Err("unsupported_capability", 400, message)
	}
	if mode == "unsupported" {
		return fail("Target does not support reasoning controls")
	}
	effort := control.Effort
	disabled := effort == "none" || control.Enabled != nil && !*control.Enabled
	if disabled && t.Reasoning.CanDisable != nil && !*t.Reasoning.CanDisable {
		return fail("Target model cannot disable thinking")
	}
	if effort != "" && len(t.Reasoning.AllowedEfforts) > 0 {
		found := false
		for _, level := range t.Reasoning.AllowedEfforts {
			found = found || level == effort
		}
		if !found {
			return fail("Target does not support this reasoning effort")
		}
	}
	if control.Budget != nil && !t.Reasoning.Budget {
		return fail("Target cannot translate a thinking token budget")
	}
	if disabled && control.Budget != nil {
		return fail("Thinking budget conflicts with disabled thinking")
	}
	if mode == "thinking" && effort != "" && effort != "none" {
		return fail("Target supports a thinking switch, not reasoning effort levels")
	}
	if (mode == "effort" || mode == "google" || mode == "thinking_effort" || mode == "anthropic") && control.Enabled != nil && *control.Enabled && effort == "" && control.Budget == nil {
		effort = t.Reasoning.DefaultEffort
		if effort == "" {
			effort = "medium"
			if len(t.Reasoning.AllowedEfforts) > 0 {
				effort = ""
				for _, level := range t.Reasoning.AllowedEfforts {
					if level != "none" {
						effort = level
						break
					}
				}
				if effort == "" {
					return fail("Target has no enabled thinking effort")
				}
			}
		}
	}
	if disabled {
		effort = "none"
	}
	switch mode {
	case "effort":
		if effort != "" {
			out["reasoning_effort"] = effort
		}
	case "thinking", "thinking_effort":
		if control.Enabled != nil || effort != "" || control.Adaptive {
			typ := "enabled"
			if disabled {
				typ = "disabled"
			}
			thinking := map[string]any{"type": typ}
			if mode == "thinking_effort" && t.Reasoning.History == "preserve" {
				thinking["clear_thinking"] = false
			}
			out["thinking"] = thinking
		}
		if mode == "thinking_effort" && effort != "" && !disabled {
			out["reasoning_effort"] = effort
		}
	case "google":
		google := map[string]any{}
		if control.Budget != nil {
			if effort != "" {
				return fail("Thinking budget and effort cannot both be sent to Gemini")
			}
			google["thinking_budget"] = *control.Budget
		} else if effort != "" {
			out["reasoning_effort"] = effort
		}
		if control.Include != nil {
			google["include_thoughts"] = *control.Include
		}
		if len(google) > 0 {
			out["extra_body"] = map[string]any{"google": map[string]any{"thinking_config": google}}
		}
	case "anthropic":
		thinking := map[string]any{}
		if disabled {
			thinking["type"] = "disabled"
		} else if control.Budget != nil {
			if *control.Budget < 1024 || r.MaxTokens != nil && *control.Budget >= *r.MaxTokens {
				return fail("Claude thinking budget must be at least 1024 and below max_tokens")
			}
			thinking["type"], thinking["budget_tokens"] = "enabled", *control.Budget
		} else if effort != "" || control.Enabled != nil || control.Adaptive {
			thinking["type"] = "adaptive"
		}
		if control.Include != nil && len(thinking) > 0 {
			if *control.Include {
				thinking["display"] = "summarized"
			} else {
				thinking["display"] = "omitted"
			}
		}
		if len(thinking) > 0 {
			out["thinking"] = thinking
		}
		if effort != "" && !disabled {
			out["output_config"] = map[string]any{"effort": effort}
		}
	}
	return out, nil
}
