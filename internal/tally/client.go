package tally

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client sends one request at a time to Tally, with a timeout and a pause between calls,
// so Tally is never flooded.
type Client struct {
	URL     string        // e.g. http://localhost:9000
	Timeout time.Duration // per request
	Pause   time.Duration // gap after each request
	http    *http.Client
	mu      sync.Mutex
	last    time.Time
}

// NewClient returns a client with safe defaults (60 s timeout, 1 s pause).
func NewClient(host string, port int) *Client {
	return &Client{URL: fmt.Sprintf("http://%s:%d", host, port), Timeout: 60 * time.Second,
		Pause: time.Second, http: &http.Client{}}
}

// Do posts a request and calls handle with the (sanitised) streaming response body.
// The body is never fully loaded into memory.
func (c *Client) Do(ctx context.Context, body string, handle func(io.Reader) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := c.Pause - time.Since(c.last); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() { c.last = time.Now() }()

	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tally not reachable at %s: %w", c.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tally returned HTTP %d", resp.StatusCode)
	}
	return handle(NewSanitizer(resp.Body))
}

// Ping checks Tally answers on the configured port.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tally not reachable at %s: %w", c.URL, err)
	}
	resp.Body.Close()
	return nil
}
