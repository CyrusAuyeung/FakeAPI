package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
)

type Protocol string

const (
	Chat      Protocol = "chat"
	Responses Protocol = "responses"
	Anthropic Protocol = "anthropic"
	Gemini    Protocol = "gemini"
)

type Error struct {
	Code       string
	Status     int
	Message    string
	Param      string
	RetryAfter int
}

func (e *Error) Error() string { return e.Code }
func Err(code string, status int, message string) *Error {
	return &Error{Code: code, Status: status, Message: message}
}
func Invalid(path string) *Error {
	return &Error{Code: "unsupported_parameter", Status: 400, Message: "Unsupported or invalid parameter: " + path, Param: path}
}

type Part struct {
	Kind      string
	Text      string
	URL       string
	MIME      string
	Data      string
	Detail    string
	ID        string
	Name      string
	Arguments json.RawMessage
	Opaque    bool
	Signature string
}
type Message struct {
	Role        string
	Parts       []Part
	SourceIndex int
}
type LatestUser struct {
	Present   bool
	Index     int
	Text      string
	HasImages bool
}
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
}
type Constraint struct {
	Kind   string
	Schema map[string]any
	Name   string
	Strict bool
}
type Request struct {
	Protocol     Protocol
	Model        string
	Stream       bool
	Messages     []Message
	Latest       LatestUser
	Tools        []Tool
	ToolChoice   string // auto, none, required, or a function name
	Constraint   Constraint
	MaxTokens    *int
	Stop         []string
	IncludeUsage bool
	ThinkingOff  bool
	Reasoning    Reasoning
	Parameters   map[string]any
	Extras       map[string]any
}

// Reasoning separates generation controls from whether a client displays them.
// A nil field preserves the upstream's default instead of inventing a setting.
type Reasoning struct {
	Enabled  *bool
	Effort   string
	Budget   *int
	Include  *bool
	Adaptive bool
}

func Normalize(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"))
}
func Extract(messages []Message) LatestUser {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != "user" {
			continue
		}
		l := LatestUser{Present: true, Index: i}
		var texts []string
		for _, p := range m.Parts {
			if p.Kind == "text" {
				texts = append(texts, p.Text)
			}
			if p.Kind == "image" {
				l.HasImages = true
			}
		}
		l.Text = Normalize(strings.Join(texts, "\n"))
		return l
	}
	return LatestUser{Index: -1}
}

type Usage struct {
	Input     int
	Visible   int
	Reasoning int
	Cached    int
	Source    string
}

func Estimate(s string) int { return (len([]byte(s)) + 3) / 4 }
func InputUsage(r *Request) int {
	n := 0
	for _, m := range r.Messages {
		n += 4
		for _, p := range m.Parts {
			switch p.Kind {
			case "image":
				n += 256
			default:
				n += Estimate(p.Text + p.Name + string(p.Arguments))
			}
		}
	}
	for _, t := range r.Tools {
		b, _ := json.Marshal(t.Schema)
		n += Estimate(t.Name + t.Description + string(b))
	}
	return n
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}
type Result struct {
	ID        string
	Text      string
	Reasoning string
	Thinking  []Part
	Tool      *ToolCall
	Usage     Usage
	Finish    string
	Stop      string
}
type Event struct {
	Kind   string
	Text   string
	Tool   *ToolCall
	Part   *Part
	Usage  Usage
	Finish string
	Stop   string
}

func ID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("E_RANDOM")
	}
	return prefix + hex.EncodeToString(b)
}
