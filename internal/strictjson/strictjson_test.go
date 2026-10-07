package strictjson

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValueRejectsAmbiguousJSON(t *testing.T) {
	for _, raw := range []string{
		`{"a":1,"a":2}`, `{"a":{"b":1,"b":1}}`, `[{"a":1,"a":1}]`, `null`, `{"a":null}`, `[1,null]`,
		strings.Repeat("[", MaxDepth+2) + strings.Repeat("]", MaxDepth+2), `{"a":`,
	} {
		if Value(json.NewDecoder(strings.NewReader(raw))) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"a":1,"b":[true,"x",{"c":2}]}`, `"text"`, `0`, strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)} {
		if err := Value(json.NewDecoder(strings.NewReader(raw))); err != nil {
			t.Fatalf("rejected %s: %v", raw, err)
		}
	}
}
