package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"fakeapi/internal/schema"
	"github.com/rivo/uniseg"
	"go.yaml.in/yaml/v3"
)

var Identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func Strict(data []byte, out any) error {
	var node yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&node); err != nil {
		return fmt.Errorf("E_YAML syntax")
	}
	if err := nodes(&node); err != nil {
		return err
	}
	var extra yaml.Node
	if e := d.Decode(&extra); e != io.EOF {
		return fmt.Errorf("E_YAML multiple documents")
	}
	d = yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if e := d.Decode(out); e != nil {
		return fmt.Errorf("E_YAML type or unknown field: %s", safeYAMLError(e))
	}
	return nil
}
func safeYAMLError(e error) string { // Keep field names/line numbers, never scalar values.
	if t, ok := e.(*yaml.TypeError); ok {
		var lines []string
		for _, s := range t.Errors {
			if i := strings.Index(s, "field "); i >= 0 {
				lines = append(lines, s)
			} else if i := strings.Index(s, ":"); i >= 0 {
				lines = append(lines, s[:i]+": invalid type")
			}
		}
		return strings.Join(lines, "; ")
	}
	return "invalid type"
}
func nodes(n *yaml.Node) error {
	if n.Anchor != "" || n.Kind == yaml.AliasNode {
		return fmt.Errorf("E_YAML line %d: aliases forbidden", n.Line)
	}
	if n.Tag != "" && !strings.HasPrefix(n.Tag, "!!") {
		return fmt.Errorf("E_YAML line %d: tags forbidden", n.Line)
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Value == "<<" || seen[k.Value] {
				return fmt.Errorf("E_YAML line %d: duplicate/merge key", k.Line)
			}
			seen[k.Value] = true
		}
	}
	for _, c := range n.Content {
		if e := nodes(c); e != nil {
			return e
		}
	}
	return nil
}
func path(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}
func Load(file string, skipEnv bool) (*Config, error) {
	b, e := os.ReadFile(file)
	if e != nil {
		return nil, fmt.Errorf("E_CONFIG_READ")
	}
	c := new(Config)
	if e = Strict(b, c); e != nil {
		return nil, e
	}
	if e = positiveValues(b); e != nil {
		return nil, e
	}
	c.Path, _ = filepath.Abs(file)
	base := filepath.Dir(c.Path)
	if c.SchemaVersion != 1 {
		return nil, fmt.Errorf("E_CONFIG schema_version")
	}
	s := &c.Server
	if s.Listen == "" {
		s.Listen = "127.0.0.1:8787"
	}
	if _, _, e = net.SplitHostPort(s.Listen); e != nil {
		return nil, fmt.Errorf("E_CONFIG server.listen")
	}
	if s.MaxRequestBytes == 0 {
		s.MaxRequestBytes = 8 << 20
	}
	if s.MaxResponseBytes == 0 {
		s.MaxResponseBytes = 1 << 20
	}
	if s.MaxSSEEventBytes == 0 {
		s.MaxSSEEventBytes = 1 << 20
	}
	if s.MaxConcurrent == 0 {
		s.MaxConcurrent = 64
	}
	if s.MaxMessages == 0 {
		s.MaxMessages = 256
	}
	if s.MaxTools == 0 {
		s.MaxTools = 32
	}
	if s.ReadHeaderTimeout == 0 {
		s.ReadHeaderTimeout = Duration(5 * time.Second)
	}
	if s.ReadBodyTimeout == 0 {
		s.ReadBodyTimeout = Duration(15 * time.Second)
	}
	if s.WriteTimeout == 0 {
		s.WriteTimeout = Duration(10 * time.Second)
	}
	if s.ShutdownTimeout == 0 {
		s.ShutdownTimeout = Duration(10 * time.Second)
	}
	if s.MaxRequestBytes < 1 || s.MaxResponseBytes < 1 || s.MaxSSEEventBytes < 1 || s.MaxConcurrent < 1 || s.MaxMessages < 1 || s.MaxTools < 1 || s.ReadHeaderTimeout <= 0 || s.ReadBodyTimeout <= 0 || s.WriteTimeout <= 0 || s.ShutdownTimeout <= 0 {
		return nil, fmt.Errorf("E_CONFIG server limits")
	}
	if c.Storage.Path == "" {
		c.Storage.Path = "data/state.sqlite"
	}
	c.Storage.Path = path(base, c.Storage.Path)
	if c.Storage.BusyTimeout == 0 {
		c.Storage.BusyTimeout = Duration(2 * time.Second)
	}
	if c.Storage.BusyTimeout < 0 {
		return nil, fmt.Errorf("E_CONFIG storage.busy_timeout")
	}
	if c.RequestLog.Output == "" {
		c.RequestLog.Output = "stdout"
	}
	if c.RequestLog.Output != "stdout" {
		c.RequestLog.Output = path(base, c.RequestLog.Output)
	}
	if c.RequestLog.MaxBytes == 0 {
		c.RequestLog.MaxBytes = 10 << 20
	}
	if c.RequestLog.Backups == 0 {
		c.RequestLog.Backups = 3
	}
	if c.RequestLog.MaxBytes < 1 || c.RequestLog.Backups < 1 {
		return nil, fmt.Errorf("E_CONFIG request_log")
	}
	c.Assets.Manifest = path(base, c.Assets.Manifest)
	if e = c.loadAssets(); e != nil {
		return nil, e
	}
	if c.Defaults.Profile == "" {
		c.Defaults.Profile = "natural"
	}
	if _, ok := c.Profiles[c.Defaults.Profile]; !ok {
		return nil, fmt.Errorf("E_CONFIG_REF defaults.profile")
	}
	if c.Defaults.Random.Mode != "random" && c.Defaults.Random.Mode != "deterministic" {
		return nil, fmt.Errorf("E_CONFIG defaults.random.mode")
	}
	if c.Defaults.Random.Algorithm != "pcg_v1" {
		return nil, fmt.Errorf("E_CONFIG defaults.random.algorithm")
	}
	if c.Defaults.Random.Mode == "deterministic" && c.Defaults.Random.Seed == nil {
		return nil, fmt.Errorf("E_CONFIG defaults.random.seed")
	}
	for id, p := range c.Profiles {
		if !Identifier.MatchString(id) {
			return nil, fmt.Errorf("E_CONFIG profile id")
		}
		if e = c.checkProfile(p); e != nil {
			return nil, fmt.Errorf("profiles.%s: %w", id, e)
		}
	}
	for id, p := range c.KeyPolicies {
		if !Identifier.MatchString(id) || p.MaxConcurrent < 1 || negative(p.TotalLimit) || negative(p.AILimit) || negative(p.RPM) {
			return nil, fmt.Errorf("E_CONFIG key_policies.%s", id)
		}
	}
	seen := map[string]bool{}
	if len(c.Models) == 0 {
		return nil, fmt.Errorf("E_CONFIG models empty")
	}
	for i := range c.Models {
		m := &c.Models[i]
		if !Identifier.MatchString(m.ID) || seen[m.ID] {
			return nil, fmt.Errorf("E_CONFIG model id")
		}
		seen[m.ID] = true
		if m.Profile != "" {
			if _, ok := c.Profiles[m.Profile]; !ok {
				return nil, fmt.Errorf("E_CONFIG_REF models.profile")
			}
		}
		if m.CreatedAt.IsZero() {
			m.CreatedAt = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
		}
		if m.Limits.InputTokens == 0 {
			m.Limits.InputTokens = 32768
		}
		if m.Limits.OutputTokens == 0 {
			m.Limits.OutputTokens = 8192
		}
		if m.Limits.InputTokens < 1 || m.Limits.OutputTokens < 1 {
			return nil, fmt.Errorf("E_CONFIG model limits")
		}
	}
	for id, u := range c.Upstreams {
		if !Identifier.MatchString(id) || u.Kind != "openai_chat" || u.MaxConcurrent < 1 {
			return nil, fmt.Errorf("E_CONFIG upstreams.%s", id)
		}
		if u.ReasoningTranslation == "" {
			u.ReasoningTranslation = "omit"
		}
		if u.ReasoningTranslation != "omit" && u.ReasoningTranslation != "portable_text" {
			return nil, fmt.Errorf("E_CONFIG reasoning_translation")
		}
		if u.Timeouts.Connect == 0 {
			u.Timeouts.Connect = Duration(5 * time.Second)
		}
		if u.Timeouts.FirstEvent == 0 {
			u.Timeouts.FirstEvent = Duration(20 * time.Second)
		}
		if u.Timeouts.Idle == 0 {
			u.Timeouts.Idle = Duration(30 * time.Second)
		}
		if u.Timeouts.Total == 0 {
			u.Timeouts.Total = Duration(120 * time.Second)
		}
		for _, t := range []Duration{u.Timeouts.Connect, u.Timeouts.FirstEvent, u.Timeouts.Idle} {
			if t <= 0 || t > u.Timeouts.Total {
				return nil, fmt.Errorf("E_CONFIG upstream timeouts")
			}
		}
		if u.BaseURL.Env == "" || !skipEnv {
			v, err := url.Parse(u.BaseURL.Resolve())
			if err != nil || v.Host == "" || (v.Scheme != "http" && v.Scheme != "https") || v.User != nil || v.RawQuery != "" || v.Fragment != "" {
				return nil, fmt.Errorf("E_CONFIG upstream base_url")
			}
		}
		c.Upstreams[id] = u
	}
	for id, t := range c.Targets {
		if _, ok := c.Upstreams[t.Upstream]; !ok {
			return nil, fmt.Errorf("E_CONFIG_REF targets.%s.upstream", id)
		}
		if t.MaxTokensParameter == "" {
			t.MaxTokensParameter = "max_tokens"
		}
		if t.MaxTokensParameter != "max_tokens" && t.MaxTokensParameter != "max_completion_tokens" {
			return nil, fmt.Errorf("E_CONFIG target token parameter")
		}
		if t.DeveloperRoleFallback == "" {
			t.DeveloperRoleFallback = "reject"
		}
		if t.DeveloperRoleFallback != "reject" && t.DeveloperRoleFallback != "system" {
			return nil, fmt.Errorf("E_CONFIG developer_role_fallback")
		}
		if t.Capabilities.JSONSchema && !t.Capabilities.JSONObject {
			return nil, fmt.Errorf("E_CONFIG target capabilities")
		}
		c.Targets[id] = t
	}
	if c.DefaultTarget != "" {
		if _, ok := c.Targets[c.DefaultTarget]; !ok {
			return nil, fmt.Errorf("E_CONFIG_REF default_target")
		}
	}
	for _, m := range c.Models {
		if m.Target != "" {
			if _, ok := c.Targets[m.Target]; !ok {
				return nil, fmt.Errorf("E_CONFIG_REF model.target")
			}
		}
	}
	for id, m := range c.Matchers {
		if len(m.Any)+len(m.All) == 0 || len(m.Any)+len(m.All)+len(m.None) > 32 || m.MaxChars < 0 {
			return nil, fmt.Errorf("E_CONFIG matcher %s", id)
		}
		for _, a := range append(append(append([]Atom{}, m.Any...), m.All...), m.None...) {
			n := 0
			for _, v := range []*string{a.Exact, a.Keyword, a.Regex} {
				if v != nil {
					n++
				}
			}
			if n != 1 {
				return nil, fmt.Errorf("E_CONFIG matcher atom")
			}
			if a.Regex != nil {
				if len(*a.Regex) > 2048 {
					return nil, fmt.Errorf("E_CONFIG regexp length")
				}
				if _, err := regexp.Compile(*a.Regex); err != nil {
					return nil, fmt.Errorf("E_CONFIG matcher %s: invalid RE2", id)
				}
			}
			if a.Boundary != "" && a.Boundary != "ascii_word" {
				return nil, fmt.Errorf("E_CONFIG matcher boundary")
			}
		}
	}
	seen = map[string]bool{}
	for _, r := range append(append([]Rule{}, c.Rules...), c.Fallback) {
		if !Identifier.MatchString(r.ID) || seen[r.ID] {
			return nil, fmt.Errorf("E_CONFIG rule id")
		}
		seen[r.ID] = true
		for _, id := range r.When.MatchersAny {
			if _, ok := c.Matchers[id]; !ok {
				return nil, fmt.Errorf("E_CONFIG_REF rule.matcher")
			}
		}
		for _, id := range r.When.Models {
			if c.Model(id) == nil {
				return nil, fmt.Errorf("E_CONFIG_REF rule.model")
			}
		}
		if r.When.UserText != "" && r.When.UserText != "absent" && r.When.UserText != "empty" && r.When.UserText != "present" {
			return nil, fmt.Errorf("E_CONFIG when.user_text")
		}
		if r.Behavior.Profile != "" {
			if _, ok := c.Profiles[r.Behavior.Profile]; !ok {
				return nil, fmt.Errorf("E_CONFIG_REF behavior.profile")
			}
		}
		if e = c.checkAction(r.Action); e != nil {
			return nil, fmt.Errorf("rules.%s: %w", r.ID, e)
		}
		for _, m := range c.Models {
			if len(r.When.Models) > 0 {
				matches := false
				for _, id := range r.When.Models {
					if id == m.ID {
						matches = true
					}
				}
				if !matches {
					continue
				}
			}
			if e = c.checkProfile(c.EffectiveProfile(&m, r)); e != nil {
				return nil, e
			}
			if r.Action.Type == "upstream" && r.Active() {
				id := c.TargetID(&m, r)
				t, ok := c.Targets[id]
				if !ok {
					return nil, fmt.Errorf("E_CONFIG_REF rules.%s.action.target", r.ID)
				}
				u := c.Upstreams[t.Upstream]
				if !skipEnv && (t.Model.Resolve() == "" || os.Getenv(u.APIKeyEnv) == "") {
					return nil, fmt.Errorf("E_CONFIG_ENV rules.%s", r.ID)
				}
			}
		}
		if r.Behavior.Thinking != nil && r.Behavior.Thinking.Enabled != nil && *r.Behavior.Thinking.Enabled && c.StrictAction(r.Action) {
			return nil, fmt.Errorf("E_CONFIG strict action thinking")
		}
	}
	if !c.Fallback.Active() || c.Fallback.Action.Type != "preset" {
		return nil, fmt.Errorf("E_CONFIG fallback")
	}
	return c, nil
}

