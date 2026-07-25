package orchestrate

// Max Mode: best-of-N generation with a judge pick (change 0016, ADR-0011).
//
// Sampling reuses Runner's bounded fan-out, so parallelism, cancellation, and
// partial-failure behavior are the same ones subagents already have. Everything
// here talks to the neutral Provider port only: no provider-specific type
// crosses this boundary, and the port itself is unchanged — Max Mode is pure
// orchestration, not a provider feature.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/7solutions/openplus/internal/ports"
)

// DefaultSamples is the N used when the caller names none. Three is enough for a
// judge to have a real choice without tripling cost for no reason.
const DefaultSamples = 3

// MaxSamples caps N. Each sample is a full generation the user pays for, so the
// ceiling is explicit rather than left to whatever number gets typed.
const MaxSamples = 8

// ClampSamples resolves a requested N to the effective one, returning a non-empty
// note when the request was changed. A note means the user must be told: silently
// running fewer samples than asked for would misrepresent what they got.
func ClampSamples(n int) (int, string) {
	if n < 1 {
		return DefaultSamples, ""
	}
	if n > MaxSamples {
		return MaxSamples, fmt.Sprintf("requested %d samples, clamped to the maximum of %d", n, MaxSamples)
	}
	return n, ""
}

// Candidate is one sampled answer. Err records that this generation failed, in
// which case Text is empty and the candidate is not offered to the judge —
// siblings are unaffected.
type Candidate struct {
	// Index is the candidate's stable position in the sample set.
	Index int
	// Text is the generated answer.
	Text string
	// Rationale is the judge's reason for choosing this candidate. Set only on
	// the winner returned by MaxMode.Run.
	Rationale string
	// Err is this generation's own failure.
	Err error
}

// Sampler produces N independent answers to the same request.
type Sampler struct {
	// Provider is the generating model backend.
	Provider ports.Provider
	// Runner supplies the bounded fan-out. Its Isolator is deliberately unused:
	// generations do not touch the filesystem, so isolation buys nothing.
	Runner Runner
}

// Sample runs n tool-free generations of req concurrently, bounded by the
// Runner's MaxParallel, and returns them in stable index order regardless of
// completion timing. A generation's failure is recorded on its candidate rather
// than aborting the set: N-1 usable answers still beat none.
func (s Sampler) Sample(ctx context.Context, req ports.Request, n int) ([]Candidate, error) {
	if n < 1 {
		return nil, fmt.Errorf("orchestrate: sample count must be >= 1, got %d", n)
	}
	if s.Provider == nil {
		return nil, fmt.Errorf("orchestrate: sampler has no provider configured")
	}

	// Tool-free: a candidate answers, it does not act. Copy so the caller's
	// Request (which may carry the session's tools) is left alone.
	gen := req
	gen.Tools = nil

	tasks := make([]Task, n)
	for i := 0; i < n; i++ {
		tasks[i] = Task{
			ID: fmt.Sprintf("candidate-%d", i),
			Run: func(ctx context.Context, _ string) (string, error) {
				return generateText(ctx, s.Provider, gen)
			},
		}
	}

	results, err := s.Runner.RunAll(ctx, tasks)
	if err != nil {
		return nil, fmt.Errorf("orchestrate: sample: %w", err)
	}

	cands := make([]Candidate, len(results))
	for i, res := range results {
		cands[i] = Candidate{Index: i, Text: res.Output, Err: res.Err}
	}
	return cands, nil
}

// generateText streams one generation and accumulates its text. A streamed error
// event fails the generation: a truncated answer must not be judged as if it were
// complete.
func generateText(ctx context.Context, p ports.Provider, req ports.Request) (string, error) {
	events, err := p.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for ev := range events {
		switch ev.Kind {
		case ports.EventTextDelta:
			out.WriteString(ev.Text)
		case ports.EventError:
			return "", ev.Err
		}
	}
	return out.String(), nil
}

// rankerSystemPrompt instructs the judge to answer machine-readably. Like the
// goal Judge, the ranker has its own model, no tools, and no ability to act.
const rankerSystemPrompt = `You are an independent judge. You are given a PROMPT and several numbered CANDIDATE answers to it.
Choose the single best candidate.

Answer with exactly one line, then your reasoning:
BEST: <candidate number>
<one or two sentences of justification>

Choose exactly one number. If two candidates are genuinely equal, choose the lower number.`

// Ranker asks a judge model to pick the best candidate. It is a sibling of Judge:
// an independent model that renders a verdict and nothing more.
type Ranker struct {
	// Provider is the judge's model backend — independent of the sampler's.
	Provider ports.Provider
	// Model is the judge model id ("<provider>/<model>").
	Model string
}

