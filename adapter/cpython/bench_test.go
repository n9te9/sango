package cpython_test

import (
	"fmt"
	"testing"

	"github.com/n9te9/sango"
	"github.com/n9te9/sango/adapter/cpython"
)

// HOW TO BENCHMARK:
//
//	go test -bench . -benchmem -run '^$' ./adapter/cpython/

func newRuntime(b *testing.B, opts ...sango.Option) *sango.Runtime {
	b.Helper()
	stdlibOpt, err := cpython.WithStdlib()
	if err != nil {
		b.Fatal(err)
	}
	opts = append(opts, sango.WithWASI(), stdlibOpt)
	rt, err := sango.New(b.Context(), cpython.Wasm(), cpython.CPython(), opts...)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { rt.Close(b.Context()) })
	return rt
}

func newNumpyRuntime(b *testing.B, opts ...sango.Option) *sango.Runtime {
	b.Helper()
	stdlibOpt, err := cpython.WithStdlib()
	if err != nil {
		b.Fatal(err)
	}
	opts = append(opts, sango.WithWASI(), stdlibOpt)
	rt, err := sango.New(b.Context(), cpython.Wasm(), cpython.CPython(), opts...)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { rt.Close(b.Context()) })
	return rt
}

func acquire(b *testing.B, rt *sango.Runtime) *sango.Instance {
	b.Helper()
	inst, err := rt.Acquire(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	return inst
}

func benchEvalOK(b *testing.B, inst *sango.Instance, code string) {
	b.Helper()
	res, err := inst.Eval(b.Context(), []byte(code))
	if err != nil {
		b.Fatal(err)
	}
	if !res.OK() {
		b.Fatal(res.Err)
	}
}

func snapLen[T ~[]byte](snap T) int { return len(snap) }

func benchEval(b *testing.B, inst *sango.Instance, code string) {
	b.Helper()
	src := []byte(code)
	b.ResetTimer()
	for b.Loop() {
		res, err := inst.Eval(b.Context(), src)
		if err != nil {
			b.Fatal(err)
		}
		if !res.OK() {
			b.Fatal(res.Err)
		}
	}
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

func BenchmarkAcquire_Parallel(b *testing.B) {
	rt := newRuntime(b, sango.WithPoolSize(4))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			inst, err := rt.Acquire(b.Context())
			if err != nil {
				b.Error(err)
				return
			}
			inst.Release()
		}
	})
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

