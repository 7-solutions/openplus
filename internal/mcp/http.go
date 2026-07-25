package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultHTTPTimeout bounds one streamable-HTTP call when the caller's context
// carries no deadline. A server that never answers must not hold a turn open
// forever.
const DefaultHTTPTimeout = 2 * time.Minute

// HTTPConfig describes a streamable-HTTP MCP endpoint.
type HTTPConfig struct {
	// URL is the MCP endpoint. Required.
	URL string
	// Headers are sent on every request (an Authorization header, typically).
	Headers map[string]string
	// Client overrides the HTTP client. Nil uses a default one.
	Client *http.Client
}

// httpTransport speaks JSON-RPC over streamable HTTP: each request is a POST, and
// the response arrives either as a single JSON body or as an SSE stream carrying
// the response among other events.
//
// There is no long-lived connection to demux, so unlike stdio each call is
// self-contained. The only shared state is the session id the server assigns
// during initialize, which every later request must echo.
type httpTransport struct {
	url     string
	headers map[string]string
	client  *http.Client

	nextID atomic.Int64

	mu        sync.Mutex
	sessionID string
	closed    bool
}

// NewHTTP returns a Transport for a streamable-HTTP endpoint. Nothing is
// contacted until the first call, so this cannot fail.
func NewHTTP(cfg HTTPConfig) *httpTransport {
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	return &httpTransport{url: cfg.URL, headers: cfg.Headers, client: client}
}

// Call posts a request and returns its result.
func (t *httpTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	req := Request{
		JSONRPC: Version,
		ID:      json.RawMessage(fmt.Sprintf("%d", id)),
		Method:  method,
		Params:  params,
	}

	res, err := t.post(ctx, req, method)
	if err != nil {
		return nil, err
	}
	if res.Error != nil {
		return nil, res.Error
	}
	if len(res.Result) == 0 {
		return nil, fmt.Errorf("mcp: %s: response has neither result nor error", method)
	}
	return res.Result, nil
}

// Notify posts a message that expects no response. An empty or accepted body is
// success; the server is entitled to answer 202 with nothing.
func (t *httpTransport) Notify(ctx context.Context, method string, params json.RawMessage) error {
	req := Request{JSONRPC: Version, Method: method, Params: params}
	_, err := t.post(ctx, req, method)
	if err != nil && !isEmptyResponse(err) {
		return err
	}
	return nil
}

// errEmptyResponse marks a successful HTTP exchange that carried no JSON-RPC
// message — correct for a notification, an error for a call.
var errEmptyResponse = fmt.Errorf("mcp: no jsonrpc message in response")

func isEmptyResponse(err error) bool { return strings.Contains(err.Error(), errEmptyResponse.Error()) }

// post sends one JSON-RPC message and reads the response, from either a JSON body
// or an SSE stream.
func (t *httpTransport) post(ctx context.Context, msg Request, method string) (*Response, error) {
	t.mu.Lock()
	closed := t.closed
	session := t.sessionID
	t.mu.Unlock()
	if closed {
		return nil, errTransportClosed
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: encode: %w", method, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: build request: %w", method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Either shape is acceptable; the server picks.
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	// The session id is protocol state, not a user preference: it is set after
	// the configured headers so a stale one in config cannot break tracking.
	if session != "" {
		httpReq.Header.Set("Mcp-Session-Id", session)
	}

	httpRes, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: %w", method, err)
	}
	defer httpRes.Body.Close()

	// A server assigns the session on initialize; keep it for later requests.
	if got := httpRes.Header.Get("Mcp-Session-Id"); got != "" {
		t.mu.Lock()
		t.sessionID = got
		t.mu.Unlock()
	}

	if httpRes.StatusCode < 200 || httpRes.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(httpRes.Body, 512))
		return nil, fmt.Errorf("mcp: %s: http %d: %s", method, httpRes.StatusCode,
			strings.TrimSpace(string(snippet)))
	}

	if strings.Contains(httpRes.Header.Get("Content-Type"), "text/event-stream") {
		return readSSEResponse(httpRes.Body, msg.ID, method)
	}

	raw, err := io.ReadAll(httpRes.Body)
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: read response: %w", method, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, fmt.Errorf("mcp: %s: %w", method, errEmptyResponse)
	}
	var res Response
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("mcp: %s: decode response: %w", method, err)
	}
	return &res, nil
}

// readSSEResponse scans an SSE stream for the response matching id. Other events
// (progress notifications, keep-alive comments) are skipped: they are the reason
// the stream shape exists, and treating the first frame as the answer would pick
// up the wrong message.
//
// A stream that ends without the response is an error, never an empty result.
func readSSEResponse(body io.Reader, id json.RawMessage, method string) (*Response, error) {
	want := string(id)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var data strings.Builder
	flush := func() (*Response, bool) {
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "" {
			return nil, false
		}
		var res Response
		if err := json.Unmarshal([]byte(payload), &res); err != nil {
			return nil, false
		}
		if string(res.ID) != want {
			return nil, false
		}
		return &res, true
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if res, ok := flush(); ok {
				return res, nil
			}
		case strings.HasPrefix(line, ":"):
			// comment / keep-alive
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		default:
			// "event:", "id:", "retry:" — irrelevant to which JSON-RPC message
			// this is.
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("mcp: %s: read stream: %w", method, err)
	}
	// Handle a final event that was not terminated by a blank line.
	if res, ok := flush(); ok {
		return res, nil
	}
	return nil, fmt.Errorf("mcp: %s: stream ended without a response: %w", method, errEmptyResponse)
}

// Close releases the transport. There is no persistent connection to tear down;
// idle connections are returned to the pool.
func (t *httpTransport) Close() error {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	t.client.CloseIdleConnections()
	return nil
}
