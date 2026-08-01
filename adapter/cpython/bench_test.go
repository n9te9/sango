package cpython_test

import (
	"fmt"
	"runtime"
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

func BenchmarkColdInit_Cached(b *testing.B) {
	dir := b.TempDir()

	warm := func() {
		stdlibOpt, err := cpython.WithStdlib()
		if err != nil {
			b.Fatal(err)
		}
		opts := []sango.Option{
			sango.WithWASI(),
			stdlibOpt,
			sango.WithCompilationCacheDir(dir),
		}
		rt, err := sango.New(b.Context(), cpython.Wasm(), cpython.CPython(), opts...)
		if err != nil {
			b.Fatal(err)
		}
		rt.Close(b.Context())
	}
	warm()

	b.ResetTimer()
	for b.Loop() {
		stdlibOpt, err := cpython.WithStdlib()
		if err != nil {
			b.Fatal(err)
		}
		opts := []sango.Option{
			sango.WithWASI(),
			stdlibOpt,
			sango.WithCompilationCacheDir(dir),
		}
		rt, err := sango.New(b.Context(), cpython.Wasm(), cpython.CPython(), opts...)
		if err != nil {
			b.Fatal(err)
		}
		rt.Close(b.Context())
	}
}

type releaser interface {
	Release() error
}

func BenchmarkForkFanout_Hold(b *testing.B) {
	for _, withNumpy := range []bool{false, true} {
		label := "plain"
		setup := `x = 40`
		if withNumpy {
			label = "numpy"
			setup = `import numpy; x = numpy.zeros(1000)`
		}

		for _, n := range []int{1, 10, 100, 1000} {
			b.Run(fmt.Sprintf("%s/N=%d", label, n), func(b *testing.B) {
				rt := newRuntime(b, sango.WithPoolSize(0))

				inst, err := rt.Acquire(b.Context())
				if err != nil {
					b.Fatal(err)
				}
				if res, err := inst.Eval(b.Context(), []byte(setup)); err != nil || !res.OK() {
					b.Fatalf("setup: %v / %v", err, res.Err)
				}
				snap, err := rt.Snapshot(inst)
				if err != nil {
					b.Fatal(err)
				}
				inst.Release()

				forks := make([]releaser, 0, n)
				var ms runtime.MemStats
				var peakDelta uint64

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					b.StopTimer()
					runtime.GC()
					runtime.ReadMemStats(&ms)
					base := ms.HeapAlloc
					forks = forks[:0]
					b.StartTimer()

					for i := 0; i < n; i++ {
						f, err := rt.Restore(b.Context(), snap)
						if err != nil {
							b.Fatal(err)
						}
						forks = append(forks, f)
					}

					b.StopTimer()
					runtime.ReadMemStats(&ms)
					if d := ms.HeapAlloc - base; d > peakDelta {
						peakDelta = d
					}
					for _, f := range forks {
						f.Release()
					}
					b.StartTimer()
				}
				b.StopTimer()

				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/fork")
				b.ReportMetric(float64(peakDelta)/float64(n), "B/fork-held")
			})
		}
	}
}

func BenchmarkForkFanout_Discard(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			rt := newRuntime(b, sango.WithPoolSize(0))

			inst, err := rt.Acquire(b.Context())
			if err != nil {
				b.Fatal(err)
			}
			if _, err := inst.Eval(b.Context(), []byte(`x = 40`)); err != nil {
				b.Fatal(err)
			}
			snap, err := rt.Snapshot(inst)
			if err != nil {
				b.Fatal(err)
			}
			inst.Release()

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for i := 0; i < n; i++ {
					f, err := rt.Restore(b.Context(), snap)
					if err != nil {
						b.Fatal(err)
					}
					f.Release()
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/fork")
		})
	}
}

func BenchmarkBranchExecute(b *testing.B) {
	cases := []struct {
		name  string
		setup string
		code  string
	}{
		{
			name:  "trivial",
			setup: `x = 40`,
			code:  `1 + 1`,
		},
		{
			name:  "medium",
			setup: `x = 40`,
			code: `
def fib(n):
    a, b = 0, 1
    for _ in range(n):
        a, b = b, a + b
    return a
fib(2000)`,
		},
		{
			name:  "numpy",
			setup: `import numpy`,
			code:  `int(numpy.arange(1_000_000).sum())`,
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			rt := newRuntime(b, sango.WithPoolSize(0))

			inst, err := rt.Acquire(b.Context())
			if err != nil {
				b.Fatal(err)
			}
			if res, err := inst.Eval(b.Context(), []byte(tc.setup)); err != nil || !res.OK() {
				b.Fatalf("setup: %v / %v", err, res.Err)
			}
			snap, err := rt.Snapshot(inst)
			if err != nil {
				b.Fatal(err)
			}
			inst.Release()

			code := []byte(tc.code)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				f, err := rt.Restore(b.Context(), snap)
				if err != nil {
					b.Fatal(err)
				}
				res, err := f.Eval(b.Context(), code)
				if err != nil {
					f.Release()
					b.Fatal(err)
				}
				if !res.OK() {
					f.Release()
					b.Fatal(res.Err)
				}
				f.Release()
			}
		})
	}
}

