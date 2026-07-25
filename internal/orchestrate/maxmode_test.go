package orchestrate

import (
	"context"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/7solutions/openplus/internal/provider"
)

// textProvider streams one fixed reply per call, cycling through replies, and
// records how many calls overlap so the concurrency cap can be asserted.
type textProvider struct {
	replies []string

	mu       sync.Mutex
	calls    int
	inFlight int32
	peak     int32
	// block, when non-nil, holds every call until closed — that keeps
	// generations overlapping long enough for the peak to be meaningful.
	block chan struct{}
}

func (p *textProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	p.mu.Lock()
	i := p.calls
	p.calls++
	p.mu.Unlock()

	n := atomic.AddInt32(&p.inFlight, 1)
	for {
		peak := atomic.LoadInt32(&p.peak)
		if n <= peak || atomic.CompareAndSwapInt32(&p.peak, peak, n) {
			break
		}
	}
	if p.block != nil {
		<-p.block
	}
	atomic.AddInt32(&p.inFlight, -1)

	reply := ""
	if len(p.replies) > 0 {
		reply = p.replies[i%len(p.replies)]
	}

	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Kind: provider.EventTextDelta, Text: reply}
	ch <- provider.Event{Kind: provider.EventTurnEnd}
	close(ch)
	return ch, nil
}

// askProvider is a request for one text generation, used to build a Request.
func askProvider(text string) provider.Request {
	return provider.Request{
		Model: "fake/fake",
		Messages: []provider.Message{{
			Role:   provider.RoleUser,
			Blocks: []provider.Block{{Kind: provider.BlockText, Text: text}},
		}},
	}
}

// T-1610: Sample runs n generations and returns n candidates in stable index order.
func TestSamplerSampleReturnsNCandidates(t *testing.T) {
	p := &textProvider{replies: []string{"alpha", "beta", "gamma"}}
	s := Sampler{Provider: p}

	cands, err := s.Sample(context.Background(), askProvider("q"), 3)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(cands) != 3 {
		t.Fatalf("got %d candidates, want 3", len(cands))
	}
	for i, c := range cands {
		if c.Index != i {
			t.Errorf("candidate %d has Index %d", i, c.Index)
		}
		if c.Err != nil {
			t.Errorf("candidate %d: %v", i, c.Err)
		}
		if strings.TrimSpace(c.Text) == "" {
			t.Errorf("candidate %d has no text", i)
		}
	}
}

// T-1610: sampling is tool-free — a candidate must not act, only answer.
func TestSamplerStripsTools(t *testing.T) {
	var sawTools bool
	p := &rankProvider{onRequest: func(req provider.Request) {
		if len(req.Tools) > 0 {
			sawTools = true
		}
	}}
	s := Sampler{Provider: p}
	req := askProvider("q")
	req.Tools = []provider.ToolSchema{{Name: "bash"}}

	if _, err := s.Sample(context.Background(), req, 2); err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if sawTools {
		t.Error("Sample passed tools to the provider; generations must be tool-free")
	}
	if len(req.Tools) != 1 {
		t.Error("Sample mutated the caller's Request")
	}
}

// T-1610: n < 1 is a programming error, not a silent empty result.
func TestSamplerRejectsBadN(t *testing.T) {
	s := Sampler{Provider: &textProvider{replies: []string{"a"}}}
	if _, err := s.Sample(context.Background(), askProvider("q"), 0); err == nil {
		t.Error("Sample(n=0) should error")
	}
	if _, err := (Sampler{}).Sample(context.Background(), askProvider("q"), 2); err == nil {
		t.Error("Sample with no provider should error")
	}
}

