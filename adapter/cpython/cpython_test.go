package cpython_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/n9te9/sango"
	"github.com/n9te9/sango/adapter/cpython"
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

func TestCPython_ID(t *testing.T) {
	tests := []struct {
		name    string
		adapter sango.Adapter
		want    string
	}{
		{
			name:    "ok: ID returned \"cpython\"",
			adapter: cpython.CPython(),
			want:    "cpython",
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

func TestCPython_Initialize(t *testing.T) {
	tests := []struct {
		name    string
		adapter sango.Adapter
		mod     api.Module
		wantErr error
	}{
		{
			name:    "ok: Initialize success and return nil",
			adapter: cpython.CPython(),
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
			adapter: cpython.CPython(),
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

func TestCPython_Eval(t *testing.T) {
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
			adapter: cpython.CPython(),
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
			adapter: cpython.CPython(),
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
	if testing.Short() {
		t.Skip("skip real wasm test in -short mode")
	}
	stdlib, err := cpython.WithStdlib()
	if err != nil {
		t.Fatal(err)
	}
	rt, err := sango.New(t.Context(), cpython.Wasm(), cpython.CPython(), sango.WithWASI(), stdlib)
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

	t.Logf("got %q", res.Value)
}

func TestRealWasm_Fork(t *testing.T) {
	if testing.Short() {
		t.Skip("skip real wasm test in -short mode")
	}

	stdlib, err := cpython.WithStdlib()
	if err != nil {
		t.Fatal(err)
	}
	rt, err := sango.New(t.Context(), cpython.Wasm(), cpython.CPython(), sango.WithWASI(), stdlib)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(t.Context())

	ctx := t.Context()

	inst, err := rt.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	inst.Eval(ctx, []byte(`x = 40`))
	snap, err := rt.Snapshot(inst)
	if err != nil {
		t.Fatal(err)
	}

	fork, err := rt.Restore(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	fork.Eval(ctx, []byte(`x = -999`))

	res1, err := fork.Eval(ctx, []byte(`x`))
	if err != nil {
		t.Fatal(err)
	}
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

	closureSrc := `
def _make_counter():
    n = [0]
    def inc():
        n[0] += 1
        return n[0]
    return inc
f = _make_counter()
`
	if r, _ := inst.Eval(ctx, []byte(closureSrc)); !r.OK() {
		t.Fatalf("define closure: %v", r.Err)
	}
	if r, _ := inst.Eval(ctx, []byte(`f()`)); string(r.Value) != "1" {
		t.Fatalf("closure call 1: got %q", r.Value)
	}
	snap2, err := rt.Snapshot(inst)
	if err != nil {
		t.Fatal(err)
	}
	fork2, err := rt.Restore(ctx, snap2)
	if err != nil {
		t.Fatal(err)
	}
	r, err := fork2.Eval(ctx, []byte(`f()`))
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Value) != "2" {
		t.Fatalf("closure survived fork? got %q", r.Value)
	}
	fork2.Release()
	if r, _ := inst.Eval(ctx, []byte(`f()`)); string(r.Value) != "2" {
		t.Fatalf("fork leaked into original: got %q", r.Value)
	}

	t.Logf("got %q", r.Value)
	fork3, err := rt.Restore(ctx, snap2)
	if err != nil {
		t.Fatal(err)
	}
	defer fork3.Release()

	r, err = fork3.Eval(ctx, []byte(`import json; json.dumps({"a": 1})`))
	if err != nil {
		t.Fatal(err)
	}

	if !r.OK() || string(r.Value) != `'{"a": 1}'` {
		t.Fatalf("json.dumps after fork: got %q / %v", r.Value, r.Err)
	}

	t.Logf("got %q by fork3", r.Value)

	fork4, err := rt.Restore(ctx, snap2)
	if err != nil {
		t.Fatal(err)
	}
	defer fork4.Release()

	r, err = fork4.Eval(ctx, []byte(`import json`))
	if err != nil {
		t.Fatal(err)
	}

	if !r.OK() {
		t.Fatalf("import json after fork: got %q / %v", r.Value, r.Err)
	}

	r, err = fork4.Eval(ctx, []byte(`json.dumps({"a": 1})`))
	if err != nil {
		t.Fatal(err)
	}

	if !r.OK() || string(r.Value) != `'{"a": 1}'` {
		t.Fatalf("json.dumps after fork: got %q / %v", r.Value, r.Err)
	}

	t.Logf("got %q by fork4", r.Value)
}

func evalOK(t *testing.T, inst *sango.Instance, code string) string {
	t.Helper()
	res, err := inst.Eval(t.Context(), []byte(code))
	if err != nil {
		t.Fatalf("infra error on %q: %v", code, err)
	}
	if !res.OK() {
		t.Fatalf("guest error on %q: %s", code, res.Err)
	}
	return string(res.Value)
}

func newExtRuntime(t *testing.T) *sango.Runtime {
	t.Helper()
	stdlib, _ := cpython.WithStdlib()
	rt, err := sango.New(t.Context(), cpython.Wasm(), cpython.CPython(),
		sango.WithWASI(), stdlib)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Close(t.Context()) })
	return rt
}

func TestExt_SurvivesFork(t *testing.T) {
	if testing.Short() {
		t.Skip("skip real wasm test in -short mode")
	}
	rt := newExtRuntime(t)
	ctx := t.Context()

	inst, err := rt.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Release()

	// import 済みの状態を snapshot に含める
	evalOK(t, inst, `from markupsafe._speedups import _escape_inner`)
	snap, err := rt.Snapshot(inst)
	if err != nil {
		t.Fatal(err)
	}

	// fork 側で C 拡張の関数を呼ぶ(import 済み状態が複製されているか)
	fork, err := rt.Restore(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	defer fork.Release()

	if got := evalOK(t, fork, `str(_escape_inner("<i>"))`); got != "'&lt;i&gt;'" {
		t.Fatalf("C extension broken after fork: got %s", got)
	}

	// fork 後に新規 import する経路も確認(ホスト側 fd テーブルが新品の状態)
	fresh, err := rt.Restore(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Release()

	if got := evalOK(t, fresh, `import markupsafe; str(markupsafe.escape("<u>"))`); got != "'&lt;u&gt;'" {
		t.Fatalf("import after fork: got %s", got)
	}
}
