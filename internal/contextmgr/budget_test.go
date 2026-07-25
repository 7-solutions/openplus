package contextmgr

import (
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/ports"
)

func userMsg(text string) ports.Message {
	return ports.Message{Role: ports.RoleUser, Blocks: []ports.Block{
		{Kind: ports.BlockText, Text: text},
	}}
}

func TestBudgeterFitsEverythingWhenBudgetIsAmple(t *testing.T) {
	b := Budgeter{Tokenizer: Heuristic{}, Budget: 100_000}
	in := Input{
		System:     "you are openplus",
		Task:       "T-061 budgeter",
		Progress:   "wrote the test first",
		Checkpoint: "earlier: implemented the tokenizer",
		Memory:     []string{"prefers TDD", "cgo-free build"},
		Recent:     []ports.Message{userMsg("hello"), userMsg("world")},
	}
	got := b.Fit(in)
	if got.System != in.System {
		t.Errorf("system dropped: %q", got.System)
	}
	if got.Task != in.Task || got.Progress != in.Progress {
		t.Errorf("task/progress dropped: %+v", got)
	}
	if got.Checkpoint != in.Checkpoint {
		t.Errorf("checkpoint dropped: %q", got.Checkpoint)
	}
	if len(got.Memory) != 2 {
		t.Errorf("memory = %v, want 2 entries", got.Memory)
	}
	if len(got.Recent) != 2 {
		t.Errorf("recent = %d messages, want 2", len(got.Recent))
	}
	if got.Dropped != 0 {
		t.Errorf("Dropped = %d, want 0", got.Dropped)
	}
}

// TestBudgeterKeepsSystemAndTaskUnderPressure proves the ADR-0008 priority
// order: system and the active task survive even when everything else is cut.
func TestBudgeterKeepsSystemAndTaskUnderPressure(t *testing.T) {
	sys := "SYSTEM PROMPT"
	task := "ACTIVE TASK"
	tk := Heuristic{}
	// budget just large enough for system + task + a sliver
	budget := tk.Count(sys) + tk.Count(task) + 8

	b := Budgeter{Tokenizer: tk, Budget: budget}
	got := b.Fit(Input{
		System:     sys,
		Task:       task,
		Progress:   strings.Repeat("progress detail. ", 200),
		Checkpoint: strings.Repeat("checkpoint detail. ", 200),
		Memory:     []string{strings.Repeat("memory. ", 200)},
		Recent:     []ports.Message{userMsg(strings.Repeat("recent. ", 200))},
	})
	if got.System != sys {
		t.Fatalf("system must never be dropped, got %q", got.System)
	}
	if got.Task != task {
		t.Fatalf("active task must survive, got %q", got.Task)
	}
	// lower-priority material must have been cut
	if got.Checkpoint != "" || len(got.Memory) != 0 || len(got.Recent) != 0 {
		t.Errorf("expected lower-priority material dropped, got %+v", got)
	}
	if got.Dropped == 0 {
		t.Error("Dropped should count the cut sections")
	}
}

// TestBudgeterPriorityOrder walks the budget up and asserts sections come back
// in exactly the ADR-0008 order: system, task+progress, checkpoint, memory,
// recent messages.
func TestBudgeterPriorityOrder(t *testing.T) {
	tk := Heuristic{}
	in := Input{
		System:     "sys",
		Task:       "task",
		Progress:   "prog",
		Checkpoint: "ckpt",
		Memory:     []string{"mem"},
		Recent:     []ports.Message{userMsg("recent")},
	}

	// With a tiny budget only system fits.
	tiny := Budgeter{Tokenizer: tk, Budget: tk.Count("sys")}.Fit(in)
	if tiny.System == "" {
		t.Fatal("system must fit even at minimum budget")
	}
	if tiny.Task != "" {
		t.Errorf("task should not fit at minimum budget: %q", tiny.Task)
	}

	// Growing the budget admits sections in priority order; checkpoint must
	// never appear before the task.
	for budget := 1; budget < 200; budget++ {
		got := Budgeter{Tokenizer: tk, Budget: budget}.Fit(in)
		if got.Checkpoint != "" && got.Task == "" {
			t.Fatalf("budget %d: checkpoint admitted before task", budget)
		}
		if len(got.Memory) > 0 && got.Checkpoint == "" {
			t.Fatalf("budget %d: memory admitted before checkpoint", budget)
		}
		if len(got.Recent) > 0 && len(got.Memory) == 0 {
			t.Fatalf("budget %d: recent admitted before memory", budget)
		}
	}
}

