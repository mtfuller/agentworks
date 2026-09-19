package inspector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/mcpclient"
)

// section is which kind of thing the browse pane (paneList) is listing. A server
// that advertises only tools -- the common case -- only ever has sectionTools,
// and the inspector looks exactly as it always has; resources and prompts are
// added when the server's capabilities include them.
type section int

const (
	sectionTools section = iota
	sectionResources
	sectionPrompts
)

func (s section) label() string {
	switch s {
	case sectionResources:
		return "Resources"
	case sectionPrompts:
		return "Prompts"
	default:
		return "Tools"
	}
}

// resourceItem adapts an mcpclient.Resource for bubbles/list.
type resourceItem struct{ res mcpclient.Resource }

func (i resourceItem) Title() string {
	if i.res.Name != "" {
		return i.res.Name
	}
	return i.res.URI
}
func (i resourceItem) Description() string {
	if i.res.Description != "" {
		return i.res.Description
	}
	return i.res.URI
}
func (i resourceItem) FilterValue() string { return i.res.Name + " " + i.res.URI }

// promptItem adapts an mcpclient.Prompt for bubbles/list.
type promptItem struct{ prompt mcpclient.Prompt }

func (i promptItem) Title() string { return i.prompt.Name }
func (i promptItem) Description() string {
	if i.prompt.Description == "" {
		return "(no description)"
	}
	return i.prompt.Description
}
func (i promptItem) FilterValue() string { return i.prompt.Name }

// promptAsTool describes a prompt's arguments as a tool with a JSON Schema, so
// the same form builder that collects a tool call's parameters collects a
// prompt's. Every prompt argument is a string, per the MCP spec.
func promptAsTool(p mcpclient.Prompt) mcpclient.Tool {
	props := map[string]any{}
	var required []string
	for _, a := range p.Arguments {
		prop := map[string]any{"type": "string"}
		if a.Description != "" {
			prop["description"] = a.Description
		}
		props[a.Name] = prop
		if a.Required {
			required = append(required, a.Name)
		}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	raw, _ := json.Marshal(schema)
	return mcpclient.Tool{Name: p.Name, Description: p.Description, InputSchema: raw}
}

// stringArgs converts a form's collected values to the map[string]string a
// prompt takes, dropping empty optional values.
func stringArgs(args map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range args {
		if s := fmt.Sprint(v); s != "" && v != nil {
			out[k] = s
		}
	}
	return out
}

// describeTarget names what the inspector is connecting to for display. For a
// remote server that is the URL without its query string or userinfo, either
// of which can carry a credential; headers are never shown.
func describeTarget(t mcpclient.Target) string {
	if !t.IsRemote() {
		return t.Command
	}
	u, err := url.Parse(t.URL)
	if err != nil || u.Host == "" {
		return "(remote server)"
	}
	return fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)
}

// -- reading a resource / rendering a prompt --

// resourceResultMsg reports the outcome of reading a resource.
type resourceResultMsg struct {
	res      mcpclient.Resource
	contents []mcpclient.ResourceContents
	err      error
	took     time.Duration
}

func readResourceCmd(conn *mcpclient.Conn, res mcpclient.Resource) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		contents, err := conn.ReadResource(ctx, res.URI)
		return resourceResultMsg{res: res, contents: contents, err: err, took: time.Since(start)}
	}
}

// promptResultMsg reports the outcome of rendering a prompt.
type promptResultMsg struct {
	prompt mcpclient.Prompt
	args   map[string]string
	result *mcpclient.GetPromptResult
	err    error
	took   time.Duration
}

func getPromptCmd(conn *mcpclient.Conn, prompt mcpclient.Prompt, args map[string]string) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := conn.GetPrompt(ctx, prompt.Name, args)
		return promptResultMsg{prompt: prompt, args: args, result: result, err: err, took: time.Since(start)}
	}
}

// maxShownBytes bounds how much of a resource is put in the viewport, so a
// large file doesn't stall rendering.
const maxShownBytes = 256 << 10

func renderResourceResult(msg resourceResultMsg) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(msg.res.Name + " (" + msg.res.URI + ")"))
	b.WriteString("\n\n")
	if msg.err != nil {
		b.WriteString(errorStyle.Render("read failed: " + msg.err.Error()))
		return b.String()
	}
	if len(msg.contents) == 0 {
		b.WriteString(helpStyle.Render("(the server returned no contents)"))
		return b.String()
	}
	for _, c := range msg.contents {
		if len(msg.contents) > 1 {
			fmt.Fprintf(&b, "%s\n", helpStyle.Render(c.URI))
		}
		switch {
		case c.Text != "":
			text := c.Text
			if len(text) > maxShownBytes {
				text = text[:maxShownBytes] + "\n... (truncated)"
			}
			b.WriteString(text)
		case c.Blob != "":
			fmt.Fprintf(&b, "%s", helpStyle.Render(fmt.Sprintf("(binary content, %s, %d bytes encoded)", orDefault(c.MimeType, "unknown type"), len(c.Blob))))
		default:
			b.WriteString(helpStyle.Render("(empty)"))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderPromptResult(msg promptResultMsg) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(msg.prompt.Name))
	if len(msg.args) > 0 {
		keys := make([]string, 0, len(msg.args))
		for k := range msg.args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + "=" + msg.args[k]
		}
		b.WriteString(" " + helpStyle.Render("("+strings.Join(parts, ", ")+")"))
	}
	b.WriteString("\n\n")
	if msg.err != nil {
		b.WriteString(errorStyle.Render("prompts/get failed: " + msg.err.Error()))
		return b.String()
	}
	if msg.result.Description != "" {
		b.WriteString(msg.result.Description + "\n\n")
	}
	for _, m := range msg.result.Messages {
		fmt.Fprintf(&b, "%s\n%s\n\n", headerStyle.Render(m.Role), renderContentBlock(m.Content))
	}
	return b.String()
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// renderResourceDetail describes the selected resource (before it is read).
func renderResourceDetail(res mcpclient.Resource) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(orDefault(res.Name, res.URI)) + "\n")
	fmt.Fprintf(&b, "URI: %s\n", res.URI)
	if res.MimeType != "" {
		fmt.Fprintf(&b, "Type: %s\n", res.MimeType)
	}
	if res.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", res.Description)
	}
	b.WriteString("\n" + helpStyle.Render("enter: read this resource"))
	return b.String()
}

// renderPromptDetail describes the selected prompt (before it is rendered).
func renderPromptDetail(p mcpclient.Prompt) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(p.Name) + "\n")
	if p.Description != "" {
		b.WriteString(p.Description + "\n")
	}
	if len(p.Arguments) == 0 {
		b.WriteString("\nArguments: none\n")
	} else {
		b.WriteString("\nArguments:\n")
		for _, a := range p.Arguments {
			req := ""
			if a.Required {
				req = " (required)"
			}
			fmt.Fprintf(&b, "  %s%s", a.Name, req)
			if a.Description != "" {
				fmt.Fprintf(&b, " -- %s", a.Description)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n" + helpStyle.Render("enter: render this prompt"))
	return b.String()
}
