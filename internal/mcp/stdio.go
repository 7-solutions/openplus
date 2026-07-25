package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
)

// errTransportClosed is returned by a call on a torn-down transport.
var errTransportClosed = errors.New("mcp: transport is closed")

// StdioConfig describes a subprocess MCP server.
type StdioConfig struct {
	// Command is the executable to run. Required.
	Command string
	// Args are its arguments.
	Args []string
	// Env is added to the child's environment as "K=V" entries. The parent's
	// environment is inherited, because these servers routinely need PATH, HOME
	// and a credential the user already exported.
	Env []string
	// Dir is the child's working directory. Empty means the parent's.
	Dir string
}

// stdioTransport speaks JSON-RPC to a subprocess over its stdin/stdout.
//
// One reader goroutine owns stdout and hands each response to the goroutine
// waiting on that id. Without the demux, two concurrent tool calls could read
// each other's replies.
type stdioTransport struct {
	cmd *exec.Cmd
	in  io.WriteCloser

	nextID atomic.Int64

	mu      sync.Mutex
	writing sync.Mutex // serializes frame writes; a torn frame breaks the stream
	pending map[string]chan *Response
	closed  bool

	// exited is closed once the subprocess has been reaped.
	exited chan struct{}
	// readErr records why the reader stopped, so a waiting call reports the real
	// cause (server died) rather than a bare timeout.
	readErr error
}

// NewStdio starts the configured subprocess and returns a Transport for it. A
// command that cannot start is an error here rather than at the first call, so a
// typo in config surfaces at assemble time.
//
// The subprocess is stopped when ctx is cancelled or Close is called — whichever
// happens first. Cancelling ctx is the session-teardown path.
func NewStdio(ctx context.Context, cfg StdioConfig) (*stdioTransport, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp: stdio server has no command")
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = cfg.Dir
	if len(cfg.Env) > 0 {
		cmd.Env = append(cmd.Environ(), cfg.Env...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdio %s: stdin: %w", cfg.Command, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdio %s: stdout: %w", cfg.Command, err)
	}
	// The server's stderr is its own diagnostics channel; it must not be mixed
	// into the protocol stream, and discarding it keeps a chatty server from
	// filling a pipe and blocking.
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: stdio %s: start: %w", cfg.Command, err)
	}

	t := &stdioTransport{
		cmd:     cmd,
		in:      stdin,
		pending: map[string]chan *Response{},
		exited:  make(chan struct{}),
	}
	go t.read(bufio.NewReader(stdout))
	go func() {
		<-ctx.Done()
		_ = t.Close()
	}()
	return t, nil
}

// PID reports the subprocess id (for tests and diagnostics).
func (t *stdioTransport) PID() int {
	if t.cmd.Process == nil {
		return 0
	}
	return t.cmd.Process.Pid
}

// Exited reports whether the subprocess has been reaped.
func (t *stdioTransport) Exited() bool {
	select {
	case <-t.exited:
		return true
	default:
		return false
	}
}

// read demuxes responses until the stream ends, then fails every waiter: a call
// whose server died must not wait for its context to expire.
func (t *stdioTransport) read(r *bufio.Reader) {
	for {
		var res Response
		if err := DecodeFrame(r, &res); err != nil {
			t.fail(err)
			return
		}
		key := string(res.ID)
		if key == "" {
			// A server-initiated request or notification. This change consumes
			// no server requests, so it is ignored rather than answered.
			continue
		}

		t.mu.Lock()
		ch, ok := t.pending[key]
		delete(t.pending, key)
		t.mu.Unlock()
		if ok {
			copied := res
			ch <- &copied
		}
	}
}

// fail records the reader's exit cause and releases every waiting call.
func (t *stdioTransport) fail(err error) {
	t.mu.Lock()
	if t.readErr == nil {
		t.readErr = err
	}
	waiting := t.pending
	t.pending = map[string]chan *Response{}
	t.mu.Unlock()

	for _, ch := range waiting {
		close(ch)
	}
}

// Call sends a request and waits for its response, honoring ctx.
func (t *stdioTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	id := strconv.FormatInt(t.nextID.Add(1), 10)
	ch := make(chan *Response, 1)

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, errTransportClosed
	}
	t.pending[id] = ch
	t.mu.Unlock()

	req := Request{JSONRPC: Version, ID: json.RawMessage(id), Method: method, Params: params}
	if err := t.write(req); err != nil {
		t.forget(id)
		return nil, err
	}

	select {
	case res, ok := <-ch:
		if !ok {
			t.mu.Lock()
			cause := t.readErr
			t.mu.Unlock()
			if cause == nil {
				cause = errTransportClosed
			}
			return nil, fmt.Errorf("mcp: %s: server stopped responding: %w", method, cause)
		}
		if res.Error != nil {
			return nil, res.Error
		}
		if len(res.Result) == 0 {
			return nil, fmt.Errorf("mcp: %s: response has neither result nor error", method)
		}
		return res.Result, nil

	case <-ctx.Done():
		// Stop tracking the id: the reader must not block handing a response to
		// a caller that has gone away.
		t.forget(id)
		return nil, fmt.Errorf("mcp: %s: %w", method, ctx.Err())
	}
}

// Notify sends a message that expects no response.
func (t *stdioTransport) Notify(ctx context.Context, method string, params json.RawMessage) error {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return errTransportClosed
	}
	return t.write(Request{JSONRPC: Version, Method: method, Params: params})
}

func (t *stdioTransport) write(req Request) error {
	t.writing.Lock()
	defer t.writing.Unlock()
	if err := EncodeFrame(t.in, req); err != nil {
		return fmt.Errorf("mcp: send %s: %w", req.Method, err)
	}
	return nil
}

func (t *stdioTransport) forget(id string) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

// Close stops the subprocess and releases every waiting call. It is idempotent:
// teardown may run after an error path already closed the transport.
//
// Closing stdin first gives a well-behaved server the chance to exit on its own;
// the kill is the fallback for one that does not.
func (t *stdioTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		<-t.exited
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	_ = t.in.Close()
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	// Reap the child so it cannot linger as a zombie, then release waiters.
	_ = t.cmd.Wait()
	close(t.exited)
	t.fail(errTransportClosed)
	return nil
}
