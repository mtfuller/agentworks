package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type apiClient struct {
	base    *url.URL
	client  *http.Client
	headers map[string]string
}

type HTTPError struct {
	Host   string
	Status int
}

func (err *HTTPError) Error() string {
	return fmt.Sprintf("connector request to %s returned HTTP %d", err.Host, err.Status)
}

func newAPIClient(rawBase string, client *http.Client, headers map[string]string) (*apiClient, error) {
	base, err := url.Parse(strings.TrimRight(rawBase, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, errors.New("connector API URL is invalid")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	clone := *client
	previousRedirect := clone.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 0 && !strings.EqualFold(request.URL.Host, via[0].URL.Host) {
			return errors.New("connector refused a cross-host redirect")
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		if len(via) >= 5 {
			return errors.New("connector redirect limit exceeded")
		}
		return nil
	}
	return &apiClient{base: base, client: &clone, headers: headers}, nil
}

func (client *apiClient) json(ctx context.Context, method, path string, query url.Values, body io.Reader, output any) (http.Header, error) {
	target := *client.base
	target.Path = strings.TrimRight(client.base.Path, "/") + "/" + strings.TrimLeft(path, "/")
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, errors.New("build connector request")
	}
	for key, value := range client.headers {
		request.Header.Set(key, value)
	}
	if request.Header.Get("Accept") == "" {
		request.Header.Set("Accept", "application/json")
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("connector request to %s failed", target.Host)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0" {
		return response.Header, &RateLimitError{ResetAt: rateLimitReset(response.Header, time.Now().UTC())}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.Header, &HTTPError{Host: target.Host, Status: response.StatusCode}
	}
	reader := io.LimitReader(response.Body, 10<<20)
	decoder := json.NewDecoder(reader)
	if output != nil {
		if err := decoder.Decode(output); err != nil {
			return response.Header, errors.New("decode connector response")
		}
	}
	return response.Header, nil
}

func rateLimitReset(header http.Header, now time.Time) time.Time {
	if value, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil && value > 0 {
		return time.Unix(value, 0).UTC()
	}
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if value, err := http.ParseTime(header.Get("Retry-After")); err == nil {
		return value.UTC()
	}
	return now.Add(time.Minute)
}
