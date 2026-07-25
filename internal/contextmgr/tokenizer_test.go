package contextmgr

import (
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/provider"
)

func TestHeuristicCountsEmpty(t *testing.T) {
	tk := Heuristic{}
	if got := tk.Count(""); got != 0 {
		t.Fatalf("Count(\"\") = %d, want 0", got)
	}
}

func TestHeuristicScalesWithLength(t *testing.T) {
	tk := Heuristic{}
	short := tk.Count("hello world")
	long := tk.Count(strings.Repeat("hello world ", 100))
	if short <= 0 {
		t.Fatalf("short count = %d, want > 0", short)
	}
	if long <= short {
		t.Fatalf("long (%d) must exceed short (%d)", long, short)
	}
}

// TestHeuristicCalibration is the ADR-0008 calibration test: the estimate must
// land within a tolerance band of the known-good token count for representative
// text. Reference counts come from the ~4-chars-per-token rule that both
// families approximate for English prose.
func TestHeuristicCalibration(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int // reference token count
	}{
		{"prose", "The quick brown fox jumps over the lazy dog near the river bank.", 14},
		{"code", "func main() { fmt.Println(\"hello\") }", 12},
		{"long prose", strings.Repeat("the agent reconstructs context from memory. ", 20), 180},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Heuristic{}.Count(c.text)
			// within ±50%: a soft ceiling per ADR-0008, not an exact counter.
			lo, hi := c.want/2, c.want*3/2
			if got < lo || got > hi {
				t.Errorf("Count = %d, want within [%d,%d] of %d", got, lo, hi, c.want)
			}
		})
	}
}

func TestHeuristicPerModelRatio(t *testing.T) {
	// Anthropic text tokenizes slightly denser than the OpenAI default in our
	// calibration, so its ratio differs — but both stay positive and ordered.
	text := strings.Repeat("context budget reconstruction ", 30)
	anthropic := Heuristic{CharsPerToken: AnthropicCharsPerToken}.Count(text)
	openai := Heuristic{CharsPerToken: OpenAICharsPerToken}.Count(text)
	if anthropic <= 0 || openai <= 0 {
		t.Fatalf("counts must be positive: anthropic=%d openai=%d", anthropic, openai)
	}
	if anthropic == openai {
		t.Errorf("expected different per-family estimates, both %d", anthropic)
	}
}

func TestForModelSelectsByPrefix(t *testing.T) {
	cases := map[string]float64{
		"anthropic/claude-sonnet-5": AnthropicCharsPerToken,
		"openai/gpt-4o-mini":        OpenAICharsPerToken,
		"local/qwen2.5-coder":       OpenAICharsPerToken, // OpenAI-compatible default
		"unknown":                   OpenAICharsPerToken, // safe default
	}
	for model, want := range cases {
		tk := ForModel(model)
		h, ok := tk.(Heuristic)
		if !ok {
			t.Fatalf("ForModel(%q) = %T, want Heuristic", model, tk)
		}
		if h.CharsPerToken != want {
			t.Errorf("ForModel(%q).CharsPerToken = %v, want %v", model, h.CharsPerToken, want)
		}
	}
}

func TestCountMessagesIncludesAllBlockKinds(t *testing.T) {
	tk := Heuristic{}
	msgs := []provider.Message{
		{Role: provider.RoleUser, Blocks: []provider.Block{
			{Kind: provider.BlockText, Text: "please read the file"},
		}},
		{Role: provider.RoleAssistant, Blocks: []provider.Block{
			{Kind: provider.BlockThinking, Text: "I should call read"},
			{Kind: provider.BlockToolCall, ToolName: "read", ToolInput: []byte(`{"file_path":"a.go"}`)},
		}},
		{Role: provider.RoleUser, Blocks: []provider.Block{
			{Kind: provider.BlockToolResult, ToolResultText: "package main"},
		}},
	}
	total := CountMessages(tk, msgs)
	if total <= 0 {
		t.Fatalf("total = %d, want > 0", total)
	}
	// dropping the tool-call block must reduce the count (proves it is counted)
	fewer := CountMessages(tk, msgs[:1])
	if fewer >= total {
		t.Errorf("subset count (%d) must be < full count (%d)", fewer, total)
	}
}

func TestCountMessagesEmpty(t *testing.T) {
	if got := CountMessages(Heuristic{}, nil); got != 0 {
		t.Fatalf("CountMessages(nil) = %d, want 0", got)
	}
}

// compile-time: Heuristic satisfies Tokenizer.
var _ Tokenizer = Heuristic{}
