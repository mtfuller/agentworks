package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// Client speaks MCP to a single already-connected server over a Transport:
// it correlates requests with responses, runs the initialize handshake, and
// exposes the protocol's calls. New starts a background goroutine reading
// the transport immediately; there's no separate Start step.
//
// For a real child process (an mcp artifact's declared "command"), use
// StartProcess; for a hosted server, use DialHTTP or DialSSE. Each wires up
// the right Transport and returns a Client already bound to it.
type Client struct {
	t Transport

	nextID int64

	mu      sync.Mutex
	pending map[int64]chan envelope
	closed  bool
	readErr error

	readDone chan struct{}

	// OnTrace, if set, is called for every raw JSON-RPC message sent ("->")
	// or received ("<-") -- the inspector's raw-traffic log pane. May be
	// called from the read-loop goroutine ("<-") or from whichever
	// goroutine is calling a request method ("->"); implementations must
	// not block or call back into the Client. Only message bodies are
	// traced, never HTTP headers, so a credential in a header can't leak
	// into the log.
	OnTrace func(direction string, raw []byte)

	// OnStderr, if set, is called with each line the server process
	// writes to its stderr (MCP servers log there, never stdout -- stdout
	// is reserved for the JSON-RPC channel). Only populated when the
	// Client came from StartProcess; a remote server has no stderr of its
	// own to report.
	OnStderr func(line string)
}

// New wraps an already-connected stdio-style transport: w is where messages
// are written, r is where the server's are read from (one JSON value per
// line).
func New(w io.Writer, r io.Reader) *Client {
	return NewWithTransport(newStdioTransport(w, r))
}

