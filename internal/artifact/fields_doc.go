package artifact

import (
	"fmt"
	"strings"
)

// FieldsMarkdown renders the frontmatter reference (docs/reference/
// frontmatter.md) from the field registry, so the documentation can't drift
// from what `agentworks validate` enforces. TestFrontmatterReferenceIsCurrent
// fails when the checked-in file is stale; regenerate it with
// UPDATE_GOLDEN=1 go test ./internal/artifact.
func FieldsMarkdown() string {
	var b strings.Builder
	b.WriteString("<!-- Generated from internal/artifact/fields.go. Do not edit by hand:\n")
	b.WriteString("     run `UPDATE_GOLDEN=1 go test ./internal/artifact` to regenerate. -->\n\n")
	b.WriteString("# Frontmatter reference\n\n")
	b.WriteString("Every artifact is a `<kind>.md` file: YAML frontmatter, then a Markdown body. This lists\n")
	b.WriteString("every supported frontmatter field. `agentworks validate` rejects a supported field with\n")
	b.WriteString("the wrong type and warns about any other key, because an unrecognized key is ignored on\n")
	b.WriteString("export -- usually a typo. Prefix a key with `x-` (for example `x-owner`) to keep your own\n")
	b.WriteString("metadata without the warning.\n\n")
	b.WriteString("Fields marked *deprecated* are still accepted for now but warned about; they will be\n")
	b.WriteString("removed in a later minor version (see COMPATIBILITY.md).\n")

	for _, kind := range Kinds() {
		fmt.Fprintf(&b, "\n## %s\n\n", kind)
		b.WriteString("| Field | Type | Required | Description |\n| --- | --- | --- | --- |\n")
		for _, f := range FieldsFor(kind) {
			required := ""
			if f.Required {
				required = "yes"
			}
			doc := strings.ReplaceAll(f.Doc, "|", `\|`)
			if f.Deprecated != "" {
				doc = "*Deprecated:* " + f.Deprecated + ". " + doc
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", f.Name, f.Type, required, doc)
		}
	}
	return b.String()
}
