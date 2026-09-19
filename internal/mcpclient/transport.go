package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"sync"
)

// Transport moves JSON-RPC messages between a Client and one MCP server. The
// protocol above it (request/response correlation, the initialize handshake,
// tools/list, ...) doesn't care whether the bytes travel over a child
// process's stdin/stdout, a streamable-HTTP endpoint, or a legacy SSE stream.
//
// A Transport delivers whole messages: Send takes one JSON-RPC message
// (a request or a notification), and Incoming yields each message the server
// sends back -- responses, but also any request or notification the server
// initiates -- as raw JSON.
type Transport interface {
	// Send delivers one JSON-RPC message to the server. ctx bounds only the
	// delivery; for HTTP that includes reading the response's headers.
	Send(ctx context.Context, msg []byte) error
	// Incoming yields every message from the server. It is closed when the
	// connection ends, after which Err says why.
	Incoming() <-chan []byte
	// Err reports why Incoming closed. It is only meaningful after that.
	Err() error
	// Close ends the connection and releases its resources. It is safe to
	// call more than once.
	Close() error
}

// protocolVersionSetter is implemented by a Transport that must echo the
// negotiated protocol version on every request after initialize (streamable
// HTTP's MCP-Protocol-Version header). The Client calls it once the handshake
// succeeds.
type protocolVersionSetter interface {
	SetProtocolVersion(version string)
}

// stdioTransport speaks newline-delimited JSON-RPC over a writer (the
// server's stdin) and a reader (its stdout), the MCP stdio transport.
type stdioTransport struct {
	w   io.Writer
	wMu sync.Mutex

	in  chan []byte
	err error
}

func newStdioTransport(w io.Writer, r io.Reader) *stdioTransport {
	t := &stdioTransport{w: w, in: make(chan []byte, 16)}
	go t.read(r)
	return t
}

func (t *stdioTransport) read(r io.Reader) {
	defer close(t.in)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		t.in <- append([]byte(nil), line...)
	}
	t.err = scanner.Err()
	if t.err == nil {
		t.err = io.ErrClosedPipe // stdout closed with no scanner error = the server exited
	}
}

func (t *stdioTransport) Send(_ context.Context, msg []byte) error {
	t.wMu.Lock()
	defer t.wMu.Unlock()

	buf := make([]byte, len(msg)+1)
	copy(buf, msg)
	buf[len(msg)] = '\n'
	_, err := t.w.Write(buf)
	return err
}

func (t *stdioTransport) Incoming() <-chan []byte { return t.in }
func (t *stdioTransport) Err() error              { return t.err }

// Close closes the write side. For a real process that is stdin, and EOF on
// stdin is the stdio transport's own signal for a server to shut down.
func (t *stdioTransport) Close() error {
	if closer, ok := t.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
