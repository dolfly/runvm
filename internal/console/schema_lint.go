package console

import (
	"reflect"
	"strconv"
	"strings"
)

// SchemaLintIssue is one portability problem found in a tool input schema.
// The shape mirrors MCP Inspector's schemaLint output: the rule id, its
// severity, the offending subschema, the path from the tool root, and prose
// about what is wrong and how to fix it. The rule semantics follow Inspector's
// four checks, but the wording here is original — runvm is MIT and Inspector's
// text is not copied.
type SchemaLintIssue struct {
	Rule       string `json:"rule"`
	Severity   string `json:"severity"` // "error" | "warning"
	Schema     any    `json:"schema"`
	Path       string `json:"path"`
	Issue      string `json:"issue"`
	Suggestion string `json:"suggestion"`
}

// ToolInfo is one tool name plus its input schema — exactly what tools/list
// reports for each entry.
type ToolInfo struct {
	Name        string `json:"name"`
	InputSchema any    `json:"inputSchema"`
}

// LintTools runs the four portability rules over every tool's input schema and
// returns the issues in tool-then-depth order.
func LintTools(tools []ToolInfo) []SchemaLintIssue {
	var out []SchemaLintIssue
	for _, t := range tools {
		out = append(out, lintSchema(t.Name, t.InputSchema, "")...)
	}
	return out
}

// lintSchema walks one schema node. A node may be an object schema (the usual
// case) or a boolean schema; JSON Schema allows a bare true/false in place of
// a schema, which is exactly what the boolean-schema rule flags.
func lintSchema(rootName string, node any, path string) []SchemaLintIssue {
	full := rootName
	if path != "" {
		full = rootName + "." + path
	}

	if b, ok := node.(bool); ok {
		return []SchemaLintIssue{{
			Rule:       "boolean-schema",
			Severity:   "warning",
			Schema:     b,
			Path:       full,
			Issue:      "schema is a bare boolean, so it describes no shape at all",
			Suggestion: "describe the tool's arguments as an object with named, typed properties",
		}}
	}

	obj, ok := node.(map[string]any)
	if !ok {
		return nil
	}

	var out []SchemaLintIssue

	// A type written as a list (["string", "number"]) is a union that many
	// MCP clients translate poorly; prefer a single type plus anyOf/oneOf.
	// The slice may be []string or []any depending on how the caller built the
	// schema, so check the shape rather than the concrete Go type.
	if _, isList := asSlice(obj["type"]); isList {
		out = append(out, SchemaLintIssue{
			Rule:       "type-union",
			Severity:   "warning",
			Schema:     obj,
			Path:       full,
			Issue:      "schema declares its type as a list of several types",
			Suggestion: "pick one JSON Schema type, or model the alternatives with anyOf/oneOf",
		})
	}

	// A schema with no type, $ref, properties, items, enum, const or
	// combinator describes nothing and is almost always a mistake.
	if !hasShape(obj) {
		out = append(out, SchemaLintIssue{
			Rule:       "untyped-schema",
			Severity:   "error",
			Schema:     obj,
			Path:       full,
			Issue:      "schema carries no type, $ref, properties, items, enum, const or combinator",
			Suggestion: "add a type (object is the usual choice for tool arguments)",
		})
	}

	// A $ref to a remote URL means a client would have to fetch it at runtime;
	// that breaks offline/air-gapped clients and single-binary servers. It is a
	// portability warning rather than a hard error, since some clients do fetch.
	if ref, ok := obj["$ref"].(string); ok && isRemoteRef(ref) {
		out = append(out, SchemaLintIssue{
			Rule:       "remote-ref",
			Severity:   "warning",
			Schema:     obj,
			Path:       full,
			Issue:      "schema $ref points at a remote URL",
			Suggestion: "inline the referenced schema instead of referencing a URL",
		})
	}

	out = append(out, lintChildren(rootName, obj, path)...)
	return out
}

// hasShape reports whether a schema carries at least one constraint keyword.
// The boolean-valued keywords (additionalProperties etc.) are included as
// constraints but are never walked as subschemas: their false/true value is a
// normal "closed object" declaration, not a boolean schema to flag.
func hasShape(obj map[string]any) bool {
	for _, key := range []string{
		"type", "$ref", "properties", "items", "enum", "const",
		"anyOf", "oneOf", "allOf",
		"additionalProperties", "additionalItems", "unevaluatedProperties", "unevaluatedItems",
	} {
		if _, ok := obj[key]; ok {
			return true
		}
	}
	return false
}

func isRemoteRef(ref string) bool {
	return strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}

// lintChildren descends into the subschema positions a tool schema actually
// uses. additionalProperties is deliberately skipped: it is routinely the
// boolean false (a normal "closed object" declaration), and linting that as a
// boolean schema would drown the report in noise.
func lintChildren(rootName string, obj map[string]any, path string) []SchemaLintIssue {
	var out []SchemaLintIssue

	if props, ok := obj["properties"].(map[string]any); ok {
		for name, v := range props {
			out = append(out, lintSchema(rootName, v, joinPath(path, "properties", name))...)
		}
	}
	if items, ok := obj["items"].(map[string]any); ok {
		out = append(out, lintSchema(rootName, items, joinPath(path, "items"))...)
	}
	if items, ok := asSlice(obj["items"]); ok {
		for i, v := range items {
			out = append(out, lintSchema(rootName, v, joinPath(path, "items", strconv.Itoa(i)))...)
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := asSlice(obj[key]); ok {
			for i, v := range arr {
				out = append(out, lintSchema(rootName, v, joinPath(path, key, strconv.Itoa(i)))...)
			}
		}
	}
	return out
}

// asSlice returns the elements of any slice/array value ([]string, []any,
// []map[string]any, ...) as []any, so linting works on schemas built with Go
// maps rather than decoded from JSON.
func asSlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

func joinPath(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, ".")
}
