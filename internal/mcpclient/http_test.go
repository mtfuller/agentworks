package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// rpcRequest is the part of an incoming JSON-RPC message a fake server needs.
type rpcRequest struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func rpcResult(id int64, result any) []byte {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	return data
}

// fakeStreamableServer is a minimal streamable-HTTP MCP server. It records the
// headers of every request, requires a bearer token, and answers tools/list as
// an SSE stream (to exercise both response forms).
type fakeStreamableServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []http.Header
	methods  []string
	deleted  bool
}

func newFakeStreamable(t *testing.T) *fakeStreamableServer {
	t.Helper()
	f := &fakeStreamableServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Header.Clone())
		f.mu.Unlock()

		if r.Header.Get("Authorization") != "Bearer sekrit" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodDelete {
			f.mu.Lock()
			f.deleted = true
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req rpcRequest
		json.Unmarshal(body, &req)
		f.mu.Lock()
		f.methods = append(f.methods, req.Method)
		f.mu.Unlock()

		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "sess-1")
			w.Write(rpcResult(*req.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"serverInfo":      map[string]any{"name": "fake", "version": "9"},
				"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{}},
			}))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, ": a comment\nevent: message\ndata: %s\n\n", rpcResult(*req.ID, map[string]any{
				"tools": []map[string]any{{"name": "hello", "description": "Greets"}},
			}))
		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			w.Write(rpcResult(*req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "hi there"}}}))
		case "resources/list":
			w.Header().Set("Content-Type", "application/json")
			w.Write(rpcResult(*req.ID, map[string]any{"resources": []map[string]any{{"uri": "mem://a", "name": "a"}}}))
		default:
			http.Error(w, "no such method", http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func TestDialHTTPFullFlow(t *testing.T) {
	srv := newFakeStreamable(t)
	c := DialHTTP(srv.URL, map[string]string{"Authorization": "Bearer sekrit", "X-Custom": "yes"})

	var trace []string
	var traceMu sync.Mutex
	c.OnTrace = func(dir string, raw []byte) {
		traceMu.Lock()
		trace = append(trace, dir+" "+string(raw))
		traceMu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := c.Initialize(ctx, "agentworks-test", "0")
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if info.ServerInfo.Name != "fake" || info.ProtocolVersion != "2025-06-18" {
		t.Errorf("Initialize() = %+v", info)
	}

	tools, err := c.ListTools(ctx) // answered as an SSE stream
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "hello" {
		t.Errorf("ListTools() = %+v, want [hello]", tools)
	}

	result, err := c.CallTool(ctx, "hello", map[string]any{"name": "x"}) // answered as plain JSON
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hi there" {
		t.Errorf("CallTool() = %+v", result)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	for i, h := range srv.requests {
		if h.Get("Authorization") != "Bearer sekrit" || h.Get("X-Custom") != "yes" {
			t.Errorf("request %d lost the configured headers: %v", i, h)
		}
		if !strings.Contains(h.Get("Accept"), "text/event-stream") {
			t.Errorf("request %d Accept = %q, want it to accept an SSE reply", i, h.Get("Accept"))
		}
	}
	// After initialize, every request echoes the session id and protocol version.
	for i, h := range srv.requests[1:] {
		if h.Get("Mcp-Session-Id") != "sess-1" {
			t.Errorf("request %d has no session id echoed: %v", i+1, h)
		}
		if h.Get("MCP-Protocol-Version") != "2025-06-18" {
			t.Errorf("request %d MCP-Protocol-Version = %q, want the negotiated version", i+1, h.Get("MCP-Protocol-Version"))
		}
	}
	if !srv.deleted {
		t.Error("Close() should end the session with a DELETE")
	}

	// The trace shows message bodies only -- never the credential in a header.
	traceMu.Lock()
	defer traceMu.Unlock()
	if len(trace) == 0 {
		t.Fatal("OnTrace received nothing")
	}
	for _, line := range trace {
		if strings.Contains(line, "sekrit") {
			t.Errorf("the credential leaked into the trace: %s", line)
		}
	}
}

func TestDialHTTPAuthFailureHintsWithoutLeakingTheCredential(t *testing.T) {
	srv := newFakeStreamable(t)
	c := DialHTTP(srv.URL, map[string]string{"Authorization": "Bearer wrong-token-12345"})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Initialize(ctx, "t", "0")
	if err == nil {
		t.Fatal("Initialize() with a bad token expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "401") || !strings.Contains(msg, "environment variables") {
		t.Errorf("error = %q, want the status and a hint about headers/environment", msg)
	}
	if strings.Contains(msg, "wrong-token-12345") {
		t.Errorf("the credential appears in the error: %q", msg)
	}
}

func TestDialHTTPUnreachableErrorDoesNotEchoTheURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/mcp?token=in-the-url"
	srv.Close() // now nothing is listening

	c := DialHTTP(url, nil)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Initialize(ctx, "t", "0")
	if err == nil {
		t.Fatal("Initialize() against a closed server expected an error")
	}
	if strings.Contains(err.Error(), "in-the-url") {
		t.Errorf("a token in the URL leaked into the error: %v", err)
	}
}

func TestDialHTTPRefusesACrossHostRedirect(t *testing.T) {
	var otherSaw string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherSaw = r.Header.Get("X-Api-Key")
	}))
	defer other.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	c := DialHTTP(redirector.URL, map[string]string{"X-Api-Key": "topsecret"})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Initialize(ctx, "t", "0")
	if err == nil || !strings.Contains(err.Error(), "different host") {
		t.Errorf("Initialize() error = %v, want the cross-host redirect refused", err)
	}
	if otherSaw != "" {
		t.Errorf("the other host received the credential header: %q", otherSaw)
	}
}

