// Package approval coordinates durable, bounded, run-only permission grants.
package approval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	pathpkg "path"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

type Request struct {
	Kind    string
	Scope   json.RawMessage
	Summary string
}

func (request Request) Validate() error {
	request.Kind = strings.TrimSpace(request.Kind)
	if request.Kind == "" || !json.Valid(request.Scope) {
		return errors.New("approval kind and valid scope are required")
	}
	switch request.Kind {
	case "filesystem.write":
		var scope struct {
			Directory string `json:"directory"`
		}
		if err := decodeScope(request.Scope, &scope); err != nil {
			return err
		}
		normalized := strings.ReplaceAll(scope.Directory, "\\", "/")
		clean := pathpkg.Clean(normalized)
		windowsAbsolute := len(normalized) >= 3 && normalized[1] == ':' && normalized[2] == '/'
		if scope.Directory == "" || strings.HasPrefix(normalized, "/") || windowsAbsolute || clean == ".." || strings.HasPrefix(clean, "../") {
			return errors.New("filesystem approval directory must stay within the workspace")
		}
	case "command.execute":
		var scope struct {
			Executable string   `json:"executable"`
			Subcommand []string `json:"subcommand"`
		}
		if err := decodeScope(request.Scope, &scope); err != nil {
			return err
		}
		if strings.TrimSpace(scope.Executable) == "" || strings.ContainsAny(scope.Executable, "/\\:") || len(scope.Subcommand) == 0 {
			return errors.New("command approval requires an executable name and non-empty subcommand prefix")
		}
		for _, argument := range scope.Subcommand {
			if strings.TrimSpace(argument) == "" {
				return errors.New("command approval subcommand arguments must not be empty")
			}
		}
	default:
		return fmt.Errorf("unsupported approval kind %q", request.Kind)
	}
	return nil
}

// CoveredBy reports whether one previously approved run grant covers request.
func (request Request) CoveredBy(grant store.Approval) bool {
	if grant.State != store.ApprovalApproved || grant.Kind != request.Kind {
		return false
	}
	switch request.Kind {
	case "filesystem.write":
		var wanted, allowed struct {
			Directory string `json:"directory"`
		}
		if decodeScope(request.Scope, &wanted) != nil || decodeScope(grant.Scope, &allowed) != nil {
			return false
		}
		wantedPath := pathpkg.Clean(strings.ReplaceAll(wanted.Directory, "\\", "/"))
		allowedPath := pathpkg.Clean(strings.ReplaceAll(allowed.Directory, "\\", "/"))
		return wantedPath == allowedPath || allowedPath == "." || strings.HasPrefix(wantedPath, allowedPath+"/")
	case "command.execute":
		var wanted, allowed struct {
			Executable string   `json:"executable"`
			Subcommand []string `json:"subcommand"`
		}
		if decodeScope(request.Scope, &wanted) != nil || decodeScope(grant.Scope, &allowed) != nil ||
			wanted.Executable != allowed.Executable || len(wanted.Subcommand) < len(allowed.Subcommand) {
			return false
		}
		for index := range allowed.Subcommand {
			if wanted.Subcommand[index] != allowed.Subcommand[index] {
				return false
			}
		}
		return true
	default:
		return false
	}
}

type Broker struct {
	Store        *store.Store
	PollInterval time.Duration
	LeaseTTL     time.Duration
}

// Authorize blocks until a durable decision is available while keeping the
// worker's lease alive. Its bool is true only for an approved grant.
func (broker Broker) Authorize(ctx context.Context, claim store.Claim, request Request) (bool, error) {
	if broker.Store == nil {
		return false, errors.New("approval store is required")
	}
	if err := request.Validate(); err != nil {
		return false, err
	}
	grants, err := broker.Store.ListApprovals(ctx, claim.Run.ID)
	if err != nil {
		return false, err
	}
	for _, grant := range grants {
		if request.CoveredBy(grant) {
			return true, nil
		}
	}
	if broker.PollInterval <= 0 {
		broker.PollInterval = 250 * time.Millisecond
	}
	if broker.LeaseTTL <= 0 {
		broker.LeaseTTL = 30 * time.Second
	}
	id, err := approvalID()
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	approval, created, err := broker.Store.RequestApproval(ctx, claim, store.Approval{
		ID: id, Kind: request.Kind, Summary: request.Summary, Scope: request.Scope,
	}, now)
	if err != nil {
		return false, err
	}
	if !created && approval.State == store.ApprovalApproved {
		return true, nil
	}
	ticker := time.NewTicker(broker.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, broker.cancelPending(ctx.Err(), approval.ID)
		case now := <-ticker.C:
			current, err := broker.Store.GetApproval(ctx, approval.ID)
			if err != nil {
				if ctx.Err() != nil {
					return false, broker.cancelPending(ctx.Err(), approval.ID)
				}
				return false, err
			}
			switch current.State {
			case store.ApprovalApproved:
				return true, nil
			case store.ApprovalDenied, store.ApprovalCancelled, store.ApprovalExpired:
				return false, nil
			}
			if ctx.Err() != nil {
				return false, broker.cancelPending(ctx.Err(), approval.ID)
			}
			if err := broker.Store.HeartbeatLease(ctx, claim, now.UTC(), broker.LeaseTTL); err != nil {
				if ctx.Err() != nil {
					return false, broker.cancelPending(ctx.Err(), approval.ID)
				}
				return false, fmt.Errorf("heartbeat while awaiting approval: %w", err)
			}
		}
	}
}

func (broker Broker) cancelPending(cause error, approvalID string) error {
	cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := broker.Store.CancelApproval(cancelCtx, approvalID, "run cancelled", time.Now().UTC()); err != nil {
		return errors.Join(cause, fmt.Errorf("cancel pending approval: %w", err))
	}
	return cause
}

func decodeScope(data json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid approval scope: %w", err)
	}
	return nil
}

func approvalID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("create approval ID: %w", err)
	}
	return "apr_" + hex.EncodeToString(value), nil
}
