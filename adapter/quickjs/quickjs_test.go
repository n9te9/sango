package quickjs_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

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
