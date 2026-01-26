package chatplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the main SDK client for the real-time chat platform
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
	userID     string

	// Service clients
	Chat     *ChatClient
	Presence *PresenceClient
	Call     *CallClient
}

// Config holds configuration for the SDK client
type Config struct {
	BaseURL    string
	APIKey     string
	UserID     string
	Timeout    time.Duration
	MaxRetries int
}

// NewClient creates a new SDK client instance
func NewClient(config Config) *Client {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}

	httpClient := &http.Client{
		Timeout: config.Timeout,
	}

	client := &Client{
		baseURL:    config.BaseURL,
		httpClient: httpClient,
		apiKey:     config.APIKey,
		userID:     config.UserID,
	}

	// Initialize service clients
	client.Chat = &ChatClient{client: client}
	client.Presence = &PresenceClient{client: client}
	client.Call = &CallClient{client: client}

	return client
}

// doRequest performs an HTTP request with retry logic
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	fullURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			return &APIError{
				StatusCode: resp.StatusCode,
				Message:    errResp.Error,
				Details:    errResp.Message,
			}
		}
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
		}
	}

	// Parse response
	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

// ErrorResponse represents an error response from the API
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Details string `json:"details"`
}

// APIError represents an API error
type APIError struct {
	StatusCode int
	Message    string
	Details    string
}

func (e *APIError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("API error (status %d): %s - %s", e.StatusCode, e.Message, e.Details)
	}
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}
