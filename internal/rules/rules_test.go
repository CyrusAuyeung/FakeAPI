package rules

import (
	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"testing"
)

func TestDefaultBoundaries(t *testing.T) {
	c, e := config.Load("../../examples/hybrid/fakeapi.yaml", true)
	if e != nil {
		t.Fatal(e)
	}
	engine := New(c)
	tests := []struct {
		text, rule string
		tags       []string
	}{
		{"你好", "ai-greeting", []string{"ai"}},
		{"hello!", "ai-greeting", []string{"ai"}},
		{"你好，请分析以下代码", "default-frog", []string{"ai"}},
		{"12 * -3", "preset-math", nil},
		{"1 + 2 * 3", "default-frog", nil},
		{"用 SVG 画骑自行车的鹈鹕", "ai-pelican", []string{"ai"}},
		{"用 SVG 画骑自行车的鹈鹕", "preset-pelican", nil},
		{"奶蛙正在思考", "easter-thinking", nil},
		{"不要让奶蛙思考", "default-frog", nil},
		{"哈哈哈", "easter-laugh", nil},
		{"只回答 OK", "preset-ok", nil},
		{"", "default-frog", nil},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			r := engine.Match(Input{Latest: core.LatestUser{Present: true, Text: tc.text}, Model: "fakeapi-test", Tags: tc.tags})
			if r.ID != tc.rule {
				t.Fatalf("got %s want %s", r.ID, tc.rule)
			}
		})
	}
}
func TestAtomicMatchers(t *testing.T) {
	v := "cat"
	a := compiledAtom{atom: config.Atom{Keyword: &v, Boundary: "ascii_word"}}
	for s, want := range map[string]bool{"cat": true, "a cat!": true, "concatenate": false, "_cat": false} {
		if a.match(s) != want {
			t.Fatalf("boundary %s", s)
		}
	}
	a.atom.CaseInsensitive = true
	if !a.match("CAT") {
		t.Fatal("case fold")
	}
}
