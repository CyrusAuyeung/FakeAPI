package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExamples(t *testing.T) {
	for _, name := range []string{"preset", "hybrid", "testing"} {
		t.Run(name, func(t *testing.T) {
			c, e := Load("../../examples/"+name+"/fakeapi.yaml", true)
			if e != nil {
				t.Fatal(e)
			}
			ascii := 0
			for _, a := range c.AssetData {
				if a.Kind == "ascii" {
					ascii++
					if !strings.HasPrefix(a.ID, "nailong-") {
						t.Fatal("unexpected pose")
					}
				}
			}
			if ascii != 4 {
				t.Fatalf("ASCII count %d", ascii)
			}
			if !filepath.IsAbs(c.Storage.Path) {
				t.Fatal("relative path not resolved")
			}
		})
	}
}
func TestStrictYAML(t *testing.T) {
	for _, s := range []string{"schema_version: 1\nschema_version: 2\n", "schema_version: &x 1\n", "schema_version: !custom 1\n", "schema_version: 1\n---\nschema_version: 1\n", "schema_version: 1\nunknown: secret-canary\n", "schema_version: one-secret-canary\n"} {
		var c Config
		e := Strict([]byte(s), &c)
		if e == nil {
			t.Fatalf("accepted invalid document %q", s)
		}
		if strings.Contains(e.Error(), "secret-canary") {
			t.Fatal("configuration error leaked scalar")
		}
	}
}
func TestConfigReferencesAndThinkingPatch(t *testing.T) {
	c, e := Load("../../examples/testing/fakeapi.yaml", true)
	if e != nil {
		t.Fatal(e)
	}
	v := false
	r := Rule{Behavior: Behavior{Thinking: &ThinkingPatch{Enabled: &v}}}
	p := c.EffectiveProfile(c.Model("fakeapi-frog"), r)
	if p.Thinking.Enabled {
		t.Fatal("false override ignored")
	}
	b, e := os.ReadFile("../../examples/preset/fakeapi.yaml")
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	for _, replace := range []struct{ old, new string }{{"matchers_any: [greeting]", "matchers_any: [unknown]"}, {"regex: '(?i)^", "regex: '(?<=x)^"}, {"priority: 600", "priority: invalid"}} {
		body := strings.Replace(string(b), replace.old, replace.new, 1)
		body = strings.ReplaceAll(body, "../../assets/manifest.yaml", filepath.ToSlash(c.Assets.Manifest))
		f := filepath.Join(dir, "bad.yaml")
		if e := os.WriteFile(f, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := Load(f, true); e == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
