package selectadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/7solutions/openplus/internal/config"
	"github.com/7solutions/openplus/internal/provider"
	"github.com/7solutions/openplus/internal/provider/anthropic"
	"github.com/7solutions/openplus/internal/provider/openaicompat"
)

func providers(ps ...config.Provider) map[string]config.Provider {
	m := make(map[string]config.Provider, len(ps))
	for _, p := range ps {
		m[p.ID] = p
	}
	return m
}

func TestSelectAnthropicByPrefix(t *testing.T) {
	in := providers(config.Provider{
		ID:      "anthropic",
		APIKey:  "sk-ant",
		BaseURL: "https://proxy.example.com",
	})

	got, err := Select("anthropic/claude-sonnet-5", in)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	a, ok := got.(*anthropic.Adapter)
	if !ok {
		t.Fatalf("Select returned %T, want *anthropic.Adapter", got)
	}
	if a.APIKey != "sk-ant" {
		t.Fatalf("apiKey = %q", a.APIKey)
	}
	if a.BaseURL != "https://proxy.example.com" {
		t.Fatalf("baseURL = %q", a.BaseURL)
	}
}

func TestSelectAnthropicDefaultsBaseURLWhenEmpty(t *testing.T) {
	// opencode.json anthropic entry has no baseURL → adapter must still build
	// and resolve to the public endpoint at Stream time.
	in := providers(config.Provider{ID: "anthropic", APIKey: "k"})
	got, err := Select("anthropic/claude-sonnet-5", in)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if _, ok := got.(*anthropic.Adapter); !ok {
		t.Fatalf("Select returned %T", got)
	}
}

func TestSelectUnconfiguredProviderErrors(t *testing.T) {
	// prefix parses but no provider configured under it
	_, err := Select("anthropic/claude-sonnet-5", providers())
	if err == nil {
		t.Fatal("want error for unconfigured provider")
	}
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("err = %v, want wraps ErrNoProvider", err)
	}
}

func TestSelectUnregisteredPrefixErrors(t *testing.T) {
	// provider configured, but no adapter factory registered for its prefix
	// (the situation until T-013 lands the openaicompat adapter).
	in := providers(config.Provider{ID: "custom"})
	_, err := Select("custom/some-model", in)
	if err == nil {
		t.Fatal("want error for unregistered prefix")
	}
	if !errors.Is(err, ErrNoAdapter) {
		t.Fatalf("err = %v, want wraps ErrNoAdapter", err)
	}
}

func TestSelectBadModelStringErrors(t *testing.T) {
	in := providers(config.Provider{ID: "anthropic"})
	if _, err := Select("anthropic", in); err == nil {
		t.Fatal("want error for model without slash")
	}
	if _, err := Select("", in); err == nil {
		t.Fatal("want error for empty model")
	}
}

// fakeProv is a test-only provider.Provider capturing the options the factory
// received, used to prove Register wires resolved config into the factory.
type fakeProv struct{ apiKey string }

func (f fakeProv) Stream(context.Context, provider.Request) (<-chan provider.Event, error) {
	return nil, errors.New("not implemented")
}

// TestSelectCustomFactoryRegistration proves the registry is open for extension
// (T-013 will Register an openaicompat factory the same way), and that the
// factory receives the resolved provider options.
func TestSelectCustomFactoryRegistration(t *testing.T) {
	const prefix = "test-fake-provider"
	Register(prefix, func(p config.Provider) provider.Provider {
		return fakeProv{apiKey: p.APIKey}
	})
	t.Cleanup(func() { Unregister(prefix) })

	in := providers(config.Provider{ID: prefix, APIKey: "factory-key"})
	got, err := Select(prefix+"/model-x", in)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	fp, ok := got.(fakeProv)
	if !ok {
		t.Fatalf("Select returned %T, want fakeProv", got)
	}
	if fp.apiKey != "factory-key" {
		t.Fatalf("api key not propagated to factory: %q", fp.apiKey)
	}
}

// TestSelectReturnsProviderPort proves whatever Select returns satisfies the
// provider.Provider interface — no concrete adapter type escapes the selector
// in its contract.
func TestSelectReturnsProviderPort(t *testing.T) {
	in := providers(config.Provider{ID: "anthropic", APIKey: "k"})
	got, err := Select("anthropic/claude-sonnet-5", in)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	var _ provider.Provider = got
}

// TestSelectOpenAICompatPrefixes proves the openaicompat factory covers both
// the openai (default endpoint) and local (baseURL) prefixes — one adapter,
// many endpoints (ADR-0005).
func TestSelectOpenAICompatPrefixes(t *testing.T) {
	cases := []struct {
		prefix  string
		baseURL string
	}{
		{"openai", ""},
		{"local", "http://localhost:11434/v1"},
	}
	for _, c := range cases {
		t.Run(c.prefix, func(t *testing.T) {
			in := providers(config.Provider{ID: c.prefix, APIKey: "k", BaseURL: c.baseURL})
			got, err := Select(c.prefix+"/model-x", in)
			if err != nil {
				t.Fatalf("Select: %v", err)
			}
			oc, ok := got.(*openaicompat.Adapter)
			if !ok {
				t.Fatalf("Select(%s) returned %T, want *openaicompat.Adapter", c.prefix, got)
			}
			if oc.BaseURL != c.baseURL {
				t.Fatalf("baseURL = %q, want %q", oc.BaseURL, c.baseURL)
			}
		})
	}
}
