package quickjs_test

import (
	"testing"

	"github.com/n9te9/sango"
	"github.com/n9te9/sango/adapter/quickjs"

	_ "embed"
)

// HOW TO BENCHMARK:
//
// go test -bench . -benchmem -run '^$' ./adapter/quickjs/

//go:embed quickjs.wasm
var wasmBinary []byte

// Wasm returns the embedded QuickJS wasm binary for sango.New.
func Wasm() []byte { return wasmBinary }

func newRuntime(b *testing.B, opts ...sango.Option) *sango.Runtime {
	b.Helper()
	opts = append(opts, sango.WithWASI())
	rt, err := sango.New(b.Context(), Wasm(), quickjs.QuickJS(), opts...)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { rt.Close(b.Context()) })
	return rt
}

func BenchmarkAcquire_NoPool(b *testing.B) {
	rt := newRuntime(b, sango.WithPoolSize(0))

	b.ResetTimer()
	for b.Loop() {
		inst, err := rt.Acquire(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		inst.Release()
	}
}
func BenchmarkAcquire_Warm(b *testing.B) {
	rt := newRuntime(b, sango.WithPoolSize(4))

	b.ResetTimer()
	for b.Loop() {
		inst, err := rt.Acquire(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		inst.Release()
	}
}

func BenchmarkEval(b *testing.B) {
	rt := newRuntime(b)
	inst, err := rt.Acquire(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Release()

	code := []byte(`1 + 1`)
	b.ResetTimer()
	for b.Loop() {
		res, err := inst.Eval(b.Context(), code)
		if err != nil {
			b.Fatal(err)
		}
		if !res.OK() {
			b.Fatal(res.Err)
		}
	}
}

func BenchmarkOneshot(b *testing.B) {
	rt := newRuntime(b, sango.WithPoolSize(4))

	code := []byte(`JSON.stringify({ok: 1 + 1})`)
	b.ResetTimer()
	for b.Loop() {
		inst, err := rt.Acquire(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := inst.Eval(b.Context(), code); err != nil {
			inst.Release()
			b.Fatal(err)
		}
		inst.Release()
	}
}

func BenchmarkSnapshot(b *testing.B) {
	rt := newRuntime(b)
	inst, err := rt.Acquire(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Release()
	if _, err := inst.Eval(b.Context(), []byte(`var x = 40`)); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		if _, err := rt.Snapshot(inst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFork(b *testing.B) {
	rt := newRuntime(b)
	inst, err := rt.Acquire(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Release()
	if _, err := inst.Eval(b.Context(), []byte(`var x = 40`)); err != nil {
		b.Fatal(err)
	}
	snap, err := rt.Snapshot(inst)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		fork, err := rt.Restore(b.Context(), snap)
		if err != nil {
			b.Fatal(err)
		}
		fork.Release()
	}
}

func BenchmarkColdInit(b *testing.B) {
	wasm := Wasm()
	b.ResetTimer()
	for b.Loop() {
		rt, err := sango.New(b.Context(), wasm, quickjs.QuickJS(), sango.WithWASI())
		if err != nil {
			b.Fatal(err)
		}
		rt.Close(b.Context())
	}
}
