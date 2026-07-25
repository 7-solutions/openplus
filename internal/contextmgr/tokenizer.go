// Package contextmgr implements the Tokenizer, Budgeter, and Checkpointer ports
// (ADR-0008): estimate token cost, decide what enters the context window in
// priority order, and checkpoint/reconstruct when the window fills up.
//
// Token counts are deliberately approximate. ADR-0008 treats the budget as a
// soft ceiling with a safety margin, so a calibrated per-family heuristic is
// the default. An exact counter (tiktoken BPE, or a provider count endpoint)
// can be dropped in behind the Tokenizer port without touching callers — the
// heuristic ships first because a BPE table fetched over the network at runtime
// would break the local-first guarantee.
package contextmgr

import (
	"fmt"
	"os"
	"strings"

	"github.com/pkoukk/tiktoken-go"

	"github.com/7-solutions/openplus/internal/ports"
)

// Characters-per-token ratios, calibrated against representative English prose
// and source code for each family. Anthropic's tokenizer runs slightly denser
// than the OpenAI cl100k/o200k families on the same text.
const (
	AnthropicCharsPerToken = 3.6
	OpenAICharsPerToken    = 4.0
)

// Tokenizer estimates the token cost of text. Implementations must be safe for
// concurrent use.
type Tokenizer interface {
	Count(text string) int
}

// Heuristic is a characters-per-token estimator. A zero CharsPerToken falls
// back to the OpenAI ratio.
type Heuristic struct {
	CharsPerToken float64
}

// Count estimates the token count of text, rounding up so a non-empty string is
// never free.
func (h Heuristic) Count(text string) int {
	if text == "" {
		return 0
	}
	ratio := h.CharsPerToken
	if ratio <= 0 {
		ratio = OpenAICharsPerToken
	}
	n := int(float64(len(text))/ratio + 0.999)
	if n < 1 {
		n = 1
	}
	return n
}

// Tiktoken is the exact token counter backed by github.com/pkoukk/tiktoken-go
// (Change 0005 / T-452). It satisfies the Tokenizer port for OpenAI-shaped
// model prefixes. Anthropic stays on the heuristic — Anthropic's BPE isn't
// publicly published.
type Tiktoken struct {
	// inner is the underlying *tiktoken.Tiktoken. Underscore-prefixed on
	// purpose: the field is accessed by tests in this package to exercise
	// the real library's Encode/Decode round-trip, not a re-implementation.
	// Naming avoids the `tk.tk` shadow that would trip go vet.
	inner *tiktoken.Tiktoken
	// encoding names the tiktoken encoding (e.g. "cl100k_base"). Kept for
	// debugging / future logging.
	encoding string
}

// NewTiktoken builds a Tiktoken for the given "<provider>/<model>" string.
// It picks cl100k_base for gpt-4/gpt-3.5-turbo-style models and o200k_base
// for gpt-4o/gpt-4.1/gpt-4.5 family, falling back to cl100k_base when the
// model is unrecognized. Returns an error when the underlying tiktoken-go
// call fails (e.g. network blip on first download) — callers should
// fall back to Heuristic rather than failing the whole turn.
func NewTiktoken(model string) (*Tiktoken, error) {
	if err := ensureCacheDir(); err != nil {
		return nil, err
	}
	encName, err := pickEncoding(model)
	if err != nil {
		return nil, err
	}
	enc, err := tiktoken.GetEncoding(encName)
	if err != nil {
		return nil, fmt.Errorf("contextmgr: tiktoken.GetEncoding(%q): %w", encName, err)
	}
	return &Tiktoken{inner: enc, encoding: encName}, nil
}

// Count runs the underlying BPE on text and returns the token count.
func (t *Tiktoken) Count(text string) int {
	if text == "" {
		return 0
	}
	return len(t.inner.Encode(text, nil, nil))
}

