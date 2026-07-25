// Package ports declares the ten seams the OpenPlus core depends on (T-004,
// design.md). It is the single place to read the architecture: the core talks to
// these interfaces, and every external system is an adapter behind one of them.
//
// The interfaces here are intentionally narrow restatements of the seams the
// concrete packages implement — internal/provider, internal/embed,
// internal/memory, internal/tool, internal/skills, internal/contextmgr,
// internal/policy, and internal/orchestrate. Keeping the catalogue separate lets
// a test depend on a seam without importing the implementation (and its
// dependencies) behind it.
package ports

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/7solutions/openplus/internal/provider"
)

// PortNames lists every declared port. The count is asserted in tests so a port
// cannot quietly disappear.
func PortNames() []string {
	return []string{
		"Provider", "Embedder", "MemoryStore", "Tool", "SkillIndex",
		"Tokenizer", "Budgeter", "Checkpointer", "PolicyGate", "Workflow",
	}
}

// Provider is the model backend seam (ADR-0005).
type Provider interface {
	Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error)
}

// Embedder turns text into vectors (ADR-0004).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
}

// MemoryStore persists and retrieves memory chunks (ADR-0003).
type MemoryStore interface {
	Write(ctx context.Context, text, source string) (int64, error)
	Search(ctx context.Context, query string, k int) ([]string, error)
}

// Tool is one callable capability exposed to the model.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// SkillIndex discovers and ranks skills (ADR-0002).
type SkillIndex interface {
	Rank(query string, k int) []string
	Find(name string) (string, bool)
}

// Tokenizer estimates token cost (ADR-0008).
type Tokenizer interface {
	Count(text string) int
}

// Budgeter decides what fits in the context window (ADR-0008).
type Budgeter interface {
	Fit(budget int, msgs []provider.Message) []provider.Message
}

// Checkpointer snapshots and restores session state (ADR-0008).
type Checkpointer interface {
	ShouldCheckpoint(used int) bool
	Save(state string) error
	Load() (string, error)
}

// PolicyGate authorizes tool calls (ADR-0007).
type PolicyGate interface {
	Permit(ctx context.Context, call provider.ToolCall) (bool, error)
}

// Workflow runs an ordered set of phases (ADR-0006).
type Workflow interface {
	Phases() []string
	Run(ctx context.Context) error
}

// --- no-op fakes ---
//
// Each fake is the cheapest thing that satisfies its port: no network, no disk,
// no clock. They exist so a test can exercise a collaborator's behavior without
// standing up a real adapter.

// FakeProvider streams a single TurnEnd.
type FakeProvider struct{}

func (FakeProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 1)
	ch <- provider.Event{Kind: provider.EventTurnEnd}
	close(ch)
	return ch, nil
}

// FakeEmbedder returns deterministic vectors of a fixed dimension.
type FakeEmbedder struct {
	Dimension int
}

func (f FakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, tx := range texts {
		v := make([]float32, f.Dimension)
		for j := range v {
			v[j] = float32(len(tx)+j) / 10
		}
		out[i] = v
	}
	return out, nil
}

func (f FakeEmbedder) Dim() int { return f.Dimension }

// FakeMemoryStore is an in-memory substring-matching store.
type FakeMemoryStore struct {
	chunks []string
}

func (m *FakeMemoryStore) Write(_ context.Context, text, _ string) (int64, error) {
	m.chunks = append(m.chunks, text)
	return int64(len(m.chunks)), nil
}

func (m *FakeMemoryStore) Search(_ context.Context, query string, k int) ([]string, error) {
	var out []string
	for _, c := range m.chunks {
		if len(out) == k {
			break
		}
		if strings.Contains(strings.ToLower(c), strings.ToLower(query)) {
			out = append(out, c)
		}
	}
	return out, nil
}

// FakeTool returns a canned result.
type FakeTool struct {
	ToolName string
	Result   string
}

func (f FakeTool) Name() string            { return f.ToolName }
func (f FakeTool) Description() string     { return "fake tool for tests" }
func (f FakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f FakeTool) Execute(context.Context, json.RawMessage) (string, error) {
	return f.Result, nil
}

// FakeSkillIndex matches skills by substring.
type FakeSkillIndex struct {
	Names []string
}

func (f FakeSkillIndex) Rank(query string, k int) []string {
	var out []string
	for _, n := range f.Names {
		if len(out) == k {
			break
		}
		if strings.Contains(strings.ToLower(query), strings.ToLower(n)) {
			out = append(out, n)
		}
	}
	return out
}

func (f FakeSkillIndex) Find(name string) (string, bool) {
	for _, n := range f.Names {
		if n == name {
			return n, true
		}
	}
	return "", false
}

// FakeTokenizer counts whitespace-separated words.
type FakeTokenizer struct{}

func (FakeTokenizer) Count(text string) int { return len(strings.Fields(text)) }

// FakeBudgeter passes every message through.
type FakeBudgeter struct{}

func (FakeBudgeter) Fit(_ int, msgs []provider.Message) []provider.Message { return msgs }

// FakeCheckpointer stores one state string in memory and never asks to
// checkpoint.
type FakeCheckpointer struct {
	state string
}

func (*FakeCheckpointer) ShouldCheckpoint(int) bool { return false }

func (c *FakeCheckpointer) Save(state string) error {
	c.state = state
	return nil
}

func (c *FakeCheckpointer) Load() (string, error) { return c.state, nil }

// FakePolicyGate allows everything unless DenyAll is set.
type FakePolicyGate struct {
	DenyAll bool
}

func (f FakePolicyGate) Permit(context.Context, provider.ToolCall) (bool, error) {
	return !f.DenyAll, nil
}

// FakeWorkflow reports named phases and honors cancellation.
type FakeWorkflow struct {
	PhaseNames []string
}

func (f FakeWorkflow) Phases() []string { return f.PhaseNames }

func (f FakeWorkflow) Run(ctx context.Context) error {
	for range f.PhaseNames {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

// Compile-time assertions: every port has a working fake. Breaking a port
// signature fails the build here rather than at some distant call site.
var (
	_ Provider     = FakeProvider{}
	_ Embedder     = FakeEmbedder{}
	_ MemoryStore  = (*FakeMemoryStore)(nil)
	_ Tool         = FakeTool{}
	_ SkillIndex   = FakeSkillIndex{}
	_ Tokenizer    = FakeTokenizer{}
	_ Budgeter     = FakeBudgeter{}
	_ Checkpointer = (*FakeCheckpointer)(nil)
	_ PolicyGate   = FakePolicyGate{}
	_ Workflow     = FakeWorkflow{}
)

// ErrNotImplemented is returned by fakes that deliberately refuse an operation.
var ErrNotImplemented = errors.New("ports: not implemented by this fake")
