// Package jsworkflow is the goja-backed adapter that compiles a `.js` workflow
// into an orchestrate.Workflow (ADR-0009). It is the only package that knows JS
// exists; the engine, the Workflow/Phase port, and the report shape live in
// internal/orchestrate and are unchanged.
//
// JS contract (defined by change 0014 — no MiMoCode reference existed to copy):
//
//	module.exports = {
//	  name: "deep-research",        // required: registration key
//	  maxRetries: 2,                // optional, defaults to 0
//	  phases: [                     // required, non-empty, ordered
//	    { name: "query", run: (state) => "output" },
//	    { name: "go",    run: (state) => { state.set("k","v"); return "..." } },
//	  ],
//	};
//
// state shim: { last: string, get(key): string|undefined, set(key, value): void }
// over the engine's *orchestrate.State. A thrown value is a phase error and
// retries per maxRetries, like a Go phase. The sandbox binds only module,
// exports, state, and console.log — no require, fs, or net.
package jsworkflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dop251/goja"

	"github.com/7solutions/openplus/internal/orchestrate"
)

// Compiled is a JS workflow ready to run, with its declared name (the key a
// caller registers it under) and the orchestrate.Workflow that runs it.
type Compiled struct {
	// Name is module.exports.name — the registration key.
	Name string
	// Run is the workflow, runnable through the unchanged engine.
	Run orchestrate.Workflow

	// One runtime is shared by every phase (they were parsed from one script).
	// goja runtimes are not safe for concurrent use, so a Compiled workflow is
	// run sequentially — which the engine does within a single Run.
	vm   *goja.Runtime
	logf func(string, ...any) // console.log sink; nil = discard
}

// Option tunes Compile.
type Option func(*Compiled)

// WithLogger routes JS console.log to fn. Default is discard.
func WithLogger(fn func(string, ...any)) Option {
	return func(c *Compiled) { c.logf = fn }
}

// jsPhase adapts one JS phase function to orchestrate.Phase.
type jsPhase struct {
	name string
	fn   goja.Callable
	c    *Compiled // runtime + logger live on the compiled workflow
}

func (p jsPhase) Name() string { return p.name }

// Run builds a state shim over the engine's State, calls the JS phase function,
// and maps the result back: a returned value is coerced to the phase output
// (undefined/null → ""), a thrown value is a phase error (retried per the
// workflow's MaxRetries, like a Go phase). Cancellation interrupts the script
// mid-phase via vm.Interrupt (T-1414).
func (p jsPhase) Run(ctx context.Context, st *orchestrate.State) (string, error) {
	if st == nil {
		st = &orchestrate.State{}
	}

	// state shim: a plain JS object backed by the engine's *State, so hand-off
	// works identically to a Go phase.
	shim := p.c.vm.NewObject()
	shim.Set("last", st.Last)
	shim.Set("get", func(fc goja.FunctionCall) goja.Value {
		v, ok := st.Get(fc.Argument(0).String())
		if !ok {
			return goja.Undefined()
		}
		return p.c.vm.ToValue(v)
	})
	shim.Set("set", func(fc goja.FunctionCall) goja.Value {
		st.Set(fc.Argument(0).String(), fc.Argument(1).String())
		return goja.Undefined()
	})

	// Wire cancellation to the runtime so a cancelled context aborts the script
	// mid-phase, not only between phases. vm.Interrupt is safe to call from
	// another goroutine; the JS interpreter checks it at loop/call boundaries.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			p.c.vm.Interrupt(ctx.Err())
		case <-done:
		}
	}()

	out, err := p.fn(goja.Undefined(), shim)

	// An interruption surfaces as a JS error; if the context is cancelled, the
	// cause is the cancellation regardless of how goja reported it.
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("jsworkflow: phase %q threw: %w", p.name, err)
	}
	if out == nil || out == goja.Undefined() || out == goja.Null() {
		return "", nil
	}
	return out.String(), nil
}