// T-1611: concurrency is bounded by MaxParallel and every goroutine exits.
func TestSamplerBoundedParallelism(t *testing.T) {
	block := make(chan struct{})
	p := &textProvider{replies: []string{"a"}, block: block}
	s := Sampler{Provider: p, Runner: Runner{MaxParallel: 2}}

	done := make(chan struct{})
	var (
		cands []Candidate
		err   error
	)
	go func() {
		cands, err = s.Sample(context.Background(), askProvider("q"), 6)
		close(done)
	}()

	// Let the first slots fill, then release everything.
	for atomic.LoadInt32(&p.inFlight) < 2 {
	}
	close(block)
	<-done

	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(cands) != 6 {
		t.Fatalf("got %d candidates, want 6", len(cands))
	}
	if peak := atomic.LoadInt32(&p.peak); peak > 2 {
		t.Errorf("peak concurrency %d exceeds MaxParallel 2", peak)
	}
	if left := atomic.LoadInt32(&p.inFlight); left != 0 {
		t.Errorf("%d generations still in flight after Sample returned", left)
	}
}

// T-1612: one failing generation still returns the others, with the failure
// recorded on that candidate.
func TestSamplerPartialFailure(t *testing.T) {
	p := &provider.Fake{Scripts: [][]provider.Event{
		{{Kind: provider.EventTextDelta, Text: "ok one"}, {Kind: provider.EventTurnEnd}},
		{{Kind: provider.EventError, Err: errJudgeBoom}},
		{{Kind: provider.EventTextDelta, Text: "ok three"}, {Kind: provider.EventTurnEnd}},
	}}
	s := Sampler{Provider: p}

	cands, err := s.Sample(context.Background(), askProvider("q"), 3)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(cands) != 3 {
		t.Fatalf("got %d candidates, want 3", len(cands))
	}

	// Which candidate index draws the failing script depends on goroutine
	// scheduling, so assert the set: one failure, two usable answers, and the
	// failure confined to its own candidate.
	failed, ok := 0, 0
	for _, c := range cands {
		switch {
		case c.Err != nil:
			failed++
			if c.Text != "" {
				t.Errorf("candidate %d failed but carries text %q", c.Index, c.Text)
			}
		case strings.HasPrefix(c.Text, "ok"):
			ok++
		default:
			t.Errorf("candidate %d: no error and no text", c.Index)
		}
	}
	if failed != 1 || ok != 2 {
		t.Errorf("got %d failed / %d ok, want 1 / 2", failed, ok)
	}
}

// rankProvider inspects each Request and replies with fixed text.
type rankProvider struct {
	onRequest func(provider.Request)
	reply     string

	mu    sync.Mutex
	seen  []provider.Request
	calls int
}

func (p *rankProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	p.mu.Lock()
	p.seen = append(p.seen, req)
	p.calls++
	p.mu.Unlock()
	if p.onRequest != nil {
		p.onRequest(req)
	}
	reply := p.reply
	if reply == "" {
		reply = "text"
	}
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Kind: provider.EventTextDelta, Text: reply}
	ch <- provider.Event{Kind: provider.EventTurnEnd}
	close(ch)
	return ch, nil
}

func threeCandidates() []Candidate {
	return []Candidate{
		{Index: 0, Text: "first answer"},
		{Index: 1, Text: "second answer"},
		{Index: 2, Text: "third answer"},
	}
}

// T-1620: the judge's pick becomes the returned index, with its rationale.
func TestRankerPicksJudgeIndex(t *testing.T) {
	p := &rankProvider{reply: "BEST: 1\nsecond answer is the most complete"}
	r := Ranker{Provider: p, Model: "fake/judge"}

	best, rationale, err := r.Rank(context.Background(), "q", threeCandidates())
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if best != 1 {
		t.Fatalf("best = %d, want 1", best)
	}
	if !strings.Contains(rationale, "complete") {
		t.Errorf("rationale = %q", rationale)
	}
	// The judge sees every candidate, and never gets tools.
	req := p.seen[0]
	for _, want := range []string{"first answer", "second answer", "third answer"} {
		if !strings.Contains(req.Messages[0].Blocks[0].Text, want) {
			t.Errorf("judge prompt missing %q", want)
		}
	}
	if len(req.Tools) != 0 {
		t.Error("the judge must not receive tools")
	}
}

