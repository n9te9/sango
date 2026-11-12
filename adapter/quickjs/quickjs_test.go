package quickjs_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/n9te9/sango"
	"github.com/n9te9/sango/adapter/quickjs"
	"github.com/tetratelabs/wazero/api"
)

type fakeMemory struct {
	api.Memory
	data []byte
}

type fakeFunc struct {
	api.Function
	fn func(ctx context.Context, params []uint64) ([]uint64, error)
}

func (f *fakeFunc) Call(ctx context.Context, params ...uint64) ([]uint64, error) {
	return f.fn(ctx, params)
}

type fakeModule struct {
	api.Module
	mem   *fakeMemory
	funcs map[string]api.Function
}

func (m *fakeModule) Memory() api.Memory { return m.mem }

func (m *fakeModule) ExportedFunction(name string) api.Function {
	return m.funcs[name]
}

func TestQuickJS_ID(t *testing.T) {
	tests := []struct {
		name    string
		adapter sango.Adapter
		want    string
	}{
		{
			name:    "ok: ID returned \"quickjs\"",
			adapter: quickjs.QuickJS(),
			want:    "quickjs",
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.adapter.ID()
			if d := cmp.Diff(got, tt.want); d != "" {
				t.Fatalf("%v", d)
			}
		})
	}
}

func TestQuickJS_Initialize(t *testing.T) {
	tests := []struct {
		name    string
		adapter sango.Adapter
		mod     api.Module
		wantErr error
	}{
		{
			name:    "ok: Initialize success and return nil",
			adapter: quickjs.QuickJS(),
			mod: func() api.Module {
				mem := &fakeMemory{data: make([]byte, 64*1024)}
				mod := &fakeModule{mem: mem, funcs: map[string]api.Function{}}
				mod.funcs["initialize"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
					return []uint64{0}, nil
				}}

				return mod
			}(),
			wantErr: nil,
		},
		{
			name:    "fail: Initialize fail and return error",
			adapter: quickjs.QuickJS(),
			mod: func() api.Module {
				mem := &fakeMemory{data: make([]byte, 64*1024)}
				mod := &fakeModule{mem: mem, funcs: map[string]api.Function{}}
				mod.funcs["initialize"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
					return []uint64{0}, errors.New("failed to initialize")
				}}

				return mod
			}(),
			wantErr: errors.New("initialize call failed: failed to initialize"),
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.adapter.Initialize(t.Context(), tt.mod)
			if err != nil {
				if tt.wantErr == nil {
					t.Fatalf("error expected nil, but got %v\n", err)
				}

				if err.Error() != tt.wantErr.Error() {
					t.Fatalf("error expected %v, but got %v\n", tt.wantErr, err)
				}
			}

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("error expected %v, but got nil\n", tt.wantErr)
				}
			}
		})
	}
}

func (m *fakeMemory) Size() uint32 { return uint32(len(m.data)) }

func (m *fakeMemory) Read(offset, count uint32) ([]byte, bool) {
	if uint64(offset)+uint64(count) > uint64(len(m.data)) {
		return nil, false
	}

	return m.data[offset : offset+count], true
}

func (m *fakeMemory) Write(offset uint32, v []byte) bool {
	if uint64(offset)+uint64(len(v)) > uint64(len(m.data)) {
		return false
	}

	copy(m.data[offset:], v)
	return true
}

type deallocCall struct{ ptr, size uint32 }

type guest struct {
	mod      *fakeModule
	next     uint32
	deallocs []deallocCall
	evalFn   func(code []byte) ([]byte, error)
}

const heapBase = 1024

