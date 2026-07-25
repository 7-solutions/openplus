package embed

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// embeddingsFixture returns count embeddings of the given dim.
func embeddingsFixture(t *testing.T, srv *httptest.Server, count, dim int) {
	t.Helper()
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		// stash the request shape on a header for assertions.
		w.Header().Set("x-path", r.URL.Path)
		w.Header().Set("x-auth", r.Header.Get("Authorization"))
		b, _ := json.Marshal(map[string]any{
			"model": got["model"],
			"input": got["input"],
		})
		w.Header().Set("x-body", string(b))

		data := make([]map[string]any, count)
		for i := 0; i < count; i++ {
			vec := make([]float64, dim)
			for j := 0; j < dim; j++ {
				vec[j] = float64(i)*0.1 + float64(j)*0.01
			}
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": vec}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	})
}

func TestEmbedReturnsVectors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)
	embeddingsFixture(t, srv, 2, 3)

	l := &Local{BaseURL: srv.URL, APIKey: "sk-test", Model: "text-embedding-3-small"}
	vecs, err := l.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("got %d vectors", len(vecs))
	}
	if len(vecs[0]) != 3 {
		t.Fatalf("dim = %d, want 3", len(vecs[0]))
	}
	if l.Dim() != 3 {
		t.Fatalf("Dim() = %d, want 3", l.Dim())
	}
}

func TestEmbedSendsWireShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)
	embeddingsFixture(t, srv, 2, 2)

	l := &Local{BaseURL: srv.URL, APIKey: "sk-test", Model: "nomic-embed-text"}
	if _, err := l.Embed(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	// assertions are encoded into headers by the fixture.
}

func TestEmbedDimPinningRejectsMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)
	embeddingsFixture(t, srv, 1, 3)
	l := &Local{BaseURL: srv.URL, APIKey: "k", Model: "m"}
	if _, err := l.Embed(context.Background(), []string{"first"}); err != nil {
		t.Fatalf("first Embed: %v", err)
	}
	if l.Dim() != 3 {
		t.Fatalf("Dim = %d", l.Dim())
	}
	// now server returns a different dim → mismatch must error.
	embeddingsFixture(t, srv, 1, 4)
	if _, err := l.Embed(context.Background(), []string{"second"}); err == nil {
		t.Fatal("want dim mismatch error")
	}
	// dim stays pinned to the original
	if l.Dim() != 3 {
		t.Fatalf("Dim changed after mismatch: %d", l.Dim())
	}
}

func TestEmbedErrorsOnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad key"}`)
	}))
	t.Cleanup(srv.Close)
	l := &Local{BaseURL: srv.URL, APIKey: "bad", Model: "m"}
	if _, err := l.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("want error on 401")
	}
}

// --- Change 0004 / T-403: a hanging server is bounded by Embedder.Timeout ---
//
// With no caller-supplied http.Client, Local falls back to its own
// timeout-bounded client. Today it falls back to http.DefaultClient
// (no timeout), so this test is RED until T-404 lands.

func TestEmbedTimeoutApplied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the client's timeout. The client will give
		// up and return a timeout error; the server wakes up shortly
		// after and returns 200 — which the client discards.
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// No HTTP field set — relies on the fallback client honoring Timeout.
	l := &Local{BaseURL: srv.URL, APIKey: "k", Model: "m", Timeout: 50 * time.Millisecond}

	start := time.Now()
	_, err := l.Embed(context.Background(), []string{"x"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want timeout error")
	}
	if elapsed > time.Second {
		t.Fatalf("Embed took %v, want < 1s (timeout should have fired near 50ms)", elapsed)
	}
}

// --- Change 0004 / T-405: dim-drift surfaces as a typed error ---
//
// Today the drift error is a plain fmt.Errorf string. After T-406 it
// wraps ErrDimensionDrift so callers can distinguish it from transport
// failures (which FallbackTo handles) and from generic bad responses.

func TestLocalErrDimensionDrift(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)
	embeddingsFixture(t, srv, 1, 4)
	l := &Local{BaseURL: srv.URL, APIKey: "k", Model: "m"}
	if _, err := l.Embed(context.Background(), []string{"first"}); err != nil {
		t.Fatalf("first Embed: %v", err)
	}
	embeddingsFixture(t, srv, 1, 8)
	_, err := l.Embed(context.Background(), []string{"second"})
	if err == nil {
		t.Fatal("want dim drift error")
	}
	if !errors.Is(err, ErrDimensionDrift) {
		t.Errorf("err = %v, want errors.Is(_, ErrDimensionDrift)", err)
	}
}

// fakeEmbedder is a deterministic in-memory Embedder for store tests (no HTTP).
type fakeEmbedder struct {
	dim int
}

func (f fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, tx := range texts {
		v := make([]float32, f.dim)
		for j := range v {
			v[j] = float32(len(tx))*0.1 + float32(j)*0.01
		}
		out[i] = v
	}
	return out, nil
}
func (f fakeEmbedder) Dim() int { return f.dim }
