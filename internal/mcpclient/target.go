package mcpclient

import (
	"context"
	"fmt"

	"github.com/mtfuller/agentworks/internal/version"
)

// Transport names, matching an mcp artifact's `transport:` field.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
	TransportSSE   = "sse"
)

// Target describes an MCP server to connect to, local or remote.
type Target struct {
	// Transport is TransportStdio (the default when empty), TransportHTTP, or
	// TransportSSE.
	Transport string

	// Command, Dir, and Env start a local server (stdio): Command is run via
	// `sh -c` in Dir with Env as its whole environment (nil inherits ours).
	Command string
	Dir     string
	Env     []string

	// URL and Headers reach a remote server. Header values must already have
	// their ${VAR} references expanded (see ExpandEnv).
	URL     string
	Headers map[string]string
}

// IsRemote reports whether t is an http/sse server with no local process.
func (t Target) IsRemote() bool {
	return t.Transport == TransportHTTP || t.Transport == TransportSSE
}

// Conn is a connected MCP server: a Client, plus whatever must be torn down
// with it (a child process, an HTTP session).
type Conn struct {
	*Client
	closeFn func() error
}

// Close disconnects, and for a local server also stops its process.
func (c *Conn) Close() error { return c.closeFn() }

// Open connects to t. onTrace and onStderr, either may be nil, are installed
// before any traffic flows, so the handshake itself is visible to them
// (onStderr only ever fires for a local server). ctx bounds connecting, not
// the connection's lifetime.
func Open(ctx context.Context, t Target, onTrace func(direction string, raw []byte), onStderr func(line string)) (*Conn, error) {
	switch t.Transport {
	case "", TransportStdio:
		p, err := StartProcess(t.Command, t.Dir, t.Env)
		if err != nil {
			return nil, err
		}
		p.OnTrace = onTrace
		p.OnStderr = onStderr
		return &Conn{Client: p.Client, closeFn: p.Close}, nil

	case TransportHTTP:
		if t.URL == "" {
			return nil, fmt.Errorf("mcp: an http server needs a url")
		}
		c := DialHTTP(t.URL, t.Headers)
		c.OnTrace = onTrace
		return &Conn{Client: c, closeFn: c.Close}, nil

	case TransportSSE:
		if t.URL == "" {
			return nil, fmt.Errorf("mcp: an sse server needs a url")
		}
		c, err := DialSSE(ctx, t.URL, t.Headers)
		if err != nil {
			return nil, err
		}
		c.OnTrace = onTrace
		return &Conn{Client: c, closeFn: c.Close}, nil
	}
	return nil, fmt.Errorf("mcp: unknown transport %q", t.Transport)
}

// ProbeResult is what a one-shot connect found out about a server.
type ProbeResult struct {
	Info      InitializeResult
	Tools     []Tool
	Resources []Resource // only if the server advertised the capability
	Prompts   []Prompt   // likewise
}

// Probe connects to t, runs the initialize handshake, lists its tools -- plus
// its resources and prompts if it advertises them -- and disconnects. It is the
// smallest end-to-end proof that a server speaks MCP, and what `agentworks test`
// runs for an mcp artifact that declares no test: of its own. Stderr from a
// local server is forwarded to onStderr so a failure isn't opaque.
func Probe(ctx context.Context, t Target, onStderr func(string)) (ProbeResult, error) {
	var res ProbeResult
	conn, err := Open(ctx, t, nil, onStderr)
	if err != nil {
		return res, err
	}
	defer conn.Close()

	res.Info, err = conn.Initialize(ctx, "agentworks", version.GetShortVersion())
	if err != nil {
		return res, fmt.Errorf("initialize: %w", err)
	}
	if res.Tools, err = conn.ListTools(ctx); err != nil {
		return res, fmt.Errorf("tools/list: %w", err)
	}
	if res.Info.HasCapability("resources") {
		if res.Resources, err = conn.ListResources(ctx); err != nil {
			return res, fmt.Errorf("resources/list: %w", err)
		}
	}
	if res.Info.HasCapability("prompts") {
		if res.Prompts, err = conn.ListPrompts(ctx); err != nil {
			return res, fmt.Errorf("prompts/list: %w", err)
		}
	}
	return res, nil
}
