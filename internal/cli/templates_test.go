package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fakeapi/internal/config"
)

func TestProviderTemplates(t *testing.T) {
	t.Setenv("FAKEAPI_UPSTREAM_API_KEY", "")
	for _, name := range []string{"deepseek", "gpt", "claude", "gemini", "glm", "kimi"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := Init(name, dir, new(bytes.Buffer)); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "fakeapi.yaml")
			c, err := config.Load(file, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := config.Load(file, false); err == nil {
				t.Fatal("provider key was not required")
			}
			if c.KeyPolicies["hybrid"].Tags[0] != "ai" || len(c.Targets) != 2 {
				t.Fatal("hybrid policy/targets missing")
			}
			ascii := 0
			for _, asset := range c.AssetData {
				if asset.Kind == "ascii" {
					ascii++
				}
			}
			if ascii != 4 {
				t.Fatal("provider template changed portraits")
			}
			if c.Targets["primary-fast"].Model.Literal == "" || c.Upstreams["primary"].BaseURL.Literal == "" {
				t.Fatal("template requires manual endpoint/model setup")
			}
			environment, _ := os.ReadFile(filepath.Join(dir, ".env.example"))
			if !strings.Contains(string(environment), "FAKEAPI_UPSTREAM_API_KEY=\n") {
				t.Fatal("missing empty credential example")
			}
			if err := Init(name, dir, new(bytes.Buffer)); err == nil {
				t.Fatal("provider init overwrote a deployment")
			}
		})
	}
}
