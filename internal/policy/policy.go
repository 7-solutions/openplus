// Package policy implements the PolicyGate port (ADR-0007). Local mode uses
// allow/ask/deny rules; server mode verifies an ES256 claim from a control
// plane. Rules support glob matching on tool name and path argument, with
// last-match-wins ordering. Ask decisions are resolved by an injected
// Prompter (with the caller's context deadline acting as the forced-ask
// timeout); when no Prompter is wired, Ask safely degrades to Deny.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/7solutions/openplus/internal/glob"
	"github.com/7solutions/openplus/internal/provider"
)

type Decision int

const (
	Allow Decision = iota
	Ask
	Deny
)

// Gate is the port the agent loop calls before executing any tool call.
type Gate interface {
	Permit(ctx context.Context, call provider.ToolCall) (Decision, error)
}

// AllowAll is a scaffold gate that permits everything. Equivalent to
// --dangerously-skip-permissions with no rules layered on top — use only
// in tests and local scaffolding, never as a shipped default.
type AllowAll struct{}

func (AllowAll) Permit(ctx context.Context, call provider.ToolCall) (Decision, error) {
	return Allow, nil
}

// DenyList denies any tool call whose name is in Denied, allows everything
// else. A minimal, illustrative precursor to the full rule engine.
type DenyList struct {
	Denied map[string]bool
}

func (d DenyList) Permit(ctx context.Context, call provider.ToolCall) (Decision, error) {
	if d.Denied[call.Name] {
		return Deny, nil
	}
	return Allow, nil
}

// Rule is one permission rule. A call matches if the tool name matches ToolGlob
// (empty = any tool) AND a path argument, if the rule specifies one, matches
// PathGlob (empty = any/no path). Rules are evaluated in list order; the last
// matching rule wins.
type Rule struct {
	ToolGlob string // glob against the tool name; "" matches any
	PathGlob string // glob against a path argument; "" matches any/no path
	Decision Decision
}

// Rules is the rule engine. It implements Gate.
type Rules struct {
	Default Decision // decision when no rule matches
	List    []Rule
}

// Permit evaluates List against the call, last-match-wins, falling back to
// Default. It never returns an error for the rule match itself; Prompting
// (below) layers prompt resolution on top.
func (r Rules) Permit(ctx context.Context, call provider.ToolCall) (Decision, error) {
	path := extractPath(call.Input)
	result := r.Default
	for _, rule := range r.List {
		if ruleMatches(rule, call.Name, path) {
			result = rule.Decision // last match wins
		}
	}
	return result, nil
}

func ruleMatches(rule Rule, toolName, path string) bool {
	if rule.ToolGlob != "" {
		if ok, _ := filepath.Match(rule.ToolGlob, toolName); !ok {
			return false
		}
	}
	if rule.PathGlob != "" {
		// A path rule only matches when the call actually carries a path.
		if path == "" {
			return false
		}
		if !glob.Match(rule.PathGlob, path) {
			return false
		}
	}
	return true
}

// pathKeys are JSON argument keys treated as a path, in priority order.
var pathKeys = []string{"file_path", "path", "dir", "cwd"}

// extractPath pulls a path-like argument from a tool call's JSON input so path
// glob rules can apply. Returns "" when no path argument is present (or the
// input is not a JSON object).
func extractPath(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	for _, k := range pathKeys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// ParseDecision parses an allow/ask/deny string (case-insensitive).
func ParseDecision(s string) (Decision, error) {
	switch lower(s) {
	case "allow":
		return Allow, nil
	case "ask":
		return Ask, nil
	case "deny":
		return Deny, nil
	}
	return Allow, fmt.Errorf("policy: unknown decision %q", s)
}

func lower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// NewRules builds a deterministic Rules from decision maps: tool rules first
// (sorted by key), then path rules (sorted by key) — so path rules win over
// tool rules when both apply, via last-match-wins. Returns an error if any
// decision string is unrecognized.
func NewRules(def Decision, toolRules, pathRules map[string]string) (*Rules, error) {
	r := &Rules{Default: def}

	toolKeys := sortedKeys(toolRules)
	for _, k := range toolKeys {
		d, err := ParseDecision(toolRules[k])
		if err != nil {
			return nil, err
		}
		r.List = append(r.List, Rule{ToolGlob: k, Decision: d})
	}

	pathKeysSorted := sortedKeys(pathRules)
	for _, k := range pathKeysSorted {
		d, err := ParseDecision(pathRules[k])
		if err != nil {
			return nil, err
		}
		r.List = append(r.List, Rule{PathGlob: k, Decision: d})
	}
	return r, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Prompter asks an operator to approve an Ask decision. The context carries the
// forced-ask timeout (deadline). Implementations are UI seams (T-031 TUI).
type Prompter interface {
	Ask(ctx context.Context, call provider.ToolCall) (bool, error)
}

// Prompting wraps a Rules engine with a Prompter, resolving Ask decisions
// interactively. It implements Gate. When Prompter is nil, Ask safely degrades
// to Deny (never silently allow).
type Prompting struct {
	Rules    *Rules
	Prompter Prompter // optional; nil → Ask becomes Deny
}

// Permit delegates to Rules; on Ask it calls Prompter (if present) within ctx.
// A prompter denial or a ctx timeout yields Deny + the wrapped error.
func (p Prompting) Permit(ctx context.Context, call provider.ToolCall) (Decision, error) {
	d, err := p.Rules.Permit(ctx, call)
	if err != nil {
		return Deny, err
	}
	if d != Ask {
		return d, nil
	}
	if p.Prompter == nil {
		return Deny, nil
	}
	approved, err := p.Prompter.Ask(ctx, call)
	if err != nil {
		return Deny, err
	}
	if !approved {
		return Deny, nil
	}
	return Allow, nil
}

// Skip implements --dangerously-skip-permissions (T-024): an allow-all base
// where explicit rules still win, and prompts are skipped (Ask becomes Allow).
// It wraps a Rules engine whose Default is forced to Allow. Explicit Deny rules
// still deny; explicit Ask rules do not block. Use only for local/trusted runs.
type Skip struct {
	Rules *Rules
}

// Permit returns the rule decision with Ask promoted to Allow, over an
// allow-all base.
func (s Skip) Permit(ctx context.Context, call provider.ToolCall) (Decision, error) {
	d, err := s.Rules.Permit(ctx, call)
	if err != nil {
		return Deny, err
	}
	if d == Ask {
		return Allow, nil
	}
	return d, nil
}

// NewSkip builds a Skip gate from explicit rules over an allow-all base. Tool
// rules are evaluated first, then path rules (last-match-wins), so an explicit
// path deny still overrides the base. Returns an error on unknown decisions.
func NewSkip(toolRules, pathRules map[string]string) (*Skip, error) {
	r, err := NewRules(Allow, toolRules, pathRules)
	if err != nil {
		return nil, err
	}
	return &Skip{Rules: r}, nil
}
