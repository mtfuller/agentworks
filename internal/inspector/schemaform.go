package inspector

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/mtfuller/agentworks/internal/mcpclient"
)

// jsonSchema is the minimal subset of JSON Schema an MCP tool's
// inputSchema is expected to use: an object with named properties. This
// package doesn't implement a general JSON Schema validator -- it only
// needs enough structure to build a call form and turn its answers back
// into the right JSON types.
type jsonSchema struct {
	Properties map[string]propSchema `json:"properties"`
	Required   []string              `json:"required"`
}

type propSchema struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum"`
}

// paramField describes one input schema property, used both to build the
// call form (buildField) and to render the parameter table in the tool
// detail view (see model.go's renderToolDetail).
type paramField struct {
	Name        string
	Type        string
	Description string
	Enum        []string
	Required    bool
}

// parseSchema decodes a tool's raw inputSchema. An empty/absent schema
// (a tool that takes no arguments) decodes to a zero jsonSchema, not an
// error.
func parseSchema(raw json.RawMessage) (jsonSchema, error) {
	var s jsonSchema
	if len(strings.TrimSpace(string(raw))) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return jsonSchema{}, fmt.Errorf("parsing inputSchema: %w", err)
	}
	return s, nil
}

// paramFields flattens a jsonSchema's properties into a stable,
// alphabetically-sorted list (object property order isn't preserved
// through Go's map-based JSON decoding, and a stable order matters for a
// form the user fills in every time they open it).
func paramFields(s jsonSchema) []paramField {
	required := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		required[r] = true
	}

	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]paramField, 0, len(names))
	for _, name := range names {
		p := s.Properties[name]
		fields = append(fields, paramField{
			Name:        name,
			Type:        p.Type,
			Description: p.Description,
			Enum:        p.Enum,
			Required:    required[name],
		})
	}
	return fields
}

// callForm is a huh.Form generated from a tool's inputSchema, plus enough
// bookkeeping (the typed pointers each field is bound to) to turn its
// filled-in answers back into typed JSON arguments -- the same
// embed-a-huh.Form-as-a-child-model pattern internal/tui/actions.go uses
// for create/export, applied here to "call this tool".
type callForm struct {
	Form   *huh.Form
	fields []paramField

	strs  map[string]*string
	bools map[string]*bool
}

// newCallForm builds a call form from a tool's raw inputSchema:
//   - a string property with an "enum" becomes a select
//   - a plain string becomes a text input
//   - integer/number become a text input with numeric validation
//   - boolean becomes a confirm (true/false -- see Arguments' note on why
//     an optional boolean left untouched is still sent as false)
//   - array/object -- schema shapes a single-line input can't represent --
//     fall back to a raw-JSON textarea, validated with json.Unmarshal at
//     submit time
//
// Every required property gets a non-empty validator. The group carries
// the tool's own name/description (huh.Group.Title/Description) so the
// form doesn't just show a bare field list with no reminder of which
// tool -- or what it does -- the user is about to call.
func newCallForm(tool mcpclient.Tool) (*callForm, error) {
	schema, err := parseSchema(tool.InputSchema)
	if err != nil {
		return nil, err
	}
	fields := paramFields(schema)

	cf := &callForm{
		fields: fields,
		strs:   make(map[string]*string, len(fields)),
		bools:  make(map[string]*bool, len(fields)),
	}

	if len(fields) == 0 {
		cf.Form = huh.NewForm(huh.NewGroup(
			huh.NewNote().Title(tool.Name).Description(tool.Description).Next(true).NextLabel("Call"),
		).Description("This tool takes no parameters."))
		return cf, nil
	}

	huhFields := make([]huh.Field, 0, len(fields))
	for _, f := range fields {
		huhFields = append(huhFields, cf.buildField(f))
	}
	cf.Form = huh.NewForm(huh.NewGroup(huhFields...).Title(tool.Name).Description(tool.Description))
	return cf, nil
}

