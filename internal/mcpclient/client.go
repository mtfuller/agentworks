package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// Client speaks MCP's stdio JSON-RPC transport to a single already-
// connected server: w is where requests/notifications are written, r is
// where the server's responses/notifications are read from (one JSON
// value per line). New starts a background goroutine reading r
// immediately; there's no separate Start step.
//
// For a real child process (a tool artifact's declared "command"), use
// StartProcess instead of New directly -- it wires up the process's own
// stdin/stdout/stderr and returns a Client already bound to them.
type Client struct {
	w   io.Writer
	wMu sync.Mutex

	nextID int64

	mu      sync.Mutex
	pending map[int64]chan envelope
	closed  bool
	readErr error

	readDone chan struct{}

	// OnTrace, if set, is called for every raw JSON-RPC line sent ("->")
	// or received ("<-") -- the inspector's raw-traffic log pane. May be
	// called from the read-loop goroutine ("<-") or from whichever
	// goroutine is calling a request method ("->"); implementations must
	// not block or call back into the Client.
	OnTrace func(direction string, raw []byte)

	// OnStderr, if set, is called with each line the server process
	// writes to its stderr (MCP servers log there, never stdout -- stdout
	// is reserved for the JSON-RPC channel). Only populated when the
	// Client came from StartProcess; a Client built from New has no
	// stderr of its own to report.
	OnStderr func(line string)
}

// New wraps an already-connected transport (see the Client doc comment).
func New(w io.Writer, r io.Reader) *Client {
	c := &Client{
		w:        w,
		pending:  make(map[int64]chan envelope),
		readDone: make(chan struct{}),
	}
	go c.readLoop(r)
	return c
}

func (c *Client) readLoop(r io.Reader) {
	defer close(c.readDone)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		traced := append([]byte(nil), line...)
		if c.OnTrace != nil {
			c.OnTrace("<-", traced)
		}

		var env envelope
		if err := json.Unmarshal(line, &env); err != nil {
			// Not a JSON-RPC message -- most likely a server that logged
			// to stdout by mistake instead of stderr. Already surfaced
			// via OnTrace above; keep reading rather than tearing the
			// connection down over one bad line.
			continue
		}
		c.dispatch(env)
	}

	c.mu.Lock()
	c.readErr = scanner.Err()
	if c.readErr == nil {
		c.readErr = io.ErrClosedPipe // stdout closed with no scanner error = server exited
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

	if err := c.writeLine(data); err != nil {
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
	return c.writeLine(data)
}

func (c *Client) writeLine(data []byte) error {
	c.wMu.Lock()
	defer c.wMu.Unlock()

	if c.OnTrace != nil {
		c.OnTrace("->", data)
	}
	buf := make([]byte, len(data)+1)
	copy(buf, data)
	buf[len(data)] = '\n'
	_, err := c.w.Write(buf)
	return err
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

// Close stops accepting new work: it fails any in-flight call with "client
// is closed", releases anything still waiting in call(), and closes the
// write side (w), which for a real process (see StartProcess) is stdin --
// EOF on stdin is the MCP stdio transport's own signal for a server to
// shut down. Close does not wait for the read side to finish; a caller
// that also owns the underlying process (StartProcess's Process) is
// responsible for waiting for/killing it -- see Process.Close.
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

	if closer, ok := c.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
