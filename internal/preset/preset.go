package preset

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"fakeapi/internal/pacing"
	"fakeapi/internal/schema"
	"github.com/rivo/uniseg"
)

var arithmetic = regexp.MustCompile(`^(?:计算\s*)?([+-]?\d{1,6})\s*([+*/-])\s*([+-]?\d{1,6})\s*(?:=\s*\??|\?)?$`)

func Calculate(text string) (string, bool) {
	m := arithmetic.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	a, _ := new(big.Int).SetString(m[1], 10)
	b, _ := new(big.Int).SetString(m[3], 10)
	switch m[2] {
	case "+":
		return new(big.Int).Add(a, b).String(), true
	case "-":
		return new(big.Int).Sub(a, b).String(), true
	case "*":
		return new(big.Int).Mul(a, b).String(), true
	case "/":
		if b.Sign() == 0 {
			return "不能除以零。", true
		}
		return new(big.Rat).SetFrac(a, b).RatString(), true
	}
	return "", false
}
func Prepare(c *config.Config, r *core.Request, rule config.Rule, seed int64) (core.Result, config.Profile, *core.Error) {
	result := core.Result{ID: core.ID("fake_"), Finish: "stop"}
	p := c.EffectiveProfile(c.Model(r.Model), rule)
	a := rule.Action
	strict := c.StrictAction(a)
	switch a.Type {
	case "preset":
		kind := "text"
		asset := a.Asset
		text := a.Text
		if len(a.Choices) > 0 {
			rng := pacing.RNG(seed, r, rule.ID, "body")
			total := 0
			for _, q := range a.Choices {
				w := q.Weight
				if w == 0 {
					w = 1
				}
				total += w
			}
			n := rng.IntN(total)
			for _, q := range a.Choices {
				w := q.Weight
				if w == 0 {
					w = 1
				}
				n -= w
				if n < 0 {
					text = q.Text
					asset = q.Asset
					break
				}
			}
		}
		if text != nil {
			result.Text = strings.ReplaceAll(strings.ReplaceAll(*text, "{{model}}", r.Model), "{{rule_id}}", rule.ID)
		} else {
			v := c.AssetData[asset]
			result.Text = v.Content
			kind = v.Kind
		}
		if kind == "ascii" && a.Render != "plain" || a.Render == "markdown_code" {
			fence := "```"
			for strings.Contains(result.Text, fence) {
				fence += "`"
			}
			result.Text = fence + "text\n" + result.Text + "\n" + fence
		}
		if kind == "svg" || kind == "json" {
			strict = true
		}
	case "arithmetic":
		s, ok := Calculate(r.Latest.Text)
		if !ok {
			return result, p, core.Err("unsupported_arithmetic", 400, "Expression outside the supported arithmetic grammar")
		}
		result.Text = s
	case "json_fixture":
		b, _ := json.Marshal(a.JSON)
		result.Text = string(b)
	case "tool_fixture":
		call, e := fixture(r, a)
		if e != nil {
			return result, p, e
		}
		if call == nil {
			result.Text = a.FinalText
		} else {
			result.Tool = call
			result.Finish = "tool_calls"
		}
	default:
		return result, p, core.Err("invalid_action", 500, "Unable to prepare response")
	}
	if strict || r.ThinkingOff {
		p.Thinking.Enabled = false
	}
	if p.Thinking.Enabled {
		t := p.Thinking.Text
		if p.Thinking.TextAsset != "" {
			t = c.AssetData[p.Thinking.TextAsset].Content
		}
		t = strings.TrimSuffix(t, "\n")
		if p.Thinking.Channel == "native" && r.Protocol != core.Anthropic {
			result.Reasoning = "[模拟思考] " + t
		} else {
			result.Text = "[模拟思考] " + t + "\n\n" + result.Text
		}
	}
	if e := CheckConstraints(r, result); e != nil {
		return result, p, e
	}
	budget := c.Model(r.Model).Limits.OutputTokens
	if r.MaxTokens != nil {
		budget = *r.MaxTokens
	}
	if strict || result.Tool != nil {
		size := core.Estimate(result.Text)
		if result.Tool != nil {
			size += core.Estimate(result.Tool.Name + result.Tool.Arguments)
		}
		if size > budget {
			return result, p, core.Err("fixture_constraint_conflict", 400, "Output budget cannot contain the complete fixture")
		}
		for _, stop := range r.Stop {
			if strings.Contains(result.Text, stop) || result.Tool != nil && strings.Contains(result.Tool.Arguments, stop) {
				return result, p, core.Err("fixture_constraint_conflict", 400, "Stop sequence conflicts with the complete fixture")
			}
		}
	} else {
		at := -1
		for _, stop := range r.Stop {
			if i := strings.Index(result.Text, stop); i >= 0 && (at < 0 || i < at) {
				at = i
				result.Stop = stop
			}
		}
		if at >= 0 {
			result.Text = result.Text[:at]
		}
		var truncated bool
		result.Reasoning, truncated = truncate(result.Reasoning, budget*4)
		remaining := (budget - core.Estimate(result.Reasoning)) * 4
		var t bool
		result.Text, t = truncate(result.Text, remaining)
		if truncated || t {
			result.Finish = "length"
			result.Stop = ""
		}
	}
	if len(result.Text)+len(result.Reasoning) > c.Server.MaxResponseBytes {
		return result, p, core.Err("response_too_large", 400, "Prepared response exceeds output limit")
	}
	result.Usage = Estimate(r, result)
	return result, p, nil
}
func truncate(s string, bytes int) (string, bool) {
	if len(s) <= bytes {
		return s, false
	}
	g := uniseg.NewGraphemes(s)
	out := ""
	for g.Next() {
		if len(out)+len(g.Str()) > bytes {
			break
		}
		out += g.Str()
	}
	return out, true
}
func Estimate(r *core.Request, v core.Result) core.Usage {
	u := core.Usage{Input: core.InputUsage(r), Visible: core.Estimate(v.Text), Reasoning: core.Estimate(v.Reasoning), Source: "estimated"}
	if v.Tool != nil {
		u.Visible += core.Estimate(v.Tool.Name + v.Tool.Arguments)
	}
	return u
}
func CheckConstraints(r *core.Request, v core.Result) *core.Error {
	if r.Constraint.Kind != "" {
		if v.Tool != nil || v.Reasoning != "" || !json.Valid([]byte(v.Text)) {
			return core.Err("unsupported_constraint", 400, "Selected response cannot satisfy JSON output")
		}
		var j any
		_ = json.Unmarshal([]byte(v.Text), &j)
		if _, ok := j.(map[string]any); !ok && r.Constraint.Kind == "json_object" {
			return core.Err("unsupported_constraint", 400, "Selected response is not a JSON object")
		}
		if len(r.Constraint.Schema) > 0 {
			if e := schema.Validate(r.Constraint.Schema, j); e != nil {
				return core.Err("unsupported_constraint", 400, "Selected response does not satisfy the requested schema")
			}
		}
	}
	if v.Tool == nil && r.ToolChoice != "auto" && r.ToolChoice != "none" {
		return core.Err("unsupported_constraint", 400, "Selected response cannot satisfy forced tool choice")
	}
	return nil
}
func fixture(r *core.Request, a config.Action) (*core.ToolCall, *core.Error) {
	var calls, results []core.Part
	seenResult := false
	for _, m := range r.Messages[r.Latest.Index+1:] {
		for _, p := range m.Parts {
			switch p.Kind {
			case "tool_call":
				if seenResult {
					return nil, core.Err("unsupported_tool_sequence", 400, "Tool call appears after its result")
				}
				calls = append(calls, p)
			case "tool_result":
				seenResult = true
				results = append(results, p)
			}
		}
	}
	if len(calls) > 0 || len(results) > 0 {
		if len(calls) != 1 || len(results) != 1 || calls[0].Name != a.Name || results[0].Opaque {
			return nil, core.Err("unsupported_tool_sequence", 400, "Expected exactly one fixture call and result")
		}
		c, v := calls[0], results[0]
		if v.ID != c.ID && (v.ID != "" || v.Name != c.Name) {
			return nil, core.Err("unsupported_tool_sequence", 400, "Tool result does not match the call")
		}
		if r.ToolChoice != "auto" && r.ToolChoice != "none" {
			return nil, core.Err("fixture_constraint_conflict", 400, "Fixture final text conflicts with forced tool choice")
		}
		return nil, nil
	}
	if r.ToolChoice == "none" || r.ToolChoice != "auto" && r.ToolChoice != "required" && r.ToolChoice != a.Name {
		return nil, core.Err("fixture_constraint_conflict", 400, "Tool choice prevents this fixture")
	}
	var tool *core.Tool
	for i := range r.Tools {
		if r.Tools[i].Name == a.Name {
			tool = &r.Tools[i]
			break
		}
	}
	if tool == nil {
		return nil, core.Err("unsupported_constraint", 400, "Fixture function must be declared")
	}
	if len(tool.Schema) > 0 {
		if e := schema.Validate(tool.Schema, a.Arguments); e != nil {
			return nil, core.Err("unsupported_constraint", 400, "Fixture arguments do not satisfy the function schema")
		}
	}
	b, e := json.Marshal(a.Arguments)
	if e != nil {
		return nil, core.Err("invalid_fixture", 500, "Unable to encode fixture")
	}
	return &core.ToolCall{ID: core.ID("call_fixture_"), Name: a.Name, Arguments: string(b)}, nil
}
func DebugSummary(v core.Result) string { return fmt.Sprintf("kind=%s", v.Finish) }
