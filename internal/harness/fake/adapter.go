// Package fake provides an explicit development-only harness for exercising
// the complete local runtime without contacting a model vendor.
package fake

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/approval"
	awprocess "github.com/mtfuller/agentworks/internal/process"
	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/worker"
)

type Adapter struct {
	Executable string
}

type eventData struct {
	Prompt string `json:"prompt"`
}

func (adapter Adapter) ApprovalRequest(_ context.Context, request worker.Request) (*approval.Request, error) {
	var data eventData
	if err := json.Unmarshal(request.Event.Data, &data); err != nil {
		return nil, errors.New("decode fake harness request")
	}
	prompt := strings.ToLower(data.Prompt)
	if strings.Contains(prompt, "[fake:write]") {
		if request.Run.Permission == store.PermissionReadonly {
			return nil, errors.New("fake write requested by readonly run")
		}
		return &approval.Request{
			Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"."}`),
			Summary: "Allow edits anywhere in this workspace for this run",
		}, nil
	}
	if strings.Contains(prompt, "[fake:command]") {
		if request.Run.Permission == store.PermissionReadonly {
			return nil, errors.New("fake command requested by readonly run")
		}
		return &approval.Request{
			Kind: "command.execute", Scope: json.RawMessage(`{"executable":"agentworks","subcommand":["__fake-harness"]}`),
			Summary: "Allow this AgentWorks fake-harness subcommand for this run",
		}, nil
	}
	return nil, nil
}

func (adapter Adapter) Invocation(_ context.Context, request worker.Request) (awprocess.Spec, error) {
	if strings.TrimSpace(adapter.Executable) == "" {
		return awprocess.Spec{}, errors.New("fake harness executable is required")
	}
	var data eventData
	if err := json.Unmarshal(request.Event.Data, &data); err != nil {
		return awprocess.Spec{}, errors.New("decode fake harness request")
	}
	mode := modeFor(data.Prompt)
	if strings.Contains(strings.ToLower(data.Prompt), "[fake:retry-once]") {
		if request.Attempt.Number == 1 {
			mode = "fail"
		} else {
			mode = "success"
		}
	}
	if mode == "write" && request.Run.Permission == store.PermissionReadonly {
		return awprocess.Spec{}, errors.New("fake write requested by readonly run")
	}
	return awprocess.Spec{
		Executable:  adapter.Executable,
		Args:        []string{"__fake-harness", "--mode", mode},
		Stdin:       strings.NewReader(data.Prompt),
		GracePeriod: 2 * time.Second,
	}, nil
}

func (adapter Adapter) RetryDecision(_ context.Context, request worker.Request, _ awprocess.Result, processErr error) worker.RetryDecision {
	var data eventData
	if processErr == nil || json.Unmarshal(request.Event.Data, &data) != nil ||
		!strings.Contains(strings.ToLower(data.Prompt), "[fake:retry-once]") || request.Attempt.Number != 1 {
		return worker.RetryDecision{}
	}
	return worker.RetryDecision{
		Retry: true, Delay: 50 * time.Millisecond, Reason: "fake transient failure",
		// This development-only mode performs no writes on either attempt.
		SafeForWrite: true,
	}
}

func modeFor(prompt string) string {
	for _, candidate := range []string{"fail", "hang", "write", "slow", "descendants"} {
		if strings.Contains(strings.ToLower(prompt), "[fake:"+candidate+"]") {
			return candidate
		}
	}
	return "success"
}
