package rules

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
)

type Input struct {
	Latest core.LatestUser
	Model  string
	Tags   []string
	Group  string
}
type compiledAtom struct {
	atom config.Atom
	re   *regexp.Regexp
}
type compiledMatcher struct {
	max            int
	any, all, none []compiledAtom
}
type Engine struct {
	rules    []config.Rule
	matchers map[string]compiledMatcher
	fallback config.Rule
}

func New(c *config.Config) *Engine {
	e := &Engine{rules: append([]config.Rule{}, c.Rules...), fallback: c.Fallback, matchers: map[string]compiledMatcher{}}
	sort.SliceStable(e.rules, func(i, j int) bool { return e.rules[i].Priority > e.rules[j].Priority })
	compile := func(a []config.Atom) []compiledAtom {
		var out []compiledAtom
		for _, v := range a {
			x := compiledAtom{atom: v}
			if v.Regex != nil {
				x.re = regexp.MustCompile(*v.Regex)
			}
			out = append(out, x)
		}
		return out
	}
	for id, m := range c.Matchers {
		e.matchers[id] = compiledMatcher{max: m.MaxChars, any: compile(m.Any), all: compile(m.All), none: compile(m.None)}
	}
	return e
}
func Contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func (e *Engine) Match(in Input) config.Rule {
	for _, r := range e.rules {
		if !r.Active() {
			continue
		}
		w := r.When
		if len(w.Models) > 0 && !Contains(w.Models, in.Model) || len(w.KeyGroups) > 0 && !Contains(w.KeyGroups, in.Group) {
			continue
		}
		ok := true
		for _, tag := range w.KeyTagsAll {
			if !Contains(in.Tags, tag) {
				ok = false
			}
		}
		if !ok {
			continue
		}
		if len(w.KeyTagsAny) > 0 {
			ok = false
			for _, tag := range w.KeyTagsAny {
				if Contains(in.Tags, tag) {
					ok = true
				}
			}
			if !ok {
				continue
			}
		}
		if w.UserText == "absent" && in.Latest.Present || w.UserText == "empty" && (!in.Latest.Present || in.Latest.Text != "") || w.UserText == "present" && !in.Latest.Present {
			continue
		}
		if len(w.MatchersAny) > 0 {
			ok = false
			if in.Latest.Present && in.Latest.Text != "" {
				for _, id := range w.MatchersAny {
					if e.matchers[id].match(in.Latest.Text) {
						ok = true
						break
					}
				}
			}
			if !ok {
				continue
			}
		}
		return r
	}
	return e.fallback
}
func (m compiledMatcher) match(s string) bool {
	if s == "" || m.max > 0 && utf8.RuneCountInString(s) > m.max {
		return false
	}
	if len(m.any) > 0 {
		ok := false
		for _, a := range m.any {
			if a.match(s) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, a := range m.all {
		if !a.match(s) {
			return false
		}
	}
	for _, a := range m.none {
		if a.match(s) {
			return false
		}
	}
	return true
}
func word(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}
func (a compiledAtom) match(s string) bool {
	if a.re != nil {
		return a.re.MatchString(s)
	}
	v := ""
	if a.atom.Exact != nil {
		v = *a.atom.Exact
	} else {
		v = *a.atom.Keyword
	}
	if a.atom.CaseInsensitive {
		s = strings.ToLower(s)
		v = strings.ToLower(v)
	}
	if a.atom.Exact != nil {
		return s == core.Normalize(v)
	}
	if a.atom.Boundary != "ascii_word" {
		return strings.Contains(s, v)
	}
	for start := 0; start <= len(s); {
		i := strings.Index(s[start:], v)
		if i < 0 {
			return false
		}
		i += start
		j := i + len(v)
		if (i == 0 || !word(s[i-1])) && (j == len(s) || !word(s[j])) {
			return true
		}
		start = i + 1
	}
	return false
}
