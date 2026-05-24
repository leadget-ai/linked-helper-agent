package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	requestTimeout = 30 * time.Second
	// Cap server responses so a misbehaving backend can't blow agent memory.
	maxResponseBytes = 1 << 20 // 1 MB

	maxRetries    = 3
	baseBackoff   = 1 * time.Second
	maxBackoff    = 30 * time.Second
	backoffFactor = 2.0
)

// Client is the HTTP-layer companion to models.go. One instance is shared by
// the whole agent process: keep-alives are on so per-account requests can
// reuse the same TCP connection.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New constructs a Client. `baseURL` may include a trailing slash; it's
// normalized internally.
func New(baseURL, token string, disableKeepAlive bool) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				DisableKeepAlives:   disableKeepAlive,
				TLSHandshakeTimeout: 10 * time.Second,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Bootstrap announces this agent install to the server and learns the cadence
// the server wants the agent to run at.
func (c *Client) Bootstrap(ctx context.Context, req *BootstrapRequest) (*BootstrapResponse, error) {
	var out BootstrapResponse
	if err := c.do(ctx, http.MethodPost, "/v1/integrations/linkedin-helper/agent/bootstrap", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Report submits a delta of campaign metadata + funnel counters for one LH
// account. The response carries the refreshed known-state snapshot.
func (c *Client) Report(
	ctx context.Context,
	accountID int,
	req *AccountReportRequest,
) (*AccountReportResponse, error) {
	var out AccountReportResponse
	path := fmt.Sprintf("/v1/integrations/linkedin-helper/agent/accounts/%d/report", accountID)
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// do performs the request with retry/backoff/jitter. 4xx other than 429 fail
// immediately — the server is telling us the call is malformed.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody []byte
	if body != nil {
		marshaled, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = marshaled
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(float64(baseBackoff) * math.Pow(backoffFactor, float64(attempt-1)))
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			// ±25% jitter prevents synchronized retries from many agents.
			jitter := 0.75 + rand.Float64()*0.5
			backoff = time.Duration(float64(backoff) * jitter)
			log.WithFields(log.Fields{"attempt": attempt + 1, "backoff": backoff, "path": path}).
				Warn("retrying request")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := c.doOnce(ctx, method, path, reqBody, out)
		if err == nil {
			return nil
		}
		lastErr = err

		if isClientError(err) {
			return err
		}
	}

	return fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

func (c *Client) doOnce(ctx context.Context, method, path string, reqBody []byte, out any) error {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	var body io.Reader
	if reqBody != nil {
		body = bytes.NewReader(reqBody)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// Always drain through a LimitReader so an oversized response can't
	// exhaust memory and so keep-alive can reclaim the connection.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.WithFields(log.Fields{"status": resp.StatusCode, "path": path, "body": truncate(respBody, 512)}).
			Warn("backend returned non-2xx")
		return &apiError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("backend error: status %d", e.StatusCode)
}

func isClientError(err error) bool {
	if ae, ok := err.(*apiError); ok {
		return ae.StatusCode >= 400 && ae.StatusCode < 500 && ae.StatusCode != 429
	}
	return false
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
