// Package schema validates a bounded subset with external references disabled.
package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type denyLoader struct{}

func (denyLoader) Load(string) (any, error) { return nil, fmt.Errorf("external references disabled") }
func JSON(v any) any                        { b, _ := json.Marshal(v); var n any; _ = json.Unmarshal(b, &n); return n }
func Compile(s map[string]any) (*jsonschema.Schema, error) {
	n := JSON(s).(map[string]any)
	if err := check(n, n, 0, map[string]bool{}); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(denyLoader{})
	if e := c.AddResource("https://fakeapi.invalid/schema", n); e != nil {
		return nil, e
	}
	return c.Compile("https://fakeapi.invalid/schema")
}
func Validate(s map[string]any, v any) error {
	c, e := Compile(s)
	if e != nil {
		return e
	}
	return c.Validate(JSON(v))
}

var allowed = map[string]bool{"type": true, "properties": true, "required": true, "additionalProperties": true, "items": true, "enum": true, "const": true, "minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true, "minLength": true, "maxLength": true, "minItems": true, "maxItems": true, "$defs": true, "$ref": true, "$schema": true, "title": true, "description": true, "default": true, "examples": true, "anyOf": true, "propertyOrdering": true}

func check(s, root map[string]any, depth int, refs map[string]bool) error {
	if depth > 16 {
		return fmt.Errorf("schema depth exceeded")
	}
	for k, v := range s {
		if !allowed[k] {
			return fmt.Errorf("unsupported schema keyword: %s", k)
		}
		switch k {
		case "$schema":
			if v != "https://json-schema.org/draft/2020-12/schema" && v != "http://json-schema.org/draft-07/schema#" && v != "https://json-schema.org/draft/2019-09/schema" {
				return fmt.Errorf("unsupported schema draft")
			}
		case "$ref":
			r, ok := v.(string)
			if !ok || !strings.HasPrefix(r, "#/") || refs[r] {
				return fmt.Errorf("external or recursive reference")
			}
			var cur any = root
			for _, p := range strings.Split(r[2:], "/") {
				m, ok := cur.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid reference")
				}
				cur = m[strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")]
			}
			m, ok := cur.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid reference")
			}
			refs[r] = true
			e := check(m, root, depth+1, refs)
			delete(refs, r)
			if e != nil {
				return e
			}
		case "properties", "$defs":
			m, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("expected schema map")
			}
			for _, a := range m {
				child, ok := a.(map[string]any)
				if !ok {
					return fmt.Errorf("expected schema object")
				}
				if e := check(child, root, depth+1, refs); e != nil {
					return e
				}
			}
		case "items", "additionalProperties":
			if _, ok := v.(bool); ok {
				if k == "items" {
					return fmt.Errorf("unsupported schema boolean")
				}
				continue
			}
			m, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("expected schema object")
			}
			if e := check(m, root, depth+1, refs); e != nil {
				return e
			}
		case "anyOf":
			a, ok := v.([]any)
			if !ok || len(a) != 2 {
				return fmt.Errorf("only nullable anyOf supported")
			}
			nullable := false
			for _, x := range a {
				m, ok := x.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid nullable schema")
				}
				if m["type"] == "null" {
					nullable = true
				}
				if e := check(m, root, depth+1, refs); e != nil {
					return e
				}
			}
			if !nullable {
				return fmt.Errorf("only nullable anyOf supported")
			}
		case "propertyOrdering":
			a, ok := v.([]any)
			if !ok {
				return fmt.Errorf("invalid propertyOrdering")
			}
			properties, _ := s["properties"].(map[string]any)
			seen := map[string]bool{}
			for _, x := range a {
				p, ok := x.(string)
				if !ok || seen[p] || properties[p] == nil {
					return fmt.Errorf("invalid propertyOrdering")
				}
				seen[p] = true
			}
		}
	}
	return nil
}
