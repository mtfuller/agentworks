// Package permissionbridge adapts vendor permission protocols to the
// run-scoped AgentWorks approval gateway.
package permissionbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mtfuller/agentworks/internal/approval"
)

const (
	EndpointEnv = "AGENTWORKS_PERMISSION_ENDPOINT"
	TokenEnv    = "AGENTWORKS_PERMISSION_TOKEN"
	maxMessage  = 1 << 20
)

type hookInput struct {
	ToolName             string          `json:"toolName"`
	ToolInput            json.RawMessage `json:"toolInput"`
	RequestSandboxBypass bool            `json:"requestSandboxBypass"`
	ToolNameSnake        string          `json:"tool_name"`
	ToolInputSnake       json.RawMessage `json:"tool_input"`
	SandboxBypassSnake   bool            `json:"request_sandbox_bypass"`
}

// RunHook handles one GitHub Copilot permissionRequest hook. It always emits
// an explicit allow or deny decision so transport failures never become a
// fail-open hook error.
func RunHook(ctx context.Context, input io.Reader, output io.Writer) error {
	decision := map[string]any{"behavior": "deny", "message": "Denied by AgentWorks"}
	data, err := io.ReadAll(io.LimitReader(input, maxMessage+1))
	if err == nil && len(data) <= maxMessage {
		var payload hookInput
		if json.Unmarshal(data, &payload) == nil {
			name, toolInput, bypass := payload.ToolName, payload.ToolInput, payload.RequestSandboxBypass
			if name == "" {
				name, toolInput, bypass = payload.ToolNameSnake, payload.ToolInputSnake, payload.SandboxBypassSnake
			}
			if len(toolInput) == 0 {
				toolInput = json.RawMessage(`{}`)
			}
			approved, authorizeErr := clientFromEnvironment().Authorize(ctx, approval.ToolRequest{
				ToolName: name, Input: toolInput, SandboxBypass: bypass,
			})
			if authorizeErr == nil && approved {
				decision = map[string]any{"behavior": "allow"}
			}
		}
	}
	return json.NewEncoder(output).Encode(decision)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type permissionArguments struct {
	ToolName              string          `json:"tool_name"`
	Input                 json.RawMessage `json:"input"`
	PermissionSuggestions json.RawMessage `json:"permission_suggestions,omitempty"`
}

// RunMCP serves the minimal stdio MCP surface Claude Code needs for
// --permission-prompt-tool. MCP stdio messages are newline-delimited JSON.
func RunMCP(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), maxMessage)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		var request rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		result, rpcErr := handleRPC(ctx, request)
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		if rpcErr != nil {
			response["error"] = map[string]any{"code": -32602, "message": rpcErr.Error()}
		} else {
			response["result"] = result
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handleRPC(ctx context.Context, request rpcRequest) (any, error) {
	switch request.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]string{"name": "agentworks-permissions", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		}, nil
	case "tools/list":
		return map[string]any{"tools": []any{map[string]any{
			"name": "approve", "description": "Request a bounded, run-only AgentWorks permission decision.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tool_name":              map[string]string{"type": "string"},
					"input":                  map[string]string{"type": "object"},
					"permission_suggestions": map[string]string{"type": "array"},
				},
				"required": []string{"tool_name", "input"},
			},
		}}}, nil
	case "tools/call":
		var call toolCallParams
		if json.Unmarshal(request.Params, &call) != nil || call.Name != "approve" {
			return nil, errors.New("unknown permission tool")
		}
		var arguments permissionArguments
		if json.Unmarshal(call.Arguments, &arguments) != nil || arguments.ToolName == "" || len(arguments.Input) == 0 {
			return nil, errors.New("invalid permission request")
		}
		approved, err := clientFromEnvironment().Authorize(ctx, approval.ToolRequest{ToolName: arguments.ToolName, Input: arguments.Input})
		decision := map[string]any{"behavior": "deny", "message": "Denied by AgentWorks"}
		if err == nil && approved {
			decision = map[string]any{"behavior": "allow", "updatedInput": arguments.Input}
		}
		text, _ := json.Marshal(decision)
		return map[string]any{"content": []any{map[string]string{"type": "text", "text": string(text)}}}, nil
	default:
		return nil, fmt.Errorf("method %q is not supported", request.Method)
	}
}

func clientFromEnvironment() approval.GatewayClient {
	return approval.GatewayClient{Endpoint: strings.TrimSpace(os.Getenv(EndpointEnv)), Token: os.Getenv(TokenEnv)}
}
