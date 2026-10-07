// Package strictjson checks untrusted JSON before it is decoded. encoding/json
// silently accepts duplicate member names (last wins), null for any type and
// unbounded nesting; worker evidence and goal requests must reject all three.
package strictjson

import (
	"encoding/json"
	"errors"
)

// MaxDepth bounds nesting for every protocol that uses this check.
const MaxDepth = 8

// Value consumes exactly one JSON value from d, rejecting nesting deeper than
// MaxDepth and duplicate names or null values at any level.
func Value(d *json.Decoder) error { return value(d, 0) }

func value(d *json.Decoder, depth int) error {
	if depth > MaxDepth {
		return errors.New("excessive JSON nesting")
	}
	token, err := d.Token()
	if err != nil || token == nil {
		return errors.New("invalid JSON value")
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return errors.New("invalid JSON member")
			}
			seen[name] = true
			if err := value(d, depth+1); err != nil {
				return err
			}
		}
		if end, err := d.Token(); err != nil || end != json.Delim('}') {
			return errors.New("invalid object")
		}
	case '[':
		for d.More() {
			if err := value(d, depth+1); err != nil {
				return err
			}
		}
		if end, err := d.Token(); err != nil || end != json.Delim(']') {
			return errors.New("invalid array")
		}
	default:
		return errors.New("invalid delimiter")
	}
	return nil
}
