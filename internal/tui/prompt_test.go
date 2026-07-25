package tui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/7solutions/openplus/internal/provider"
)

func TestPrompterAskApproved(t *testing.T) {
	answer := make(chan bool, 1)
	var (
		mu       sync.Mutex
		captured promptMsg
	)
	p := NewPrompter(func(m tea.Msg) {
		mu.Lock()
		captured = m.(promptMsg)
		mu.Unlock()
	}, answer)

	call := provider.ToolCall{ID: "c1", Name: "bash", Input: []byte(`{"command":"rm -rf x"}`)}
	type res struct {
		ok  bool
		err error
	}
	resCh := make(chan res, 1)
	go func() { ok, err := p.Ask(context.Background(), call); resCh <- res{ok, err} }()

	// give the goroutine a moment to send the prompt
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	name := captured.call.Name
	sent := captured
	mu.Unlock()
	if name != "bash" {
		t.Fatalf("prompt not sent: %+v", sent)
	}
	answer <- true
	r := <-resCh
	if !r.ok || r.err != nil {
		t.Fatalf("Ask = (%v,%v), want (true,nil)", r.ok, r.err)
	}
}

func TestPrompterAskDenied(t *testing.T) {
	answer := make(chan bool, 1)
	p := NewPrompter(SendNoOp(), answer)
	resCh := make(chan bool, 1)
	go func() { ok, _ := p.Ask(context.Background(), provider.ToolCall{}); resCh <- ok }()
	time.Sleep(20 * time.Millisecond)
	answer <- false
	if ok := <-resCh; ok {
		t.Fatal("Ask should return false on deny")
	}
}

func TestPrompterAskCtxTimeout(t *testing.T) {
	answer := make(chan bool, 1) // never written
	p := NewPrompter(SendNoOp(), answer)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	ok, err := p.Ask(ctx, provider.ToolCall{})
	if ok || err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Ask = (%v,%v), want (false, DeadlineExceeded)", ok, err)
	}
}
