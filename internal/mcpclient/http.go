package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// maxMessageBytes caps one JSON-RPC message read from a remote server.
const maxMessageBytes = 16 << 20

// httpClientFor returns the client used for a remote server. It has no overall
// timeout (an SSE stream is long-lived; callers bound work with a context), and
// it refuses to follow a redirect to a different host: request headers usually
// carry a credential (an API key, a bearer token), and Go would otherwise
// forward custom headers to wherever a redirect points.
func httpClientFor() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("stopped after 5 redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("refusing to follow a redirect from %s to a different host (%s), which would forward your headers there", via[0].URL.Host, req.URL.Host)
			}
			return nil
		},
	}
}

// ExpandEnv replaces ${NAME} references in each header value using lookup
// (normally os.LookupEnv), and returns the names it couldn't find, sorted. A
// header with a missing variable is left out of the result rather than sent
// with a half-expanded credential.
func ExpandEnv(headers map[string]string, lookup func(string) (string, bool)) (expanded map[string]string, missing []string) {
	expanded = make(map[string]string, len(headers))
	seen := map[string]bool{}
	for name, value := range headers {
		complete := true
		out := envRef.ReplaceAllStringFunc(value, func(ref string) string {
			varName := envRef.FindStringSubmatch(ref)[1]
			if v, found := lookup(varName); found && v != "" {
				return v
			}
			complete = false
			if !seen[varName] {
				seen[varName] = true
				missing = append(missing, varName)
			}
			return ref
		})
		if complete {
			expanded[name] = out
		}
	}
	sort.Strings(missing)
	return expanded, missing
}

// LookupOSEnv is os.LookupEnv, named for use as ExpandEnv's lookup.
func LookupOSEnv(name string) (string, bool) { return os.LookupEnv(name) }

// ---- streamable HTTP ------------------------------------------------------

// httpTransport is MCP's streamable-HTTP transport: every client message is a
// POST to one endpoint, and the reply arrives either as a plain JSON body or as
// an SSE stream in the response, ending when the response has been sent. A
// session id the server assigns on initialize is echoed on later requests.
type httpTransport struct {
	url     string
	headers map[string]string
	client  *http.Client

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	in  chan []byte
	err error

	mu        sync.Mutex
	sessionID string
	protocol  string
	closeOnce sync.Once
}

// DialHTTP returns a Client for a streamable-HTTP MCP server at endpoint,
// sending headers on every request. Nothing is sent until the first call
// (Initialize).
func DialHTTP(endpoint string, headers map[string]string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return NewWithTransport(&httpTransport{
		url: endpoint, headers: headers, client: httpClientFor(),
		ctx: ctx, cancel: cancel, in: make(chan []byte, 16),
	})
}

func (t *httpTransport) SetProtocolVersion(v string) {
	t.mu.Lock()
	t.protocol = v
	t.mu.Unlock()
}

func (t *httpTransport) newRequest(ctx context.Context, method string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	t.mu.Lock()
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	if t.protocol != "" {
		req.Header.Set("MCP-Protocol-Version", t.protocol)
	}
	t.mu.Unlock()
	return req, nil
}

func (t *httpTransport) Send(ctx context.Context, msg []byte) error {
	// The request ends when the caller's context does or the transport closes.
	rctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-t.ctx.Done():
			cancel()
		case <-rctx.Done():
		}
	}()

	req, err := t.newRequest(rctx, http.MethodPost, msg)
	if err != nil {
		cancel()
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		cancel()
		return redactURLError(err)
	}
	if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
		t.mu.Lock()
		if t.sessionID == "" {
			t.sessionID = id
		}
		t.mu.Unlock()
	}

	if resp.StatusCode/100 != 2 {
		defer cancel()
		defer resp.Body.Close()
		return httpStatusError(resp, t.sessionID != "")
	}

	switch mediaType(resp.Header.Get("Content-Type")) {
	case "text/event-stream":
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			defer cancel()
			defer resp.Body.Close()
			readSSE(resp.Body, func(event string, data []byte) {
				if event == "" || event == "message" {
					t.push(data)
				}
			})
		}()
		return nil
	case "application/json":
		defer cancel()
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxMessageBytes))
		if err != nil {
			return fmt.Errorf("reading the response: %w", err)
		}
		t.pushJSON(body)
		return nil
	default: // 202 Accepted for a notification or response: no body of interest
		defer cancel()
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
}

// pushJSON delivers a JSON body that is one message or a batch of them.
func (t *httpTransport) pushJSON(body []byte) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return
	}
	if body[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(body, &batch); err == nil {
			for _, m := range batch {
				t.push(m)
			}
			return
		}
	}
	t.push(body)
}

func (t *httpTransport) push(msg []byte) {
	select {
	case t.in <- append([]byte(nil), msg...):
	case <-t.ctx.Done():
	}
}

func (t *httpTransport) Incoming() <-chan []byte { return t.in }
func (t *httpTransport) Err() error              { return t.err }

