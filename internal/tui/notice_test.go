package tui

import (
	"strings"
	"testing"
)

// TestNoticeMsgRendersInTranscript pins that a session-level notice (compaction,
// a failed checkpoint) is visible to the user rather than silent.
func TestNoticeMsgRendersInTranscript(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(NoticeMsg{Text: "context compacted: 9 → 6 messages"})
	m = mm.(Model)

	joined := strings.Join(m.log, "\n")
	if !strings.Contains(joined, "context compacted") {
		t.Errorf("notice missing from the transcript:\n%s", joined)
	}
}

// TestNoticeMsgIsSetApart pins that a notice is distinguishable from
// conversation, so the user does not read it as model output.
func TestNoticeMsgIsSetApart(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(NoticeMsg{Text: "something happened"})
	m = mm.(Model)

	last := m.log[len(m.log)-1]
	if last == "something happened" {
		t.Errorf("notice rendered as bare text, indistinguishable from output: %q", last)
	}
}

// TestNoticeFlushesPendingText pins that a notice arriving mid-stream does not
// interleave with a half-rendered assistant message.
func TestNoticeFlushesPendingText(t *testing.T) {
	m := newTestModel()
	m.cur.WriteString("partial assistant text")

	mm, _ := m.Update(NoticeMsg{Text: "notice arrived"})
	m = mm.(Model)

	if m.cur.Len() != 0 {
		t.Errorf("pending text not flushed before the notice: %q", m.cur.String())
	}
	joined := strings.Join(m.log, "\n")
	if !strings.Contains(joined, "partial assistant text") {
		t.Errorf("pending text lost:\n%s", joined)
	}
	// order: the flushed text precedes the notice
	if strings.Index(joined, "partial assistant text") > strings.Index(joined, "notice arrived") {
		t.Errorf("notice jumped ahead of the text it interrupted:\n%s", joined)
	}
}
