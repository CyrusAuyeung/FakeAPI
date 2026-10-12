package config

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

func TestExplicitLimitsAndYAMLMaterial(t *testing.T) {
	for _, source := range []string{"server: {max_tools: 0}", "server: {max_concurrent: null}", "server: {write_timeout: 0s}", "models: [{limits: {output_tokens: 0}}]", "upstreams: {local: {timeouts: {total: 0s}}}"} {
		if e := positiveValues([]byte(source)); e == nil {
			t.Fatal("explicit invalid limit treated as omitted", source)
		}
	}
	if e := positiveValues([]byte("server: {}\nmodels: [{}]")); e != nil {
		t.Fatal("omitted limits must retain defaults", e)
	}
	for _, data := range [][]byte{[]byte("missing newline"), []byte("tab\t\n"), []byte("CRLF\r\n"), []byte("\xef\xbb\xbfBOM\n"), []byte("bidi\u202e\n"), []byte("\xff\n")} {
		if e := Lint(data, "unicode"); e == nil {
			t.Fatal("invalid material accepted")
		}
	}
	c, e := Load("../../examples/testing/fakeapi.yaml", true)
	if e != nil {
		t.Fatal(e)
	}
	p := c.Profiles["instant"]
	p.Timing.GraphemesPerSecond = math.NaN()
	if e := c.checkProfile(p); e == nil {
		t.Fatal("NaN speed accepted")
	}
	text := "hello"
	if e := c.checkAction(Action{Type: "preset", Choices: []Choice{{Text: &text, Weight: math.MaxInt}, {Text: &text, Weight: 1}}}); e == nil {
		t.Fatal("weight overflow accepted")
	}
}

func TestModelScopedTargetValidation(t *testing.T) {
	c, e := Load("../../examples/hybrid/fakeapi.yaml", true)
	if e != nil {
		t.Fatal(e)
	}
	target := ""
	for id := range c.Targets {
		target = id
		break
	}
	c.DefaultTarget = ""
	for i := range c.Models {
		c.Models[i].Target = ""
	}
	c.Models[0].Target = target
	c.Rules = []Rule{{ID: "scoped-ai", When: When{Models: []string{c.Models[0].ID}}, Action: Action{Type: "upstream"}}}
	data, e := yaml.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "config.yaml")
	if e := os.WriteFile(file, data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Load(file, true); e != nil {
		t.Fatal("unmatched model incorrectly required an AI target", e)
	}
}

func TestGeneratedSchemaAcceptsExamples(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	encodedSchema, e := json.Marshal(JSONSchema())
	if e != nil {
		t.Fatal(e)
	}
	var publishedSchema any
	if e := json.Unmarshal(encodedSchema, &publishedSchema); e != nil {
		t.Fatal(e)
	}
	if e := compiler.AddResource("urn:fakeapi:config", publishedSchema); e != nil {
		t.Fatal(e)
	}
	schema, e := compiler.Compile("urn:fakeapi:config")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"preset", "hybrid", "testing"} {
		data, e := os.ReadFile("../../examples/" + name + "/fakeapi.yaml")
		if e != nil {
			t.Fatal(e)
		}
		var values map[string]any
		if e := Strict(data, &values); e != nil {
			t.Fatal(e)
		}
		encoded, e := json.Marshal(values)
		if e != nil {
			t.Fatal(e)
		}
		var jsonValues any
		if e := json.Unmarshal(encoded, &jsonValues); e != nil {
			t.Fatal(e)
		}
		if e := schema.Validate(jsonValues); e != nil {
			t.Fatal(name, e)
		}
	}
}
