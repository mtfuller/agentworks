package approval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GatewayClient struct {
	Endpoint string
	Token    string
	Client   *http.Client
}

func (client GatewayClient) Authorize(ctx context.Context, request ToolRequest) (bool, error) {
	endpoint, err := url.Parse(client.Endpoint)
	if err != nil || endpoint.Scheme != "http" || !isLoopbackHost(endpoint.Hostname()) || endpoint.Path != "/v1/authorize" {
		return false, errors.New("permission endpoint must be the AgentWorks loopback gateway")
	}
	if strings.TrimSpace(client.Token) == "" {
		return false, errors.New("permission token is required")
	}
	data, err := json.Marshal(request)
	if err != nil {
		return false, err
	}
	httpClient := client.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 24 * time.Hour}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.Endpoint, bytes.NewReader(data))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+client.Token)
	req.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("contact approval gateway: %w", err)
	}
	defer response.Body.Close()
	var result struct {
		Approved bool   `json:"approved"`
		Error    string `json:"error"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&result); err != nil {
		return false, errors.New("approval gateway returned an invalid response")
	}
	if response.StatusCode != http.StatusOK {
		if result.Error == "" {
			result.Error = "approval denied"
		}
		return false, errors.New(result.Error)
	}
	return result.Approved, nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1"
}
