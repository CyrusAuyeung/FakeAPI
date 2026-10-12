package preset

import (
	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"testing"
)

func TestArithmetic(t *testing.T) {
	for input, want := range map[string]string{"12 * -3": "-36", "1 / 3": "1/3", "2 / 4": "1/2", "1 / 0": "不能除以零。", "999999*999999": "999998000001", "计算 -12 - -3": "-9"} {
		got, ok := Calculate(input)
		if !ok || got != want {
			t.Fatal(input, got, ok)
		}
	}
	for _, input := range []string{"1+2*3", "9999999+1", "eval(hello)", "1.2+3"} {
		if _, ok := Calculate(input); ok {
			t.Fatal("accepted", input)
		}
	}
}
func TestNativeThinkingAndLimits(t *testing.T) {
	c, e := config.Load("../../examples/preset/fakeapi.yaml", true)
	if e != nil {
		t.Fatal(e)
	}
	p := c.Profiles["natural"]
	p.Thinking.Channel = "native"
	c.Profiles["natural"] = p
	max := 8
	r := &core.Request{Protocol: core.Chat, Model: "fakeapi-frog", Latest: core.LatestUser{Present: true, Text: "task"}, ToolChoice: "auto", MaxTokens: &max}
	v, _, err := Prepare(c, r, c.Fallback, 42)
	if err != nil {
		t.Fatal(err)
	}
	if v.Finish != "length" || v.Usage.Reasoning+v.Usage.Visible > max {
		t.Fatal("output limit violated", v)
	}
	r.Protocol = core.Anthropic
	r.MaxTokens = nil
	v, _, err = Prepare(c, r, c.Fallback, 42)
	if err != nil || v.Reasoning != "" || len(v.Text) == 0 {
		t.Fatal("anthropic portable fallback", err)
	}
}