// NewWithTransport wraps any Transport.
func NewWithTransport(t Transport) *Client {
	c := &Client{
		t:        t,
		pending:  make(map[int64]chan envelope),
		readDone: make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *Client) readLoop() {
	defer close(c.readDone)

	for raw := range c.t.Incoming() {
		if c.OnTrace != nil {
			c.OnTrace("<-", raw)
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			// Not a JSON-RPC message -- most likely a server that logged
			// to stdout by mistake instead of stderr. Already surfaced
			// via OnTrace above; keep reading rather than tearing the
			// connection down over one bad message.
			continue
		}
		c.dispatch(env)
	}

	c.mu.Lock()
	c.readErr = c.t.Err()
	if c.readErr == nil {
		c.readErr = io.ErrClosedPipe
	}
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

// dispatch routes one decoded incoming message to whichever call() is
// waiting on its ID, if any. A message with no ID (a notification) or an
// ID nothing is waiting on (an abandoned/duplicate response) is dropped
// here -- both are already visible in the raw trace.
func (c *Client) dispatch(env envelope) {
	if env.ID == nil {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[*env.ID]
	if ok {
		delete(c.pending, *env.ID)
	}
	c.mu.Unlock()
	if ok {
		ch <- env
	}
}

// call sends a request and blocks for its matching response (or ctx
// cancellation, or the connection closing first). result, if non-nil, is
// decoded from the response's "result" field.
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	id := atomic.AddInt64(&c.nextID, 1)
	data, err := json.Marshal(request{JSONRPC: rpcVersion, ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("mcp: encoding %s request: %w", method, err)
	}

	ch := make(chan envelope, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("mcp: client is closed")
	}
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.send(ctx, data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("mcp: sending %s request: %w", method, err)
	}

	select {
	case env, ok := <-ch:
		if !ok {
			c.mu.Lock()
			readErr := c.readErr
			c.mu.Unlock()
			return fmt.Errorf("mcp: connection closed while waiting for %s response: %w", method, readErr)
		}
		if env.Error != nil {
			return env.Error
		}
		if result != nil && len(env.Result) > 0 {
			if err := json.Unmarshal(env.Result, result); err != nil {
				return fmt.Errorf("mcp: decoding %s result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}

// notify sends a notification (no response expected).
func (c *Client) notify(method string, params any) error {
	data, err := json.Marshal(notification{JSONRPC: rpcVersion, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("mcp: encoding %s notification: %w", method, err)
	}
	return c.send(context.Background(), data)
}

// send traces and delivers one outgoing message.
func (c *Client) send(ctx context.Context, data []byte) error {
	if c.OnTrace != nil {
		c.OnTrace("->", data)
	}
	return c.t.Send(ctx, data)
}

// Initialize performs MCP's handshake: an "initialize" request followed
// by a "notifications/initialized" notification once the server responds
// successfully (per spec, the client must send this before any other
// request). clientName/clientVersion identify AgentWorks to the server as
// ClientInfo.
func (c *Client) Initialize(ctx context.Context, clientName, clientVersion string) (InitializeResult, error) {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      ClientInfo{Name: clientName, Version: clientVersion},
	}
	var result InitializeResult
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return InitializeResult{}, err
	}
	if setter, ok := c.t.(protocolVersionSetter); ok {
		setter.SetProtocolVersion(result.ProtocolVersion)
	}
	if err := c.notify("notifications/initialized", map[string]any{}); err != nil {
		return InitializeResult{}, fmt.Errorf("mcp: sending initialized notification: %w", err)
	}
	return result, nil
}

// ListTools returns every tool the server exposes, following
// "nextCursor" pagination until the server stops returning one.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor,omitempty"`
		}
		if err := c.call(ctx, "tools/list", params, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Tools...)
		if page.NextCursor == "" {
			return all, nil
		}
		cursor = page.NextCursor
	}
}

// CallTool invokes one tool by name with the given arguments (may be nil
// for a tool that takes none).
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*CallToolResult, error) {
	params := map[string]any{"name": name}
	if arguments != nil {
		params["arguments"] = arguments
	}
	var result CallToolResult
	if err := c.call(ctx, "tools/call", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// paginate calls method repeatedly, following "nextCursor" until the server
// stops returning one, and hands each page's raw result to collect.
func (c *Client) paginate(ctx context.Context, method string, collect func(json.RawMessage) (next string, err error)) error {
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var raw json.RawMessage
		if err := c.call(ctx, method, params, &raw); err != nil {
			return err
		}
		next, err := collect(raw)
		if err != nil {
			return fmt.Errorf("mcp: decoding %s result: %w", method, err)
		}
		if next == "" {
			return nil
		}
		cursor = next
	}
}

// ListResources returns every resource the server exposes. Only ask a server
// that advertised the "resources" capability.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	var all []Resource
	err := c.paginate(ctx, "resources/list", func(raw json.RawMessage) (string, error) {
		var page struct {
			Resources  []Resource `json:"resources"`
			NextCursor string     `json:"nextCursor,omitempty"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return "", err
		}
		all = append(all, page.Resources...)
		return page.NextCursor, nil
	})
	return all, err
}

// ReadResource fetches one resource's contents by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContents, error) {
	var result struct {
		Contents []ResourceContents `json:"contents"`
	}
	if err := c.call(ctx, "resources/read", map[string]any{"uri": uri}, &result); err != nil {
		return nil, err
	}
	return result.Contents, nil
}

// ListPrompts returns every prompt the server offers. Only ask a server that
// advertised the "prompts" capability.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var all []Prompt
	err := c.paginate(ctx, "prompts/list", func(raw json.RawMessage) (string, error) {
		var page struct {
			Prompts    []Prompt `json:"prompts"`
			NextCursor string   `json:"nextCursor,omitempty"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return "", err
		}
		all = append(all, page.Prompts...)
		return page.NextCursor, nil
	})
	return all, err
}

// GetPrompt renders one prompt with the given arguments (may be nil).
func (c *Client) GetPrompt(ctx context.Context, name string, arguments map[string]string) (*GetPromptResult, error) {
	params := map[string]any{"name": name}
	if len(arguments) > 0 {
		params["arguments"] = arguments
	}
	var result GetPromptResult
	if err := c.call(ctx, "prompts/get", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Close stops accepting new work: it fails any in-flight call with "client
// is closed", releases anything still waiting in call(), and closes the
// transport -- for a real process (see StartProcess) that closes stdin, and
// EOF on stdin is the MCP stdio transport's own signal for a server to shut
// down. Close does not wait for the read side to finish; a caller that also
// owns the underlying process (StartProcess's Process) is responsible for
// waiting for/killing it -- see Process.Close.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()

	return c.t.Close()
}