func newGuest(evalFn func(code []byte) ([]byte, error)) *guest {
	g := &guest{
		next:   heapBase,
		evalFn: evalFn,
	}
	mem := &fakeMemory{data: make([]byte, 64*1024)}
	g.mod = &fakeModule{mem: mem, funcs: map[string]api.Function{}}

	g.mod.funcs["allocate"] = &fakeFunc{
		fn: func(_ context.Context, p []uint64) ([]uint64, error) {
			ptr := g.next
			g.next += uint32(p[0])
			return []uint64{uint64(ptr)}, nil
		},
	}
	g.mod.funcs["deallocate"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
		g.deallocs = append(g.deallocs, deallocCall{uint32(p[0]), uint32(p[1])})
		return nil, nil
	}}
	g.mod.funcs["initialize"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
		return []uint64{0}, nil
	}}
	g.mod.funcs["eval"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
		ptr, length := uint32(p[0]), uint32(p[1])
		code, _ := mem.Read(ptr, length)
		out, err := g.evalFn(bytes.Clone(code))
		if err != nil {
			return nil, err
		}
		outPtr := g.next
		g.next += uint32(len(out))
		copy(mem.data[outPtr:], out)
		return []uint64{uint64(outPtr)<<32 | uint64(uint32(len(out)))}, nil
	}}

	return g
}

