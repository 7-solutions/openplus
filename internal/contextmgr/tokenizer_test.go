package contextmgr

import (
	"os"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/ports"
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

// TestForModelSelectsByPrefix: per Change 0005, OpenAI-shaped prefixes now
// dispatch to the exact tiktoken counter; anthropic stays on Heuristic;
// local/unknown falls back to Heuristic.
func TestForModelSelectsByPrefix(t *testing.T) {
	cases := []struct {
		model string
		isTik bool
		isH   bool
	}{
		{"anthropic/claude-sonnet-5", false, true},
		{"openai/gpt-4o-mini", true, false},
		{"local/qwen2.5-coder", false, true}, // OpenAI-compatible prefix, unknown model → fallback
		{"unknown", false, true},             // safe default
	}
	for _, c := range cases {
		tk := ForModel(c.model)
		if _, ok := tk.(*Tiktoken); ok != c.isTik {
			t.Errorf("ForModel(%q) tiktoken match = %v, want %v (%T)", c.model, ok, c.isTik, tk)
		}
		if _, ok := tk.(Heuristic); ok != c.isH {
			t.Errorf("ForModel(%q) heuristic match = %v, want %v (%T)", c.model, ok, c.isH, tk)
		}
	}
}

func TestCountMessagesIncludesAllBlockKinds(t *testing.T) {
	tk := Heuristic{}
	msgs := []ports.Message{
		{Role: ports.RoleUser, Blocks: []ports.Block{
			{Kind: ports.BlockText, Text: "please read the file"},
		}},
		{Role: ports.RoleAssistant, Blocks: []ports.Block{
			{Kind: ports.BlockThinking, Text: "I should call read"},
			{Kind: ports.BlockToolCall, ToolName: "read", ToolInput: []byte(`{"file_path":"a.go"}`)},
		}},
		{Role: ports.RoleUser, Blocks: []ports.Block{
			{Kind: ports.BlockToolResult, ToolResultText: "package main"},
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

// --- Change 0005 / T-450..T-451: tiktoken-backed Tokenizer ---

// TestTiktokenCountMatchesReference pins the new Tiktoken.Count against
// the published tiktoken number for "Hello, world!" on cl100k_base (the
// canonical OpenAI encoding for gpt-4 / gpt-3.5-turbo). tiktoken-go
// returns 4 tokens for this string on both cl100k_base and o200k_base —
// ["Hello", ",", " world", "!"] — verified empirically against v0.1.8.
//
// RED until Tiktoken exists.
func TestTiktokenCountMatchesReference(t *testing.T) {
	tk, err := NewTiktoken("openai/gpt-4")
	if err != nil {
		t.Fatalf("NewTiktoken: %v", err)
	}
	if got := tk.Count("Hello, world!"); got != 4 {
		t.Errorf("Count(\"Hello, world!\") = %d, want 4 (cl100k_base reference)", got)
	}
}

// TestTiktokenRoundTrip pins that the BPE doesn't mangle input: encode
// then decode returns the original string. The decode isn't part of the
// Tokenizer port but the test exercises the underlying *tiktoken.Tiktoken
// through the same handle the port will hold.
//
// RED until NewTiktoken exists.
func TestTiktokenRoundTrip(t *testing.T) {
	tk, err := NewTiktoken("openai/gpt-4")
	if err != nil {
		t.Fatalf("NewTiktoken: %v", err)
	}
	// Sanity: Count > 0 means an encoding happened, which means decode
	// has tokens to operate on.
	if n := tk.Count("the quick brown fox"); n <= 0 {
		t.Fatalf("Count = %d, want > 0", n)
	}
	// The Encode/Decode pair on the underlying *tiktoken.Tiktoken is
	// the round-trip we care about. Since Tiktoken embeds it, the
	// integration test exercises the actual library, not a re-implementation.
	decoded := tk.inner.Decode(tk.inner.Encode("the quick brown fox", nil, nil))
	if decoded != "the quick brown fox" {
		t.Errorf("Decode(Encode(s)) = %q, want %q", decoded, "the quick brown fox")
	}
}

// --- Change 0005 / T-453: ForModel picks tiktoken for OpenAI ---

// TestForModelPicksTiktokenForOpenAI pins that ForModel("openai/...")
// returns the new Tiktoken implementation, while anthropic stays on Heuristic.
//
// RED until ForModel dispatches to NewTiktoken.
func TestForModelPicksTiktokenForOpenAI(t *testing.T) {
	tk := ForModel("openai/gpt-4")
	if _, ok := tk.(*Tiktoken); !ok {
		t.Errorf("ForModel(openai/gpt-4) = %T, want *Tiktoken", tk)
	}
	tk = ForModel("anthropic/claude-sonnet-5")
	if _, ok := tk.(Heuristic); !ok {
		t.Errorf("ForModel(anthropic/...) = %T, want Heuristic", tk)
	}
}

// --- Change 0005 / T-455: ForModel falls back to Heuristic on unknown ---

// TestForModelFallsBackOnUnknownModel pins that a model prefix tiktoken-go
// doesn't know about returns Heuristic rather than panicking.
//
// RED until ForModel handles unknown models.
func TestForModelFallsBackOnUnknownModel(t *testing.T) {
	tk := ForModel("local/qwen2.5-coder")
	if _, ok := tk.(Heuristic); !ok {
		t.Errorf("ForModel(local/qwen2.5-coder) = %T, want Heuristic (fallback)", tk)
	}
}

// --- Change 0005 / T-456: NewTiktoken sets TIKTOKEN_CACHE_DIR ---

// TestTiktokenInitSetsCacheDir pins that NewTiktoken persists the BPE
// across processes by setting TIKTOKEN_CACHE_DIR to a sane local path
// when unset. The local-first guarantee (ADR-0001) depends on this.
//
// RED until ensureCacheDir is wired.
func TestTiktokenInitSetsCacheDir(t *testing.T) {
	t.Setenv("TIKTOKEN_CACHE_DIR", "")
	if err := ensureCacheDir(); err != nil {
		t.Fatalf("ensureCacheDir: %v", err)
	}
	got := os.Getenv("TIKTOKEN_CACHE_DIR")
	if got == "" {
		t.Fatal("TIKTOKEN_CACHE_DIR still unset after ensureCacheDir")
	}
	if !strings.HasSuffix(got, "openplus/tiktoken") {
		t.Errorf("TIKTOKEN_CACHE_DIR = %q, want suffix openplus/tiktoken", got)
	}
}

// TestTiktokenInitRespectsExistingCacheDir: when TIKTOKEN_CACHE_DIR is set
// in the environment, ensureCacheDir must not override it. (Local-first
// opt-in.)
func TestTiktokenInitRespectsExistingCacheDir(t *testing.T) {
	t.Setenv("TIKTOKEN_CACHE_DIR", "/tmp/my-tiktoken")
	if err := ensureCacheDir(); err != nil {
		t.Fatalf("ensureCacheDir: %v", err)
	}
	if got := os.Getenv("TIKTOKEN_CACHE_DIR"); got != "/tmp/my-tiktoken" {
		t.Errorf("TIKTOKEN_CACHE_DIR = %q, want /tmp/my-tiktoken (preserved)", got)
	}
}

// compile-time: Tiktoken satisfies Tokenizer.
var _ Tokenizer = (*Tiktoken)(nil)

// TestTiktokenOfflineBuildSeparation: the offline loader must NOT be
// pulled into the default build. This is a build-time property — the
// only way to check it is by inspecting the package's source files for
// the build tag. If a future refactor removes the //go:build offline
// constraint, the default build will start pulling tiktoken-go-loader.
//
// RED if a future change removes the build tag or adds a default-build
// reference to tiktoken-go-loader. The test passes today because the
// default build does not compile tiktoken_offline.go (the file is
// gated by the offline tag).
func TestTiktokenOfflineBuildSeparation(t *testing.T) {
	data, err := os.ReadFile("tiktoken_offline.go")
	if err != nil {
		t.Fatalf("read tiktoken_offline.go: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, "//go:build offline") {
		t.Errorf("tiktoken_offline.go missing //go:build offline tag; the offline loader would now compile into the default build")
	}
	if !strings.Contains(src, "tiktoken-go-loader") {
		t.Errorf("tiktoken_offline.go no longer references tiktoken-go-loader; the offline build would break")
	}
}