func BenchmarkBranchExecute_Parallel(b *testing.B) {
	rt := newRuntime(b, sango.WithPoolSize(0))

	inst, err := rt.Acquire(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	if _, err := inst.Eval(b.Context(), []byte(`x = 40`)); err != nil {
		b.Fatal(err)
	}
	snap, err := rt.Snapshot(inst)
	if err != nil {
		b.Fatal(err)
	}
	inst.Release()

	code := []byte(`sum(range(100))`)
	ctx := b.Context()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			f, err := rt.Restore(ctx, snap)
			if err != nil {
				b.Error(err)
				return
			}
			if _, err := f.Eval(ctx, code); err != nil {
				f.Release()
				b.Error(err)
				return
			}
			f.Release()
		}
	})
}

func BenchmarkAmortized(b *testing.B) {
	for _, withNumpy := range []bool{false, true} {
		label := "plain"
		setup := `x = 40`
		if withNumpy {
			label = "numpy"
			setup = `import numpy`
		}

		for _, k := range []int{1, 10, 100, 1000} {
			b.Run(fmt.Sprintf("%s/branches=%d", label, k), func(b *testing.B) {
				code := []byte(`sum(range(100))`)
				iters := 0

				b.ResetTimer()
				for b.Loop() {
					iters++

					stdlibOpt, err := cpython.WithStdlib()
					if err != nil {
						b.Fatal(err)
					}
					rt, err := sango.New(b.Context(), cpython.Wasm(), cpython.CPython(),
						sango.WithWASI(), stdlibOpt, sango.WithPoolSize(0))
					if err != nil {
						b.Fatal(err)
					}

					inst, err := rt.Acquire(b.Context())
					if err != nil {
						b.Fatal(err)
					}
					if res, err := inst.Eval(b.Context(), []byte(setup)); err != nil || !res.OK() {
						b.Fatalf("setup: %v / %v", err, res.Err)
					}
					snap, err := rt.Snapshot(inst)
					if err != nil {
						b.Fatal(err)
					}
					inst.Release()

					for j := 0; j < k; j++ {
						f, err := rt.Restore(b.Context(), snap)
						if err != nil {
							b.Fatal(err)
						}
						if _, err := f.Eval(b.Context(), code); err != nil {
							f.Release()
							b.Fatal(err)
						}
						f.Release()
					}

					rt.Close(b.Context())
				}
				b.StopTimer()

				if iters > 0 {
					perBranch := float64(b.Elapsed().Nanoseconds()) / float64(iters*k)
					b.ReportMetric(perBranch, "ns/branch-amortized")
				}
			})
		}
	}
}

func BenchmarkAmortized_Cached(b *testing.B) {
	dir := b.TempDir()

	warm := func() {
		stdlibOpt, err := cpython.WithStdlib()
		if err != nil {
			b.Fatal(err)
		}
		rt, err := sango.New(b.Context(), cpython.Wasm(), cpython.CPython(),
			sango.WithWASI(), stdlibOpt, sango.WithPoolSize(0),
			sango.WithCompilationCacheDir(dir))
		if err != nil {
			b.Fatal(err)
		}
		rt.Close(b.Context())
	}
	warm()

	for _, withNumpy := range []bool{false, true} {
		label := "plain"
		setup := `x = 40`
		if withNumpy {
			label = "numpy"
			setup = `import numpy`
		}

		for _, k := range []int{1, 10, 100, 1000} {
			b.Run(fmt.Sprintf("%s/branches=%d", label, k), func(b *testing.B) {
				code := []byte(`sum(range(100))`)
				iters := 0

				b.ResetTimer()
				for b.Loop() {
					iters++

					stdlibOpt, err := cpython.WithStdlib()
					if err != nil {
						b.Fatal(err)
					}
					rt, err := sango.New(b.Context(), cpython.Wasm(), cpython.CPython(),
						sango.WithWASI(), stdlibOpt, sango.WithPoolSize(0),
						sango.WithCompilationCacheDir(dir))
					if err != nil {
						b.Fatal(err)
					}

					inst, err := rt.Acquire(b.Context())
					if err != nil {
						b.Fatal(err)
					}
					if res, err := inst.Eval(b.Context(), []byte(setup)); err != nil || !res.OK() {
						b.Fatalf("setup: %v / %v", err, res.Err)
					}
					snap, err := rt.Snapshot(inst)
					if err != nil {
						b.Fatal(err)
					}
					inst.Release()

					for j := 0; j < k; j++ {
						f, err := rt.Restore(b.Context(), snap)
						if err != nil {
							b.Fatal(err)
						}
						if _, err := f.Eval(b.Context(), code); err != nil {
							f.Release()
							b.Fatal(err)
						}
						f.Release()
					}

					rt.Close(b.Context())
				}
				b.StopTimer()

				if iters > 0 {
					perBranch := float64(b.Elapsed().Nanoseconds()) / float64(iters*k)
					b.ReportMetric(perBranch, "ns/branch-amortized")
				}
			})
		}
	}
}

func BenchmarkAcquire_PoolExhausted(b *testing.B) {
	const poolSize = 4
	rt := newRuntime(b, sango.WithPoolSize(poolSize))

	held := make([]releaser, 0, poolSize)
	for i := 0; i < poolSize; i++ {
		inst, err := rt.Acquire(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		held = append(held, inst)
	}
	b.Cleanup(func() {
		for _, h := range held {
			h.Release()
		}
	})

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		inst, err := rt.Acquire(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		inst.Release()
	}
}