func TestQuickJS_Eval(t *testing.T) {
	tests := []struct {
		name    string
		adapter sango.Adapter
		mod     func(ctx context.Context) api.Module
		code    []byte
		want    sango.Result
		wantErr error
	}{
		{

			name:    "ok: Eval success and return error is nil",
			adapter: quickjs.QuickJS(),
			mod: func(ctx context.Context) api.Module {
				evalFn := func(code []byte) ([]byte, error) {
					return append([]byte{0x00}, "hogehoge"...), nil
				}
				return newGuest(evalFn).mod
			},
			code: []byte(``),
			want: sango.Result{
				Value: []byte(`hogehoge`),
				Err:   nil,
			},
			wantErr: nil,
		},
		{
			name:    "fail: Initialize fail and return error",
			adapter: quickjs.QuickJS(),
			mod: func(ctx context.Context) api.Module {
				evalFn := func(code []byte) ([]byte, error) {
					return nil, errors.New("occured an error")
				}
				return newGuest(evalFn).mod
			},
			code:    []byte(``),
			wantErr: errors.New("eval call failed: occured an error"),
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.adapter.Eval(t.Context(), tt.mod(t.Context()), tt.code)
			if err != nil {
				if tt.wantErr == nil {
					t.Fatalf("error expected nil, but got %v\n", err)
				}

				if err.Error() != tt.wantErr.Error() {
					t.Fatalf("error expected %v, but got %v\n", tt.wantErr, err)
				}
			}

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("error expected %v, but got nil\n", tt.wantErr)
				}
			}

			if d := cmp.Diff(got, tt.want); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func TestRealWasm_OneshotAcquire(t *testing.T) {
	path := os.Getenv("SANGO_QUICKJS_WASM")
	if path == "" {
		t.Skip("SANGO_QUICKJS_WASM not set")
	}
	wasm, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	rt, err := sango.New(t.Context(), wasm, quickjs.QuickJS(), sango.WithWASI())
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(t.Context())

	inst, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Release()

	res, err := inst.Eval(t.Context(), []byte(`1 + 1`))
	if err != nil {
		t.Fatal(err)
	}

	if !res.OK() || string(res.Value) != "2" {
		t.Fatalf("got %q / %v", res.Value, res.Err)
	}
}

func newQuickJSRuntime(t *testing.T) *sango.Runtime {
	rt, err := sango.New(t.Context(), quickjs.Wasm(), quickjs.QuickJS(), sango.WithWASI())
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestRealWasm_Fork(t *testing.T) {
	rt := newQuickJSRuntime(t)

	ctx := t.Context()

	inst, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	inst.Eval(ctx, []byte(`var x = 40`))
	snap, _ := rt.Snapshot(inst)

	fork, _ := rt.Restore(ctx, snap)
	fork.Eval(ctx, []byte(`x = -999`))

	res1, _ := fork.Eval(ctx, []byte(`x`))
	fork.Release()

	if !res1.OK() || string(res1.Value) != "-999" {
		t.Fatalf("got %q / %v", res1.Value, res1.Err)
	}

	t.Logf("got %q", res1.Value)

	res2, err := inst.Eval(ctx, []byte(`x`))
	if err != nil {
		t.Fatal(err)
	}
	if !res2.OK() || string(res2.Value) != "40" {
		t.Fatalf("fork mutated the original: got %q / %v", res2.Value, res2.Err)
	}

	if r, _ := inst.Eval(ctx, []byte(`var f = (() => { let n = 0; return () => ++n })()`)); !r.OK() {
		t.Fatalf("define closure: %v", r.Err)
	}
	if r, _ := inst.Eval(ctx, []byte(`f()`)); string(r.Value) != "1" {
		t.Fatalf("closure call 1: got %q", r.Value)
	}
	snap2, _ := rt.Snapshot(inst)
	fork2, _ := rt.Restore(ctx, snap2)
	if r, _ := fork2.Eval(ctx, []byte(`f()`)); string(r.Value) != "2" {
		t.Fatalf("closure survived fork? got %q", r.Value)
	}
	fork2.Release()
	if r, _ := inst.Eval(ctx, []byte(`f()`)); string(r.Value) != "2" {
		t.Fatalf("fork leaked into original: got %q", r.Value)
	}

	t.Logf("got %q", res2.Value)
}

func TestQuickJS_ResultConvention(t *testing.T) {
	rt := newQuickJSRuntime(t)
	inst, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Release()

	tests := []struct {
		name string
		code string
		want string
	}{
		{"number", `1 + 1`, `2`},
		{"float", `1.5`, `1.5`},
		{"string", `"hello"`, `hello`},
		{"bool", `true`, `true`},
		{"null", `null`, `null`},
		{"undefined", `undefined`, `undefined`},
		{"object", `({a: 1})`, `{"a":1}`},
		{"array", `[1, 2, 3]`, `[1,2,3]`},
		{"nested array", `[[1, 2], [3, 4]]`, `[[1,2],[3,4]]`},
		{"comma in string", `["a,b", "c"]`, `["a,b","c"]`},
		{"circular reference",
			`(() => { const a = {}; a.self = a; return a; })()`,
			`[object Object]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := inst.Eval(t.Context(), []byte(tt.code))
			if err != nil {
				t.Fatalf("infra error on %q: %v", tt.code, err)
			}
			if !res.OK() {
				t.Fatalf("guest error on %q: %s", tt.code, res.Err)
			}
			if got := string(res.Value); got != tt.want {
				t.Errorf("%s: got %q, want %q", tt.code, got, tt.want)
			}
		})
	}

	t.Run("stringify failure does not leak an exception", func(t *testing.T) {
		if _, err := inst.Eval(t.Context(),
			[]byte(`(() => { const a = {}; a.self = a; return a; })()`)); err != nil {
			t.Fatal(err)
		}
		res, err := inst.Eval(t.Context(), []byte(`1 + 1`))
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK() {
			t.Fatalf("exception leaked into the next eval: %s", res.Err)
		}
		if got := string(res.Value); got != "2" {
			t.Fatalf("got %q, want %q", got, "2")
		}
	})
}

func TestQuickJS_KnownLimitations(t *testing.T) {
	rt := newQuickJSRuntime(t)
	inst, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Release()

	t.Run("deep recursion traps (known limitation)", func(t *testing.T) {
		res, err := inst.Eval(t.Context(), []byte(`(function f(){ return f(); })()`))
		if err == nil {
			t.Fatalf("stack overflow is now catchable (%q / %v) — update the docs", res.Value, res.Err)
		}
		t.Logf("traps as expected: %v", err)
	})
}

func TestRealWasm_EvalInterruptedByContext(t *testing.T) {
	rt := newQuickJSRuntime(t)
	defer rt.Close(t.Context())

	inst, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Release()

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := inst.Eval(ctx, []byte(`for (;;) {}`))
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from an interrupted eval, got nil")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("infinite loop was not interrupted by context deadline")
	}

	// The runtime keeps serving clean instances after an interrupt.
	next, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	res, err := next.Eval(t.Context(), []byte(`1 + 1`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() || string(res.Value) != "2" {
		t.Fatalf("got %q / %v", res.Value, res.Err)
	}
}

func TestRealWasm_MemoryLimitPages(t *testing.T) {
	// 1024 pages = 64 MiB cap on linear memory.
	rt, err := sango.New(t.Context(), quickjs.Wasm(), quickjs.QuickJS(),
		sango.WithWASI(), sango.WithMemoryLimitPages(1024))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(t.Context())

	inst, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Release()

	// 256 MiB does not fit under the cap.
	res, err := inst.Eval(t.Context(), []byte(`new ArrayBuffer(256 * 1024 * 1024).byteLength`))
	if err != nil {
		t.Fatalf("want guest error, got infra error: %v", err)
	}
	if res.OK() {
		t.Fatalf("allocation beyond the memory limit succeeded: %q", res.Value)
	}

	// The runtime keeps serving clean instances.
	next, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	res, err = next.Eval(t.Context(), []byte(`1 + 1`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() || string(res.Value) != "2" {
		t.Fatalf("got %q / %v", res.Value, res.Err)
	}
}

func TestRealWasm_HostFunc(t *testing.T) {
	rt, err := sango.New(t.Context(), quickjs.Wasm(), quickjs.QuickJS(),
		sango.WithWASI(),
		sango.WithHostFunc("echo", func(ctx context.Context, arg []byte) ([]byte, error) {
			return append([]byte("echo:"), arg...), nil
		}),
		sango.WithHostFunc("fail", func(ctx context.Context, arg []byte) ([]byte, error) {
			return nil, errors.New("upstream returned 503")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(t.Context())

	tests := []struct {
		name    string
		code    string
		want    string
		wantErr string
	}{
		{name: "registered function returns a value", code: `sango.call("echo", JSON.stringify({city: "Tokyo"}))`, want: `echo:{"city":"Tokyo"}`},
		{name: "argument is optional", code: `sango.call("echo")`, want: `echo:`},
		{name: "host error becomes a guest exception", code: `try { sango.call("fail", "") } catch (e) { "caught: " + e.message }`, want: "caught: upstream returned 503"},
		{name: "unregistered function is unreachable", code: `sango.call("fetch", "https://example.com")`, wantErr: `host function "fetch" is not provided`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst, err := rt.Acquire(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer inst.Release()
			res, err := inst.Eval(t.Context(), []byte(tt.code))
			if err != nil {
				t.Fatalf("infra error: %v", err)
			}
			if tt.wantErr != "" {
				if res.OK() || !bytes.Contains(res.Err.Message, []byte(tt.wantErr)) {
					t.Fatalf("want error containing %q, got %q / %v", tt.wantErr, res.Value, res.Err)
				}
				return
			}
			if !res.OK() {
				t.Fatalf("guest error: %v", res.Err)
			}
			if diff := cmp.Diff(tt.want, string(res.Value)); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

func TestRealWasm_HostFuncRespectsEvalDeadline(t *testing.T) {
	rt, err := sango.New(t.Context(), quickjs.Wasm(), quickjs.QuickJS(),
		sango.WithWASI(),
		sango.WithHostFunc("slow", func(ctx context.Context, arg []byte) ([]byte, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
				return []byte("too late"), nil
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(t.Context())
	inst, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Release()

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := inst.Eval(ctx, []byte(`sango.call("slow")`))
	if time.Since(start) > 5*time.Second {
		t.Fatal("host call was not bounded by the eval deadline")
	}
	if err == nil && res.OK() {
		t.Fatalf("expected failure, got %q", res.Value)
	}
}

type principalKey struct{}

func TestRealWasm_HostFuncReceivesEvalContextValues(t *testing.T) {
	rt, err := sango.New(t.Context(), quickjs.Wasm(), quickjs.QuickJS(),
		sango.WithWASI(),
		sango.WithHostFunc("whoami", func(ctx context.Context, arg []byte) ([]byte, error) {
			user, ok := ctx.Value(principalKey{}).(string)
			if !ok {
				return nil, errors.New("no principal")
			}
			return []byte(user), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(t.Context())

	for _, user := range []string{"alice", "bob"} {
		inst, err := rt.Acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.WithValue(t.Context(), principalKey{}, user)
		// The guest cannot choose whose identity the host function sees.
		res, err := inst.Eval(ctx, []byte(`sango.call("whoami", JSON.stringify({user: "admin"}))`))
		inst.Release()
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK() || string(res.Value) != user {
			t.Fatalf("want %q, got %q / %v", user, res.Value, res.Err)
		}
	}
}
