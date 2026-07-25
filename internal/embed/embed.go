// Package embed defines the Embedder port (ADR-0004) and a local adapter that
// speaks the OpenAI-compatible /embeddings endpoint. Embeddings stay local in
// the sense that the store persists them on host; the endpoint may be local
// (Ollama, vLLM) or remote (OpenAI) — configured per provider.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

const DefaultBaseURL = "https://api.openai.com/v1"

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
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: http %d", resp.StatusCode)
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
		return nil, fmt.Errorf("embed: dimension drift: got %d, pinned %d", len(vecs[0]), l.dim)
	}
	return vecs, nil
}

// Dim returns the pinned embedding dimension (0 before the first Embed).
func (l *Local) Dim() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dim
}
