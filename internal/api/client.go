package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// APIError carries the HTTP status and body from a failed Nutanix API call.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error %d: %s", e.StatusCode, e.Message)
}

func (e *APIError) IsNotFound() bool     { return e.StatusCode == http.StatusNotFound }
func (e *APIError) IsUnauthorized() bool { return e.StatusCode == http.StatusUnauthorized }
func (e *APIError) IsServerError() bool  { return e.StatusCode >= 500 }
func (e *APIError) IsClientError() bool  { return e.StatusCode >= 400 && e.StatusCode < 500 }

// ClassifyStatus maps an HTTP status code to a simple health category.
func ClassifyStatus(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "healthy"
	case code >= 300 && code < 400:
		return "warning"
	default:
		return "failure"
	}
}

// BaseClient is a shared HTTP client for raw REST calls to Nutanix APIs.
type BaseClient struct {
	Host     string
	Username string
	Password string
	HTTP     *http.Client
}

// NewBaseClient creates a BaseClient with the given host, credentials, and TLS settings.
func NewBaseClient(host, username, password string, insecure bool, timeout time.Duration) *BaseClient {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure}, //nolint:gosec
	}
	return &BaseClient{
		Host:     host,
		Username: username,
		Password: password,
		HTTP: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
	}
}

// DoJSON executes an HTTP request and decodes the JSON response into result.
// If body is non-nil it is JSON-encoded as the request body.
func (c *BaseClient) DoJSON(method, path string, body, result interface{}) (int, error) {
	url := fmt.Sprintf("https://%s:9440%s", c.Host, path)

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return resp.StatusCode, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(data),
		}
	}

	if result != nil && len(data) > 0 {
		if err := json.Unmarshal(data, result); err != nil {
			return resp.StatusCode, fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return resp.StatusCode, nil
}