func TestBudgeterKeepsNewestRecentMessages(t *testing.T) {
	tk := Heuristic{}
	msgs := []ports.Message{
		userMsg("oldest message here"),
		userMsg("middle message here"),
		userMsg("newest message here"),
	}
	// budget for system + roughly one message
	b := Budgeter{Tokenizer: tk, Budget: tk.Count("sys") + CountMessages(tk, msgs[:1]) + 4}
	got := b.Fit(Input{System: "sys", Recent: msgs})
	if len(got.Recent) == 0 {
		t.Fatal("expected at least one recent message to fit")
	}
	// retention is newest-first: the last message must be present
	last := got.Recent[len(got.Recent)-1]
	if last.Blocks[0].Text != "newest message here" {
		t.Fatalf("expected newest message retained, got %q", last.Blocks[0].Text)
	}
}

func TestBudgeterRecentStaysChronological(t *testing.T) {
	tk := Heuristic{}
	msgs := []ports.Message{userMsg("first"), userMsg("second"), userMsg("third")}
	got := Budgeter{Tokenizer: tk, Budget: 100_000}.Fit(Input{Recent: msgs})
	if len(got.Recent) != 3 {
		t.Fatalf("recent = %d, want 3", len(got.Recent))
	}
	for i, want := range []string{"first", "second", "third"} {
		if got.Recent[i].Blocks[0].Text != want {
			t.Fatalf("recent[%d] = %q, want %q (order must be chronological)",
				i, got.Recent[i].Blocks[0].Text, want)
		}
	}
}

func TestBudgeterZeroBudgetIsUnlimited(t *testing.T) {
	// A zero budget means "no limit configured" — do not silently drop context.
	got := Budgeter{Tokenizer: Heuristic{}}.Fit(Input{
		System: "sys",
		Recent: []ports.Message{userMsg(strings.Repeat("x ", 5000))},
	})
	if got.System != "sys" || len(got.Recent) != 1 {
		t.Fatalf("zero budget should pass everything through, got %+v", got)
	}
}

func TestBudgeterMemoryTruncatesLowestFirst(t *testing.T) {
	tk := Heuristic{}
	mem := []string{"first memory entry", "second memory entry", "third memory entry"}
	// budget for system + checkpoint + ~2 memory entries
	budget := tk.Count("sys") + tk.Count("ckpt") + tk.Count(mem[0]) + tk.Count(mem[1]) + 4
	got := Budgeter{Tokenizer: tk, Budget: budget}.Fit(Input{
		System: "sys", Checkpoint: "ckpt", Memory: mem,
	})
	if len(got.Memory) == 0 || len(got.Memory) == 3 {
		t.Fatalf("expected partial memory retention, got %v", got.Memory)
	}
	// retained entries keep their original (relevance) order
	if got.Memory[0] != mem[0] {
		t.Errorf("memory[0] = %q, want %q (highest-ranked first)", got.Memory[0], mem[0])
	}
}

func TestBudgeterUsedReportsTotal(t *testing.T) {
	tk := Heuristic{}
	in := Input{System: "system prompt", Task: "the task"}
	got := Budgeter{Tokenizer: tk, Budget: 100_000}.Fit(in)
	if got.Used <= 0 {
		t.Fatalf("Used = %d, want > 0", got.Used)
	}
	if got.Used > 100_000 {
		t.Fatalf("Used (%d) must not exceed budget", got.Used)
	}
}
