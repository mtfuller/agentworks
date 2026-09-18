package artifact

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// FieldType is the shape a frontmatter field's value must have.
type FieldType string

const (
	TypeString FieldType = "string"
	TypeInt    FieldType = "integer"
	TypeList   FieldType = "list of strings"
	TypeMap    FieldType = "map of strings"
	TypeObject FieldType = "object"
	// TypeObjects is a list of objects whose inner shape is validated by the
	// field's owner (e.g. HookHandlers for `handlers`), not here.
	TypeObjects FieldType = "list of objects"
)

// Field describes one public frontmatter field: the contract AgentWorks makes
// about it. Only fields listed here are supported -- any other key round-trips
// through Frontmatter.Extra untouched but is ignored on export, and
// `agentworks validate` warns about it (a typo like `comand:` would otherwise
// silently do nothing). Prefix a key with "x-" to opt out of that warning.
type Field struct {
	Name string
	Type FieldType
	// Kinds lists the kinds the field applies to; nil means every kind.
	Kinds    []Kind
	Required bool
	// Deprecated, when non-empty, explains what replaced the field. It is
	// still accepted (and still warned about) for one minor version.
	Deprecated string
	Doc        string
}

// commonFields are the fields every artifact has, typed directly on Frontmatter
// rather than living in Extra.
var commonFields = []Field{
	{Name: "kind", Type: TypeString, Required: true, Doc: "The artifact kind: `agent`, `skill`, `mcp`, or `hook`. Must match the file name (`<kind>.md`)."},
	{Name: "name", Type: TypeString, Required: true, Doc: "Lowercase letters, digits, and hyphens. Must match the directory name."},
	{Name: "description", Type: TypeString, Required: true, Doc: "What the artifact does and when to use it. This is how an agent decides whether to use it, so it is linted for quality. Keep it under 1024 characters."},
	{Name: "version", Type: TypeString, Doc: "The artifact's own version (default `0.1.0`)."},
	{Name: "namespace", Type: TypeString, Doc: "Scopes the artifact under a team or origin so two artifacts can share a name. Files it at `<kind-dir>/<namespace>/<name>/`."},
}

// extraFields are the supported fields that live in Frontmatter.Extra.
var extraFields = []Field{
	// Every kind.
	{Name: "test", Type: TypeString, Doc: "Shell command `agentworks test` runs from the artifact's directory."},
	{Name: "build", Type: TypeString, Doc: "Shell command `agentworks build` runs from the artifact's directory. `agentworks export` runs it first."},
	{Name: "entrypoint", Type: TypeString, Doc: "Path (relative to the artifact) to its main file. `agentworks doctor` checks it exists."},
	{Name: "eval_runner", Type: TypeString, Doc: "Shell command `agentworks eval` pipes each case's prompt to; its stdout is what the assertions check. Falls back to the project's `eval.default_runner`."},
	{Name: "source", Type: TypeObject, Doc: "Provenance written by `agentworks add` (where an imported artifact came from). Not meant to be edited by hand."},
	{Name: "targets", Type: TypeList, Deprecated: "targets are set once in agentworks.yaml, not per artifact", Doc: "Ignored. Targets live in `agentworks.yaml`."},

	// Skill (Agent Skills spec fields carried through on export).
	{Name: "license", Type: TypeString, Kinds: []Kind{KindSkill}, Doc: "SPDX license identifier, exported into the skill's `SKILL.md`."},
	{Name: "compatibility", Type: TypeString, Kinds: []Kind{KindSkill}, Doc: "Environment requirements, exported into the skill's `SKILL.md`."},

	// Agent.
	{Name: "tools", Type: TypeList, Kinds: []Kind{KindAgent}, Doc: "What the agent may do, from a closed vendor-agnostic set (`agentworks validate` lists the values). Mapped to Claude Code's and Gemini CLI's real tool fields."},
	{Name: "model", Type: TypeString, Kinds: []Kind{KindAgent}, Doc: "How capable a model the agent needs: `fast`, `balanced`, or `powerful`."},
	{Name: "resources", Type: TypeList, Kinds: []Kind{KindAgent}, Doc: "Files under the agent's `resources/` directory it should consult. Informational: the whole directory ships either way."},

	// MCP server.
	{Name: "transport", Type: TypeString, Kinds: []Kind{KindMCP}, Doc: "`stdio` (default, a local process), `http` (streamable HTTP), or `sse`."},
	{Name: "command", Type: TypeString, Kinds: []Kind{KindMCP, KindHook}, Doc: "Shell command. For an mcp server, what starts it (stdio only; run via `sh -c`, or exec'd directly when `args` is set). For a hook, shorthand for a single handler command together with `events`."},
	{Name: "args", Type: TypeList, Kinds: []Kind{KindMCP}, Doc: "Arguments to `command`. When set, `command` is run directly rather than through a shell."},
	{Name: "env", Type: TypeMap, Kinds: []Kind{KindMCP}, Doc: "Non-secret environment variables for the server. Never put a secret here."},
	{Name: "auth", Type: TypeList, Kinds: []Kind{KindMCP}, Doc: "Names of secret environment variables the server needs. Exported as `${VAR}` references, never as values."},
	{Name: "url", Type: TypeString, Kinds: []Kind{KindMCP}, Doc: "The endpoint of a remote (`http` or `sse`) server."},
	{Name: "headers", Type: TypeMap, Kinds: []Kind{KindMCP}, Doc: "Headers for a remote server. A sensitive header (Authorization, or anything with token/key/secret/password) must reference `${VAR}` rather than carry a literal."},

	// Hook.
	{Name: "events", Type: TypeList, Kinds: []Kind{KindHook}, Doc: "Shorthand: the lifecycle events that trigger `command`. Use the target vendor's own event names."},
	{Name: "handlers", Type: TypeObjects, Kinds: []Kind{KindHook}, Doc: "Explicit handlers: a list of `{event, matcher, command, timeout}`. `matcher` filters the event (a regex for tool events on most vendors); `timeout` is seconds. Use either this or the `events` + `command` shorthand, not both."},
}

