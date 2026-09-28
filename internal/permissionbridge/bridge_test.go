package permissionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHookAllowsApprovedCamelCaseRequest(t *testing.T) {
	withGateway(t, true, func() {
		var output bytes.Buffer
		err := RunHook(context.Background(), strings.NewReader(`{"toolName":"edit","toolInput":{"file_path":"item.go"}}`), &output)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if json.Unmarshal(output.Bytes(), &result) != nil || result["behavior"] != "allow" {
			t.Fatalf("result = %s", output.String())
		}
	})
}

func TestHookFailuresEmitExplicitDeny(t *testing.T) {
	t.Setenv(EndpointEnv, "http://example.com/v1/authorize")
	t.Setenv(TokenEnv, "secret")
	var output bytes.Buffer
	if err := RunHook(context.Background(), strings.NewReader(`{"toolName":"bash","toolInput":{"command":"git push"}}`), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"behavior":"deny"`) || strings.Contains(output.String(), "git push") {
		t.Fatalf("fail-closed output = %s", output.String())
	}
}

func TestMCPPermissionToolReturnsClaudeDecisionContract(t *testing.T) {
	withGateway(t, true, func() {
		input := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"approve","arguments":{"tool_name":"Write","input":{"file_path":"result.txt"}}}}`,
		}, "\n") + "\n"
		var output bytes.Buffer
		if err := RunMCP(context.Background(), strings.NewReader(input), &output); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(output.String()), "\n")
		if len(lines) != 3 || !strings.Contains(lines[1], `"name":"approve"`) ||
			!strings.Contains(lines[2], `\"behavior\":\"allow\"`) || !strings.Contains(lines[2], `\"updatedInput\"`) {
			t.Fatalf("MCP output = %s", output.String())
		}
	})
}

func withGateway(t *testing.T, approved bool, run func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/authorize" || request.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]bool{"approved": approved})
	}))
	defer server.Close()
	t.Setenv(EndpointEnv, server.URL+"/v1/authorize")
	t.Setenv(TokenEnv, "test-token")
	run()
}
