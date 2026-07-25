package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/7solutions/openplus/internal/ports"
)

// Automatic diagnostics (change 0026, ADR-0017). The agent should see the
// breakage it just caused without having to ask for it, which is why the
// LanguageService is a port the runtime consumes and not only a tool the model
// calls.
//
// The flow: a mutating tool result records the file it touched; a refresh asks
// the language server what is wrong with the touched files; the next assembled
// context carries a bounded "# Diagnostics" section.
//
// Concurrency: the tool-result hook fires on the agent goroutine
// (internal/agent/loop.go), so it does the cheapest possible thing — record a
// path under a mutex. The refresh itself runs on its own goroutine and the
// loop never waits for it. Whatever has landed by the time context is
// assembled is what the model sees; a slow language server costs freshness,
// never latency.

const (
	// maxInjectedDiagnostics caps how many problems enter the prompt. A file
	// with hundreds of errors usually has one root cause, and the rest would
	// crowd out retrieved context for no gain.
	maxInjectedDiagnostics = 20

	// maxDiagnosticMessage truncates a single message. Some type errors are
	// paragraphs long.
	maxDiagnosticMessage = 300
)

// mutatingTools are the tools whose results can change what a language server
// would say. A read cannot, so it must not schedule work.
var mutatingTools = map[string]bool{
	"write": true,
	"edit":  true,
	"bash":  true,
}

// noteEditedFile records that a file may have changed.
func (s *Session) noteEditedFile(path string) {
	if path == "" {
		return
	}
	s.diagMu.Lock()
	defer s.diagMu.Unlock()
	if s.editedFiles == nil {
		s.editedFiles = map[string]bool{}
	}
	s.editedFiles[path] = true
}

// hasEditedFiles reports whether any file is waiting for a refresh.
func (s *Session) hasEditedFiles() bool {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()
	return len(s.editedFiles) > 0
}

// takeEditedFiles drains the pending set. Draining rather than reading keeps
// the refresh idempotent: a file is re-queried only after it changes again.
func (s *Session) takeEditedFiles() []string {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()
	if len(s.editedFiles) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.editedFiles))
	for p := range s.editedFiles {
		out = append(out, p)
	}
	s.editedFiles = map[string]bool{}
	sort.Strings(out) // deterministic order in the prompt
	return out
}

// refreshDiagnostics queries the language server for every file touched since
// the last refresh and caches the result for the next context assembly.
//
// It is synchronous: callers on the hot path use refreshDiagnosticsAsync.
func (s *Session) refreshDiagnostics(ctx context.Context) {
	if s.LanguageService == nil {
		return
	}
	paths := s.takeEditedFiles()
	if len(paths) == 0 {
		return
	}

	fresh := make(map[string][]ports.Diagnostic, len(paths))
	for _, p := range paths {
		diags, err := s.LanguageService.Diagnostics(ctx, p)
		if err != nil {
			// Diagnostics are enrichment. A failure costs this file's
			// feedback, not the turn.
			continue
		}
		fresh[p] = diags
	}

	s.diagMu.Lock()
	if s.diagnostics == nil {
		s.diagnostics = map[string][]ports.Diagnostic{}
	}
	for p, d := range fresh {
		if len(d) == 0 {
			// A file that got fixed must stop being reported.
			delete(s.diagnostics, p)
			continue
		}
		s.diagnostics[p] = d
	}
	s.diagMu.Unlock()
}

// refreshDiagnosticsAsync runs a refresh without blocking the caller. It is
// what the tool-result hook uses, because that hook runs on the agent
// goroutine and must never stall the loop.
//
// The context is deliberately not the tool call's: that context is cancelled
// when the call returns, which would abort the refresh we just started.
func (s *Session) refreshDiagnosticsAsync() {
	if s.LanguageService == nil || !s.hasEditedFiles() {
		return
	}
	go s.refreshDiagnostics(context.Background())
}

// toolResultHook returns the callback handed to the agent loop. It composes the
// caller's render hook with the diagnostics trigger, and returns nil when
// neither is needed so the loop does no per-tool-call work for nothing.
func (s *Session) toolResultHook() func(ports.ToolCall, ports.Block) {
	user := s.OnToolResult
	if s.LanguageService == nil {
		return user // may be nil; the loop handles that
	}
	return func(call ports.ToolCall, result ports.Block) {
		if user != nil {
			user(call, result)
		}
		if !mutatingTools[call.Name] {
			return
		}
		if p := editedPath(call); p != "" {
			s.noteEditedFile(p)
		}
		s.refreshDiagnosticsAsync()
	}
}

// editedPath extracts the file a tool call touched. Bash has no path argument,
// so a bash call refreshes whatever is already pending rather than naming a new
// file — a build or a formatter can change files we cannot enumerate from here.
func editedPath(call ports.ToolCall) string {
	if len(call.Input) == 0 {
		return ""
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return ""
	}
	return in.Path
}

// diagnosticsSection renders the cached diagnostics for the prompt, or "" when
// there is nothing to report. Silence is the correct signal for working code:
// an empty "# Diagnostics" heading would read as a failed lookup.
func (s *Session) diagnosticsSection() string {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()

	if len(s.diagnostics) == 0 {
		return ""
	}

	paths := make([]string, 0, len(s.diagnostics))
	for p := range s.diagnostics {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var (
		b       strings.Builder
		written int
		total   int
	)
	for _, p := range paths {
		total += len(s.diagnostics[p])
	}

	b.WriteString("# Diagnostics\n")
	b.WriteString("Problems reported by the language server for files you changed.\n")
	for _, p := range paths {
		for _, d := range s.diagnostics[p] {
			if written == maxInjectedDiagnostics {
				break
			}
			msg := d.Message
			if len(msg) > maxDiagnosticMessage {
				msg = msg[:maxDiagnosticMessage] + "…"
			}
			fmt.Fprintf(&b, "%s:%d:%d: %s: %s\n", d.Path, d.Line, d.Column, d.Severity, msg)
			written++
		}
	}
	if total > written {
		fmt.Fprintf(&b, "(%d more not shown)\n", total-written)
	}
	return strings.TrimRight(b.String(), "\n")
}