func TestDialHTTPAcceptsABatchedJSONReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req rpcRequest
		json.Unmarshal(body, &req)
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// A server notification alongside the response, as a batch.
		fmt.Fprintf(w, `[{"jsonrpc":"2.0","method":"notifications/message","params":{}},%s]`, rpcResult(*req.ID, map[string]any{"tools": []any{}}))
	}))
	defer srv.Close()

	c := DialHTTP(srv.URL, nil)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.ListTools(ctx); err != nil {
		t.Fatalf("ListTools() with a batched reply error = %v", err)
	}
}

// ---- legacy SSE -----------------------------------------------------------

// fakeSSEServer speaks the older HTTP+SSE transport: GET /sse opens a stream and
// announces /messages; POSTs to /messages are answered on the stream.
type fakeSSEServer struct {
	*httptest.Server
	endpoint string // what the "endpoint" event announces
	mu       sync.Mutex
	stream   chan []byte
	posted   []http.Header
}

func newFakeSSE(t *testing.T, endpoint string) *fakeSSEServer {
	t.Helper()
	f := &fakeSSEServer{endpoint: endpoint, stream: make(chan []byte, 8)}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			ep := f.endpoint
			if ep == "" {
				ep = "/messages?sid=1"
			}
			fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", ep)
			flusher.Flush()
			for {
				select {
				case msg := <-f.stream:
					fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
					flusher.Flush()
				case <-r.Context().Done():
					return
				}
			}
		case r.Method == http.MethodPost && r.URL.Path == "/messages":
			f.mu.Lock()
			f.posted = append(f.posted, r.Header.Clone())
			f.mu.Unlock()
			body, _ := io.ReadAll(r.Body)
			var req rpcRequest
			json.Unmarshal(body, &req)
			w.WriteHeader(http.StatusAccepted)
			if req.ID != nil {
				switch req.Method {
				case "initialize":
					f.stream <- rpcResult(*req.ID, map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "sse-fake"}})
				case "tools/list":
					f.stream <- rpcResult(*req.ID, map[string]any{"tools": []map[string]any{{"name": "ping"}}})
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func TestDialSSEFullFlow(t *testing.T) {
	srv := newFakeSSE(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := DialSSE(ctx, srv.URL+"/sse", map[string]string{"X-Api-Key": "k"})
	if err != nil {
		t.Fatalf("DialSSE() error = %v", err)
	}
	defer c.Close()

	info, err := c.Initialize(ctx, "t", "0")
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if info.ServerInfo.Name != "sse-fake" {
		t.Errorf("serverInfo = %+v", info.ServerInfo)
	}
	tools, err := c.ListTools(ctx)
	if err != nil || len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("ListTools() = %+v, %v", tools, err)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, h := range srv.posted {
		if h.Get("X-Api-Key") != "k" {
			t.Errorf("a POST lost the configured header: %v", h)
		}
	}
}

func TestDialSSERefusesAnEndpointOnAnotherHost(t *testing.T) {
	srv := newFakeSSE(t, "http://evil.example.com/steal")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if c, err := DialSSE(ctx, srv.URL+"/sse", map[string]string{"Authorization": "Bearer s"}); err == nil {
		c.Close()
		t.Fatal("DialSSE() should refuse an endpoint on a different host, which would receive the headers")
	} else if !strings.Contains(err.Error(), "same host") {
		t.Errorf("error = %v, want it to explain the host mismatch", err)
	}
}

func TestDialSSEBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := DialSSE(ctx, srv.URL, nil); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("DialSSE() error = %v, want the 403 reported", err)
	}
}

func TestDialSSETimesOutWaitingForTheEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // never announces an endpoint
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := DialSSE(ctx, srv.URL, nil); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("DialSSE() error = %v, want a timeout", err)
	}
}

// ---- helpers --------------------------------------------------------------

func TestReadSSE(t *testing.T) {
	stream := ": keepalive\n" +
		"event: message\ndata: one\n\n" +
		"data: two-a\ndata: two-b\n\n" +
		"event: endpoint\ndata: /x\n\n" +
		"data: trailing-without-blank-line"
	type ev struct{ event, data string }
	var got []ev
	readSSE(strings.NewReader(stream), func(event string, data []byte) { got = append(got, ev{event, string(data)}) })
	want := []ev{{"message", "one"}, {"", "two-a\ntwo-b"}, {"endpoint", "/x"}, {"", "trailing-without-blank-line"}}
	if len(got) != len(want) {
		t.Fatalf("got %d events %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestExpandEnv(t *testing.T) {
	env := map[string]string{"TOKEN": "abc", "EMPTY": ""}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	got, missing := ExpandEnv(map[string]string{
		"Authorization": "Bearer ${TOKEN}",
		"X-Static":      "plain",
		"X-Two":         "${TOKEN}-${TOKEN}",
		"X-Missing":     "${NOPE}",
		"X-Empty":       "${EMPTY}",
		"X-Half":        "a-${TOKEN}-${ALSO_NOPE}",
	}, lookup)

	if got["Authorization"] != "Bearer abc" || got["X-Static"] != "plain" || got["X-Two"] != "abc-abc" {
		t.Errorf("expanded = %v", got)
	}
	for _, name := range []string{"X-Missing", "X-Empty", "X-Half"} {
		if _, present := got[name]; present {
			t.Errorf("%s has an unresolved variable and must be left out, not sent half-expanded: %q", name, got[name])
		}
	}
	if want := []string{"ALSO_NOPE", "EMPTY", "NOPE"}; strings.Join(missing, ",") != strings.Join(want, ",") {
		t.Errorf("missing = %v, want %v (an empty value counts as missing)", missing, want)
	}
}