// Defaults apply only to omitted positive limits, not to an explicit zero/null.
// Keeping presence in the YAML tree avoids changing the public runtime types.
func positiveValues(data []byte) error {
	var root yaml.Node
	if e := yaml.Unmarshal(data, &root); e != nil {
		return fmt.Errorf("E_YAML syntax")
	}
	var walk func(*yaml.Node, string) error
	walk = func(n *yaml.Node, path string) error {
		if n.Kind == yaml.DocumentNode {
			return walk(n.Content[0], path)
		}
		if n.Kind == yaml.SequenceNode {
			for _, child := range n.Content {
				if e := walk(child, path+".[]"); e != nil {
					return e
				}
			}
		}
		if n.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i < len(n.Content); i += 2 {
			key, value := n.Content[i].Value, n.Content[i+1]
			p := key
			if path != "" {
				p = path + "." + key
			}
			positive, duration := false, false
			switch p {
			case "server.max_request_bytes", "server.max_response_bytes", "server.max_sse_event_bytes", "server.max_concurrent", "server.max_messages", "server.max_tools", "request_log.max_bytes", "request_log.backups", "models.[].limits.input_tokens", "models.[].limits.output_tokens":
				positive = true
			case "server.read_header_timeout", "server.read_body_timeout", "server.write_timeout", "server.shutdown_timeout":
				positive, duration = true, true
			}
			if strings.HasPrefix(p, "upstreams.") && strings.Contains(p, ".timeouts.") {
				positive, duration = true, true
			}
			if positive {
				valid := false
				if duration {
					d, e := time.ParseDuration(value.Value)
					valid = e == nil && d > 0
				} else {
					var v int64
					valid = value.Decode(&v) == nil && v > 0
				}
				if !valid {
					return fmt.Errorf("E_CONFIG %s must be positive (line %d)", p, value.Line)
				}
			}
			if e := walk(value, p); e != nil {
				return e
			}
		}
		return nil
	}
	return walk(&root, "")
}
func negative(n *int64) bool { return n != nil && *n < 0 }
func (c *Config) checkProfile(p Profile) error {
	t := p.Timing
	if t.ChunkSize < 1 || t.GraphemesPerSecond < 0 || math.IsInf(t.GraphemesPerSecond, 0) || math.IsNaN(t.GraphemesPerSecond) || math.IsNaN(t.PauseProbability) || t.FirstOutputDelay < 0 || t.ThinkingToBodyDelay < 0 || t.Jitter < 0 || t.PauseDuration < 0 || t.PauseProbability < 0 || t.PauseProbability > 1 || t.MaxTotal <= 0 {
		return fmt.Errorf("E_CONFIG timing")
	}
	if p.Thinking.Enabled {
		q := p.Thinking
		if q.Channel != "portable_text" && q.Channel != "native" {
			return fmt.Errorf("E_CONFIG thinking.channel")
		}
		if (q.Text == "") == (q.TextAsset == "") {
			return fmt.Errorf("E_CONFIG thinking text")
		}
		if q.TextAsset != "" {
			if _, ok := c.AssetData[q.TextAsset]; !ok {
				return fmt.Errorf("E_CONFIG_REF thinking.text_asset")
			}
		}
	}
	return nil
}
func (c *Config) StrictAction(a Action) bool {
	return a.StrictText || a.Type == "arithmetic" || a.Type == "json_fixture" || a.Type == "tool_fixture" || c.AssetData[a.Asset].Kind == "svg" || c.AssetData[a.Asset].Kind == "json"
}
func (c *Config) checkAction(a Action) error {
	if a.Target != "" {
		if _, ok := c.Targets[a.Target]; !ok {
			return fmt.Errorf("E_CONFIG_REF action.target")
		}
	}
	if a.Type != "upstream" && a.OnError != nil {
		return fmt.Errorf("E_CONFIG on_error")
	}
	switch a.Type {
	case "preset":
		n := 0
		if a.Text != nil {
			n++
		}
		if a.Asset != "" {
			n++
			if _, ok := c.AssetData[a.Asset]; !ok {
				return fmt.Errorf("E_CONFIG_REF action.asset")
			}
		}
		if len(a.Choices) > 0 {
			n++
			total := 0
			for _, q := range a.Choices {
				if (q.Text == nil) == (q.Asset == "") || q.Weight < 0 {
					return fmt.Errorf("E_CONFIG choice")
				}
				weight := q.Weight
				if weight == 0 {
					weight = 1
				}
				if total > math.MaxInt-weight {
					return fmt.Errorf("E_CONFIG choice weights overflow")
				}
				total += weight
				if q.Asset != "" {
					if _, ok := c.AssetData[q.Asset]; !ok {
						return fmt.Errorf("E_CONFIG_REF choice.asset")
					}
				}
			}
		}
		if n != 1 {
			return fmt.Errorf("E_CONFIG preset content")
		}
		if a.Render != "" && a.Render != "auto" && a.Render != "plain" && a.Render != "markdown_code" {
			return fmt.Errorf("E_CONFIG render")
		}
	case "arithmetic":
	case "json_fixture":
		if a.JSON == nil {
			return fmt.Errorf("E_CONFIG json_fixture")
		}
		if len(a.Schema) > 0 {
			if e := schema.Validate(a.Schema, a.JSON); e != nil {
				return fmt.Errorf("E_CONFIG fixture schema")
			}
		}
	case "tool_fixture":
		if a.Name == "" || a.Arguments == nil || a.FinalText == "" {
			return fmt.Errorf("E_CONFIG tool_fixture")
		}
	case "upstream":
		if a.OnError != nil {
			switch a.OnError.Mode {
			case "api_error", "frog":
				if a.OnError.Response != nil {
					return fmt.Errorf("E_CONFIG on_error.response")
				}
			case "preset":
				if a.OnError.Response == nil || a.OnError.Response.Type != "preset" || a.OnError.Response.OnError != nil {
					return fmt.Errorf("E_CONFIG on_error.response")
				}
				return c.checkAction(*a.OnError.Response)
			default:
				return fmt.Errorf("E_CONFIG on_error.mode")
			}
		}
	default:
		return fmt.Errorf("E_CONFIG action.type")
	}
	return nil
}
func (c *Config) loadAssets() error {
	b, e := os.ReadFile(c.Assets.Manifest)
	if e != nil {
		return fmt.Errorf("E_ASSET manifest read")
	}
	var m Manifest
	if e = Strict(b, &m); e != nil {
		return e
	}
	if m.SchemaVersion != 1 {
		return fmt.Errorf("E_ASSET schema_version")
	}
	c.AssetData = map[string]Asset{}
	for _, a := range m.Assets {
		if !Identifier.MatchString(a.ID) || a.Author == "" || a.Origin == "" || a.License == "" || c.AssetData[a.ID].ID != "" {
			return fmt.Errorf("E_ASSET metadata")
		}
		p := path(filepath.Dir(c.Assets.Manifest), a.File)
		b, e := os.ReadFile(p)
		if e != nil {
			return fmt.Errorf("E_ASSET file read")
		}
		if len(b) > c.Server.MaxResponseBytes {
			return fmt.Errorf("E_ASSET size")
		}
		if e = Lint(b, a.WidthPolicy); e != nil {
			return fmt.Errorf("assets.%s: %w", a.ID, e)
		}
		switch a.Kind {
		case "ascii", "text", "markdown", "svg", "json":
		default:
			return fmt.Errorf("E_ASSET kind")
		}
		if a.Kind == "json" && !json.Valid(b) {
			return fmt.Errorf("E_ASSET json")
		}
		sum := sha256.Sum256(b)
		if a.SHA256 != "" && a.SHA256 != hex.EncodeToString(sum[:]) {
			return fmt.Errorf("E_ASSET checksum")
		}
		a.Content = string(b)
		c.AssetData[a.ID] = a
	}
	return nil
}
func Lint(b []byte, width string) error {
	if !utf8.Valid(b) || bytes.HasPrefix(b, []byte{239, 187, 191}) || bytes.ContainsRune(b, '\r') || len(b) == 0 || b[len(b)-1] != '\n' {
		return fmt.Errorf("E_ASSET UTF-8/LF/newline")
	}
	for _, r := range string(b) {
		if (unicode.IsControl(r) && r != '\n') || r == 0x202e || r == 0x202d || (r >= 0x2066 && r <= 0x2069) || (width == "ascii" && r > 126) {
			return fmt.Errorf("E_ASSET forbidden character")
		}
	}
	if width == "ascii" {
		for _, line := range strings.Split(string(b), "\n") {
			if uniseg.StringWidth(line) > 80 {
				return fmt.Errorf("E_ASSET width exceeds 80")
			}
		}
	}
	return nil
}