// pickEncoding picks a tiktoken encoding name from the model suffix. Falls
// back to cl100k_base for OpenAI-shaped prefixes whose model isn't in
// tiktoken-go's table yet. Unknown prefixes return an error so the caller
// can fall back to Heuristic.
func pickEncoding(model string) (string, error) {
	prefix, suffix, ok := strings.Cut(model, "/")
	if !ok {
		suffix = model
		prefix = ""
	}
	_ = prefix // prefix is decided upstream; pickEncoding only sees the model suffix
	// Model families per tiktoken-go v0.1.8 MODEL_PREFIX_TO_ENCODING.
	switch {
	case strings.HasPrefix(suffix, "gpt-4.5"),
		strings.HasPrefix(suffix, "gpt-4.1"),
		strings.HasPrefix(suffix, "gpt-4o"),
		strings.HasPrefix(suffix, "o200k"):
		return tiktoken.MODEL_O200K_BASE, nil
	case strings.HasPrefix(suffix, "gpt-4"),
		strings.HasPrefix(suffix, "gpt-3.5"),
		strings.HasPrefix(suffix, "text-embedding-3"),
		strings.HasPrefix(suffix, "text-embedding-ada-002"),
		strings.HasPrefix(suffix, "cl100k"):
		return tiktoken.MODEL_CL100K_BASE, nil
	}
	return "", fmt.Errorf("contextmgr: no tiktoken encoding for model %q", model)
}

// ensureCacheDir pins TIKTOKEN_CACHE_DIR to a sane local path so the
// first-use BPE download is cached and never re-downloaded. Idempotent —
// safe to call on every NewTiktoken.
//
// Local-first guarantee (ADR-0001): the binary must work offline after the
// first run. tiktoken-go fetches its BPE from openaipublic.blob.core.windows.net
// on first use; without a cache dir, every fresh process would re-download.
// $XDG_CACHE_HOME/openplus/tiktoken is the project-local cache; $HOME/.cache/
// openplus/tiktoken is the fallback when XDG isn't set.
func ensureCacheDir() error {
	if os.Getenv("TIKTOKEN_CACHE_DIR") != "" {
		return nil
	}
	cacheRoot := os.Getenv("XDG_CACHE_HOME")
	if cacheRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("contextmgr: cache dir: %w", err)
		}
		cacheRoot = home + "/.cache"
	}
	dir := cacheRoot + "/openplus/tiktoken"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("contextmgr: mkdir %s: %w", dir, err)
	}
	return os.Setenv("TIKTOKEN_CACHE_DIR", dir)
}

// ForModel returns the Tokenizer calibrated for a "<provider>/<model>" string,
// selecting by prefix (the same convention as the provider adapters, ADR-0005).
// Unknown prefixes get the OpenAI-compatible default.
func ForModel(model string) Tokenizer {
	prefix, _, ok := strings.Cut(model, "/")
	if !ok {
		prefix = model
	}
	switch prefix {
	case "anthropic":
		return Heuristic{CharsPerToken: AnthropicCharsPerToken}
	}
	// openai, local, and any OpenAI-compatible endpoint: try the exact
	// BPE counter; fall back to the heuristic when tiktoken-go doesn't
	// know the model.
	if tk, err := NewTiktoken(model); err == nil {
		return tk
	}
	return Heuristic{CharsPerToken: OpenAICharsPerToken}
}

// blockOverheadTokens approximates the per-block wire framing (role markers,
// JSON envelope) that providers bill for beyond the visible text.
const blockOverheadTokens = 4

// CountMessages estimates the total token cost of a neutral message history,
// counting every block kind that carries text or JSON plus per-block framing
// overhead.
func CountMessages(tk Tokenizer, msgs []ports.Message) int {
	total := 0
	for _, m := range msgs {
		for _, b := range m.Blocks {
			total += blockOverheadTokens
			switch b.Kind {
			case ports.BlockText, ports.BlockThinking:
				total += tk.Count(b.Text)
			case ports.BlockToolCall:
				total += tk.Count(b.ToolName)
				total += tk.Count(string(b.ToolInput))
			case ports.BlockToolResult:
				total += tk.Count(b.ToolResultText)
			case ports.BlockImage:
				// Images are billed by tile, not text; approximate a flat cost.
				total += imageTokenEstimate
			}
		}
	}
	return total
}

// imageTokenEstimate is a flat per-image approximation (real cost is
// resolution-dependent; refine when image input is wired end to end).
const imageTokenEstimate = 800