// T-1621: a judge answer naming a tie picks the lowest index it names.
func TestRankerTiePicksLowestIndex(t *testing.T) {
	p := &rankProvider{reply: "candidates 0 and 1 are equally good"}
	r := Ranker{Provider: p, Model: "fake/judge"}

	best, _, err := r.Rank(context.Background(), "q", threeCandidates())
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if best != 0 {
		t.Fatalf("tie picked %d, want lowest index 0", best)
	}
}

// T-1622: unparseable or out-of-range answers error rather than defaulting.
func TestRankerRejectsBadAnswer(t *testing.T) {
	for _, reply := range []string{
		"I cannot decide",
		"BEST: 5",
		"BEST: -1",
		"",
	} {
		p := &rankProvider{reply: reply}
		r := Ranker{Provider: p, Model: "fake/judge"}
		if best, _, err := r.Rank(context.Background(), "q", threeCandidates()); err == nil {
			t.Errorf("Rank(%q) = %d, want an error", reply, best)
		}
	}
}

// T-1622: ranking needs candidates and a provider.
func TestRankerRejectsBadInput(t *testing.T) {
	r := Ranker{Provider: &rankProvider{reply: "BEST: 0"}, Model: "fake/judge"}
	if _, _, err := r.Rank(context.Background(), "q", nil); err == nil {
		t.Error("Rank with no candidates should error")
	}
	if _, _, err := (Ranker{}).Rank(context.Background(), "q", threeCandidates()); err == nil {
		t.Error("Rank with no provider should error")
	}
}

// T-1622: a candidate that failed to generate is not offered to the judge, and an
// all-failed set is an error rather than a pick among nothing.
func TestRankerSkipsFailedCandidates(t *testing.T) {
	p := &rankProvider{reply: "BEST: 2"}
	r := Ranker{Provider: p, Model: "fake/judge"}
	cands := threeCandidates()
	cands[0].Err = errJudgeBoom
	cands[0].Text = ""

	best, _, err := r.Rank(context.Background(), "q", cands)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if best != 2 {
		t.Fatalf("best = %d, want 2", best)
	}
	if strings.Contains(p.seen[0].Messages[0].Blocks[0].Text, "candidate 0") {
		t.Error("a failed candidate must not be offered to the judge")
	}

	allFailed := []Candidate{{Index: 0, Err: errJudgeBoom}, {Index: 1, Err: errJudgeBoom}}
	if _, _, err := r.Rank(context.Background(), "q", allFailed); err == nil {
		t.Error("Rank should error when every candidate failed")
	}
}

// T-1623: MaxMode samples, ranks, and returns exactly the winner.
func TestMaxModeReturnsBest(t *testing.T) {
	gen := &textProvider{replies: []string{"one", "two", "three"}}
	judge := &rankProvider{reply: "BEST: 2\nthird is best"}
	m := MaxMode{
		Sampler: Sampler{Provider: gen},
		Ranker:  Ranker{Provider: judge, Model: "fake/judge"},
	}

	best, err := m.Run(context.Background(), askProvider("q"), 3)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if best.Index != 2 {
		t.Fatalf("winner index = %d, want 2", best.Index)
	}
	// Which reply lands on which index is scheduling-dependent, but the winner's
	// text must be one the generator actually produced — not a blank or a mix.
	switch best.Text {
	case "one", "two", "three":
	default:
		t.Fatalf("winner text = %q, want one of the generated replies", best.Text)
	}
	// The text the judge saw for candidate 2 is the text returned.
	if !strings.Contains(judge.seen[0].Messages[0].Blocks[0].Text,
		"CANDIDATE 2:\n"+best.Text) {
		t.Errorf("winner text %q is not what the judge saw as candidate 2:\n%s",
			best.Text, judge.seen[0].Messages[0].Blocks[0].Text)
	}
	if best.Rationale == "" {
		t.Error("the winner should carry the judge's rationale")
	}
}

