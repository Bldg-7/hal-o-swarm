package halctl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPClient wraps HTTP operations for supervisor API calls
type HTTPClient struct {
	baseURL   string
	authToken string
	client    *http.Client
}

// NewHTTPClient creates a new HTTP client for supervisor API
func NewHTTPClient(baseURL, authToken string) *HTTPClient {
	return &HTTPClient{
		baseURL:   baseURL,
		authToken: authToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// APIResponse wraps the standard API response format
type APIResponse struct {
	Data interface{} `json:"data"`
	Meta *APIMeta    `json:"meta,omitempty"`
}

// APIMeta contains metadata about the response
type APIMeta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// APIError represents an API error response
type APIError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// Get performs a GET request to the API
func (c *HTTPClient) Get(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to supervisor at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp.StatusCode, body)
	}

	return body, nil
}

// Post performs a POST request to the API
func (c *HTTPClient) Post(path string, payload interface{}) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeader(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to supervisor at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp.StatusCode, body)
	}

	return body, nil
}

// setAuthHeader adds the Bearer token to the request
func (c *HTTPClient) setAuthHeader(req *http.Request) {
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

// parseError parses HTTP error responses
func (c *HTTPClient) parseError(statusCode int, body []byte) error {
	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err != nil {
		// Fallback to generic error if JSON parsing fails
		switch statusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("authentication failed. Check your auth token")
		case http.StatusNotFound:
			return fmt.Errorf("resource not found")
		case http.StatusServiceUnavailable:
			return fmt.Errorf("supervisor service unavailable")
		default:
			return fmt.Errorf("server error (status %d)", statusCode)
		}
	}

	switch statusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("authentication failed. Check your auth token")
	case http.StatusNotFound:
		return fmt.Errorf("resource not found: %s", apiErr.Error)
	case http.StatusBadRequest:
		return fmt.Errorf("invalid request: %s", apiErr.Error)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("supervisor service unavailable: %s", apiErr.Error)
	default:
		return fmt.Errorf("server error: %s", apiErr.Error)
	}
}

// ParseResponse parses a JSON response into the target struct
func ParseResponse(body []byte, target interface{}) error {
	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Re-marshal the data field and unmarshal into target
	dataBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return fmt.Errorf("failed to process response data: %w", err)
	}

	if err := json.Unmarshal(dataBytes, target); err != nil {
		return fmt.Errorf("failed to unmarshal response data: %w", err)
	}

	return nil
}