// Rank returns the index of the best candidate and the judge's rationale.
// Candidates that failed to generate are not offered. An unparseable or
// out-of-range answer is an error: silently defaulting to candidate 0 would
// present an unjudged answer as the judged winner.
func (r Ranker) Rank(ctx context.Context, prompt string, cands []Candidate) (int, string, error) {
	if len(cands) == 0 {
		return 0, "", fmt.Errorf("orchestrate: rank needs at least one candidate")
	}
	if r.Provider == nil {
		return 0, "", fmt.Errorf("orchestrate: ranker has no provider configured")
	}

	usable := make([]int, 0, len(cands))
	for _, c := range cands {
		if c.Err == nil && strings.TrimSpace(c.Text) != "" {
			usable = append(usable, c.Index)
		}
	}
	switch len(usable) {
	case 0:
		return 0, "", fmt.Errorf("orchestrate: rank: every candidate failed to generate")
	case 1:
		return usable[0], "only one candidate generated successfully", nil
	}

	req := ports.Request{
		Model:  r.Model,
		System: rankerSystemPrompt,
		Messages: []ports.Message{{
			Role: ports.RoleUser,
			Blocks: []ports.Block{{
				Kind: ports.BlockText,
				Text: renderCandidates(prompt, cands),
			}},
		}},
		// No tools: the judge decides, it does not act.
	}

	reply, err := generateText(ctx, r.Provider, req)
	if err != nil {
		return 0, "", fmt.Errorf("orchestrate: rank: %w", err)
	}

	best, rationale, err := parseRanking(reply, usable)
	if err != nil {
		return 0, "", err
	}
	return best, rationale, nil
}

// renderCandidates builds the judge's prompt. Failed candidates are omitted
// entirely — the judge cannot rank an answer that does not exist, and offering an
// empty one invites it to pick the gap.
func renderCandidates(prompt string, cands []Candidate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PROMPT:\n%s\n", prompt)
	for _, c := range cands {
		if c.Err != nil || strings.TrimSpace(c.Text) == "" {
			continue
		}
		fmt.Fprintf(&b, "\nCANDIDATE %d:\n%s\n", c.Index, c.Text)
	}
	return b.String()
}

// parseRanking extracts the chosen index from the judge's reply. It prefers an
// explicit "BEST: n" line and otherwise takes the first number in the text, which
// also gives the tie rule for free: a reply naming several candidates yields the
// first one mentioned, and the prompt asks for the lower of a tie.
//
// The index must be one the judge was actually offered; anything else is an error.
func parseRanking(reply string, offered []int) (int, string, error) {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return 0, "", fmt.Errorf("orchestrate: rank: the judge returned nothing")
	}

	idx, ok := firstInt(trimmed)
	if !ok {
		return 0, "", fmt.Errorf("orchestrate: rank: cannot read a candidate number from the judge's answer: %q", trimmed)
	}
	for _, o := range offered {
		if o == idx {
			return idx, rationaleFrom(trimmed), nil
		}
	}
	return 0, "", fmt.Errorf("orchestrate: rank: the judge chose candidate %d, which is not among the offered candidates %v", idx, offered)
}

// firstInt returns the first non-negative integer appearing in s. A leading "-"
// is not consumed, so "BEST: -1" yields 1 and is then rejected as out of range
// only if 1 was not offered — which is why the caller validates against the
// offered set rather than trusting the number.
func firstInt(s string) (int, bool) {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			continue
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		n, err := strconv.Atoi(s[i:j])
		if err != nil {
			return 0, false
		}
		// A negative index is a malformed answer, not candidate n.
		if i > 0 && s[i-1] == '-' {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// rationaleFrom drops the machine-readable first line, leaving the judge's
// reasoning. A one-line answer is its own rationale.
func rationaleFrom(reply string) string {
	if _, rest, ok := strings.Cut(reply, "\n"); ok {
		if r := strings.TrimSpace(rest); r != "" {
			return r
		}
	}
	return strings.TrimSpace(reply)
}

// MaxMode is best-of-N: sample, rank, return the winner.
type MaxMode struct {
	Sampler Sampler
	Ranker  Ranker
}

// Run samples n candidates and returns the single best, carrying the judge's
// rationale. A ranking failure is an error rather than a fallback to candidate 0:
// without a verdict there is no "best", and presenting an arbitrary answer as the
// judged winner would be a lie.
//
// n == 1 skips the judge — there is nothing to compare.
func (m MaxMode) Run(ctx context.Context, req ports.Request, n int) (Candidate, error) {
	cands, err := m.Sampler.Sample(ctx, req, n)
	if err != nil {
		return Candidate{}, err
	}

	if len(cands) == 1 {
		if cands[0].Err != nil {
			return Candidate{}, fmt.Errorf("orchestrate: max: the only candidate failed: %w", cands[0].Err)
		}
		return cands[0], nil
	}

	best, rationale, err := m.Ranker.Rank(ctx, promptOf(req), cands)
	if err != nil {
		return Candidate{}, err
	}
	for _, c := range cands {
		if c.Index == best {
			c.Rationale = rationale
			return c, nil
		}
	}
	return Candidate{}, fmt.Errorf("orchestrate: max: ranked candidate %d is missing from the sample set", best)
}

// promptOf renders the request as the prompt text the judge is shown. The judge
// ranks answers to a question, so it needs the question — flattened the same way
// the goal Judge flattens a transcript.
func promptOf(req ports.Request) string {
	return strings.TrimSpace(flattenHistory(req.Messages))
}
