package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"fakeapi/internal/pacing"
	"fakeapi/internal/preset"
	"fakeapi/internal/protocol"
	"fakeapi/internal/requestlog"
	"fakeapi/internal/rules"
	"fakeapi/internal/store"
	"fakeapi/internal/upstream"
)

type Server struct {
	Config          *config.Config
	Store           *store.Store
	Rules           *rules.Engine
	Log             *requestlog.Logger
	clients         map[string]*upstream.Client
	mu              sync.Mutex
	active          int
	keys, providers map[string]int
	closing         atomic.Bool
	cancel          context.CancelFunc
	ctx             context.Context
}

func New(c *config.Config, db *store.Store, log *requestlog.Logger) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{Config: c, Store: db, Log: log, Rules: rules.New(c), clients: map[string]*upstream.Client{}, keys: map[string]int{}, providers: map[string]int{}, ctx: ctx, cancel: cancel}
	for id, u := range c.Upstreams {
		s.clients[id] = upstream.New(u)
	}
	return s
}
func (s *Server) StopAdmission() { s.closing.Store(true) }
func (s *Server) Close() {
	s.closing.Store(true)
	s.cancel()
	for _, c := range s.clients {
		c.Close()
	}
}
func (s *Server) Active() int { s.mu.Lock(); defer s.mu.Unlock(); return s.active }
func (s *Server) acquire(k store.Key, provider string) (func(), int, *core.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing.Load() {
		return nil, 0, core.Err("service_stopping", 503, "Service stopping")
	}
	if s.active >= s.Config.Server.MaxConcurrent || s.keys[k.ID] >= k.MaxConcurrent || provider != "" && s.providers[provider] >= s.Config.Upstreams[provider].MaxConcurrent {
		return nil, 0, core.Err("concurrency_exceeded", 503, "Concurrent request limit reached")
	}
	s.active++
	s.keys[k.ID]++
	if provider != "" {
		s.providers[provider]++
	}
	active := s.keys[k.ID]
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.active--
			s.keys[k.ID]--
			if s.keys[k.ID] == 0 {
				delete(s.keys, k.ID)
			}
			if provider != "" {
				s.providers[provider]--
				if s.providers[provider] == 0 {
					delete(s.providers, provider)
				}
			}
		})
	}, active, nil
}
func credential(r *http.Request, p core.Protocol) (string, *core.Error) {
	bearer := ""
	if h := r.Header.Get("Authorization"); h != "" {
		if !strings.HasPrefix(h, "Bearer ") {
			return "", core.Err("invalid_api_key", 401, "Expected Bearer credential")
		}
		bearer = strings.TrimPrefix(h, "Bearer ")
	}
	if p == core.Anthropic {
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			return "", core.Invalid("anthropic-version")
		}
		if r.Header.Get("anthropic-beta") != "" {
			return "", core.Invalid("anthropic-beta")
		}
		v := r.Header.Get("x-api-key")
		if v != "" && bearer != "" && v != bearer {
			return "", core.Err("conflicting_credentials", 400, "Conflicting credentials")
		}
		if v != "" {
			bearer = v
		}
	}
	if p == core.Gemini {
		v := r.Header.Get("x-goog-api-key")
		q := r.URL.Query().Get("key")
		if v != "" && q != "" && v != q {
			return "", core.Err("conflicting_credentials", 400, "Conflicting credentials")
		}
		if v == "" {
			v = q
		}
		if v != "" && bearer != "" && v != bearer {
			return "", core.Err("conflicting_credentials", 400, "Conflicting credentials")
		}
		if v != "" {
			bearer = v
		}
	}
	return bearer, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, h *http.Request) {
	p := core.Chat
	path := h.URL.Path
	if strings.HasPrefix(path, "/anthropic/") || path == "/v1/messages" {
		p = core.Anthropic
	}
	if strings.HasPrefix(path, "/v1beta/") {
		p = core.Gemini
	}
	if path == "/v1/responses" {
		p = core.Responses
	}
	if path == "/healthz" || path == "/readyz" {
		if h.Method != "GET" {
			protocol.WriteError(w, p, core.Err("method_not_allowed", 405, "Method not allowed"))
			return
		}
		status := 200
		if path == "/readyz" && s.closing.Load() {
			status = 503
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": map[bool]string{true: "stopping", false: "ok"}[s.closing.Load()]})
		return
	}
	if strings.HasPrefix(path, "/v1/models") || strings.HasPrefix(path, "/anthropic/v1/models") || strings.HasPrefix(path, "/v1beta/models") && h.Method == "GET" {
		s.models(w, h, p)
		return
	}
	isGemini := p == core.Gemini && (strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent"))
	if path != "/v1/chat/completions" && path != "/v1/responses" && path != "/anthropic/v1/messages" && !(path == "/v1/messages" && s.Config.Server.AnthropicMessagesAlias) && !isGemini {
		protocol.WriteError(w, p, core.Err("not_found", 404, "Endpoint not found"))
		return
	}
	start := time.Now()
	var model, ruleID *string
	defer func() {
		if s.Log != nil {
			s.Log.Write(start, model, time.Since(start), ruleID)
		}
	}()
	stream := false
	var wire *protocol.Stream
	defer func() {
		if recover() != nil {
			e := core.Err("internal_error", 500, "Internal service error")
			if wire != nil && wire.Committed {
				wire.Fail(e)
			} else {
				protocol.WriteError(w, p, e)
			}
		}
	}()
	fail := func(e *core.Error) {
		if h.Context().Err() != nil {
			return
		}
		if wire != nil && wire.Committed {
			wire.Fail(e)
		} else {
			protocol.WriteError(w, p, e)
		}
	}
	if h.Method != "POST" {
		fail(core.Err("method_not_allowed", 405, "Method not allowed"))
		return
	}
	mediaType, _, mediaError := mime.ParseMediaType(h.Header.Get("Content-Type"))
	if mediaError != nil || mediaType != "application/json" {
		fail(core.Err("invalid_content_type", 400, "Expected application/json"))
		return
	}
	raw, e := credential(h, p)
	if e != nil {
		fail(e)
		return
	}
	key, e := s.Store.Auth(h.Context(), raw, time.Now())
	if e != nil {
		fail(e)
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(s.Config.Server.ReadBodyTimeout.Value()))
	data, err := io.ReadAll(io.LimitReader(h.Body, s.Config.Server.MaxRequestBytes+1))
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
	if err != nil {
		fail(core.Err("invalid_json", 400, "Unable to read request body"))
		return
	}
	if int64(len(data)) > s.Config.Server.MaxRequestBytes {
		fail(core.Err("request_too_large", 413, "Request body exceeds configured limit"))
		return
	}
	pathModel := ""
	if isGemini {
		pathModel = strings.TrimPrefix(strings.Split(path, ":")[0], "/v1beta/models/")
		stream = strings.HasSuffix(path, ":streamGenerateContent")
		if stream && h.URL.Query().Get("alt") != "sse" {
			fail(core.Invalid("alt"))
			return
		}
	}
	r, e := protocol.Decode(p, strings.NewReader(string(data)), pathModel, stream)
	if e != nil {
		fail(e)
		return
	}
	m := s.Config.Model(r.Model)
	if m == nil {
		fail(core.Err("model_not_found", 404, "Model not found"))
		return
	}
	model = &m.ID
	if len(r.Messages) > s.Config.Server.MaxMessages || len(r.Tools) > s.Config.Server.MaxTools {
		fail(core.Invalid("message/tool count"))
		return
	}
	if r.MaxTokens != nil && *r.MaxTokens > m.Limits.OutputTokens {
		fail(core.Invalid("output token limit"))
		return
	}
	if core.InputUsage(r) > m.Limits.InputTokens {
		fail(core.Invalid("input token limit"))
		return
	}
	rule := s.Rules.Match(rules.Input{Latest: r.Latest, Model: r.Model, Tags: key.Tags, Group: key.Group})
	ruleID = &rule.ID
	isAI := rule.Action.Type == "upstream"
	provider := ""
	var target config.Target
	var u config.Upstream
	var result core.Result
	var profile config.Profile
	var override *int64
	if v := h.Header.Get("X-FakeAPI-Seed"); v != "" && s.Config.Defaults.Random.AllowRequestSeed {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			fail(core.Invalid("X-FakeAPI-Seed"))
			return
		}
		override = &n
	}
	seed := pacing.Seed(s.Config.Defaults.Random, override)
	if isAI {
		id := s.Config.TargetID(m, rule)
		target = s.Config.Targets[id]
		provider = target.Upstream
		u = s.Config.Upstreams[provider]
		if e = upstream.Preflight(r, target, u); e != nil {
			fail(e)
			return
		}
		if _, e = upstream.Translate(r, target); e != nil {
			fail(e)
			return
		}
	} else {
		result, profile, e = preset.Prepare(s.Config, r, rule, seed)
		if e != nil {
			fail(e)
			return
		}
	}
	release, active, e := s.acquire(key, provider)
	if e != nil {
		fail(e)
		return
	}
	defer release()
	if e = s.Store.Admit(h.Context(), key.ID, isAI, time.Now(), active); e != nil {
		fail(e)
		return
	}
	limit := profile.Timing.MaxTotal.Value()
	if isAI {
		limit = u.Timeouts.Total.Value()
	}
	ctx, cancel := context.WithTimeout(h.Context(), limit)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	aggregated := core.Result{ID: core.ID("fake_"), Finish: "stop"}
	wire = protocol.NewStream(w, r, aggregated.ID, s.Config.Server.WriteTimeout.Value())
	if !isAI || p == core.Anthropic && r.Stream {
		w.Header().Set("X-FakeAPI-Usage", "estimated")
	}
	if isAI && p != core.Chat && !(u.Kind == "anthropic" && p == core.Anthropic) && u.ReasoningTranslation == "omit" {
		w.Header().Set("X-FakeAPI-Reasoning", "omitted-in-translation")
	}
	emit := func(event core.Event) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if r.Stream {
			return wire.Emit(event)
		}
		switch event.Kind {
		case "thinking_block":
			if event.Part != nil {
				aggregated.Thinking = append(aggregated.Thinking, *event.Part)
			}
		case "text_delta":
			aggregated.Text += event.Text
		case "reasoning_delta":
			aggregated.Reasoning += event.Text
		case "tool_start":
			t := *event.Tool
			t.Arguments = ""
			aggregated.Tool = &t
		case "tool_args_delta":
			if aggregated.Tool == nil {
				return fmt.Errorf("E_TOOL_STATE")
			}
			aggregated.Tool.Arguments += event.Text
		case "finish":
			aggregated.Usage = event.Usage
			aggregated.Finish = event.Finish
			aggregated.Stop = event.Stop
		}
		return nil
	}
	if isAI {
		e = s.clients[provider].Run(ctx, r, target, u, s.Config.Server, emit)
		if e != nil && h.Context().Err() == nil && ctx.Err() == nil && !wire.Committed && rule.Action.OnError != nil && rule.Action.OnError.Mode != "api_error" && e.Status != 499 {
			fallback := s.Config.Fallback.Action
			if rule.Action.OnError.Mode == "preset" {
				fallback = *rule.Action.OnError.Response
			}
			fallbackRule := config.Rule{ID: rule.ID, Action: fallback, Behavior: config.Behavior{Profile: "instant", Thinking: &config.ThinkingPatch{Enabled: ptr(false)}}}
			v, p, err := preset.Prepare(s.Config, r, fallbackRule, seed)
			if err == nil {
				aggregated = core.Result{ID: aggregated.ID, Finish: "stop"}
				if err := pacing.Run(ctx, r, v, p, pacing.RNG(seed, r, rule.ID, "timing"), emit); err == nil {
					e = nil
				}
			}
		}
	} else {
		if err := pacing.Run(ctx, r, result, profile, pacing.RNG(seed, r, rule.ID, "timing"), emit); err != nil {
			e = core.Err("response_timeout", 504, "Response interrupted or timed out")
		}
	}
	if e != nil {
		fail(e)
		return
	}
	if ctx.Err() != nil {
		fail(core.Err("response_timeout", 504, "Response timed out"))
		return
	}
	if !r.Stream {
		if aggregated.Usage.Source == "estimated" {
			w.Header().Set("X-FakeAPI-Usage", "estimated")
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(s.Config.Server.WriteTimeout.Value()))
		_ = json.NewEncoder(w).Encode(protocol.Body(r, aggregated, time.Now().Unix()))
	}
}
func ptr[T any](v T) *T { return &v }
func (s *Server) models(w http.ResponseWriter, h *http.Request, p core.Protocol) {
	if h.Method != "GET" {
		protocol.WriteError(w, p, core.Err("method_not_allowed", 405, "Method not allowed"))
		return
	}
	raw, e := credential(h, p)
	if e != nil {
		protocol.WriteError(w, p, e)
		return
	}
	if _, e = s.Store.Auth(h.Context(), raw, time.Now()); e != nil {
		protocol.WriteError(w, p, e)
		return
	}
	models := append([]config.Model{}, s.Config.Models...)
	sort.SliceStable(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	prefix := "/v1/models"
	if p == core.Anthropic {
		prefix = "/anthropic/v1/models"
	}
	if p == core.Gemini {
		prefix = "/v1beta/models"
	}
	path := h.URL.Path
	entry := func(m config.Model) map[string]any {
		switch p {
		case core.Anthropic:
			return map[string]any{"id": m.ID, "type": "model", "display_name": m.DisplayName, "created_at": m.CreatedAt.UTC().Format(time.RFC3339), "capabilities": nil, "lifecycle": "active", "line": nil, "max_input_tokens": m.Limits.InputTokens, "max_tokens": m.Limits.OutputTokens, "deprecated_at": nil, "retires_at": nil}
		case core.Gemini:
			return map[string]any{"name": "models/" + m.ID, "baseModelId": m.ID, "displayName": m.DisplayName, "version": "1", "inputTokenLimit": m.Limits.InputTokens, "outputTokenLimit": m.Limits.OutputTokens, "supportedGenerationMethods": []string{"generateContent", "streamGenerateContent"}}
		default:
			return map[string]any{"id": m.ID, "object": "model", "created": m.CreatedAt.Unix(), "owned_by": "fakeapi"}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if path != prefix {
		if !strings.HasPrefix(path, prefix+"/") {
			protocol.WriteError(w, p, core.Err("not_found", 404, "Endpoint not found"))
			return
		}
		id := strings.TrimPrefix(path, prefix+"/")
		m := s.Config.Model(id)
		if m == nil {
			protocol.WriteError(w, p, core.Err("model_not_found", 404, "Model not found"))
			return
		}
		_ = json.NewEncoder(w).Encode(entry(*m))
		return
	}
	start, end := 0, len(models)
	limit := 100
	query := h.URL.Query()
	if p == core.Anthropic {
		if query.Get("after_id") != "" && query.Get("before_id") != "" {
			protocol.WriteError(w, p, core.Invalid("pagination"))
			return
		}
		for _, key := range []string{"after_id", "before_id"} {
			if id := query.Get(key); id != "" {
				found := -1
				for i, m := range models {
					if m.ID == id {
						found = i
					}
				}
				if found < 0 {
					protocol.WriteError(w, p, core.Invalid("pagination cursor"))
					return
				}
				if key == "after_id" {
					start = found + 1
				} else {
					end = found
				}
			}
		}
		if v := query.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 1000 {
				protocol.WriteError(w, p, core.Invalid("limit"))
				return
			}
			limit = n
		}
	}
	digest := sha256.Sum256([]byte(fmt.Sprint(models)))
	tag := fmt.Sprintf("%x", digest[:8])
	if p == core.Gemini {
		if v := query.Get("pageSize"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 1000 {
				protocol.WriteError(w, p, core.Invalid("pageSize"))
				return
			}
			limit = n
		}
		if token := query.Get("pageToken"); token != "" {
			b, err := base64.RawURLEncoding.DecodeString(token)
			parts := strings.Split(string(b), ":")
			if err != nil || len(parts) != 2 || parts[0] != tag {
				protocol.WriteError(w, p, core.Invalid("pageToken"))
				return
			}
			n, err := strconv.Atoi(parts[1])
			if err != nil || n < 0 || n > len(models) {
				protocol.WriteError(w, p, core.Invalid("pageToken"))
				return
			}
			start = n
		}
	}
	more := false
	if p != core.Chat && end-start > limit {
		end = start + limit
		more = true
	}
	data := []any{}
	for _, m := range models[start:end] {
		data = append(data, entry(m))
	}
	v := map[string]any{"object": "list", "data": data}
	if p == core.Anthropic {
		var first, last any
		if len(data) > 0 {
			first = models[start].ID
			last = models[end-1].ID
		}
		v = map[string]any{"data": data, "first_id": first, "last_id": last, "has_more": more}
	}
	if p == core.Gemini {
		v = map[string]any{"models": data}
		if more {
			v["nextPageToken"] = base64.RawURLEncoding.EncodeToString([]byte(tag + ":" + strconv.Itoa(end)))
		}
	}
	_ = json.NewEncoder(w).Encode(v)
}
