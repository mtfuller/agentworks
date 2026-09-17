package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeServer is a hermetic stand-in for a real MCP server process: it
// reads decoded requests off an io.Pipe fed by the Client under test and
// lets the test script canned responses back, without spawning a real
// subprocess (no Python/Node dependency in `go test`, and no flakiness
// from real process scheduling).
type fakeServer struct {
	t   *testing.T
	in  *bufio.Scanner // requests from the client
	out io.Writer      // responses to the client
}

// newFakeServer wires a Client up to a fakeServer and returns both. The
// caller drives fakeServer.handleNext (or a loop of it) from its own
// goroutine to script responses.
func newFakeServer(t *testing.T) (*Client, *fakeServer) {
	t.Helper()
	clientToServer := newPipe()
	serverToClient := newPipe()

	c := New(clientToServer.w, serverToClient.r)
	fs := &fakeServer{t: t, in: bufio.NewScanner(clientToServer.r), out: serverToClient.w}

	t.Cleanup(func() {
		_ = c.Close()
		// Also close the fake server's write end, so the client's
		// read-loop goroutine (blocked reading serverToClient.r) sees
		// EOF and exits instead of leaking for the rest of the test
		// binary's run.
		_ = serverToClient.w.Close()
	})
	return c, fs
}

// pipe bundles an io.Pipe's two ends so newFakeServer can close them
// together on cleanup without threading four values around.
type pipeEnds struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func newPipe() pipeEnds {
	r, w := io.Pipe()
	return pipeEnds{r: r, w: w}
}

// nextRequest reads and decodes the next line the client sent.
func (fs *fakeServer) nextRequest() request {
	fs.t.Helper()
	if !fs.in.Scan() {
		fs.t.Fatalf("fakeServer: no more requests from client: %v", fs.in.Err())
	}
	var req request
	if err := json.Unmarshal(fs.in.Bytes(), &req); err != nil {
		fs.t.Fatalf("fakeServer: decoding request: %v", err)
	}
	return req
}

// respond writes a canned successful response for the given request ID.
func (fs *fakeServer) respond(id int64, result any) {
	fs.t.Helper()
	data, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Result  any    `json:"result"`
	}{"2.0", id, result})
	if err != nil {
		fs.t.Fatalf("fakeServer: encoding response: %v", err)
	}
	if _, err := fs.out.Write(append(data, '\n')); err != nil {
		fs.t.Fatalf("fakeServer: writing response: %v", err)
	}
}

// respondError writes a canned JSON-RPC error response.
func (fs *fakeServer) respondError(id int64, code int, message string) {
	fs.t.Helper()
	data, err := json.Marshal(struct {
		JSONRPC string    `json:"jsonrpc"`
		ID      int64     `json:"id"`
		Error   *RPCError `json:"error"`
	}{"2.0", id, &RPCError{Code: code, Message: message}})
	if err != nil {
		fs.t.Fatalf("fakeServer: encoding error response: %v", err)
	}
	if _, err := fs.out.Write(append(data, '\n')); err != nil {
		fs.t.Fatalf("fakeServer: writing error response: %v", err)
	}
}

func TestInitializeHandshake(t *testing.T) {
	c, fs := newFakeServer(t)

	done := make(chan struct{})
	var initErr error
	var result InitializeResult
	go func() {
		defer close(done)
		result, initErr = c.Initialize(context.Background(), "agentworks", "test")
	}()

	req := fs.nextRequest()
	if req.Method != "initialize" {
		t.Fatalf("first request method = %q, want %q", req.Method, "initialize")
	}
	fs.respond(req.ID, InitializeResult{
		ProtocolVersion: protocolVersion,
		ServerInfo:      ClientInfo{Name: "fake-server", Version: "0.0.1"},
	})

	// The client must also send "notifications/initialized" -- it has no
	// ID, so it won't come through nextRequest's request-shaped decode
	// cleanly for ID, but Method still round-trips.
	notif := fs.nextRequest()
	if notif.Method != "notifications/initialized" {
		t.Fatalf("second message method = %q, want %q", notif.Method, "notifications/initialized")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Initialize did not return in time")
	}

	if initErr != nil {
		t.Fatalf("Initialize() error = %v", initErr)
	}
	if result.ServerInfo.Name != "fake-server" {
		t.Errorf("ServerInfo.Name = %q, want %q", result.ServerInfo.Name, "fake-server")
	}
}