// T-1623: N=1 skips the judge — there is nothing to rank.
func TestMaxModeSingleCandidateSkipsJudge(t *testing.T) {
	gen := &textProvider{replies: []string{"only"}}
	judge := &rankProvider{reply: "BEST: 0"}
	m := MaxMode{Sampler: Sampler{Provider: gen}, Ranker: Ranker{Provider: judge, Model: "fake/judge"}}

	best, err := m.Run(context.Background(), askProvider("q"), 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if best.Text != "only" {
		t.Fatalf("text = %q", best.Text)
	}
	if judge.calls != 0 {
		t.Errorf("judge consulted %d time(s) for a single candidate", judge.calls)
	}
}

// T-1624: a ranking failure errors; it does not silently return candidate 0.
func TestMaxModeRankingFailureErrors(t *testing.T) {
	gen := &textProvider{replies: []string{"one", "two", "three"}}
	judge := &rankProvider{reply: "no idea"}
	m := MaxMode{Sampler: Sampler{Provider: gen}, Ranker: Ranker{Provider: judge, Model: "fake/judge"}}

	if best, err := m.Run(context.Background(), askProvider("q"), 3); err == nil {
		t.Fatalf("Run returned %+v, want an error", best)
	}
}

// T-1624: when every generation failed there is no winner to return.
func TestMaxModeAllCandidatesFailed(t *testing.T) {
	gen := &provider.Fake{Scripts: [][]provider.Event{
		{{Kind: provider.EventError, Err: errJudgeBoom}},
		{{Kind: provider.EventError, Err: errJudgeBoom}},
	}}
	judge := &rankProvider{reply: "BEST: 0"}
	m := MaxMode{
		Sampler: Sampler{Provider: gen, Runner: Runner{MaxParallel: 1}},
		Ranker:  Ranker{Provider: judge, Model: "fake/judge"},
	}
	if _, err := m.Run(context.Background(), askProvider("q"), 2); err == nil {
		t.Error("Run should error when every candidate failed")
	}
}

// T-1625: N is bounded — zero/absent means the default, over-cap clamps, and the
// clamp is reported so the user is not silently given fewer samples.
func TestClampSamples(t *testing.T) {
	cases := []struct {
		in       int
		want     int
		wantNote bool
	}{
		{in: 0, want: DefaultSamples},
		{in: -4, want: DefaultSamples},
		{in: 1, want: 1},
		{in: MaxSamples, want: MaxSamples},
		{in: MaxSamples + 1, want: MaxSamples, wantNote: true},
		{in: 99, want: MaxSamples, wantNote: true},
	}
	for _, c := range cases {
		got, note := ClampSamples(c.in)
		if got != c.want {
			t.Errorf("ClampSamples(%d) = %d, want %d", c.in, got, c.want)
		}
		if (note != "") != c.wantNote {
			t.Errorf("ClampSamples(%d) note = %q, wantNote %v", c.in, note, c.wantNote)
		}
	}
	if DefaultSamples != 3 {
		t.Errorf("DefaultSamples = %d, want 3", DefaultSamples)
	}
	if MaxSamples < DefaultSamples {
		t.Errorf("MaxSamples %d below DefaultSamples %d", MaxSamples, DefaultSamples)
	}
}

// T-1626: orchestration is provider-neutral — it imports the neutral provider
// package and nothing under it. A provider-specific import here would let a
// vendor type reach the orchestration layer (ADR-0005).
func TestOrchestrateImportsNoProviderAdapter(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	const neutral = `"github.com/7solutions/openplus/internal/provider"`
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range af.Imports {
			if strings.Contains(imp.Path.Value, "internal/provider/") && imp.Path.Value != neutral {
				t.Errorf("%s imports provider-specific package %s; orchestration must use the neutral port only",
					f, imp.Path.Value)
			}
		}
	}
}
