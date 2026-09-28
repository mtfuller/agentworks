// Package studio owns the localhost HTTP surface for the AgentWorks runtime.
package studio

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtfuller/agentworks/internal/connectors"
	"github.com/mtfuller/agentworks/internal/router"
	runtimestorage "github.com/mtfuller/agentworks/internal/storage"
	"github.com/mtfuller/agentworks/internal/store"
	awworkspace "github.com/mtfuller/agentworks/internal/workspace"
)

//go:embed static/*
var embeddedAssets embed.FS

type Config struct {
	Host         string
	Port         int
	ProjectName  string
	ProjectRoot  string
	Store        *store.Store
	Harnesses    []string
	Controller   RunController
	LogRoot      string
	StorageLimit int64
	Workspaces   *awworkspace.Registry
	Events       *router.Service
	Connectors   *connectors.Service
	Storage      *runtimestorage.Manager
}

type Instance struct {
	server    *http.Server
	listener  net.Listener
	url       string
	done      chan struct{}
	serveDone chan struct{}

	mu  sync.Mutex
	err error
}

func Start(ctx context.Context, config Config) (*Instance, error) {
	if config.Host == "" {
		config.Host = "127.0.0.1"
	}
	if !isLoopback(config.Host) {
		return nil, fmt.Errorf("Studio host %q is not loopback; the first runtime release is localhost-only", config.Host)
	}
	if config.Port < 0 || config.Port > 65535 {
		return nil, fmt.Errorf("Studio port %d is outside 0-65535", config.Port)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(config.Host, strconv.Itoa(config.Port)))
	if err != nil {
		return nil, fmt.Errorf("start Studio listener: %w", err)
	}
	if err := ctx.Err(); err != nil {
		listener.Close()
		return nil, err
	}

	handler, err := NewHandlerWithConfig(HandlerConfig{
		ProjectName:  config.ProjectName,
		ProjectRoot:  config.ProjectRoot,
		Store:        config.Store,
		Harnesses:    config.Harnesses,
		Controller:   config.Controller,
		LogRoot:      config.LogRoot,
		StorageLimit: config.StorageLimit,
		Workspaces:   config.Workspaces,
		Events:       config.Events,
		Connectors:   config.Connectors,
		Storage:      config.Storage,
	})
	if err != nil {
		listener.Close()
		return nil, err
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	instance := &Instance{
		server: server, listener: listener,
		url: "http://" + listener.Addr().String(), done: make(chan struct{}), serveDone: make(chan struct{}),
	}
	go instance.serve()
	go instance.coordinateShutdown(ctx)
	return instance, nil
}

func NewHandler(projectName string, stores ...*store.Store) (http.Handler, error) {
	config := HandlerConfig{ProjectName: projectName}
	if len(stores) != 0 {
		config.Store = stores[0]
	}
	return NewHandlerWithConfig(config)
}

type HandlerConfig struct {
	ProjectName  string
	ProjectRoot  string
	Store        *store.Store
	Harnesses    []string
	Controller   RunController
	LogRoot      string
	StorageLimit int64
	Workspaces   *awworkspace.Registry
	Events       *router.Service
	Connectors   *connectors.Service
	Storage      *runtimestorage.Manager
}

type RunController interface {
	Wake()
	Cancel(runID string) bool
}

func NewHandlerWithConfig(config HandlerConfig) (http.Handler, error) {
	assets, err := fs.Sub(embeddedAssets, "static")
	if err != nil {
		return nil, err
	}
	guard, err := newSessionGuard()
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	runtimeStore := config.Store
	definitions := &definitionEditor{root: config.ProjectRoot}
	harnesses := config.Harnesses
	if len(harnesses) == 0 {
		harnesses = []string{"claude-code", "github-copilot"}
	}
	mux.HandleFunc("GET /api/v1/health", func(writer http.ResponseWriter, request *http.Request) {
		health := map[string]any{
			"status": "ok", "project": config.ProjectName, "time": time.Now().UTC(),
			"storage": "unavailable",
		}
		if runtimeStore != nil {
			version, err := runtimeStore.SchemaVersion(request.Context())
			if err != nil {
				health["status"] = "degraded"
				health["storage"] = "error"
				health["storage_error"] = err.Error()
			} else {
				health["storage"] = "ready"
				health["schema_version"] = version
				if metrics, metricsErr := runtimeStore.ReadRuntimeMetrics(request.Context(), time.Now().UTC()); metricsErr == nil {
					health["metrics"] = map[string]any{
						"pending_runs": metrics.PendingRuns, "oldest_queue_delay_ms": metrics.OldestQueueDelay.Milliseconds(),
						"attempts": metrics.Attempts, "retry_attempts": metrics.RetryAttempts,
						"completed_runs": metrics.CompletedRuns, "average_duration_ms": metrics.AverageDuration.Milliseconds(),
						"configured_sources": metrics.ConfiguredSources, "stale_sources": metrics.StaleSources,
					}
				}
			}
		}
		if used, available, err := runtimeStorageUsage(config.LogRoot); err != nil {
			health["status"] = "degraded"
			health["storage_usage_error"] = "measure runtime storage"
		} else if available {
			health["storage_bytes"] = used
			if config.StorageLimit > 0 {
				health["storage_limit_bytes"] = config.StorageLimit
				health["storage_limit_exceeded"] = used > config.StorageLimit
			}
		}
		writeJSON(writer, http.StatusOK, health)
	})
	mux.HandleFunc("GET /api/v1/session", guard.issue)
	mux.HandleFunc("GET /api/v1/catalog", func(writer http.ResponseWriter, request *http.Request) {
		serveCatalog(writer, request, config.ProjectRoot, harnesses)
	})
	mux.HandleFunc("GET /api/v1/definitions", definitions.list)
	mux.HandleFunc("GET /api/v1/definitions/{path...}", definitions.get)
	mux.Handle("PUT /api/v1/definitions/{path...}", guard.protect(http.HandlerFunc(definitions.put)))
	mux.HandleFunc("GET /api/v1/runs", func(writer http.ResponseWriter, request *http.Request) {
		if runtimeStore == nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"error": "runtime storage is unavailable"})
			return
		}
		runs, err := runtimeStore.ListRuns(request.Context(), 100)
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]any{"error": "list runs"})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"runs": runs})
	})
	mux.HandleFunc("GET /api/v1/inbox", func(writer http.ResponseWriter, request *http.Request) {
		serveEventInbox(writer, request, runtimeStore)
	})
	mux.HandleFunc("GET /api/v1/inbox/{eventID}", func(writer http.ResponseWriter, request *http.Request) {
		serveEventDetail(writer, request, config.Events)
	})
	mux.Handle("POST /api/v1/event-ingest", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveIngestEvent(writer, request, config.Events)
	})))
	mux.Handle("POST /api/v1/routes/preview", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveRoutePreview(writer, request, config.Events)
	})))
	mux.HandleFunc("GET /api/v1/routes", func(writer http.ResponseWriter, request *http.Request) { serveRoutes(writer, request, config.Events) })
	mux.HandleFunc("GET /api/v1/sources", func(writer http.ResponseWriter, request *http.Request) {
		serveSources(writer, request, config.Connectors)
	})
	mux.HandleFunc("GET /api/v1/sources/{sourceName}/attempts", func(writer http.ResponseWriter, request *http.Request) {
		serveSourceAttempts(writer, request, config.Connectors)
	})
	mux.Handle("POST /api/v1/sources/{sourceName}/pause", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveSourceAction(writer, request, config.Connectors, "pause")
	})))
	mux.Handle("POST /api/v1/sources/{sourceName}/test", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveSourceAction(writer, request, config.Connectors, "test")
	})))
	mux.Handle("POST /api/v1/sources/{sourceName}/poll", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveSourceAction(writer, request, config.Connectors, "poll")
	})))
	mux.Handle("POST /api/v1/routes/{routeName}/enabled", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveRouteEnabled(writer, request, config.Events)
	})))
	mux.Handle("POST /api/v1/inbox/{eventID}/route", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveEventAction(writer, request, config.Events, "route")
	})))
	mux.Handle("POST /api/v1/inbox/{eventID}/replay", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveEventAction(writer, request, config.Events, "replay")
	})))
	mux.Handle("POST /api/v1/inbox/{eventID}/reevaluate", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveEventAction(writer, request, config.Events, "reevaluate")
	})))
	mux.Handle("POST /api/v1/runs", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var wake func()
		if config.Controller != nil {
			wake = config.Controller.Wake
		}
		serveCreateManualRun(writer, request, config.ProjectRoot, runtimeStore, harnesses, config.Workspaces, wake)
	})))
	mux.Handle("POST /api/v1/workspaces/confirm", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveConfirmWorkspace(writer, request, config.Workspaces)
	})))
	mux.HandleFunc("GET /api/v1/runs/{runID}", func(writer http.ResponseWriter, request *http.Request) {
		serveRunDetail(writer, request, runtimeStore)
	})
	mux.HandleFunc("GET /api/v1/outcomes", func(writer http.ResponseWriter, request *http.Request) { serveOutcomes(writer, request, runtimeStore) })
	mux.Handle("POST /api/v1/outcomes", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveCreateOutcome(writer, request, runtimeStore)
	})))
	mux.HandleFunc("GET /api/v1/monitors", func(writer http.ResponseWriter, request *http.Request) { serveMonitors(writer, request, runtimeStore) })
	mux.HandleFunc("GET /api/v1/work-items/{workItemID}/timeline", func(writer http.ResponseWriter, request *http.Request) { serveTimeline(writer, request, runtimeStore) })
	mux.Handle("POST /api/v1/memory-proposals/{proposalID}/decision", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveMemoryDecision(writer, request, runtimeStore, config.ProjectRoot)
	})))
	mux.Handle("POST /api/v1/runs/{runID}/cancel", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveCancelRun(writer, request, runtimeStore, config.Controller)
	})))
	mux.Handle("POST /api/v1/runs/{runID}/pin", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		servePinRun(writer, request, runtimeStore)
	})))
	mux.Handle("DELETE /api/v1/runs/{runID}/detail", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveDeleteRunDetail(writer, request, config.Storage)
	})))
	mux.Handle("POST /api/v1/approvals/{approvalID}/decision", guard.protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveApprovalDecision(writer, request, runtimeStore)
	})))
	mux.HandleFunc("GET /api/v1/runs/{runID}/attempts/{attemptID}/log", func(writer http.ResponseWriter, request *http.Request) {
		serveAttemptLog(writer, request, runtimeStore, config.LogRoot)
	})
	mux.HandleFunc("GET /api/v1/events", func(writer http.ResponseWriter, request *http.Request) {
		serveRuntimeEvents(writer, request, config.ProjectName, runtimeStore)
	})
	mux.Handle("GET /", http.FileServer(http.FS(assets)))
	return securityHeaders(validateHost(mux)), nil
}

func (instance *Instance) URL() string { return instance.url }

func (instance *Instance) Wait() error {
	<-instance.done
	instance.mu.Lock()
	defer instance.mu.Unlock()
	return instance.err
}

func (instance *Instance) serve() {
	err := instance.server.Serve(instance.listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	instance.mu.Lock()
	instance.err = err
	instance.mu.Unlock()
	close(instance.serveDone)
}

func (instance *Instance) coordinateShutdown(ctx context.Context) {
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := instance.server.Shutdown(shutdown)
		cancel()
		if err != nil {
			_ = instance.server.Close()
		}
		<-instance.serveDone
	case <-instance.serveDone:
	}
	close(instance.done)
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(strings.Trim(host, "[]"))
	return address != nil && address.IsLoopback()
}

func validateHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host := request.Host
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
		}
		if !isLoopback(host) {
			http.Error(writer, "invalid Host header", http.StatusForbidden)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; script-src 'self'; style-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(writer, request)
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
