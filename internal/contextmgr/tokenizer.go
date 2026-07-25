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
	"strings"

	"github.com/7solutions/openplus/internal/provider"
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
	default:
		// openai, local, and any OpenAI-compatible endpoint.
		return Heuristic{CharsPerToken: OpenAICharsPerToken}
	}
}

// blockOverheadTokens approximates the per-block wire framing (role markers,
// JSON envelope) that providers bill for beyond the visible text.
const blockOverheadTokens = 4

// CountMessages estimates the total token cost of a neutral message history,
// counting every block kind that carries text or JSON plus per-block framing
// overhead.
func CountMessages(tk Tokenizer, msgs []provider.Message) int {
	total := 0
	for _, m := range msgs {
		for _, b := range m.Blocks {
			total += blockOverheadTokens
			switch b.Kind {
			case provider.BlockText, provider.BlockThinking:
				total += tk.Count(b.Text)
			case provider.BlockToolCall:
				total += tk.Count(b.ToolName)
				total += tk.Count(string(b.ToolInput))
			case provider.BlockToolResult:
				total += tk.Count(b.ToolResultText)
			case provider.BlockImage:
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
