package fake

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	awprocess "github.com/mtfuller/agentworks/internal/process"
	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/worker"
)

func TestAdapterBuildsStructuredInvocation(t *testing.T) {
	adapter := Adapter{Executable: "/path/to/agentworks"}
	request := worker.Request{
		Run:   store.Run{Permission: store.PermissionReadwrite},
		Event: store.Event{Data: json.RawMessage(`{"prompt":"try this [fake:write]"}`)},
	}
	specification, err := adapter.Invocation(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if specification.Executable != adapter.Executable || strings.Join(specification.Args, " ") != "__fake-harness --mode write" {
		t.Fatalf("specification = %#v", specification)
	}
}

func TestAdapterRetryOnceUsesFreshAttempt(t *testing.T) {
	adapter := Adapter{Executable: "agentworks"}
	request := worker.Request{
		Run:     store.Run{Permission: store.PermissionReadwrite},
		Event:   store.Event{Data: promptData("[fake:retry-once]")},
		Attempt: store.Attempt{Number: 1},
	}
	first, err := adapter.Invocation(context.Background(), request)
	if err != nil || first.Args[len(first.Args)-1] != "fail" {
		t.Fatalf("first invocation = %#v err=%v", first, err)
	}
	decision := adapter.RetryDecision(context.Background(), request, awprocess.Result{ExitCode: 7}, context.Canceled)
	if !decision.Retry || !decision.SafeForWrite || decision.Delay <= 0 {
		t.Fatalf("retry decision = %#v", decision)
	}
	request.Attempt.Number = 2
	second, err := adapter.Invocation(context.Background(), request)
	if err != nil || second.Args[len(second.Args)-1] != "success" {
		t.Fatalf("second invocation = %#v err=%v", second, err)
	}
	if decision := adapter.RetryDecision(context.Background(), request, awprocess.Result{}, context.Canceled); decision.Retry {
		t.Fatalf("second attempt retried: %#v", decision)
	}
}

func TestAdapterRejectsWriteForReadonlyRun(t *testing.T) {
	_, err := (Adapter{Executable: "/path/to/agentworks"}).Invocation(context.Background(), worker.Request{
		Run:   store.Run{Permission: store.PermissionReadonly},
		Event: store.Event{Data: json.RawMessage(`{"prompt":"[fake:write]"}`)},
	})
	if err == nil || !strings.Contains(err.Error(), "readonly") {
		t.Fatalf("error = %v", err)
	}
}

func TestAdapterApprovalRequestsAreBoundedByPermission(t *testing.T) {
	adapter := Adapter{Executable: "/path/to/agentworks"}
	for _, test := range []struct {
		name       string
		prompt     string
		permission store.Permission
		wantKind   string
		wantError  bool
	}{
		{"write", "[fake:write]", store.PermissionReadwrite, "filesystem.write", false},
		{"command", "[fake:command]", store.PermissionReadwrite, "command.execute", false},
		{"none", "ordinary request", store.PermissionReadonly, "", false},
		{"readonly write", "[fake:write]", store.PermissionReadonly, "", true},
		{"readonly command", "[fake:command]", store.PermissionReadonly, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := adapter.ApprovalRequest(context.Background(), worker.Request{
				Run: store.Run{Permission: test.permission}, Event: store.Event{Data: promptData(test.prompt)},
			})
			if (err != nil) != test.wantError {
				t.Fatalf("request=%#v err=%v", request, err)
			}
			if !test.wantError && test.wantKind == "" && request != nil {
				t.Fatalf("request = %#v, want nil", request)
			}
			if request != nil && request.Kind != test.wantKind {
				t.Fatalf("kind = %q, want %q", request.Kind, test.wantKind)
			}
		})
	}
	if _, err := adapter.ApprovalRequest(context.Background(), worker.Request{Event: store.Event{Data: json.RawMessage(`{`)}}); err == nil {
		t.Fatal("malformed event accepted")
	}
}

func TestAdapterInvocationValidationAndModes(t *testing.T) {
	if _, err := (Adapter{}).Invocation(context.Background(), worker.Request{Event: store.Event{Data: promptData("hello")}}); err == nil {
		t.Fatal("empty executable accepted")
	}
	if _, err := (Adapter{Executable: "agentworks"}).Invocation(context.Background(), worker.Request{Event: store.Event{Data: json.RawMessage(`{`)}}); err == nil {
		t.Fatal("malformed event accepted")
	}
	for prompt, want := range map[string]string{
		"hello": "success", "[fake:fail]": "fail", "[fake:hang]": "hang", "[fake:slow]": "slow",
		"[fake:descendants]": "descendants",
	} {
		if got := modeFor(prompt); got != want {
			t.Errorf("modeFor(%q) = %q, want %q", prompt, got, want)
		}
	}
}

func promptData(prompt string) json.RawMessage {
	data, _ := json.Marshal(map[string]string{"prompt": prompt})
	return data
}
