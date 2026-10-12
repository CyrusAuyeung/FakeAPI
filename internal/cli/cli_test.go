package cli

import (
	"bytes"
	"encoding/json"
	"fakeapi/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitAndKeyLifecycle(t *testing.T) {
	dir := t.TempDir()
	out, errout := new(bytes.Buffer), new(bytes.Buffer)
	if code := Run([]string{"init", "--template", "testing", "--dir", dir}, out, errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	if code := Run([]string{"init", "--dir", dir}, out, errout); code == 0 {
		t.Fatal("init overwrote deployment")
	}
	file := filepath.Join(dir, "fakeapi.yaml")
	if _, e := config.Load(file, false); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	errout.Reset()
	if code := Run([]string{"keys", "create", "--config", file, "--policy", "test", "--label", "local", "--format", "json", "--expires-in", "30d"}, out, errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var created struct {
		Key      string `json:"key"`
		Metadata struct {
			ID string `json:"id"`
		} `json:"metadata"`
	}
	if e := json.Unmarshal(out.Bytes(), &created); e != nil {
		t.Fatal(e)
	}
	if created.Key == "" {
		t.Fatal("key missing")
	}
	for _, sub := range []string{"show", "revoke"} {
		out.Reset()
		if code := Run([]string{"keys", sub, "--config", file, "--id", created.Metadata.ID}, out, errout); code != 0 {
			t.Fatal(code, errout.String())
		}
		if strings.Contains(out.String(), created.Key) {
			t.Fatal("secret recovered from metadata")
		}
	}
	if code := Run([]string{"db", "backup", "--config", file, "--out", filepath.Join(dir, "backup.sqlite")}, out, errout); code != 0 {
		t.Fatal(code, errout.String())
	}
}
func TestAssetImportAndSchema(t *testing.T) {
	dir := t.TempDir()
	if e := Init("testing", dir, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(dir, "fakeapi.yaml")
	c, e := config.Load(file, true)
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(dir, "custom.txt")
	if e = os.WriteFile(source, []byte("custom text\n"), 0600); e != nil {
		t.Fatal(e)
	}
	a := config.Asset{ID: "custom", Kind: "text", Author: "tester", License: "MIT", Origin: "original"}
	if e = ImportAsset(c, source, a, false); e != nil {
		t.Fatal(e)
	}
	if e = ImportAsset(c, source, a, false); e == nil {
		t.Fatal("import overwrote ID")
	}
	loaded, e := config.Load(file, true)
	if e != nil || loaded.AssetData["custom"].Content != "custom text\n" {
		t.Fatal(e)
	}
	a.ID = "../escape"
	if e = ImportAsset(c, source, a, false); e == nil {
		t.Fatal("path escape accepted")
	}
	out := new(bytes.Buffer)
	if code := Run([]string{"config", "schema"}, out, new(bytes.Buffer)); code != 0 || !json.Valid(out.Bytes()) {
		t.Fatal("schema generation")
	}
}
