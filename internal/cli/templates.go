package cli

import (
	"fmt"

	"fakeapi/examples"
	"fakeapi/internal/config"
	"go.yaml.in/yaml/v3"
)

func initializationTemplate(name string) ([]byte, []byte, error) {
	if name == "preset" || name == "hybrid" || name == "testing" {
		body, e := examples.Files.ReadFile(name + "/fakeapi.yaml")
		return body, []byte("# Export these explicitly; the binary does not load .env automatically.\nFAKEAPI_UPSTREAM_BASE_URL=http://127.0.0.1:9099/v1\nFAKEAPI_UPSTREAM_API_KEY=\nFAKEAPI_UPSTREAM_MODEL=mock-fast\nFAKEAPI_UPSTREAM_SVG_MODEL=mock-svg\n"), e
	}
	switch name {
	case "deepseek", "gpt", "claude", "gemini", "glm", "kimi":
	default:
		return nil, nil, fmt.Errorf("E_USAGE template")
	}
	body, e := examples.Files.ReadFile("hybrid/fakeapi.yaml")
	if e != nil {
		return nil, nil, e
	}
	var generated map[string]any
	if e := config.Strict(body, &generated); e != nil {
		return nil, nil, e
	}
	overlay, e := examples.Files.ReadFile("providers/" + name + ".yaml")
	if e != nil {
		return nil, nil, e
	}
	var provider struct {
		Upstreams map[string]config.Upstream `yaml:"upstreams"`
		Targets   map[string]config.Target   `yaml:"targets"`
	}
	if e := config.Strict(overlay, &provider); e != nil {
		return nil, nil, e
	}
	generated["upstreams"], generated["targets"] = provider.Upstreams, provider.Targets
	encoded, e := yaml.Marshal(generated)
	if e != nil {
		return nil, nil, e
	}
	environment := fmt.Sprintf("# %s hybrid template. Export this in the serve process; .env is not loaded.\nFAKEAPI_UPSTREAM_API_KEY=\n# Endpoint: %s\n# Default model: %s\n# To change endpoints/models, edit upstreams/targets in fakeapi.yaml.\n", name, provider.Upstreams["primary"].BaseURL.Literal, provider.Targets["primary-fast"].Model.Literal)
	return encoded, []byte(environment), nil
}
