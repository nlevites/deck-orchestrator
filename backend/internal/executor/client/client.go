// Package client is the executor's HTTP client to the orchestrator.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type Step struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type Dispatch struct {
	AttemptID string `json:"attempt_id"`
	RunID     string `json:"run_id"`
	JobID     string `json:"job_id"`
	Steps     []Step `json:"steps"`
}

func (c *Client) Poll(ctx context.Context, deckID string) (Dispatch, bool, error) {
	u := fmt.Sprintf("%s/executor/poll?deck_id=%s", c.BaseURL, url.QueryEscape(deckID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Dispatch{}, false, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Dispatch{}, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return Dispatch{}, false, nil
	case http.StatusOK:
		var d Dispatch
		if dErr := json.NewDecoder(resp.Body).Decode(&d); dErr != nil {
			return Dispatch{}, false, fmt.Errorf("decode dispatch: %w", dErr)
		}
		return d, true, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Dispatch{}, false, fmt.Errorf("poll: status %d: %s", resp.StatusCode, body)
	}
}

// ErrFleetRejected means 404 (not in fleet) or 410 (decommissioned).
// Permanent config error — executor should exit without retry.
var ErrFleetRejected = fmt.Errorf("heartbeat: orchestrator rejected this deck_id; fleet membership error")

func (c *Client) Heartbeat(ctx context.Context, deckID, endpointURL, currentAttemptID string) error {
	body := map[string]any{
		"deck_id":      deckID,
		"endpoint_url": endpointURL,
	}
	if currentAttemptID != "" {
		body["current_attempt_id"] = currentAttemptID
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/executor/heartbeat", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%w: status %d: %s", ErrFleetRejected, resp.StatusCode, b)
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("heartbeat: %d: %s", resp.StatusCode, b)
	}
	return nil
}

// PostEvent delivers one outbox event. delivered=true = stop retrying; false = retry.
//
// 2xx = delivered. 5xx and non-404 4xx = retry. 404 (UNKNOWN_ATTEMPT) is retryable:
// treating it as terminal silently deleted outbox rows (C3 fix). Executor stays loud
// until orchestrator converges or operator intervenes.
func (c *Client) PostEvent(ctx context.Context, attemptID, kind string, payload json.RawMessage, occurredAt time.Time) (bool, error) {
	body := map[string]any{
		"attempt_id":  attemptID,
		"kind":        kind,
		"occurred_at": occurredAt.UTC().Format(time.RFC3339Nano),
	}
	if len(payload) > 0 && string(payload) != "{}" {
		body["payload"] = json.RawMessage(payload)
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/executor/events", bytes.NewReader(buf))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, nil
	case resp.StatusCode == http.StatusNotFound:
		return false, fmt.Errorf("unknown attempt %s: %s", attemptID, respBody)
	default:
		return false, fmt.Errorf("post event: %d: %s", resp.StatusCode, respBody)
	}
}
