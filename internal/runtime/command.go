package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Command is one slash command. Run takes the session and the raw argument
// string (everything after the command name, trimmed at the ends only) and
// returns text to show the user.
//
// Commands return text rather than printing it, so the same command works in the
// TUI and the one-shot path without either front-end knowing about the other.
type Command struct {
	Name    string
	Usage   string
	Summary string
	Run     func(s *Session, args string) (string, error)
}

// commands is the dispatch table. Adding a command is one entry and cannot
// change dispatch behavior for the others.
func (s *Session) commands() map[string]Command {
	if s.extraCommands == nil {
		return builtinCommands
	}
	// Session-local registrations (tests, and future per-project commands)
	// shadow nothing: builtins win, so a plugin cannot hijack /help.
	merged := make(map[string]Command, len(builtinCommands)+len(s.extraCommands))
	for name, c := range s.extraCommands {
		merged[name] = c
	}
	for name, c := range builtinCommands {
		merged[name] = c
	}
	return merged
}

// registerCommand adds a session-local command. Used by tests and reserved for
// project-defined commands; it cannot override a builtin.
func (s *Session) registerCommand(c Command) {
	if s.extraCommands == nil {
		s.extraCommands = map[string]Command{}
	}
	s.extraCommands[c.Name] = c
}

// Dispatch runs input as a command when it begins with "/".
//
// handled reports whether input was a command *attempt*: it is false only for
// input the caller should run as a normal turn. An unknown command is handled
// (with an error) rather than falling through, because silently sending "/typo"
// to the model is worse than saying the command does not exist.
func (s *Session) Dispatch(ctx context.Context, input string) (output string, handled bool, err error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return "", false, nil
	}

	name, args := splitCommand(trimmed)
	if name == "" {
		return "", true, fmt.Errorf("runtime: no command given; try /help")
	}

	table := s.commands()
	cmd, ok := table[name]
	if !ok {
		return "", true, fmt.Errorf("runtime: unknown command /%s; known commands: %s",
			name, strings.Join(commandNames(table), ", "))
	}

	out, err := cmd.Run(s, args)
	if err != nil {
		return "", true, err
	}
	return out, true, nil
}

// splitCommand separates "/name rest of the args" into name and args. The args
// keep their internal spacing; only the ends are trimmed.
func splitCommand(trimmed string) (name, args string) {
	body := strings.TrimPrefix(trimmed, "/")
	name, args, found := strings.Cut(body, " ")
	if !found {
		return strings.TrimSpace(body), ""
	}
	return strings.TrimSpace(name), strings.TrimSpace(args)
}

// commandNames lists the table's commands, slash-prefixed and sorted so output
// is deterministic (a map range would make /help flaky).
func commandNames(table map[string]Command) []string {
	out := make([]string, 0, len(table))
	for name := range table {
		out = append(out, "/"+name)
	}
	sort.Strings(out)
	return out
}

// helpText renders the command list.
func helpText(table map[string]Command) string {
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("commands:\n")
	for _, name := range names {
		c := table[name]
		usage := c.Usage
		if usage == "" {
			usage = "/" + name
		}
		fmt.Fprintf(&b, "  %-28s %s\n", usage, c.Summary)
	}
	return b.String()
}
