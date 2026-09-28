package approval

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

const maxGatewayRequestBytes = 64 << 10

type gatewaySession struct {
	claim      store.Claim
	workspace  string
	permission store.Permission
	ctx        context.Context
}

// Gateway is a loopback-only, run-scoped bridge between vendor helper
// processes and the durable approval broker.
type Gateway struct {
	store    *store.Store
	leaseTTL time.Duration
	server   *http.Server
	listener net.Listener

	mu       sync.RWMutex
	sessions map[string]gatewaySession
}

type Credentials struct {
	Endpoint string
	Token    string
	close    func()
}

func (credentials Credentials) Close() {
	if credentials.close != nil {
		credentials.close()
	}
}

func StartGateway(ctx context.Context, runtimeStore *store.Store, leaseTTL time.Duration) (*Gateway, error) {
	if runtimeStore == nil {
		return nil, errors.New("approval gateway store is required")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for approval gateway: %w", err)
	}
	gateway := &Gateway{store: runtimeStore, leaseTTL: leaseTTL, listener: listener, sessions: map[string]gatewaySession{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/authorize", gateway.authorize)
	gateway.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = gateway.server.Shutdown(shutdownCtx)
	}()
	go func() { _ = gateway.server.Serve(listener) }()
	return gateway, nil
}

func (gateway *Gateway) Open(ctx context.Context, claim store.Claim, workspace string) (Credentials, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return Credentials{}, fmt.Errorf("create permission token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	gateway.mu.Lock()
	gateway.sessions[token] = gatewaySession{claim: claim, workspace: workspace, permission: claim.Run.Permission, ctx: ctx}
	gateway.mu.Unlock()
	var once sync.Once
	return Credentials{
		Endpoint: "http://" + gateway.listener.Addr().String() + "/v1/authorize",
		Token:    token,
		close: func() {
			once.Do(func() {
				gateway.mu.Lock()
				delete(gateway.sessions, token)
				gateway.mu.Unlock()
			})
		},
	}, nil
}

func (gateway *Gateway) authorize(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	gateway.mu.RLock()
	var session gatewaySession
	for candidate, value := range gateway.sessions {
		if len(candidate) == len(token) && subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1 {
			session = value
			break
		}
	}
	gateway.mu.RUnlock()
	if session.claim.Run.ID == "" || session.ctx.Err() != nil {
		writeGatewayJSON(writer, http.StatusUnauthorized, map[string]any{"approved": false, "error": "invalid or expired run token"})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxGatewayRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var tool ToolRequest
	if err := decoder.Decode(&tool); err != nil {
		writeGatewayJSON(writer, http.StatusBadRequest, map[string]any{"approved": false, "error": "invalid tool request"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeGatewayJSON(writer, http.StatusBadRequest, map[string]any{"approved": false, "error": "invalid tool request"})
		return
	}
	normalized, err := NormalizeToolRequest(tool, session.workspace, session.permission)
	if err != nil {
		writeGatewayJSON(writer, http.StatusForbidden, map[string]any{"approved": false, "error": err.Error()})
		return
	}
	approved, err := (Broker{Store: gateway.store, LeaseTTL: gateway.leaseTTL}).Authorize(session.ctx, session.claim, normalized)
	if err != nil {
		writeGatewayJSON(writer, http.StatusServiceUnavailable, map[string]any{"approved": false, "error": "approval unavailable"})
		return
	}
	writeGatewayJSON(writer, http.StatusOK, map[string]any{"approved": approved})
}

func writeGatewayJSON(writer http.ResponseWriter, status int, value any) {
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
