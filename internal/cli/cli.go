package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	builtin "fakeapi/assets"
	"fakeapi/examples"
	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"fakeapi/internal/preset"
	"fakeapi/internal/protocol"
	"fakeapi/internal/requestlog"
	"fakeapi/internal/rules"
	"fakeapi/internal/server"
	"fakeapi/internal/store"
	"github.com/gofrs/flock"
	"go.yaml.in/yaml/v3"
)

var Version = "0.5.0-dev"
var Commit = "unknown"

const help = `FakeAPI - configurable AI protocol simulator
Commands:
  init --template preset|hybrid|testing --dir PATH [--shared-config]
  serve --config FILE [--listen ADDR] [--state FILE]
  config check|show --config FILE [--skip-env] [--redact]
  keys create|import|list|show|revoke|update|reset-usage --config FILE
  rules explain --config FILE --input FILE --protocol chat|responses|anthropic|gemini --model ID
  assets list|lint|import --config FILE
  db backup|migrate --config FILE
  health --url http://127.0.0.1:8787
  version
Options must precede positional arguments. Run a command with --help for flags.
`

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func Run(args []string, out, errout io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(out, help)
		return 0
	}
	if args[0] == "version" {
		fmt.Fprintf(out, "FakeAPI %s (%s)\n", Version, Commit)
		return 0
	}
	if e := run(args, out, errout); e != nil {
		if e == flag.ErrHelp {
			return 0
		}
		fmt.Fprintln(errout, e)
		if strings.HasPrefix(e.Error(), "E_CONFIG") || strings.HasPrefix(e.Error(), "E_YAML") || strings.HasPrefix(e.Error(), "E_USAGE") || strings.HasPrefix(e.Error(), "E_ASSET") {
			return 2
		}
		return 1
	}
	return 0
}
func run(args []string, out, errout io.Writer) error {
	command := args[0]
	rest := args[1:]
	sub := ""
	if command == "config" || command == "keys" || command == "rules" || command == "assets" || command == "db" {
		if len(rest) == 0 {
			return fmt.Errorf("E_USAGE missing subcommand")
		}
		sub = rest[0]
		rest = rest[1:]
	}
	f := flag.NewFlagSet(command+" "+sub, flag.ContinueOnError)
	f.SetOutput(errout)
	file := f.String("config", env("FAKEAPI_CONFIG", "fakeapi.yaml"), "Configuration file")
	state := f.String("state", env("FAKEAPI_STATE", ""), "SQLite override")
	listen := f.String("listen", env("FAKEAPI_LISTEN", ""), "Listen override")
	template := f.String("template", "preset", "preset, hybrid or testing")
	dir := f.String("dir", ".", "Initialization directory")
	sharedConfig := f.Bool("shared-config", false, "Readable configuration/assets for a separate container user; no credentials or database are created")
	skip := f.Bool("skip-env", false, "Skip environment presence validation")
	redact := f.Bool("redact", false, "Required for safe configuration display")
	policyName := f.String("policy", "demo", "Key policy")
	label := f.String("label", "", "Key label")
	id := f.String("id", "", "Public Key ID or asset ID")
	format := f.String("format", "text", "text or json")
	expiresIn := f.String("expires-in", "", "Lifetime: 30d, 12h")
	expiresAt := f.String("expires-at", "", "RFC3339 expiry, or none for update")
	fromEnv := f.String("from-env", "", "Environment variable containing imported Key")
	confirm := f.Bool("confirm", false, "Confirm resetting usage and rate window")
	tags := f.String("tags", "", "Comma separated tags")
	group := f.String("group", "", "Rule group")
	totalLimit := f.String("total-limit", "", "Nonnegative integer or unlimited")
	aiLimit := f.String("ai-limit", "", "Nonnegative integer or unlimited")
	rpm := f.String("rpm", "", "Nonnegative integer or unlimited")
	concurrent := f.Int("max-concurrent", 0, "Positive Key concurrency limit")
	input := f.String("input", "", "Artificial protocol JSON file")
	proto := f.String("protocol", "chat", "chat, responses, anthropic or gemini")
	model := f.String("model", "fakeapi-test", "Display model ID")
	seed := f.Int64("seed", 42, "Explanation seed")
	kind := f.String("kind", "text", "text, markdown, ascii, svg or json")
	author := f.String("author", "", "Asset author")
	license := f.String("license", "", "Asset license")
	origin := f.String("origin", "imported", "Asset provenance")
	url := f.String("url", "http://127.0.0.1:8787", "Health base URL")
	backup := f.String("out", "", "Backup destination")
	overwrite := f.Bool("overwrite", false, "Explicitly replace an asset ID")
	if e := f.Parse(rest); e != nil {
		return e
	}
	provided := map[string]bool{}
	f.Visit(func(v *flag.Flag) { provided[v.Name] = true })
	if *format != "text" && *format != "json" {
		return fmt.Errorf("E_USAGE format")
	}
	if command == "init" {
		return initDeployment(*template, *dir, *sharedConfig, out)
	}
	if command == "health" {
		client := http.Client{Timeout: 3 * time.Second}
		r, e := client.Get(strings.TrimRight(*url, "/") + "/healthz")
		if e != nil {
			return fmt.Errorf("E_HEALTH_CONNECT")
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return fmt.Errorf("E_HEALTH_STATUS")
		}
		fmt.Fprintln(out, "ok")
		return nil
	}
	if command == "config" && sub == "schema" {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(config.JSONSchema())
	}
	c, e := config.Load(*file, *skip || command != "serve" && !(command == "config" && sub == "check"))
	if e != nil {
		return e
	}
	if *state != "" {
		c.Storage.Path, *state = absolute(*state), absolute(*state)
	}
	if *listen != "" {
		c.Server.Listen = *listen
	}
	switch command {
	case "config":
		switch sub {
		case "check":
			fmt.Fprintln(out, "configuration valid")
			return nil
		case "show":
			if !*redact {
				return fmt.Errorf("E_USAGE config show requires --redact")
			}
			b, e := yaml.Marshal(c)
			if e != nil {
				return fmt.Errorf("E_CONFIG_SERIALIZE")
			}
			_, e = out.Write(b)
			return e
		}
	case "serve":
		return Serve(c, out, errout)
	case "rules":
		if sub != "explain" || *input == "" {
			return fmt.Errorf("E_USAGE rules explain --input FILE")
		}
		b, e := os.ReadFile(*input)
		if e != nil {
			return fmt.Errorf("E_INPUT_READ")
		}
		r, err := protocol.Decode(core.Protocol(*proto), strings.NewReader(string(b)), *model, false)
		if err != nil {
			return err
		}
		if c.Model(r.Model) == nil {
			return fmt.Errorf("E_USAGE model not found")
		}
		rule := rules.New(c).Match(rules.Input{Latest: r.Latest, Model: r.Model, Tags: splitTags(*tags), Group: *group})
		v := map[string]any{"latest_user": r.Latest, "rule": rule.ID, "action": rule.Action.Type}
		if rule.Action.Type == "upstream" {
			v["target"] = c.TargetID(c.Model(r.Model), rule)
		} else {
			result, _, err := preset.Prepare(c, r, rule, *seed)
			if err != nil {
				return err
			}
			v["response"] = result.Text
		}
		return json.NewEncoder(out).Encode(v)
	case "assets":
		switch sub {
		case "list", "lint":
			ids := []string{}
			for id := range c.AssetData {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				a := c.AssetData[id]
				fmt.Fprintf(out, "%s\t%s\t%s\n", id, a.Kind, a.License)
			}
			return nil
		case "import":
			if f.NArg() != 1 || *id == "" || *author == "" || *license == "" {
				return fmt.Errorf("E_USAGE assets import requires id, author, license and source")
			}
			return ImportAsset(c, f.Arg(0), config.Asset{ID: *id, Kind: *kind, Author: *author, License: *license, Origin: *origin}, *overwrite)
		}
	case "keys", "db":
		db, e := store.Open(c.Storage.Path, c.Storage.BusyTimeout.Value())
		if e != nil {
			return e
		}
		defer db.Close()
		ctx := context.Background()
		if command == "db" {
			if sub == "backup" {
				if *backup == "" {
					return fmt.Errorf("E_USAGE db backup --out FILE")
				}
				return db.Backup(ctx, *backup)
			}
			if sub == "migrate" {
				fmt.Fprintln(out, "database schema 1; no pending migrations")
				return nil
			}
			return fmt.Errorf("E_USAGE db subcommand")
		}
		if sub == "list" {
			k, e := db.List(ctx)
			if e != nil {
				return fmt.Errorf("E_DB_READ")
			}
			return json.NewEncoder(out).Encode(k)
		}
		if sub == "create" || sub == "import" {
			p, ok := c.KeyPolicies[*policyName]
			if !ok {
				return fmt.Errorf("E_USAGE unknown policy")
			}
			if e := patchPolicy(&p, provided, *tags, *group, *totalLimit, *aiLimit, *rpm, *concurrent, *expiresIn, *expiresAt); e != nil {
				return e
			}
			existing := ""
			if sub == "import" {
				if *fromEnv == "" || os.Getenv(*fromEnv) == "" {
					return fmt.Errorf("E_USAGE keys import --from-env NAME")
				}
				existing = os.Getenv(*fromEnv)
			}
			k, raw, e := db.Create(ctx, p, *label, existing)
			if e != nil {
				return e
			}
			if sub == "import" {
				return json.NewEncoder(out).Encode(k)
			}
			if *format == "json" {
				return json.NewEncoder(out).Encode(map[string]any{"key": raw, "metadata": k})
			}
			fmt.Fprintf(out, "%s\nKey ID: %s (save the key; it is only shown once)\n", raw, k.ID)
			return nil
		}
		if *id == "" {
			return fmt.Errorf("E_USAGE --id required")
		}
		k, e := db.Get(ctx, *id)
		if e != nil {
			return fmt.Errorf("E_KEY_NOT_FOUND")
		}
		switch sub {
		case "show":
			return json.NewEncoder(out).Encode(k)
		case "revoke":
			return db.Revoke(ctx, *id)
		case "reset-usage":
			if !*confirm {
				return fmt.Errorf("E_USAGE --confirm required")
			}
			return db.Reset(ctx, *id)
		case "update":
			if provided["policy"] {
				p, ok := c.KeyPolicies[*policyName]
				if !ok {
					return fmt.Errorf("E_USAGE unknown policy")
				}
				k.Policy = p
			}
			if provided["label"] {
				k.Label = *label
			}
			if e := patchPolicy(&k.Policy, provided, *tags, *group, *totalLimit, *aiLimit, *rpm, *concurrent, *expiresIn, *expiresAt); e != nil {
				return e
			}
			return db.Update(ctx, k)
		}
	}
	return fmt.Errorf("E_USAGE unknown command")
}
func absolute(s string) string { p, _ := filepath.Abs(s); return p }
func splitTags(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}
func patchPolicy(p *config.Policy, provided map[string]bool, tags, group, total, ai, rpm string, max int, in, at string) error {
	if provided["tags"] {
		p.Tags = splitTags(tags)
	}
	if provided["group"] {
		p.Group = group
	}
	for _, x := range []struct {
		name, value string
		dst         **int64
	}{{"total-limit", total, &p.TotalLimit}, {"ai-limit", ai, &p.AILimit}, {"rpm", rpm, &p.RPM}} {
		if provided[x.name] {
			if x.value == "unlimited" {
				*x.dst = nil
			} else {
				n, e := strconv.ParseInt(x.value, 10, 64)
				if e != nil || n < 0 {
					return fmt.Errorf("E_USAGE nonnegative limit or unlimited required")
				}
				*x.dst = &n
			}
		}
	}
	if provided["max-concurrent"] {
		if max < 1 {
			return fmt.Errorf("E_USAGE positive concurrency required")
		}
		p.MaxConcurrent = max
	}
	if in != "" && at != "" {
		return fmt.Errorf("E_USAGE expiry flags mutually exclusive")
	}
	if in != "" {
		duration := in
		if strings.HasSuffix(in, "d") {
			n, e := strconv.ParseFloat(strings.TrimSuffix(in, "d"), 64)
			if e != nil || n <= 0 || n > 36500 {
				return fmt.Errorf("E_USAGE expiry duration")
			}
			duration = fmt.Sprint(n*24) + "h"
		}
		d, e := time.ParseDuration(duration)
		if e != nil || d <= 0 {
			return fmt.Errorf("E_USAGE expiry duration")
		}
		t := time.Now().UTC().Add(d)
		p.ExpiresAt = &t
	}
	if at != "" {
		if at == "none" {
			p.ExpiresAt = nil
		} else {
			t, e := time.Parse(time.RFC3339, at)
			if e != nil {
				return fmt.Errorf("E_USAGE expiry must include timezone")
			}
			p.ExpiresAt = &t
		}
	}
	return nil
}
func Init(template, dir string, out io.Writer) error {
	return initDeployment(template, dir, false, out)
}
func initDeployment(template, dir string, shared bool, out io.Writer) error {
	directoryMode, fileMode := os.FileMode(0700), os.FileMode(0600)
	if shared {
		directoryMode, fileMode = 0755, 0644
	}
	if template != "preset" && template != "hybrid" && template != "testing" {
		return fmt.Errorf("E_USAGE template")
	}
	b, e := examples.Files.ReadFile(template + "/fakeapi.yaml")
	if e != nil {
		return fmt.Errorf("E_INIT_TEMPLATE")
	}
	b = []byte(strings.ReplaceAll(string(b), "../../", ""))
	files := map[string][]byte{"fakeapi.yaml": b, ".env.example": []byte("# Export these explicitly; the binary does not load .env automatically.\nFAKEAPI_UPSTREAM_BASE_URL=http://127.0.0.1:9099/v1\nFAKEAPI_UPSTREAM_API_KEY=\nFAKEAPI_UPSTREAM_MODEL=mock-fast\nFAKEAPI_UPSTREAM_SVG_MODEL=mock-svg\n")}
	e = fs.WalkDir(builtin.Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := builtin.Files.ReadFile(path)
		files[filepath.Join("assets", filepath.FromSlash(path))] = b
		return err
	})
	if e != nil {
		return fmt.Errorf("E_INIT_ASSETS")
	}
	for name := range files {
		if _, e := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(e) {
			return fmt.Errorf("E_INIT_EXISTS: %s", name)
		}
	}
	for name, b := range files {
		p := filepath.Join(dir, name)
		if e = os.MkdirAll(filepath.Dir(p), directoryMode); e != nil {
			return fmt.Errorf("E_INIT_DIRECTORY")
		}
		if e = os.WriteFile(p, b, fileMode); e != nil {
			return fmt.Errorf("E_INIT_WRITE")
		}
	}
	fmt.Fprintln(out, "initialized", dir)
	return nil
}
func ImportAsset(c *config.Config, source string, a config.Asset, overwrite bool) error {
	if !config.Identifier.MatchString(a.ID) {
		return fmt.Errorf("E_ASSET_ID")
	}
	switch a.Kind {
	case "text", "markdown", "ascii", "svg", "json":
	default:
		return fmt.Errorf("E_ASSET_KIND")
	}
	info, e := os.Lstat(source)
	if e != nil || !info.Mode().IsRegular() || info.Size() > int64(c.Server.MaxResponseBytes) {
		return fmt.Errorf("E_ASSET_SOURCE")
	}
	b, e := os.ReadFile(source)
	if e != nil {
		return fmt.Errorf("E_ASSET_READ")
	}
	a.WidthPolicy = "unicode"
	if a.Kind == "ascii" {
		a.WidthPolicy = "ascii"
	}
	if e = config.Lint(b, a.WidthPolicy); e != nil {
		return e
	}
	if a.Kind == "json" && !json.Valid(b) {
		return fmt.Errorf("E_ASSET_JSON")
	}
	raw, e := os.ReadFile(c.Assets.Manifest)
	if e != nil {
		return fmt.Errorf("E_ASSET_MANIFEST")
	}
	var manifest config.Manifest
	if e = config.Strict(raw, &manifest); e != nil {
		return e
	}
	index := -1
	for i, v := range manifest.Assets {
		if v.ID == a.ID {
			index = i
		}
	}
	if index >= 0 && !overwrite {
		return fmt.Errorf("E_ASSET_EXISTS")
	}
	a.File = filepath.ToSlash(filepath.Join("imports", a.ID+".txt"))
	base := filepath.Dir(c.Assets.Manifest)
	target := filepath.Join(base, filepath.FromSlash(a.File))
	manifestInfo, e := os.Stat(c.Assets.Manifest)
	if e != nil {
		return fmt.Errorf("E_ASSET_MANIFEST")
	}
	fileMode := manifestInfo.Mode().Perm() & 0666
	directoryMode := os.FileMode(0700)
	if fileMode&0040 != 0 {
		directoryMode |= 0050
	}
	if fileMode&0004 != 0 {
		directoryMode |= 0005
	}
	if e = os.MkdirAll(filepath.Dir(target), directoryMode); e != nil {
		return fmt.Errorf("E_ASSET_DIRECTORY")
	}
	// Resolve the manifest directory too: Windows short paths and macOS /var
	// aliases may resolve to a different spelling without leaving this directory.
	realBase, e := filepath.EvalSymlinks(base)
	if e != nil {
		return fmt.Errorf("E_ASSET_PATH")
	}
	if real, e := filepath.EvalSymlinks(filepath.Dir(target)); e != nil || absolute(real) != absolute(filepath.Join(realBase, "imports")) {
		return fmt.Errorf("E_ASSET_PATH")
	}
	if e = atomicWrite(target, b, fileMode); e != nil {
		return e
	}
	if index >= 0 {
		manifest.Assets[index] = a
	} else {
		manifest.Assets = append(manifest.Assets, a)
	}
	raw, e = yaml.Marshal(manifest)
	if e != nil {
		return fmt.Errorf("E_ASSET_MANIFEST")
	}
	return atomicWrite(c.Assets.Manifest, raw)
}
func atomicWrite(path string, b []byte, permissions ...os.FileMode) error {
	mode := os.FileMode(0600)
	if info, e := os.Stat(path); e == nil {
		mode = info.Mode().Perm() & 0666
	}
	if len(permissions) > 0 {
		mode = permissions[0]
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".fakeapi-*")
	if e != nil {
		return fmt.Errorf("E_FILE_WRITE")
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(mode); e != nil {
		f.Close()
		return fmt.Errorf("E_FILE_WRITE")
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return fmt.Errorf("E_FILE_WRITE")
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return fmt.Errorf("E_FILE_WRITE")
	}
	if e = f.Close(); e != nil {
		return fmt.Errorf("E_FILE_WRITE")
	}
	if e = os.Rename(name, path); e != nil {
		return fmt.Errorf("E_FILE_REPLACE")
	}
	return nil
}
func Serve(c *config.Config, out, errout io.Writer) error {
	if e := os.MkdirAll(filepath.Dir(c.Storage.Path), 0700); e != nil {
		return fmt.Errorf("E_DB_DIRECTORY")
	}
	lock := flock.New(c.Storage.Path + ".lock")
	ok, e := lock.TryLock()
	if e != nil || !ok {
		return fmt.Errorf("E_SERVICE_ALREADY_RUNNING")
	}
	defer lock.Unlock()
	db, e := store.Open(c.Storage.Path, c.Storage.BusyTimeout.Value())
	if e != nil {
		return e
	}
	defer db.Close()
	log, e := requestlog.New(c.RequestLog, out, errout)
	if e != nil {
		return e
	}
	defer log.Close()
	app := server.New(c, db, log)
	defer app.Close()
	httpServer := &http.Server{Addr: c.Server.Listen, Handler: app, ReadHeaderTimeout: c.Server.ReadHeaderTimeout.Value(), MaxHeaderBytes: 16 << 10, ErrorLog: nil}
	// A fixed-output writer prevents net/http panic diagnostics from leaking data.
	httpServer.ErrorLog = fixedLog(errout)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- httpServer.ListenAndServe() }()
	fmt.Fprintln(errout, "FakeAPI listening", c.Server.Listen)
	select {
	case e := <-done:
		if e != http.ErrServerClosed {
			return fmt.Errorf("E_SERVER_LISTEN")
		}
	case <-ctx.Done():
		app.StopAdmission()
		shutdown, cancel := context.WithTimeout(context.Background(), c.Server.ShutdownTimeout.Value())
		defer cancel()
		if httpServer.Shutdown(shutdown) != nil {
			app.Close()
			_ = httpServer.Close()
		}
	}
	return nil
}
