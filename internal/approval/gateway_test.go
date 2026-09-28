package approval

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

func TestGatewayRoundTripAndCredentialRevocation(t *testing.T) {
	runtimeStore, claim := approvalFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gateway, err := StartGateway(ctx, runtimeStore, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := gateway.Open(ctx, claim, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := GatewayClient{Endpoint: credentials.Endpoint, Token: credentials.Token}
	done := make(chan struct {
		approved bool
		err      error
	}, 1)
	go func() {
		approved, err := client.Authorize(context.Background(), ToolRequest{
			ToolName: "Bash", Input: json.RawMessage(`{"command":"git status --short"}`),
		})
		done <- struct {
			approved bool
			err      error
		}{approved, err}
	}()
	pending := waitForBrokerApproval(t, runtimeStore, claim.Run.ID)
	if pending.Kind != "command.execute" {
		t.Fatalf("approval kind = %q", pending.Kind)
	}
	if _, err := runtimeStore.DecideApproval(context.Background(), pending.ID, store.ApprovalApproved, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil || !result.approved {
		t.Fatalf("approved=%v err=%v", result.approved, result.err)
	}
	credentials.Close()
	if _, err := client.Authorize(context.Background(), ToolRequest{ToolName: "Bash", Input: json.RawMessage(`{"command":"git status"}`)}); err == nil {
		t.Fatal("revoked credential was accepted")
	}
}

func TestGatewayRejectsTrailingJSON(t *testing.T) {
	runtimeStore, claim := approvalFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gateway, err := StartGateway(ctx, runtimeStore, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := gateway.Open(ctx, claim, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer credentials.Close()
	body := `{"tool_name":"Bash","input":{"command":"git status"}} {"extra":true}`
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, credentials.Endpoint, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+credentials.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
}

func TestGatewayFailsClosedForBadTokenAndReadonlyRun(t *testing.T) {
	runtimeStore, claim := approvalFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gateway, err := StartGateway(ctx, runtimeStore, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := gateway.Open(ctx, claim, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer credentials.Close()
	request := ToolRequest{ToolName: "Edit", Input: json.RawMessage(`{"file_path":"result.txt"}`)}
	if _, err := (GatewayClient{Endpoint: credentials.Endpoint, Token: "wrong"}).Authorize(ctx, request); err == nil {
		t.Fatal("bad token was accepted")
	}
	claim.Run.Permission = store.PermissionReadonly
	readonly, err := gateway.Open(ctx, claim, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	if _, err := (GatewayClient{Endpoint: readonly.Endpoint, Token: readonly.Token}).Authorize(ctx, request); err == nil {
		t.Fatal("readonly mutation was accepted")
	}
}
