// Package embed defines the Embedder port (ADR-0004) and a local adapter that
// speaks the OpenAI-compatible /embeddings endpoint. Embeddings stay local in
// the sense that the store persists them on host; the endpoint may be local
// (Ollama, vLLM) or remote (OpenAI) — configured per provider.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	DefaultBaseURL = "https://api.openai.com/v1"
	// DefaultTimeout bounds a single Embed call when no caller-supplied
	// http.Client is set.
	DefaultTimeout = 30 * time.Second
)

// ErrDimensionDrift is returned by Local.Embed when the endpoint returns
// vectors whose dimension disagrees with the dimension pinned on the first
// successful Embed. Callers use errors.Is to distinguish it from transport
// failures — the fallback path (FallbackTo) does NOT trigger on dim drift,
// because the fallback endpoint almost certainly uses a different model
// and a different vector space.
var ErrDimensionDrift = errors.New("embed: dimension drift")

// Embedder turns text into fixed-dimension float32 vectors. Dim() returns the
// pinned dimension (0 before the first successful Embed).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
}

// Local is the OpenAI-compatible /embeddings adapter. It pins the vector
// dimension on the first successful Embed and rejects later mismatches.
type Local struct {
	BaseURL string
	APIKey  string
	Model   string
	// Timeout bounds a single Embed call when HTTP is nil. Zero means
	// DefaultTimeout. Ignored when HTTP is non-nil (the caller's client
	// wins, including its timeout).
	Timeout time.Duration
	HTTP    *http.Client

	mu  sync.Mutex
	dim int
}

// Embed posts texts to {BaseURL}/embeddings and returns one vector per text.
func (l *Local) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	base := l.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	body, err := json.Marshal(map[string]any{"model": l.Model, "input": texts})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed: request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Authorization", "Bearer "+l.APIKey)

	client := l.HTTP
	if client == nil {
		to := l.Timeout
		if to <= 0 {
			to = DefaultTimeout
		}
		client = &http.Client{Timeout: to}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{code: resp.StatusCode}
	}

	var out struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embed: decode: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embed: got %d vectors, want %d", len(out.Data), len(texts))
	}

	vecs := make([][]float32, len(out.Data))
	for i, e := range out.Data {
		v := make([]float32, len(e.Embedding))
		for j, f := range e.Embedding {
			v[j] = float32(f)
		}
		vecs[i] = v
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dim == 0 {
		l.dim = len(vecs[0])
	} else if len(vecs[0]) != l.dim {
		return nil, fmt.Errorf("got %d, pinned %d: %w", len(vecs[0]), l.dim, ErrDimensionDrift)
	}
	return vecs, nil
}

// Dim returns the pinned embedding dimension (0 before the first Embed).
func (l *Local) Dim() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dim
}

// FallbackTo wraps l with a fallback Embedder that is consulted only on
// transport-class errors (network failure, 5xx, 429). It returns an
// Embedder; the wrapper itself is a small struct holding primary and
// fallback. Dim() reports the primary's pinned dimension.
//
// FallbackTo does NOT trigger on ErrDimensionDrift (the fallback endpoint
// almost certainly uses a different model with a different vector space)
// or on 4xx other than 429 (those are caller errors, not transport errors).
//
// Calling FallbackTo on a nil receiver is a programming error and returns
// nil.
func (l *Local) FallbackTo(fb Embedder) Embedder {
	if l == nil {
		return nil
	}
	return &fallbackEmbedder{primary: l, fallback: fb}
}

// fallbackEmbedder is the Embedder returned by Local.FallbackTo. It tries
// primary; on transport-class failure it tries fallback. Anything else
// propagates from primary.
type fallbackEmbedder struct {
	primary  Embedder
	fallback Embedder
}

func (f *fallbackEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vecs, err := f.primary.Embed(ctx, texts)
	if err == nil {
		return vecs, nil
	}
	if !isTransportErr(err) {
		return nil, err
	}
	return f.fallback.Embed(ctx, texts)
}

func (f *fallbackEmbedder) Dim() int {
	return f.primary.Dim()
}

// isTransportErr reports whether err is a transport-class failure:
// network error, HTTP 5xx, or HTTP 429. ErrDimensionDrift is not
// transport-class. 4xx other than 429 are not transport-class.
func isTransportErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDimensionDrift) {
		return false
	}
	var hErr httpErr
	if errors.As(err, &hErr) {
		code := hErr.StatusCode()
		return code >= 500 || code == http.StatusTooManyRequests
	}
	return true
}

// httpErr is the interface satisfied by errors that carry an HTTP status
// code. Local's HTTP error wraps one of these so the fallback path can
// decide without parsing strings.
type httpErr interface {
	StatusCode() int
}

// httpStatusError is returned by Local.Embed when the embedder endpoint
// responds with a non-200 status. StatusCode() lets the fallback path
// classify the error without parsing strings.
type httpStatusError struct {
	code int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("embed: http %d", e.code)
}

func (e *httpStatusError) StatusCode() int { return e.code }
