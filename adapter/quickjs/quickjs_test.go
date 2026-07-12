package quickjs_test

import (
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

func TestRealWasm_Fork(t *testing.T) {
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

	ctx := t.Context()

	inst, err := rt.Acquire(t.Context())
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