// Close ends the session (a best-effort DELETE, as the spec describes),
// cancels anything in flight, and closes Incoming.
func (t *httpTransport) Close() error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		hasSession := t.sessionID != ""
		t.mu.Unlock()
		if hasSession {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if req, err := t.newRequest(ctx, http.MethodDelete, nil); err == nil {
				if resp, err := t.client.Do(req); err == nil {
					resp.Body.Close()
				}
			}
			cancel()
		}
		t.cancel()
		t.wg.Wait()
		t.err = errors.New("connection closed")
		close(t.in)
	})
	return nil
}

// ---- legacy SSE -----------------------------------------------------------

// sseTransport is MCP's older HTTP+SSE transport: the client opens a GET stream
// on which the server sends an "endpoint" event naming where to POST, and every
// server message afterwards. Requests go to that endpoint; their responses
// come back on the stream.
type sseTransport struct {
	base    *url.URL
	headers map[string]string
	client  *http.Client

	ctx    context.Context
	cancel context.CancelFunc

	in  chan []byte
	err error

	ready    chan struct{} // closed once the endpoint is known
	postURL  string
	closed   chan struct{} // closed when the stream goroutine has finished
	closeOne sync.Once
}

// DialSSE opens a legacy SSE MCP server at streamURL and waits (bounded by ctx)
// for the server to announce its POST endpoint.
func DialSSE(ctx context.Context, streamURL string, headers map[string]string) (*Client, error) {
	base, err := url.Parse(streamURL)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("%q is not a valid URL", streamURL)
	}
	tctx, cancel := context.WithCancel(context.Background())
	t := &sseTransport{
		base: base, headers: headers, client: httpClientFor(),
		ctx: tctx, cancel: cancel,
		in: make(chan []byte, 16), ready: make(chan struct{}), closed: make(chan struct{}),
	}

	req, err := http.NewRequestWithContext(tctx, http.MethodGet, streamURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := t.client.Do(req)
	if err != nil {
		cancel()
		return nil, redactURLError(err)
	}
	if resp.StatusCode != http.StatusOK {
		defer cancel()
		defer resp.Body.Close()
		return nil, httpStatusError(resp, false)
	}

	go t.stream(resp.Body)

	select {
	case <-t.ready:
		return NewWithTransport(t), nil
	case <-t.closed:
		cancel()
		if t.err != nil {
			return nil, fmt.Errorf("the SSE stream ended before announcing an endpoint: %w", t.err)
		}
		return nil, errors.New("the SSE stream ended before announcing an endpoint")
	case <-ctx.Done():
		cancel()
		<-t.closed
		return nil, fmt.Errorf("timed out waiting for the server's SSE endpoint: %w", ctx.Err())
	}
}

func (t *sseTransport) stream(body io.ReadCloser) {
	defer close(t.closed)
	defer close(t.in)
	defer body.Close()

	var once sync.Once
	readSSE(body, func(event string, data []byte) {
		switch event {
		case "endpoint":
			target, err := t.base.Parse(strings.TrimSpace(string(data)))
			if err != nil || target.Host != t.base.Host || target.Scheme != t.base.Scheme {
				t.err = fmt.Errorf("the server announced an endpoint (%q) that isn't on the same host as %s; refusing to send your headers there", string(data), t.base.Host)
				t.cancel()
				return
			}
			once.Do(func() {
				t.postURL = target.String()
				close(t.ready)
			})
		case "", "message":
			select {
			case t.in <- append([]byte(nil), data...):
			case <-t.ctx.Done():
			}
		}
	})
	if t.err == nil {
		t.err = io.ErrClosedPipe
	}
}

func (t *sseTransport) Send(ctx context.Context, msg []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.postURL, bytes.NewReader(msg))
	if err != nil {
		return err
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return redactURLError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return httpStatusError(resp, false)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return nil
}

func (t *sseTransport) Incoming() <-chan []byte { return t.in }
func (t *sseTransport) Err() error              { return t.err }

func (t *sseTransport) Close() error {
	t.closeOne.Do(func() {
		t.cancel()
		<-t.closed
	})
	return nil
}

// ---- shared helpers -------------------------------------------------------

// readSSE parses a text/event-stream and calls fn for each complete event.
// Comment lines (": ...") and unknown fields are ignored; an event's data lines
// are joined with newlines, per the SSE spec. It returns when the stream ends.
func readSSE(r io.Reader, fn func(event string, data []byte)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)

	var event string
	var data []string
	flush := func() {
		if len(data) > 0 {
			fn(event, []byte(strings.Join(data, "\n")))
		}
		event, data = "", nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	flush()
}

func mediaType(contentType string) string {
	mt, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(mt))
}

// httpStatusError explains a non-2xx response, with a hint for the two
// statuses that mean "your credentials".
func httpStatusError(resp *http.Response, hadSession bool) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.TrimSpace(string(snippet))
	err := fmt.Errorf("the server answered %s", resp.Status)
	if msg != "" {
		err = fmt.Errorf("the server answered %s: %s", resp.Status, msg)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w -- check the headers and the environment variables they reference", err)
	case resp.StatusCode == http.StatusNotFound && hadSession:
		return fmt.Errorf("%w -- the session may have expired", err)
	}
	return err
}

// redactURLError strips the URL from a *url.Error: a URL can embed a token in
// its query string or userinfo, and the error text ends up in the terminal.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}
