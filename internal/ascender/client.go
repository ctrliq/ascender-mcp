// Package ascender is a thin HTTP client for the Ascender REST API.
//
// Ascender is CIQ's distribution of AWX and keeps AWX's REST surface
// under /api/v2/.
package ascender

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ctrliq/ascender-mcp/internal/metrics"
)

// Client is safe for concurrent use. It holds no credentials of its
// own; use WithToken to get a copy that authenticates as one caller.
type Client struct {
	host  string
	token string
	http  *http.Client
}

// New returns a Client for the Ascender API rooted at host.
func New(host string, insecureSkipVerify bool) *Client {
	var base http.RoundTripper
	if insecureSkipVerify {
		base = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	return &Client{
		host: strings.TrimRight(host, "/"),
		http: &http.Client{Timeout: 60 * time.Second, Transport: metrics.Transport(base)},
	}
}

// WithToken returns a copy of c that sends token as the bearer on
// every request.
func (c *Client) WithToken(token string) *Client {
	cp := *c
	cp.token = token
	return &cp
}

// Error is what Ascender's error response becomes after scrubbing.
// Detail carries the `detail` string or first field error, capped.
type Error struct {
	Status  int
	Message string
	Detail  string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("[%d] %s: %s", e.Status, e.Message, e.Detail)
	}
	return fmt.Sprintf("[%d] %s", e.Status, e.Message)
}

// Page is one collected page set from a paginated list endpoint.
// Total is Ascender's full count; Truncated means the page cap
// stopped collection before the end.
type Page struct {
	Results   []map[string]any `json:"results"`
	Total     int              `json:"total"`
	Truncated bool             `json:"truncated"`
}

// Get issues a GET to path and decodes the JSON object response.
func (c *Client) Get(ctx context.Context, path string, params url.Values) (map[string]any, error) {
	return c.do(ctx, http.MethodGet, path, params, nil)
}

// Post issues a POST with a JSON-encoded body and decodes the response.
func (c *Client) Post(ctx context.Context, path string, body any) (map[string]any, error) {
	return c.doBody(ctx, http.MethodPost, path, body)
}

// Patch issues a PATCH with a JSON-encoded body and decodes the response.
func (c *Client) Patch(ctx context.Context, path string, body any) (map[string]any, error) {
	return c.doBody(ctx, http.MethodPatch, path, body)
}

// Delete removes path. A 404 counts as success so deleting something
// already gone is a no-op.
func (c *Client) Delete(ctx context.Context, path string) error {
	_, err := c.do(ctx, http.MethodDelete, path, nil, nil)
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// Ping hits the unauthenticated /api/v2/ping/ endpoint.
func (c *Client) Ping(ctx context.Context) (map[string]any, error) {
	return c.Get(ctx, "ping", nil)
}

// Me returns the calling user's profile. Ascender wraps /api/v2/me/
// in a paginated envelope; unwrap it here so tools see a flat record.
func (c *Client) Me(ctx context.Context) (map[string]any, error) {
	raw, err := c.Get(ctx, "me", nil)
	if err != nil {
		return nil, err
	}
	results, _ := raw["results"].([]any)
	if len(results) == 0 {
		return nil, &Error{Status: http.StatusNotFound, Message: "no user record returned for this token"}
	}
	user, ok := results[0].(map[string]any)
	if !ok {
		return nil, &Error{Status: http.StatusInternalServerError, Message: "unexpected /me/ response shape"}
	}
	return user, nil
}

// TextCap bounds GetText payloads so job stdout cannot swamp model
// context or proxy memory.
const TextCap = 1 << 20

// GetText fetches a plain-text body (job stdout) and reports whether
// it was cut at TextCap.
func (c *Client) GetText(ctx context.Context, path string, params url.Values) (text string, truncated bool, err error) {
	req, err := c.newRequest(ctx, http.MethodGet, path, params, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", "text/plain")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", false, transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", false, readAPIError(resp)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, TextCap+1))
	if err != nil {
		return "", false, &Error{Status: resp.StatusCode, Message: "read failed"}
	}
	if len(b) > TextCap {
		return string(b[:TextCap]), true, nil
	}
	return string(b), false, nil
}

// List follows pagination for up to five pages. The cap keeps a
// broad query from turning into hundreds of upstream calls.
func (c *Client) List(ctx context.Context, path string, params url.Values) (Page, error) {
	const maxPages = 5
	p := maps.Clone(params)
	if p == nil {
		p = url.Values{}
	}
	var page Page
	for n := 1; n <= maxPages; n++ {
		p.Set("page", strconv.Itoa(n))
		resp, err := c.Get(ctx, path, p)
		if err != nil {
			return Page{}, err
		}
		if n == 1 {
			if count, ok := resp["count"].(float64); ok {
				page.Total = int(count)
			}
		}
		items, _ := resp["results"].([]any)
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				page.Results = append(page.Results, m)
			}
		}
		if resp["next"] == nil {
			return page, nil
		}
	}
	page.Truncated = true
	return page, nil
}

