// Package selectadapter is the adapter-selection composition layer (ADR-0005,
// T-014). It maps the neutral model string's "<provider>/" prefix to the
// concrete Provider adapter, wiring resolved config.Provider options into the
// adapter. This is the only place that knows both config and the adapter
// packages; everything downstream sees provider.Provider.
//
// The registry is open for extension: Register(prefix, factory) adds a new
// adapter. anthropic is registered by default; the OpenAI-compatible adapter
// (openai, local, custom, …) is registered when T-013 lands.
package selectadapter

import (
	"errors"
	"fmt"
	"sync"

	"github.com/7solutions/openplus/internal/config"
	"github.com/7solutions/openplus/internal/provider"
	"github.com/7solutions/openplus/internal/provider/anthropic"
	"github.com/7solutions/openplus/internal/provider/openaicompat"
)

// Sentinel errors returned by Select. Wrap with errors.Is to distinguish
// "no provider configured" from "no adapter for that provider".
var (
	// ErrBadModel means the model string is missing or has no "/" separator.
	ErrBadModel = errors.New("selectadapter: bad model string (want <provider>/<model>)")
	// ErrNoProvider means the prefix parsed fine but no provider is configured
	// under it in opencode.json.
	ErrNoProvider = errors.New("selectadapter: provider not configured")
	// ErrNoAdapter means the provider is configured but no adapter factory is
	// registered for its prefix (e.g. the openaicompat adapter before T-013).
	ErrNoAdapter = errors.New("selectadapter: no adapter registered for prefix")
)

// Factory builds a Provider adapter from a resolved config.Provider. Adapters
// that ignore some fields (e.g. anthropic ignoring baseURL when empty) are free
// to do so.
type Factory func(p config.Provider) provider.Provider

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

// Register installs (or replaces) the Factory for a provider prefix. Safe to
// call concurrently and from init() blocks. T-013 will Register the
// openaicompat factory for openai/local/custom prefixes.
func Register(prefix string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[prefix] = f
}

// Unregister removes a prefix's factory. Primarily for tests.
func Unregister(prefix string) {
	mu.Lock()
	defer mu.Unlock()
	delete(registry, prefix)
}

func init() {
	Register("anthropic", newAnthropic)
	// One openaicompat factory covers OpenAI plus any baseURL-configured
	// endpoint (Ollama, vLLM, LM Studio, OpenRouter, Groq, … per ADR-0005).
	Register("openai", newOpenAICompat)
	Register("local", newOpenAICompat)
}

// newAnthropic builds the Anthropic adapter from resolved config. baseURL is
// left empty when unset so the adapter falls back to its public default.
func newAnthropic(p config.Provider) provider.Provider {
	return &anthropic.Adapter{
		BaseURL: p.BaseURL,
		APIKey:  p.APIKey,
	}
}

// newOpenAICompat builds the Chat Completions adapter. baseURL (e.g. an
// Ollama/vLLM endpoint) flows straight through; empty baseURL falls back to
// the public OpenAI endpoint.
func newOpenAICompat(p config.Provider) provider.Provider {
	return &openaicompat.Adapter{
		BaseURL: p.BaseURL,
		APIKey:  p.APIKey,
	}
}

// Select resolves which adapter serves the model string and returns it, wired
// with the matching provider's options. model must be "<provider>/<model>".
// providers is the table from config (cfg.Providers).
//
// Returns a wrapped ErrBadModel / ErrNoProvider / ErrNoAdapter on the
// corresponding failures.
func Select(model string, providers map[string]config.Provider) (provider.Provider, error) {
	prefix, _, err := config.ParseModel(model)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrBadModel, model)
	}

	p, ok := providers[prefix]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoProvider, prefix)
	}

	mu.RLock()
	f, ok := registry[prefix]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoAdapter, prefix)
	}
	return f(p), nil
}
