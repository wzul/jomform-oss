package chip

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BaseURLProduction is CHIP Collect's API root (live & test keys share it).
const BaseURLProduction = "https://gate.chip-in.asia/api/v1"

// Client talks to the CHIP Collect API. The merchant secret key is provided
// per call (not stored on the client) since JomForm serves many merchants.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a client. Empty baseURL defaults to production.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = BaseURLProduction
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// errorShape matches CHIP's error envelope {"message": "...", "code": "..."}.
type apiError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

func (e *apiError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("chip: %s (%s)", e.Message, e.Code)
	}
	return "chip: " + e.Message
}

// ErrorWithStatus carries the HTTP status from a failed CHIP call.
type ErrorWithStatus struct {
	Status int
	Err    error
}

func (e *ErrorWithStatus) Error() string { return e.Err.Error() }
func (e *ErrorWithStatus) Unwrap() error { return e.Err }

// do executes an authenticated request; secretKey is the merchant's key.
func (c *Client) do(ctx context.Context, secretKey, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secretKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("chip request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		apiErr := &apiError{Message: fmt.Sprintf("http %d", resp.StatusCode)}
		var parsed apiError
		if json.Unmarshal(raw, &parsed) == nil && parsed.Message != "" {
			apiErr = &parsed
		}
		return &ErrorWithStatus{Status: resp.StatusCode, Err: apiErr}
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