// FieldsFor returns every supported field for kind, common fields first.
func FieldsFor(kind Kind) []Field {
	out := append([]Field(nil), commonFields...)
	for _, f := range extraFields {
		if f.appliesTo(kind) {
			out = append(out, f)
		}
	}
	return out
}

// AllFields returns every field in the registry, once each, for reference docs.
func AllFields() []Field {
	return append(append([]Field(nil), commonFields...), extraFields...)
}

func (f Field) appliesTo(kind Kind) bool {
	if len(f.Kinds) == 0 {
		return true
	}
	for _, k := range f.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// LookupField finds the supported field with the given key for kind.
func LookupField(kind Kind, key string) (Field, bool) {
	for _, f := range extraFields {
		if f.Name == key && f.appliesTo(kind) {
			return f, true
		}
	}
	return Field{}, false
}

// isKnownElsewhere reports whether key is supported for some other kind, which
// makes for a more useful message than "unknown" (e.g. `command` on a skill).
func isKnownElsewhere(key string) []Kind {
	var kinds []Kind
	for _, f := range extraFields {
		if f.Name == key {
			kinds = append(kinds, f.Kinds...)
		}
	}
	return kinds
}

// LintFields warns about frontmatter keys AgentWorks doesn't support -- they
// round-trip but are ignored on export -- and about deprecated ones. Keys
// prefixed "x-" are the sanctioned place for a project's own metadata.
func (a *Artifact) LintFields() []LintWarning {
	keys := make([]string, 0, len(a.Extra))
	for k := range a.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var warnings []LintWarning
	for _, key := range keys {
		if strings.HasPrefix(key, "x-") {
			continue
		}
		f, ok := LookupField(a.Kind, key)
		switch {
		case ok && f.Deprecated != "":
			warnings = append(warnings, LintWarning{a.Dir, fmt.Sprintf("field %q is deprecated: %s", key, f.Deprecated)})
		case ok:
		case len(isKnownElsewhere(key)) > 0:
			warnings = append(warnings, LintWarning{a.Dir, fmt.Sprintf("field %q doesn't apply to a %s and is ignored on export (it is for: %s)", key, a.Kind, joinKindList(isKnownElsewhere(key)))})
		default:
			warnings = append(warnings, LintWarning{a.Dir, fmt.Sprintf("unknown field %q is ignored on export -- check the spelling, or prefix it \"x-\" for your own metadata", key)})
		}
	}
	return warnings
}

func joinKindList(kinds []Kind) string {
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = string(k)
	}
	return strings.Join(names, ", ")
}

// ValidateFieldTypes reports the first supported field whose value has the
// wrong shape -- `auth: FOO` instead of `auth: [FOO]` would otherwise read as
// "no auth" without a word. Unknown fields aren't checked (LintFields covers
// them).
func (a *Artifact) ValidateFieldTypes() error {
	keys := make([]string, 0, len(a.Extra))
	for k := range a.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		f, ok := LookupField(a.Kind, key)
		if !ok {
			continue
		}
		if err := checkType(f.Type, a.Extra[key]); err != nil {
			return fmt.Errorf("%s: field %q must be a %s: %w", a.Dir, key, f.Type, err)
		}
	}
	return nil
}

// checkType checks v against t by way of JSON, so it accepts what the YAML
// decoder produces and what code assigns in memory alike. A nil value (an
// empty `key:`) is accepted as unset.
func checkType(t FieldType, v any) error {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return err
	}

	switch t {
	case TypeString:
		if _, ok := generic.(string); !ok {
			return fmt.Errorf("got %s", jsonKind(generic))
		}
	case TypeInt:
		n, ok := generic.(float64)
		if !ok || n != float64(int64(n)) {
			return fmt.Errorf("got %s", jsonKind(generic))
		}
	case TypeList:
		list, ok := generic.([]any)
		if !ok {
			return fmt.Errorf("got %s", jsonKind(generic))
		}
		for _, item := range list {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("list contains %s", jsonKind(item))
			}
		}
	case TypeMap:
		m, ok := generic.(map[string]any)
		if !ok {
			return fmt.Errorf("got %s", jsonKind(generic))
		}
		for k, item := range m {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("value of %q is %s", k, jsonKind(item))
			}
		}
	case TypeObject:
		if _, ok := generic.(map[string]any); !ok {
			return fmt.Errorf("got %s", jsonKind(generic))
		}
	case TypeObjects:
		if _, ok := generic.([]any); !ok {
			return fmt.Errorf("got %s", jsonKind(generic))
		}
	}
	return nil
}

func jsonKind(v any) string {
	switch v.(type) {
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	case []any:
		return "a list"
	case map[string]any:
		return "a mapping"
	}
	return "null"
}
