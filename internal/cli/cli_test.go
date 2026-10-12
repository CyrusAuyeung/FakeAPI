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

func TestAssetImportThroughLinkedParent(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if e := os.Mkdir(realDir, 0700); e != nil {
		t.Fatal(e)
	}
	alias := filepath.Join(root, "alias")
	if e := os.Symlink(realDir, alias); e != nil {
		t.Skipf("directory symlink unavailable: %v", e)
	}
	if e := Init("testing", alias, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	c, e := config.Load(filepath.Join(alias, "fakeapi.yaml"), true)
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(alias, "custom.txt")
	if e = os.WriteFile(source, []byte("linked parent\n"), 0600); e != nil {
		t.Fatal(e)
	}
	a := config.Asset{ID: "custom", Kind: "text", Author: "tester", License: "MIT", Origin: "original"}
	if e = ImportAsset(c, source, a, false); e != nil {
		t.Fatal(e)
	}
	loaded, e := config.Load(filepath.Join(alias, "fakeapi.yaml"), true)
	if e != nil || loaded.AssetData["custom"].Content != "linked parent\n" {
		t.Fatalf("imported asset through linked parent: %v", e)
	}
}

func TestInitializationAndImportPermissions(t *testing.T) {
	for _, shared := range []bool{false, true} {
		dir := t.TempDir()
		args := []string{"init", "--template", "testing", "--dir", dir}
		fileMode, directoryMode := os.FileMode(0600), os.FileMode(0700)
		if shared {
			args = append(args, "--shared-config")
			fileMode, directoryMode = 0644, 0755
		}
		errout := new(bytes.Buffer)
		if code := Run(args, new(bytes.Buffer), errout); code != 0 {
			t.Fatal(errout.String())
		}
		configuration := filepath.Join(dir, "fakeapi.yaml")
		c, e := config.Load(configuration, true)
		if e != nil {
			t.Fatal(e)
		}
		source := filepath.Join(dir, "custom.txt")
		if e = os.WriteFile(source, []byte("custom material\n"), 0600); e != nil {
			t.Fatal(e)
		}
		a := config.Asset{ID: "custom", Kind: "text", Author: "tester", License: "MIT", Origin: "original"}
		if e = ImportAsset(c, source, a, false); e != nil {
			t.Fatal(e)
		}
		loaded, e := config.Load(configuration, true)
		if e != nil || loaded.AssetData["custom"].Content != "custom material\n" {
			t.Fatalf("import after shared=%v initialization: %v", shared, e)
		}
		if filepath.Separator != '\\' {
			for _, file := range []string{configuration, c.Assets.Manifest, filepath.Join(filepath.Dir(c.Assets.Manifest), "imports/custom.txt")} {
				info, e := os.Stat(file)
				if e != nil || info.Mode().Perm() != fileMode {
					t.Fatalf("shared=%v file permissions: %v", shared, e)
				}
			}
			info, e := os.Stat(filepath.Join(filepath.Dir(c.Assets.Manifest), "imports"))
			if e != nil || info.Mode().Perm() != directoryMode {
				t.Fatalf("shared=%v directory permissions: %v", shared, e)
			}
		}
	}
}

func TestAssetImportRejectsEscapingImportsDirectory(t *testing.T) {
	dir := t.TempDir()
	if e := Init("testing", dir, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	c, e := config.Load(filepath.Join(dir, "fakeapi.yaml"), true)
	if e != nil {
		t.Fatal(e)
	}
	outside := t.TempDir()
	if e = os.Symlink(outside, filepath.Join(filepath.Dir(c.Assets.Manifest), "imports")); e != nil {
		t.Skipf("directory symlink unavailable: %v", e)
	}
	source := filepath.Join(dir, "custom.txt")
	if e = os.WriteFile(source, []byte("must not escape\n"), 0600); e != nil {
		t.Fatal(e)
	}
	a := config.Asset{ID: "custom", Kind: "text", Author: "tester", License: "MIT", Origin: "original"}
	if e = ImportAsset(c, source, a, false); e == nil || e.Error() != "E_ASSET_PATH" {
		t.Fatalf("escaping imports directory accepted: %v", e)
	}
	if _, e = os.Stat(filepath.Join(outside, "custom.txt")); !os.IsNotExist(e) {
		t.Fatal("asset written outside manifest directory")
	}
}
