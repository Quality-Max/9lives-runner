package contracttest

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// SchemaOptions describes one published output contract.
type SchemaOptions struct {
	ID          string
	Title       string
	Description string
	// Version, when non-zero, pins the root object's "version" property.
	Version int
	// Enums lists the closed value sets of named string types.
	Enums map[reflect.Type][]string
}

// Schema derives a JSON Schema (draft 2020-12) from the Go type that
// encoding/json serializes, so a published schema cannot drift from the
// output without a test noticing. Fields tagged omitempty are optional;
// slices, maps and pointers without omitempty may encode as null.
func Schema(root reflect.Type, opts SchemaOptions) ([]byte, error) {
	g := schemaGenerator{defs: map[string]any{}, names: map[reflect.Type]string{}, enums: opts.Enums}
	body, err := g.object(root)
	if err != nil {
		return nil, err
	}
	if opts.Version != 0 {
		properties := body["properties"].(map[string]any)
		version, ok := properties["version"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s has no version property", root)
		}
		version["const"] = opts.Version
	}
	schema := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"$id":         opts.ID,
		"title":       opts.Title,
		"description": opts.Description,
	}
	for key, value := range body {
		schema[key] = value
	}
	if len(g.defs) > 0 {
		schema["$defs"] = g.defs
	}
	raw, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

type schemaGenerator struct {
	defs  map[string]any
	names map[reflect.Type]string
	enums map[reflect.Type][]string
}

var timeType = reflect.TypeFor[time.Time]()

func (g *schemaGenerator) object(t reflect.Type) (map[string]any, error) {
	properties, required := map[string]any{}, []string{}
	if err := g.fields(t, properties, &required); err != nil {
		return nil, err
	}
	object := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		object["required"] = required
	}
	return object, nil
}

func (g *schemaGenerator) fields(t reflect.Type, properties map[string]any, required *[]string) error {
	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if field.Anonymous && name == "" && field.Type.Kind() == reflect.Struct {
			if err := g.fields(field.Type, properties, required); err != nil {
				return err
			}
			continue
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		omitEmpty := strings.Contains(","+options+",", ",omitempty,")
		schema, err := g.value(field.Type, !omitEmpty)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", t.Name(), field.Name, err)
		}
		properties[name] = schema
		if !omitEmpty {
			*required = append(*required, name)
		}
	}
	return nil
}

func nullable(schema map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}
}

// value returns the schema of t; mayBeNull marks a field whose nil value is
// encoded rather than omitted.
func (g *schemaGenerator) value(t reflect.Type, mayBeNull bool) (map[string]any, error) {
	if values, ok := g.enums[t]; ok {
		return map[string]any{"type": "string", "enum": values}, nil
	}
	if t == timeType {
		return map[string]any{"type": "string", "format": "date-time"}, nil
	}
	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Interface:
		return map[string]any{}, nil
	case reflect.Pointer:
		schema, err := g.value(t.Elem(), false)
		if err != nil || !mayBeNull {
			return schema, err
		}
		return nullable(schema), nil
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "contentEncoding": "base64"}, nil
		}
		items, err := g.value(t.Elem(), false)
		if err != nil {
			return nil, err
		}
		schema := map[string]any{"type": "array", "items": items}
		if mayBeNull && t.Kind() == reflect.Slice {
			return nullable(schema), nil
		}
		return schema, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("unsupported map key %s", t.Key())
		}
		values, err := g.value(t.Elem(), false)
		if err != nil {
			return nil, err
		}
		schema := map[string]any{"type": "object", "additionalProperties": values}
		if mayBeNull {
			return nullable(schema), nil
		}
		return schema, nil
	case reflect.Struct:
		return g.ref(t)
	}
	return nil, fmt.Errorf("unsupported kind %s", t.Kind())
}

func (g *schemaGenerator) ref(t reflect.Type) (map[string]any, error) {
	name, seen := g.names[t]
	if !seen {
		name = t.Name()
		for other, used := range g.names {
			if used == name && other != t {
				name = strings.ReplaceAll(t.String(), ".", "_")
			}
		}
		g.names[t] = name
		object, err := g.object(t)
		if err != nil {
			return nil, err
		}
		g.defs[name] = object
	}
	return map[string]any{"$ref": "#/$defs/" + name}, nil
}