// Resolve fetches a record by numeric ID or exact, case-sensitive
// name. Ascender allows duplicate names across organizations; the
// first visible match wins.
func (c *Client) Resolve(ctx context.Context, endpoint, idOrName string) (map[string]any, error) {
	if id, err := strconv.Atoi(idOrName); err == nil {
		return c.Get(ctx, fmt.Sprintf("%s/%d", endpoint, id), nil)
	}
	resp, err := c.Get(ctx, endpoint, url.Values{"name__exact": {idOrName}})
	if err != nil {
		return nil, err
	}
	results, _ := resp["results"].([]any)
	if len(results) == 0 {
		return nil, &Error{
			Status:  http.StatusNotFound,
			Message: "not found",
			Detail:  fmt.Sprintf("%s %q not found (name match is case-sensitive)", endpoint, idOrName),
		}
	}
	record, _ := results[0].(map[string]any)
	return record, nil
}

// ID extracts the numeric "id" field. JSON numbers decode as float64,
// so this avoids %v rendering large IDs in scientific notation.
func ID(record map[string]any) (int, error) {
	id, ok := record["id"].(float64)
	if !ok {
		return 0, errors.New("missing or non-numeric id field")
	}
	return int(id), nil
}

func (c *Client) doBody(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var buf []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		buf = b
	}
	return c.do(ctx, method, path, nil, buf)
}

func (c *Client) do(ctx context.Context, method, path string, params url.Values, body []byte) (map[string]any, error) {
	req, err := c.newRequest(ctx, method, path, params, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, readAPIError(resp)
	}
	if resp.StatusCode == http.StatusNoContent || resp.ContentLength == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, &Error{Status: resp.StatusCode, Message: "non-JSON response"}
	}
	return out, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, params url.Values, body []byte) (*http.Request, error) {
	u, err := url.JoinPath(c.host, "api", "v2", path)
	if err != nil {
		return nil, fmt.Errorf("bad URL: %w", err)
	}
	// Ascender 404s or redirects on a missing trailing slash.
	if !strings.HasSuffix(u, "/") {
		u += "/"
	}
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func transportError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return &Error{Message: "request canceled"}
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Message: "request timed out"}
	default:
		return &Error{Message: "transport error"}
	}
}

// readAPIError lifts Ascender's message out of either error shape,
// {"detail": "..."} or {"field": ["..."]}, and drops the rest so 5xx
// stack traces never reach the model.
func readAPIError(resp *http.Response) error {
	const maxDetail = 500
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	e := &Error{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	var parsed map[string]any
	if json.Unmarshal(body, &parsed) != nil {
		return e
	}
	if d, ok := parsed["detail"].(string); ok && d != "" {
		e.Detail = truncate(d, maxDetail)
		return e
	}
	for field, v := range parsed {
		if msgs, ok := v.([]any); ok && len(msgs) > 0 {
			if m, ok := msgs[0].(string); ok {
				e.Detail = truncate(field+": "+m, maxDetail)
				return e
			}
		}
	}
	return e
}

// truncate counts runes so it never splits a multi-byte character.
func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}