// Compile turns a CommonJS-shaped JS workflow source into a Compiled workflow.
// The source must export a non-empty name and a non-empty phases array; each
// phase needs a name and a function run. A malformed source returns an error
// describing the defect — never an empty workflow.
func Compile(src string, opts ...Option) (Compiled, error) {
	c := Compiled{
		vm:   goja.New(),
		logf: func(string, ...any) {}, // discard by default
	}
	for _, o := range opts {
		o(&c)
	}

	// CommonJS scaffold: module.exports and exports both point at a fresh object
	// so `module.exports = {...}` (reassign) and `exports.x = ...` (mutate) both
	// land somewhere Compile can read back.
	exports := c.vm.NewObject()
	module := c.vm.NewObject()
	module.Set("exports", exports)
	c.vm.Set("module", module)
	c.vm.Set("exports", exports)

	// The only host binding besides module/exports: console.log → the logger.
	// T-1415 asserts the rest (require/process/fetch) stay undefined.
	c.vm.Set("console", map[string]func(goja.FunctionCall) goja.Value{
		"log": func(fc goja.FunctionCall) goja.Value {
			if c.logf != nil {
				parts := make([]any, 0, len(fc.Arguments))
				for _, a := range fc.Arguments {
					parts = append(parts, a.String())
				}
				c.logf("jsworkflow: " + joinSpaces(parts))
			}
			return goja.Undefined()
		},
	})

	if _, err := c.vm.RunString(src); err != nil {
		return Compiled{}, fmt.Errorf("jsworkflow: script error: %w", err)
	}

	expVal := module.Get("exports")
	if expVal == nil || expVal == goja.Undefined() || expVal == goja.Null() {
		return Compiled{}, errors.New("jsworkflow: module.exports is not set")
	}
	obj := expVal.ToObject(c.vm)

	// name — required.
	nameVal := obj.Get("name")
	if nameVal == nil || nameVal == goja.Undefined() || nameVal == goja.Null() {
		return Compiled{}, errors.New("jsworkflow: module.exports.name is required")
	}
	c.Name = strings.TrimSpace(nameVal.String())
	if c.Name == "" {
		return Compiled{}, errors.New("jsworkflow: module.exports.name is empty")
	}

	// maxRetries — optional, defaults to 0, clamped at 0.
	if mr := obj.Get("maxRetries"); mr != nil && mr != goja.Undefined() && mr != goja.Null() {
		c.Run.MaxRetries = int(mr.ToInteger())
		if c.Run.MaxRetries < 0 {
			c.Run.MaxRetries = 0
		}
	}

	// phases — required, non-empty array of {name, run}.
	phasesVal := obj.Get("phases")
	if phasesVal == nil || phasesVal == goja.Undefined() || phasesVal == goja.Null() {
		return Compiled{}, errors.New("jsworkflow: module.exports.phases is required")
	}
	phasesObj := phasesVal.ToObject(c.vm)
	length := 0
	if lv := phasesObj.Get("length"); lv != nil && lv != goja.Undefined() {
		length = int(lv.ToInteger())
	}
	if length == 0 {
		return Compiled{}, errors.New("jsworkflow: module.exports.phases is empty")
	}
	for i := 0; i < length; i++ {
		phVal := phasesObj.Get(strconv.Itoa(i))
		if phVal == nil || phVal == goja.Undefined() || phVal == goja.Null() {
			return Compiled{}, fmt.Errorf("jsworkflow: phase %d is not an object", i)
		}
		phObj := phVal.ToObject(c.vm)

		pnameVal := phObj.Get("name")
		if pnameVal == nil || pnameVal == goja.Undefined() || pnameVal == goja.Null() {
			return Compiled{}, fmt.Errorf("jsworkflow: phase %d has no name", i)
		}
		pname := strings.TrimSpace(pnameVal.String())
		if pname == "" {
			return Compiled{}, fmt.Errorf("jsworkflow: phase %d has an empty name", i)
		}

		runVal := phObj.Get("run")
		fn, ok := goja.AssertFunction(runVal)
		if !ok {
			return Compiled{}, fmt.Errorf("jsworkflow: phase %d (%q) run is not a function", i, pname)
		}
		c.Run.Phases = append(c.Run.Phases, jsPhase{name: pname, fn: fn, c: &c})
	}

	return c, nil
}

// LoadFile reads a `.js` workflow file and compiles it. Errors name the file so a
// bad path or a script defect points at its source.
func LoadFile(path string, opts ...Option) (Compiled, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return Compiled{}, fmt.Errorf("jsworkflow: read %s: %w", path, err)
	}
	c, err := Compile(string(src), opts...)
	if err != nil {
		return Compiled{}, fmt.Errorf("jsworkflow: %s: %w", path, err)
	}
	return c, nil
}

// joinSpaces stringifies logger parts with single spaces.
func joinSpaces(parts []any) string {
	var b []byte
	for i, p := range parts {
		if i > 0 {
			b = append(b, ' ')
		}
		b = append(b, []byte(fmt.Sprint(p))...)
	}
	return string(b)
}
