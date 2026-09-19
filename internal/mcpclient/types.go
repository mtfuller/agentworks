// Package mcpclient is a minimal client for the Model Context Protocol
// (MCP)'s stdio JSON-RPC transport: one newline-delimited JSON value per
// line, request/response pairs correlated by numeric ID
// (https://modelcontextprotocol.io). It only implements what
// `agentworks run`'s inspector (internal/inspector) needs to talk to a
// tool artifact's declared "command" as a real MCP server:
// initialize/initialized, tools/list (with cursor pagination), and
// tools/call. It does not implement the rest of the spec (resources,
// prompts, sampling, roots) -- AgentWorks inspects tools, not full MCP
// clients-as-a-feature, and those are genuinely unused by that job.
package mcpclient

import (
	"encoding/json"
	"fmt"
)

// rpcVersion is the JSON-RPC 2.0 version string every message declares.
const rpcVersion = "2.0"

// protocolVersion is the MCP protocol version this client sends in its
// "initialize" request. MCP negotiates the version itself (a server may
// reply with a different one it supports); this client doesn't need to be
// clever about that beyond passing the server's InitializeResult back to
// the caller.
const protocolVersion = "2025-06-18"

// request is an outgoing JSON-RPC request (expects a response).
type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// notification is an outgoing JSON-RPC notification (no ID, no response).
type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// envelope is a generic incoming JSON-RPC message. ID set means a
// response to one of our requests; Method set with ID unset means a
// server-initiated notification (e.g. "notifications/message" logging,
// "notifications/progress") -- these aren't modeled individually, since
// the inspector's raw-traffic log already shows the line verbatim via
// Client.OnTrace. A message with both Method and ID set would be a
// server-to-client *request* (sampling, roots); this client doesn't
// answer those -- see the package doc comment -- so dispatch drops them.
type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object, returned as-is (it implements
// error) when a request fails at the protocol level.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("mcp: %s (code %d)", e.Message, e.Code)
}

// ClientInfo identifies a client or server by name/version, exchanged
// during initialize.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is what a server returns from "initialize".
type InitializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	ServerInfo      ClientInfo      `json:"serverInfo"`
	Capabilities    json.RawMessage `json:"capabilities,omitempty"`
	Instructions    string          `json:"instructions,omitempty"`
}

// Tool is one entry from "tools/list": a callable the server exposes.
// InputSchema is kept as raw JSON Schema rather than parsed into a Go
// struct -- internal/inspector's schemaform.go is what turns it into
// form fields, and doing that translation here would tie a protocol
// client to one particular UI's needs.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ContentBlock is one item of a "tools/call" result's content array.
// Only "text" (Type == "text") is meaningful to this client's callers
// today; other types (image, resource, ...) still round-trip via Raw so
// the inspector can at least show that something else came back, without
// this package needing a Go type for every content variant the spec
// defines.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Raw  json.RawMessage
}

func (c *ContentBlock) UnmarshalJSON(data []byte) error {
	type alias ContentBlock
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = ContentBlock(a)
	c.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// CallToolResult is what a server returns from "tools/call". IsError
// reports a *tool-level* failure (e.g. the API it called returned a 404)
// as opposed to a protocol-level one, which instead surfaces as an
// *RPCError from CallTool.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// Resource is one entry from "resources/list": something the server can hand
// the client to read (a file, a record), identified by URI.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContents is what "resources/read" returns for one URI: text, or a
// base64 blob for binary content.
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// Prompt is one entry from "prompts/list": a reusable message template the
// server offers, with named arguments.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument is one argument a Prompt accepts.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage is one message of a rendered prompt.
type PromptMessage struct {
	Role    string       `json:"role"`
	Content ContentBlock `json:"content"`
}

// GetPromptResult is what "prompts/get" returns: the prompt rendered with the
// given arguments.
type GetPromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// HasCapability reports whether the server's initialize response advertised
// the named capability ("tools", "resources", "prompts", ...). A server that
// omits a capability isn't expected to answer its methods.
func (r InitializeResult) HasCapability(name string) bool {
	var caps map[string]json.RawMessage
	if err := json.Unmarshal(r.Capabilities, &caps); err != nil {
		return false
	}
	_, ok := caps[name]
	return ok
}