func BenchmarkEval_Stdlib(b *testing.B) {
	rt := newRuntime(b)
	inst, err := rt.Acquire(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Release()

	// import は計測外(セッション内で 1 回きりの操作なので)
	if res, err := inst.Eval(b.Context(), []byte(`import json`)); err != nil || !res.OK() {
		b.Fatalf("import json: %v / %v", err, res.Err)
	}

	code := []byte(`json.dumps({"a": [1, 2, 3]})`)
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

	code := []byte(`sum(range(100))`)
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
	if _, err := inst.Eval(b.Context(), []byte(`x = 40`)); err != nil {
		b.Fatal(err)
	}

	size := 0
	b.ResetTimer()
	for b.Loop() {
		snap, err := rt.Snapshot(inst)
		if err != nil {
			b.Fatal(err)
		}
		size = snapLen(snap)
	}
	b.StopTimer()
	b.ReportMetric(float64(size)/1e6, "MB/snap")
}

func BenchmarkFork(b *testing.B) {
	rt := newRuntime(b)
	inst, err := rt.Acquire(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Release()
	if _, err := inst.Eval(b.Context(), []byte(`x = 40`)); err != nil {
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
	b.StopTimer()
	b.ReportMetric(float64(snapLen(snap))/1e6, "MB/snap")
}

func BenchmarkFork_Usable(b *testing.B) {
	rt := newRuntime(b)
	inst := acquire(b, rt)
	defer inst.Release()
	benchEvalOK(b, inst, `x = 40`)
	snap, err := rt.Snapshot(inst)
	if err != nil {
		b.Fatal(err)
	}

	probe := []byte(`x + 2`)
	b.ResetTimer()
	for b.Loop() {
		fork, err := rt.Restore(b.Context(), snap)
		if err != nil {
			b.Fatal(err)
		}
		res, err := fork.Eval(b.Context(), probe)
		if err != nil {
			fork.Release()
			b.Fatal(err)
		}
		if !res.OK() {
			fork.Release()
			b.Fatal(res.Err)
		}
		fork.Release()
	}
}

func BenchmarkFork_Fanout(b *testing.B) {
	rt := newRuntime(b)
	inst := acquire(b, rt)
	defer inst.Release()
	benchEvalOK(b, inst, `x = 40`)
	snap, err := rt.Snapshot(inst)
	if err != nil {
		b.Fatal(err)
	}

	for _, w := range []int{1, 4, 16, 64} {
		b.Run(fmt.Sprintf("w%d", w), func(b *testing.B) {
			forks := make([]*sango.Instance, 0, w)
			iters := 0
			b.ResetTimer()
			for b.Loop() {
				forks = forks[:0]
				for range w {
					fork, err := rt.Restore(b.Context(), snap)
					if err != nil {
						b.Fatal(err)
					}
					forks = append(forks, fork)
				}
				for _, f := range forks {
					f.Release()
				}
				iters++
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(iters*w), "ns/branch")
		})
	}
}

func BenchmarkColdInit(b *testing.B) {
	b.ResetTimer()
	for b.Loop() {

		stdlibOpt, err := cpython.WithStdlib()
		if err != nil {
			b.Fatal(err)
		}
		opts := []sango.Option{sango.WithWASI(), stdlibOpt}
		rt, err := sango.New(b.Context(), cpython.Wasm(), cpython.CPython(), opts...)
		if err != nil {
			b.Fatal(err)
		}
		rt.Close(b.Context())
	}
}

func BenchmarkNumpy_ColdInit(b *testing.B) {
	b.ResetTimer()
	for b.Loop() {
		stdlibOpt, err := cpython.WithStdlib()
		if err != nil {
			b.Fatal(err)
		}
		opts := []sango.Option{sango.WithWASI(), stdlibOpt}
		rt, err := sango.New(b.Context(), cpython.Wasm(), cpython.CPython(), opts...)
		if err != nil {
			b.Fatal(err)
		}
		rt.Close(b.Context())
	}
}

func BenchmarkNumpy_Acquire_NoPool(b *testing.B) {
	rt := newNumpyRuntime(b, sango.WithPoolSize(0))

	b.ResetTimer()
	for b.Loop() {
		inst, err := rt.Acquire(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		inst.Release()
	}
}

func BenchmarkNumpy_Acquire_Warm(b *testing.B) {
	rt := newNumpyRuntime(b, sango.WithPoolSize(4))

	b.ResetTimer()
	for b.Loop() {
		inst, err := rt.Acquire(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		inst.Release()
	}
}

func BenchmarkNumpy_Import(b *testing.B) {
	rt := newNumpyRuntime(b, sango.WithPoolSize(4))

	code := []byte(`import numpy as np`)
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		inst, err := rt.Acquire(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		res, err := inst.Eval(b.Context(), code)
		if err != nil {
			inst.Release()
			b.Fatal(err)
		}
		if !res.OK() {
			inst.Release()
			b.Fatal(res.Err)
		}

		b.StopTimer()
		inst.Release()
		b.StartTimer()
	}
}

func BenchmarkNumpy_Eval(b *testing.B) {
	cases := []struct {
		name  string
		setup string
		code  string
	}{
		{"Sum1k", `a = np.arange(1000, dtype=np.float64)`, `float(a.sum())`},
		{"Sum1M", `big = np.arange(1_000_000, dtype=np.float64)`, `float(big.sum())`},
		{"Matmul128", `m = np.ones((128, 128), dtype=np.float64)`, `float((m @ m).sum())`},
		{"FFT4k", `sig = np.arange(4096, dtype=np.float64)`, `float(np.fft.fft(sig).real.sum())`},
		{"Alloc1M", ``, `float(np.zeros(1_000_000, dtype=np.float64).sum())`},
	}

	rt := newNumpyRuntime(b)
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			inst := acquire(b, rt)
			defer inst.Release()
			benchEvalOK(b, inst, `import numpy as np`)
			if tc.setup != "" {
				benchEvalOK(b, inst, tc.setup)
			}
			benchEval(b, inst, tc.code)
		})
	}
}

func BenchmarkNumpy_Snapshot(b *testing.B) {
	cases := []struct {
		name  string
		setup string
	}{
		{"Imported", ``},
		{"Array8MB", `held = np.zeros(1_000_000, dtype=np.float64)`},
	}

	rt := newNumpyRuntime(b)
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			inst := acquire(b, rt)
			defer inst.Release()
			benchEvalOK(b, inst, `import numpy as np`)
			if tc.setup != "" {
				benchEvalOK(b, inst, tc.setup)
			}

			size := 0
			iters := 0
			b.ResetTimer()
			for b.Loop() {
				snap, err := rt.Snapshot(inst)
				if err != nil {
					b.Fatal(err)
				}
				size = snapLen(snap)
				iters++
			}
			b.StopTimer()
			b.ReportMetric(float64(size)/1e6, "MB/snap")
			if secs := b.Elapsed().Seconds(); secs > 0 {
				b.ReportMetric(float64(size)*float64(iters)/secs/1e9, "GB/s")
			}
		})
	}
}

func BenchmarkNumpy_Fork(b *testing.B) {
	cases := []struct {
		name  string
		setup string
	}{
		{"Imported", ``},
		{"Array8MB", `held = np.zeros(1_000_000, dtype=np.float64)`},
	}

	rt := newNumpyRuntime(b)
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			inst := acquire(b, rt)
			benchEvalOK(b, inst, `import numpy as np`)
			if tc.setup != "" {
				benchEvalOK(b, inst, tc.setup)
			}
			snap, err := rt.Snapshot(inst)
			if err != nil {
				b.Fatal(err)
			}
			inst.Release()

			b.ResetTimer()
			for b.Loop() {
				fork, err := rt.Restore(b.Context(), snap)
				if err != nil {
					b.Fatal(err)
				}
				fork.Release()
			}
			b.StopTimer()
			b.ReportMetric(float64(snapLen(snap))/1e6, "MB/snap")
		})
	}
}

func BenchmarkNumpy_Oneshot(b *testing.B) {
	rt := newNumpyRuntime(b, sango.WithPoolSize(4))

	code := []byte(`import numpy as np
float(np.arange(1000, dtype=np.float64).sum())`)
	b.ResetTimer()
	for b.Loop() {
		inst, err := rt.Acquire(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		res, err := inst.Eval(b.Context(), code)
		if err != nil {
			inst.Release()
			b.Fatal(err)
		}
		if !res.OK() {
			inst.Release()
			b.Fatal(res.Err)
		}
		inst.Release()
	}
}
