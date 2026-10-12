package schema

import (
	"testing"
)

func TestBoundedSchema(t *testing.T) {
	s := map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean", "description": "status"}, "source": map[string]any{"type": "string"}}, "propertyOrdering": []string{"ok", "source"}, "required": []string{"ok", "source"}, "additionalProperties": false}
	if e := Validate(s, map[string]any{"ok": true, "source": "fakeapi"}); e != nil {
		t.Fatal(e)
	}
	if e := Validate(s, map[string]any{"ok": "true", "source": "fakeapi"}); e == nil {
		t.Fatal("invalid fixture accepted")
	}
	for _, invalid := range []map[string]any{{"$ref": "https://example.com/schema"}, {"$ref": "file:///etc/passwd"}, {"type": "object", "unknown": true}, {"$defs": map[string]any{"loop": map[string]any{"$ref": "#/$defs/loop"}}, "$ref": "#/$defs/loop"}, {"properties": map[string]any{}, "propertyOrdering": []string{"missing"}}} {
		if _, e := Compile(invalid); e == nil {
			t.Fatal("invalid schema accepted")
		}
	}
}
