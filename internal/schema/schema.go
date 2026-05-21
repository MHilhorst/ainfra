// Package schema derives the JSON Schema for the ainfra manifest by reflecting
// over the manifest.Manifest Go type.
//
// The schema is generated from the one source of truth — the structs the
// loader actually decodes into — so it cannot drift from the parser. A schema
// hand-maintained alongside the code is a classic config-as-code failure mode
// (design §13); generating it removes the possibility.
package schema

import (
	"reflect"
	"strings"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// Generate returns the JSON Schema (draft 2020-12) for an ainfra manifest as a
// marshalable map. It describes structure only; semantic rules — such as
// "a package-launched MCP server must pin an exact version" — are enforced by
// `ainfra validate`, not expressible structurally.
func Generate() map[string]any {
	root := schemaFor(reflect.TypeOf(manifest.Manifest{}))
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["title"] = "ainfra manifest"
	root["description"] = "Structural schema for ainfra.yaml and ainfra.personal.yaml. " +
		"Semantic rules are enforced by 'ainfra validate'."
	return root
}

// schemaFor builds the schema node for a Go type. additionalProperties is set
// false on every named struct, so the schema enforces exactly the strictness
// the loader's strict decoding does — an unknown key is rejected by both.
func schemaFor(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.Pointer:
		return schemaFor(t.Elem())
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{} // an `any` field accepts any shape
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaFor(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaFor(t.Elem())}
	case reflect.Struct:
		props := map[string]any{}
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name := yamlName(f)
			if name == "" || name == "-" {
				continue
			}
			props[name] = schemaFor(f.Type)
		}
		return map[string]any{
			"type":                 "object",
			"properties":           props,
			"additionalProperties": false,
		}
	default:
		return map[string]any{}
	}
}

// yamlName returns the field's wire name from its yaml tag, falling back to the
// lower-cased Go field name when no tag is present.
func yamlName(f reflect.StructField) string {
	tag := f.Tag.Get("yaml")
	if tag == "" {
		return strings.ToLower(f.Name)
	}
	return strings.Split(tag, ",")[0]
}