func (cf *callForm) buildField(f paramField) huh.Field {
	title := f.Name
	if f.Required {
		title += " *"
	}

	if len(f.Enum) > 0 {
		v := new(string)
		cf.strs[f.Name] = v
		opts := make([]huh.Option[string], 0, len(f.Enum))
		for _, e := range f.Enum {
			opts = append(opts, huh.NewOption(e, e))
		}
		return huh.NewSelect[string]().
			Title(title).
			Description(f.Description).
			Options(opts...).
			Value(v)
	}

	switch f.Type {
	case "boolean":
		v := new(bool)
		cf.bools[f.Name] = v
		return huh.NewConfirm().
			Title(title).
			Description(f.Description).
			Affirmative("true").
			Negative("false").
			Value(v)

	case "integer", "number":
		v := new(string)
		cf.strs[f.Name] = v
		return huh.NewInput().
			Title(title).
			Description(f.Description).
			Value(v).
			Validate(numberValidator(f.Name, f.Type, f.Required))

	case "array", "object":
		v := new(string)
		cf.strs[f.Name] = v
		return huh.NewText().
			Title(title).
			Description(strings.TrimSpace(f.Description + " (raw JSON)")).
			Value(v).
			Validate(jsonValidator(f.Name, f.Required))

	default: // "string" and anything this package doesn't special-case
		v := new(string)
		cf.strs[f.Name] = v
		return huh.NewInput().
			Title(title).
			Description(f.Description).
			Value(v).
			Validate(stringValidator(f.Name, f.Required))
	}
}

func stringValidator(name string, required bool) func(string) error {
	return func(v string) error {
		if required && v == "" {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
}

func numberValidator(name, kind string, required bool) func(string) error {
	return func(v string) error {
		if v == "" {
			if required {
				return fmt.Errorf("%s is required", name)
			}
			return nil
		}
		if kind == "integer" {
			if _, err := strconv.ParseInt(v, 10, 64); err != nil {
				return fmt.Errorf("%s must be a whole number", name)
			}
			return nil
		}
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return fmt.Errorf("%s must be a number", name)
		}
		return nil
	}
}

func jsonValidator(name string, required bool) func(string) error {
	return func(v string) error {
		if strings.TrimSpace(v) == "" {
			if required {
				return fmt.Errorf("%s is required", name)
			}
			return nil
		}
		var probe any
		if err := json.Unmarshal([]byte(v), &probe); err != nil {
			return fmt.Errorf("%s must be valid JSON: %w", name, err)
		}
		return nil
	}
}

// Arguments converts the form's filled-in fields back into typed JSON
// values for Client.CallTool -- the inverse of buildField's type-specific
// widget choice. An empty, non-required string/number/array/object field
// is omitted entirely rather than sent as "" or null, so an optional
// argument the user left blank doesn't reach the tool at all.
//
// A boolean field has no such "left blank" state -- huh.Confirm is always
// either true or false -- so an optional boolean not explicitly toggled
// is still sent as false. Documented here rather than silently surprising
// a tool author: if a tool needs to distinguish "false" from "not set"
// for an optional boolean, its schema should model that as a
// string/enum instead.
func (cf *callForm) Arguments() (map[string]any, error) {
	args := make(map[string]any, len(cf.fields))
	for _, f := range cf.fields {
		switch f.Type {
		case "boolean":
			args[f.Name] = *cf.bools[f.Name]

		case "integer":
			v := *cf.strs[f.Name]
			if v == "" {
				continue
			}
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			args[f.Name] = n

		case "number":
			v := *cf.strs[f.Name]
			if v == "" {
				continue
			}
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			args[f.Name] = n

		case "array", "object":
			v := *cf.strs[f.Name]
			if strings.TrimSpace(v) == "" {
				continue
			}
			var decoded any
			if err := json.Unmarshal([]byte(v), &decoded); err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			args[f.Name] = decoded

		default:
			v := *cf.strs[f.Name]
			if v == "" {
				continue
			}
			args[f.Name] = v
		}
	}
	return args, nil
}
