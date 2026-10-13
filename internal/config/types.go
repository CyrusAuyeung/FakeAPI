package config

import (
	"fmt"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Tag != "!!str" {
		return fmt.Errorf("line %d: duration must be a string", n.Line)
	}
	v, e := time.ParseDuration(n.Value)
	*d = Duration(v)
	return e
}
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }
func (d Duration) Value() time.Duration      { return time.Duration(d) }

type StringValue struct {
	Literal string
	Env     string
}

func (s *StringValue) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		s.Literal = n.Value
		return nil
	}
	if n.Kind == yaml.MappingNode && len(n.Content) == 2 && n.Content[0].Value == "env" && n.Content[1].Tag == "!!str" {
		s.Env = n.Content[1].Value
		return nil
	}
	return fmt.Errorf("line %d: expected string or {env: NAME}", n.Line)
}
func (s StringValue) MarshalYAML() (any, error) {
	if s.Env != "" {
		return map[string]string{"env": s.Env}, nil
	}
	return s.Literal, nil
}
func (s StringValue) Resolve() string {
	if s.Env != "" {
		return os.Getenv(s.Env)
	}
	return s.Literal
}

type Config struct {
	SchemaVersion int                 `yaml:"schema_version"`
	Server        Server              `yaml:"server"`
	Storage       Storage             `yaml:"storage"`
	RequestLog    RequestLog          `yaml:"request_log"`
	Defaults      Defaults            `yaml:"defaults"`
	Profiles      map[string]Profile  `yaml:"profiles"`
	Assets        AssetConfig         `yaml:"assets"`
	KeyPolicies   map[string]Policy   `yaml:"key_policies"`
	Models        []Model             `yaml:"models"`
	Upstreams     map[string]Upstream `yaml:"upstreams"`
	Targets       map[string]Target   `yaml:"targets"`
	DefaultTarget string              `yaml:"default_target"`
	Matchers      map[string]Matcher  `yaml:"matchers"`
	Rules         []Rule              `yaml:"rules"`
	Fallback      Rule                `yaml:"fallback"`
	Path          string              `yaml:"-"`
	AssetData     map[string]Asset    `yaml:"-"`
}
type Server struct {
	Listen                 string   `yaml:"listen"`
	MaxRequestBytes        int64    `yaml:"max_request_bytes"`
	MaxResponseBytes       int      `yaml:"max_response_bytes"`
	MaxSSEEventBytes       int      `yaml:"max_sse_event_bytes"`
	MaxConcurrent          int      `yaml:"max_concurrent"`
	MaxMessages            int      `yaml:"max_messages"`
	MaxTools               int      `yaml:"max_tools"`
	ReadHeaderTimeout      Duration `yaml:"read_header_timeout"`
	ReadBodyTimeout        Duration `yaml:"read_body_timeout"`
	WriteTimeout           Duration `yaml:"write_timeout"`
	ShutdownTimeout        Duration `yaml:"shutdown_timeout"`
	AnthropicMessagesAlias bool     `yaml:"anthropic_messages_alias"`
}
type Storage struct {
	Path        string   `yaml:"path"`
	BusyTimeout Duration `yaml:"busy_timeout"`
}
type RequestLog struct {
	Enabled  bool   `yaml:"enabled"`
	Output   string `yaml:"output"`
	MaxBytes int64  `yaml:"max_bytes"`
	Backups  int    `yaml:"backups"`
}
type Defaults struct {
	Profile string `yaml:"profile"`
	Random  Random `yaml:"random"`
}
type Random struct {
	Mode             string `yaml:"mode"`
	Algorithm        string `yaml:"algorithm"`
	Seed             *int64 `yaml:"seed"`
	AllowRequestSeed bool   `yaml:"allow_request_seed"`
}
type Profile struct {
	Thinking Thinking `yaml:"thinking"`
	Timing   Timing   `yaml:"timing"`
}
type Thinking struct {
	Enabled   bool   `yaml:"enabled"`
	Channel   string `yaml:"channel"`
	Text      string `yaml:"text"`
	TextAsset string `yaml:"text_asset"`
}
type Timing struct {
	FirstOutputDelay    Duration `yaml:"first_output_delay"`
	ThinkingToBodyDelay Duration `yaml:"thinking_to_body_delay"`
	GraphemesPerSecond  float64  `yaml:"graphemes_per_second"`
	ChunkSize           int      `yaml:"chunk_size"`
	Jitter              Duration `yaml:"jitter"`
	PauseProbability    float64  `yaml:"pause_probability"`
	PauseDuration       Duration `yaml:"pause_duration"`
	MaxTotal            Duration `yaml:"max_total"`
}
type ThinkingPatch struct {
	Enabled   *bool   `yaml:"enabled"`
	Channel   *string `yaml:"channel"`
	Text      *string `yaml:"text"`
	TextAsset *string `yaml:"text_asset"`
}
type TimingPatch struct {
	FirstOutputDelay    *Duration `yaml:"first_output_delay"`
	ThinkingToBodyDelay *Duration `yaml:"thinking_to_body_delay"`
	GraphemesPerSecond  *float64  `yaml:"graphemes_per_second"`
	ChunkSize           *int      `yaml:"chunk_size"`
	Jitter              *Duration `yaml:"jitter"`
	PauseProbability    *float64  `yaml:"pause_probability"`
	PauseDuration       *Duration `yaml:"pause_duration"`
	MaxTotal            *Duration `yaml:"max_total"`
}
type Behavior struct {
	Profile  string         `yaml:"profile"`
	Thinking *ThinkingPatch `yaml:"thinking"`
	Timing   *TimingPatch   `yaml:"timing"`
}
type AssetConfig struct {
	Manifest string `yaml:"manifest"`
}
type Manifest struct {
	SchemaVersion int     `yaml:"schema_version"`
	Assets        []Asset `yaml:"assets"`
}
type Asset struct {
	ID          string `yaml:"id" json:"id"`
	File        string `yaml:"file" json:"file"`
	Kind        string `yaml:"kind" json:"kind"`
	Author      string `yaml:"author" json:"author"`
	Origin      string `yaml:"origin" json:"origin"`
	License     string `yaml:"license" json:"license"`
	SourceURL   string `yaml:"source_url,omitempty" json:"source_url,omitempty"`
	CreatedAt   string `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	WidthPolicy string `yaml:"width_policy" json:"width_policy"`
	SHA256      string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	Content     string `yaml:"-" json:"-"`
}
type Policy struct {
	Tags          []string   `yaml:"tags" json:"tags"`
	Group         string     `yaml:"group" json:"group"`
	ExpiresAt     *time.Time `yaml:"expires_at" json:"expires_at"`
	TotalLimit    *int64     `yaml:"total_limit" json:"total_limit"`
	AILimit       *int64     `yaml:"ai_limit" json:"ai_limit"`
	RPM           *int64     `yaml:"rpm" json:"rpm"`
	MaxConcurrent int        `yaml:"max_concurrent" json:"max_concurrent"`
}
type Model struct {
	ID          string    `yaml:"id"`
	DisplayName string    `yaml:"display_name"`
	Profile     string    `yaml:"profile"`
	CreatedAt   time.Time `yaml:"created_at"`
	Limits      Limits    `yaml:"limits"`
	Target      string    `yaml:"target"`
}
type Limits struct {
	InputTokens  int `yaml:"input_tokens"`
	OutputTokens int `yaml:"output_tokens"`
}
type Timeouts struct {
	Connect    Duration `yaml:"connect"`
	FirstEvent Duration `yaml:"first_event"`
	Idle       Duration `yaml:"idle"`
	Total      Duration `yaml:"total"`
}
type Upstream struct {
	Kind                 string      `yaml:"kind"`
	BaseURL              StringValue `yaml:"base_url"`
	APIKeyEnv            string      `yaml:"api_key_env"`
	MaxConcurrent        int         `yaml:"max_concurrent"`
	Timeouts             Timeouts    `yaml:"timeouts"`
	ReasoningTranslation string      `yaml:"reasoning_translation"`
}
type Target struct {
	Upstream              string       `yaml:"upstream"`
	Model                 StringValue  `yaml:"model"`
	MaxTokensParameter    string       `yaml:"max_tokens_parameter"`
	DeveloperRoleFallback string       `yaml:"developer_role_fallback"`
	Capabilities          Capabilities `yaml:"capabilities"`
	Reasoning             Reasoning    `yaml:"reasoning,omitempty"`
}
type Reasoning struct {
	Mode           string   `yaml:"mode,omitempty"`
	History        string   `yaml:"history,omitempty"`
	AllowedEfforts []string `yaml:"allowed_efforts,omitempty"`
	DefaultEffort  string   `yaml:"default_effort,omitempty"`
	CanDisable     *bool    `yaml:"can_disable,omitempty"`
	Budget         bool     `yaml:"budget,omitempty"`
}
type Capabilities struct {
	Images                     bool `yaml:"images"`
	Tools                      bool `yaml:"tools"`
	JSONObject                 bool `yaml:"json_object"`
	JSONSchema                 bool `yaml:"json_schema"`
	DeveloperRole              bool `yaml:"developer_role"`
	ParallelToolCallsParameter bool `yaml:"parallel_tool_calls_parameter"`
}
type Atom struct {
	Exact           *string `yaml:"exact"`
	Keyword         *string `yaml:"keyword"`
	Regex           *string `yaml:"regex"`
	CaseInsensitive bool    `yaml:"case_insensitive"`
	Boundary        string  `yaml:"boundary"`
}
type Matcher struct {
	MaxChars int    `yaml:"max_chars"`
	Any      []Atom `yaml:"any"`
	All      []Atom `yaml:"all"`
	None     []Atom `yaml:"none"`
}
type When struct {
	MatchersAny []string `yaml:"matchers_any"`
	Models      []string `yaml:"models"`
	KeyTagsAny  []string `yaml:"key_tags_any"`
	KeyTagsAll  []string `yaml:"key_tags_all"`
	KeyGroups   []string `yaml:"key_groups"`
	UserText    string   `yaml:"user_text"`
}
type Rule struct {
	ID       string   `yaml:"id"`
	Enabled  *bool    `yaml:"enabled,omitempty"`
	Priority int      `yaml:"priority,omitempty"`
	When     When     `yaml:"when,omitempty"`
	Action   Action   `yaml:"action"`
	Behavior Behavior `yaml:"behavior,omitempty"`
}

func (r Rule) Active() bool { return r.Enabled == nil || *r.Enabled }

type Choice struct {
	Text   *string `yaml:"text,omitempty"`
	Asset  string  `yaml:"asset,omitempty"`
	Weight int     `yaml:"weight,omitempty"`
}
type OnError struct {
	Mode     string  `yaml:"mode"`
	Response *Action `yaml:"response,omitempty"`
}
type Action struct {
	Type       string         `yaml:"type"`
	Text       *string        `yaml:"text,omitempty"`
	Asset      string         `yaml:"asset,omitempty"`
	Choices    []Choice       `yaml:"choices,omitempty"`
	Render     string         `yaml:"render,omitempty"`
	StrictText bool           `yaml:"strict_text,omitempty"`
	JSON       any            `yaml:"json,omitempty"`
	Schema     map[string]any `yaml:"schema,omitempty"`
	Name       string         `yaml:"name,omitempty"`
	Arguments  map[string]any `yaml:"arguments,omitempty"`
	FinalText  string         `yaml:"final_text,omitempty"`
	Target     string         `yaml:"target,omitempty"`
	OnError    *OnError       `yaml:"on_error,omitempty"`
}

func (c *Config) Model(id string) *Model {
	for i := range c.Models {
		if c.Models[i].ID == id {
			return &c.Models[i]
		}
	}
	return nil
}
func (c *Config) TargetID(m *Model, r Rule) string {
	if r.Action.Target != "" {
		return r.Action.Target
	}
	if m.Target != "" {
		return m.Target
	}
	return c.DefaultTarget
}
func (c *Config) EffectiveProfile(m *Model, r Rule) Profile {
	id := c.Defaults.Profile
	if m.Profile != "" {
		id = m.Profile
	}
	if r.Behavior.Profile != "" {
		id = r.Behavior.Profile
	}
	p := c.Profiles[id]
	if q := r.Behavior.Thinking; q != nil {
		if q.Enabled != nil {
			p.Thinking.Enabled = *q.Enabled
		}
		if q.Channel != nil {
			p.Thinking.Channel = *q.Channel
		}
		if q.Text != nil {
			p.Thinking.Text = *q.Text
			p.Thinking.TextAsset = ""
		}
		if q.TextAsset != nil {
			p.Thinking.TextAsset = *q.TextAsset
			p.Thinking.Text = ""
		}
	}
	if q := r.Behavior.Timing; q != nil {
		if q.FirstOutputDelay != nil {
			p.Timing.FirstOutputDelay = *q.FirstOutputDelay
		}
		if q.ThinkingToBodyDelay != nil {
			p.Timing.ThinkingToBodyDelay = *q.ThinkingToBodyDelay
		}
		if q.GraphemesPerSecond != nil {
			p.Timing.GraphemesPerSecond = *q.GraphemesPerSecond
		}
		if q.ChunkSize != nil {
			p.Timing.ChunkSize = *q.ChunkSize
		}
		if q.Jitter != nil {
			p.Timing.Jitter = *q.Jitter
		}
		if q.PauseProbability != nil {
			p.Timing.PauseProbability = *q.PauseProbability
		}
		if q.PauseDuration != nil {
			p.Timing.PauseDuration = *q.PauseDuration
		}
		if q.MaxTotal != nil {
			p.Timing.MaxTotal = *q.MaxTotal
		}
	}
	return p
}