func TestListToolsFollowsPagination(t *testing.T) {
	c, fs := newFakeServer(t)

	done := make(chan struct{})
	var listErr error
	var tools []Tool
	go func() {
		defer close(done)
		tools, listErr = c.ListTools(context.Background())
	}()

	req1 := fs.nextRequest()
	if req1.Method != "tools/list" {
		t.Fatalf("method = %q, want tools/list", req1.Method)
	}
	fs.respond(req1.ID, map[string]any{
		"tools":      []Tool{{Name: "a"}},
		"nextCursor": "page2",
	})

	req2 := fs.nextRequest()
	fs.respond(req2.ID, map[string]any{
		"tools": []Tool{{Name: "b"}},
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ListTools did not return in time")
	}

	if listErr != nil {
		t.Fatalf("ListTools() error = %v", listErr)
	}
	if len(tools) != 2 || tools[0].Name != "a" || tools[1].Name != "b" {
		t.Fatalf("ListTools() = %+v, want [a b]", tools)
	}
}

func TestCallToolSuccess(t *testing.T) {
	c, fs := newFakeServer(t)

	done := make(chan struct{})
	var callErr error
	var result *CallToolResult
	go func() {
		defer close(done)
		result, callErr = c.CallTool(context.Background(), "fetch_issue", map[string]any{"issue_key": "PROJ-1"})
	}()

	req := fs.nextRequest()
	if req.Method != "tools/call" {
		t.Fatalf("method = %q, want tools/call", req.Method)
	}
	fs.respond(req.ID, CallToolResult{Content: []ContentBlock{{Type: "text", Text: "ok"}}})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("CallTool did not return in time")
	}

	if callErr != nil {
		t.Fatalf("CallTool() error = %v", callErr)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "ok" {
		t.Fatalf("CallTool() result = %+v", result)
	}
	if result.IsError {
		t.Errorf("IsError = true, want false")
	}
}

func TestCallToolRPCError(t *testing.T) {
	c, fs := newFakeServer(t)

	done := make(chan struct{})
	var callErr error
	go func() {
		defer close(done)
		_, callErr = c.CallTool(context.Background(), "does_not_exist", nil)
	}()

	req := fs.nextRequest()
	fs.respondError(req.ID, -32601, "method not found")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("CallTool did not return in time")
	}

	if callErr == nil {
		t.Fatal("CallTool() error = nil, want an RPC error")
	}
	rpcErr, ok := callErr.(*RPCError)
	if !ok {
		t.Fatalf("CallTool() error type = %T, want *RPCError", callErr)
	}
	if rpcErr.Code != -32601 {
		t.Errorf("RPCError.Code = %d, want -32601", rpcErr.Code)
	}
}

func TestCallContextTimeout(t *testing.T) {
	c, fs := newFakeServer(t)
	// Drain the request off the pipe (an io.Pipe write blocks until
	// something reads it) but never respond -- the call must still give
	// up once its context expires rather than hanging forever.
	go fs.nextRequest()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.CallTool(ctx, "slow_tool", nil)
	if err != context.DeadlineExceeded {
		t.Fatalf("CallTool() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestTraceHookSeesBothDirections(t *testing.T) {
	c, fs := newFakeServer(t)

	// OnTrace fires from both the caller's goroutine ("->", inside
	// writeLine) and the read-loop goroutine ("<-"), so the slice it
	// appends to needs its own lock -- a real race, not just a lint nit,
	// under `go test -race`.
	var traceMu sync.Mutex
	var directions []string
	c.OnTrace = func(direction string, raw []byte) {
		traceMu.Lock()
		directions = append(directions, direction)
		traceMu.Unlock()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.Initialize(context.Background(), "agentworks", "test")
	}()

	req := fs.nextRequest()
	fs.respond(req.ID, InitializeResult{})
	fs.nextRequest() // notifications/initialized

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Initialize did not return in time")
	}

	// Give the read-loop goroutine a moment to process the last traced
	// line (the notification has no response to synchronize on).
	time.Sleep(20 * time.Millisecond)

	traceMu.Lock()
	defer traceMu.Unlock()
	var sent, received bool
	for _, d := range directions {
		if d == "->" {
			sent = true
		}
		if d == "<-" {
			received = true
		}
	}
	if !sent || !received {
		t.Errorf("OnTrace directions = %v, want both \"->\" and \"<-\"", directions)
	}
}
