package config

import (
	"reflect"
	"strings"
	"time"
)

// JSONSchema is derived from the same types as the strict YAML decoder.
// Semantic checks (references, actions, bounds, assets) remain in Load.
func JSONSchema() map[string]any {
	defs := map[string]any{}
	var describe func(reflect.Type) map[string]any
	describe = func(t reflect.Type) map[string]any {
		if t == reflect.TypeOf(Duration(0)) {
			return map[string]any{"type": "string", "description": "Go duration, for example 300ms or 5s"}
		}
		if t == reflect.TypeOf(time.Time{}) {
			return map[string]any{"type": "string", "format": "date-time"}
		}
		if t == reflect.TypeOf(StringValue{}) {
			return map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "object", "properties": map[string]any{"env": map[string]any{"type": "string", "minLength": 1}}, "required": []string{"env"}, "additionalProperties": false}}}
		}
		switch t.Kind() {
		case reflect.Pointer:
			return map[string]any{"anyOf": []any{describe(t.Elem()), map[string]any{"type": "null"}}}
		case reflect.String:
			return map[string]any{"type": "string"}
		case reflect.Bool:
			return map[string]any{"type": "boolean"}
		case reflect.Int, reflect.Int64:
			return map[string]any{"type": "integer"}
		case reflect.Float64:
			return map[string]any{"type": "number"}
		case reflect.Slice:
			return map[string]any{"type": "array", "items": describe(t.Elem())}
		case reflect.Map:
			return map[string]any{"type": "object", "additionalProperties": describe(t.Elem())}
		case reflect.Interface:
			return map[string]any{}
		case reflect.Struct:
			name := t.Name()
			if _, ok := defs[name]; !ok {
				defs[name] = map[string]any{}
				properties := map[string]any{}
				for i := 0; i < t.NumField(); i++ {
					f := t.Field(i)
					tag := strings.Split(f.Tag.Get("yaml"), ",")[0]
					if tag == "" || tag == "-" {
						continue
					}
					properties[tag] = describe(f.Type)
				}
				defs[name] = map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
			}
			return map[string]any{"$ref": "#/$defs/" + name}
		}
		return map[string]any{}
	}
	root := describe(reflect.TypeOf(Config{}))
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$defs"] = defs
	root["title"] = "FakeAPI configuration v1"
	root["description"] = "Run fakeapi config check for all semantic constraints."
	defs["Config"].(map[string]any)["required"] = []string{"schema_version", "profiles", "defaults", "assets", "key_policies", "models", "matchers", "rules", "fallback"}
	properties := defs["Config"].(map[string]any)["properties"].(map[string]any)
	properties["schema_version"].(map[string]any)["const"] = 1
	properties["default_target"] = map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}}, "description": "Null/empty means no global upstream target."}
	return root
}
